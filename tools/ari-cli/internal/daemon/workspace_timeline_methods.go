package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/process"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

type WorkspaceTimelineRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

type WorkspaceTimelineResponse struct {
	WorkspaceID string         `json:"workspace_id"`
	Items       []TimelineItem `json:"items"`
}

type TimelineItem struct {
	ID          string         `json:"id"`
	WorkspaceID string         `json:"workspace_id,omitempty"`
	RunID       string         `json:"run_id,omitempty"`
	SessionID   string         `json:"session_id,omitempty"`
	SourceKind  string         `json:"source_kind"`
	SourceID    string         `json:"source_id"`
	Kind        string         `json:"kind"`
	Status      string         `json:"status"`
	Sequence    int            `json:"sequence"`
	CreatedAt   string         `json:"created_at,omitempty"`
	Text        string         `json:"text,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

func (d *Daemon) registerWorkspaceTimelineMethods(registry *rpc.MethodRegistry, store *globaldb.Store) error {
	if registry == nil {
		return fmt.Errorf("method registry is required")
	}
	if store == nil {
		return fmt.Errorf("globaldb store is required")
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[WorkspaceTimelineRequest, WorkspaceTimelineResponse]{
		Name:        "workspace.timeline",
		Description: "Project structured workspace activity timeline",
		Handler: func(ctx context.Context, req WorkspaceTimelineRequest) (WorkspaceTimelineResponse, error) {
			workspaceID, _, err := requireWorkspaceRoots(ctx, store, req.WorkspaceID)
			if err != nil {
				return WorkspaceTimelineResponse{}, err
			}
			items, err := d.workspaceTimeline(ctx, store, workspaceID)
			if err != nil {
				return WorkspaceTimelineResponse{}, err
			}
			return WorkspaceTimelineResponse{WorkspaceID: workspaceID, Items: items}, nil
		},
	}); err != nil {
		return fmt.Errorf("register workspace.timeline: %w", err)
	}
	return nil
}

func (*Daemon) workspaceTimeline(ctx context.Context, store *globaldb.Store, workspaceID string) ([]TimelineItem, error) {
	events, err := listWorkspaceTimelineEvents(ctx, store, workspaceID)
	if err != nil {
		return nil, err
	}
	projection := newEventTimelineProjection(store, workspaceID)
	return projection.project(ctx, events)
}

type eventTimelineProjection struct {
	store           *globaldb.Store
	workspaceID     string
	sequence        int
	items           []TimelineItem
	agentMessages   map[string]globaldb.AgentMessage
	contextExcerpts map[string]globaldb.ContextExcerpt
	emittedExcerpts map[string]bool
	fanoutIndexes   map[string]int
}

func newEventTimelineProjection(store *globaldb.Store, workspaceID string) *eventTimelineProjection {
	return &eventTimelineProjection{store: store, workspaceID: strings.TrimSpace(workspaceID), sequence: 1, emittedExcerpts: map[string]bool{}, fanoutIndexes: map[string]int{}}
}

func listWorkspaceTimelineEvents(ctx context.Context, store *globaldb.Store, workspaceID string) ([]globaldb.WorkspaceEvent, error) {
	const pageSize = 500
	sequence := int64(0)
	events := make([]globaldb.WorkspaceEvent, 0)
	for {
		page, err := store.ListWorkspaceEventsAfterSequence(ctx, workspaceID, sequence, pageSize)
		if err != nil {
			return nil, err
		}
		if len(page) == 0 {
			break
		}
		for _, event := range page {
			sequence = event.Sequence
			if strings.HasPrefix(event.EventType, "delivery.") {
				continue
			}
			events = append(events, event)
		}
		if len(page) < pageSize {
			break
		}
	}
	return events, nil
}

func (p *eventTimelineProjection) project(ctx context.Context, events []globaldb.WorkspaceEvent) ([]TimelineItem, error) {
	for _, event := range events {
		if err := p.projectEvent(ctx, event); err != nil {
			return nil, err
		}
	}
	return p.items, nil
}

func (p *eventTimelineProjection) projectEvent(ctx context.Context, event globaldb.WorkspaceEvent) error {
	switch {
	case strings.HasPrefix(event.EventType, "operation."):
		p.appendItem(operationTimelineItemFromEvent(event))
	case strings.HasPrefix(event.EventType, "command."):
		p.appendItem(p.commandTimelineItemFromEvent(ctx, event))
	case event.EventType == workspaceEventMessageSent:
		return p.projectAgentMessageEvent(ctx, event)
	case event.EventType == "context_excerpt.created":
		return p.appendContextExcerpt(ctx, event.SubjectID)
	case isFanoutWorkerWorkspaceEvent(event.EventType):
		p.projectFanoutMemberEvent(event)
	case strings.HasPrefix(event.EventType, workspaceEventHarnessEventPrefix):
		p.appendItem(harnessRuntimeTimelineItemFromEvent(event))
	case strings.HasPrefix(event.EventType, "session."):
		p.appendItem(harnessSessionTimelineItemFromEvent(event))
	default:
		p.appendItem(genericTimelineItemFromEvent(event))
	}
	return nil
}

func (p *eventTimelineProjection) appendItem(item TimelineItem) {
	if strings.TrimSpace(item.ID) == "" {
		item.ID = fmt.Sprintf("timeline-%d", p.sequence)
	}
	item.Sequence = p.sequence
	p.sequence++
	p.items = append(p.items, item)
}

func operationTimelineItemFromEvent(event globaldb.WorkspaceEvent) TimelineItem {
	payload := workspaceEventStringPayload(event.PayloadJSON)
	operationType := strings.TrimSpace(payload["operation_type"])
	if operationType == "" {
		operationType = strings.TrimPrefix(event.EventType, "operation.")
	}
	status := strings.TrimSpace(payload["result"])
	if status == "" {
		status = "recorded"
	}
	metadata := map[string]any{"source": payload["source"]}
	if rollbackPointID := strings.TrimSpace(payload["rollback_point_id"]); rollbackPointID != "" {
		metadata["rollback_point_id"] = rollbackPointID
	}
	return TimelineItem{ID: event.EventID, WorkspaceID: event.WorkspaceID, SourceKind: "operation", SourceID: event.SubjectID, Kind: operationType, Status: status, CreatedAt: event.CreatedAt.Format(time.RFC3339Nano), Text: payload["request_summary"], Metadata: metadata}
}

func (p *eventTimelineProjection) commandTimelineItemFromEvent(ctx context.Context, event globaldb.WorkspaceEvent) TimelineItem {
	payload := workspaceEventStringPayload(event.PayloadJSON)
	text := strings.TrimSpace(payload["command"])
	if command, err := p.store.GetCommand(ctx, event.WorkspaceID, event.SubjectID); err == nil && command != nil {
		text = commandLabel(command.Command, command.Args)
	}
	status := strings.TrimSpace(payload["status"])
	switch event.EventType {
	case "command.started":
		status = "running"
	case "command.completed":
		status = "completed"
	case "command.failed":
		if strings.TrimSpace(payload["status"]) == "lost" {
			status = "lost"
		} else {
			status = "failed"
		}
	case "command.stopped":
		status = "stopped"
	}
	if status == "" {
		status = "recorded"
	}
	return TimelineItem{ID: event.EventID, WorkspaceID: event.WorkspaceID, SourceKind: "command", SourceID: event.SubjectID, Kind: "lifecycle", Status: status, CreatedAt: event.CreatedAt.Format(time.RFC3339Nano), Text: text, Metadata: map[string]any{"event_type": event.EventType}}
}

func (p *eventTimelineProjection) projectAgentMessageEvent(ctx context.Context, event globaldb.WorkspaceEvent) error {
	messages, err := p.agentMessageIndex(ctx)
	if err != nil {
		return err
	}
	msg, ok := messages[event.SubjectID]
	if !ok {
		p.appendItem(genericTimelineItemFromEvent(event))
		return nil
	}
	for _, excerptID := range msg.ContextExcerptIDs {
		if err := p.appendContextExcerpt(ctx, excerptID); err != nil {
			return err
		}
	}
	p.appendItem(TimelineItem{ID: msg.AgentMessageID, WorkspaceID: event.WorkspaceID, RunID: msg.SourceSessionID, SessionID: msg.SourceSessionID, SourceKind: "agent_message", SourceID: msg.AgentMessageID, Kind: "agent_message", Status: msg.Status, CreatedAt: event.CreatedAt.Format(time.RFC3339Nano), Text: msg.Body, Metadata: map[string]any{"source_session_id": msg.SourceSessionID, "source_agent_id": msg.SourceAgentID, "target_session_id": msg.TargetSessionID, "target_agent_id": msg.TargetAgentID, "context_excerpt_count": len(msg.ContextExcerptIDs)}})
	return nil
}

func (p *eventTimelineProjection) appendContextExcerpt(ctx context.Context, excerptID string) error {
	excerptID = strings.TrimSpace(excerptID)
	if excerptID == "" || p.emittedExcerpts[excerptID] {
		return nil
	}
	excerpts, err := p.contextExcerptIndex(ctx)
	if err != nil {
		return err
	}
	excerpt, ok := excerpts[excerptID]
	if !ok {
		return nil
	}
	p.emittedExcerpts[excerptID] = true
	p.appendItem(TimelineItem{ID: excerpt.ContextExcerptID, WorkspaceID: p.workspaceID, RunID: excerpt.SourceSessionID, SessionID: excerpt.SourceSessionID, SourceKind: "context_excerpt", SourceID: excerpt.ContextExcerptID, Kind: "context_excerpt", Status: "captured", Metadata: map[string]any{"source_session_id": excerpt.SourceSessionID, "source_agent_id": excerpt.SourceAgentID, "target_agent_id": excerpt.TargetAgentID, "selector_type": excerpt.SelectorType, "item_count": len(excerpt.Items)}})
	return nil
}

func (p *eventTimelineProjection) agentMessageIndex(ctx context.Context) (map[string]globaldb.AgentMessage, error) {
	if p.agentMessages != nil {
		return p.agentMessages, nil
	}
	messages, err := p.store.ListAgentMessages(ctx, p.workspaceID)
	if err != nil {
		return nil, err
	}
	p.agentMessages = make(map[string]globaldb.AgentMessage, len(messages))
	for _, msg := range messages {
		p.agentMessages[msg.AgentMessageID] = msg
	}
	return p.agentMessages, nil
}

func (p *eventTimelineProjection) contextExcerptIndex(ctx context.Context) (map[string]globaldb.ContextExcerpt, error) {
	if p.contextExcerpts != nil {
		return p.contextExcerpts, nil
	}
	excerpts, err := p.store.ListContextExcerpts(ctx, p.workspaceID)
	if err != nil {
		return nil, err
	}
	p.contextExcerpts = make(map[string]globaldb.ContextExcerpt, len(excerpts))
	for _, excerpt := range excerpts {
		p.contextExcerpts[excerpt.ContextExcerptID] = excerpt
	}
	return p.contextExcerpts, nil
}

func (p *eventTimelineProjection) projectFanoutMemberEvent(event globaldb.WorkspaceEvent) {
	item := fanoutTimelineItemFromEvent(event)
	key := strings.TrimSpace(item.SourceID)
	if key == "" {
		key = strings.TrimSpace(item.SessionID)
	}
	if key == "" {
		return
	}
	idx, ok := p.fanoutIndexes[key]
	if !ok {
		item.ID = key
		p.appendItem(item)
		p.fanoutIndexes[key] = len(p.items) - 1
		return
	}
	previous := p.items[idx]
	item.ID = previous.ID
	item.Sequence = previous.Sequence
	item.Metadata = mergeTimelineMetadata(previous.Metadata, item.Metadata)
	p.items[idx] = item
}

func mergeTimelineMetadata(previous, next map[string]any) map[string]any {
	if len(previous) == 0 {
		return next
	}
	merged := make(map[string]any, len(previous)+len(next))
	for key, value := range previous {
		if value != nil && fmt.Sprint(value) != "" {
			merged[key] = value
		}
	}
	for key, value := range next {
		if value != nil && fmt.Sprint(value) != "" {
			merged[key] = value
		}
	}
	return merged
}

func fanoutTimelineItemFromEvent(event globaldb.WorkspaceEvent) TimelineItem {
	payload := workspaceEventStringPayload(event.PayloadJSON)
	memberID := strings.TrimSpace(payload["fanout_member_id"])
	if memberID == "" {
		memberID = event.SubjectID
	}
	metadata := map[string]any{"fanout_group_id": event.CorrelationID, "worker_session_id": event.SubjectID, "target_profile_id": payload["target_profile_id"]}
	if causationID := strings.TrimSpace(event.CausationID); causationID != "" {
		switch event.EventType {
		case workspaceEventWorkerStarted:
			metadata["request_agent_message_id"] = causationID
		case workspaceEventWorkerCompleted:
			metadata["reply_agent_message_id"] = causationID
		}
	}
	if finalResponseID := finalResponseIDFromWorkspaceEventRef(event.PayloadRefJSON); finalResponseID != "" {
		metadata["final_response_id"] = finalResponseID
	}
	return TimelineItem{ID: event.EventID, WorkspaceID: event.WorkspaceID, RunID: event.SubjectID, SessionID: event.SubjectID, SourceKind: "fanout_member", SourceID: memberID, Kind: "fanout_member", Status: workerEventStatus(event.EventType), CreatedAt: event.CreatedAt.Format(time.RFC3339Nano), Text: payload["target_profile_id"], Metadata: metadata}
}

type harnessRuntimeTimelinePayload struct {
	HarnessEventID string          `json:"harness_event_id"`
	Kind           string          `json:"kind"`
	Sequence       int             `json:"sequence"`
	SessionID      string          `json:"session_id"`
	Payload        json.RawMessage `json:"payload"`
	ProviderKind   string          `json:"provider_kind"`
}

func harnessRuntimeTimelineItemFromEvent(event globaldb.WorkspaceEvent) TimelineItem {
	var outer harnessRuntimeTimelinePayload
	if err := json.Unmarshal([]byte(event.PayloadJSON), &outer); err != nil {
		log.Printf("decode harness runtime workspace event payload failed: event_id=%s payload=%q error=%v", event.EventID, event.PayloadJSON, err)
	}
	inner := workspaceEventJSONPayload(outer.Payload)
	sessionID := strings.TrimSpace(outer.SessionID)
	if sessionID == "" {
		sessionID = event.SubjectID
	}
	kind := strings.TrimSpace(outer.Kind)
	status := strings.TrimSpace(anyString(inner["status"]))
	text := strings.TrimSpace(anyString(inner["text"]))
	switch kind {
	case string(HarnessEventAgentText):
		kind = "run_log_message"
		if status == "" {
			status = "completed"
		}
	case string(HarnessEventLifecycle):
		kind = "lifecycle"
		if status == "" {
			status = "running"
		}
	case string(HarnessEventError):
		kind = "error"
		status = "failed"
		if text == "" {
			text = anyString(inner["message"])
		}
	case string(HarnessEventUsage):
		kind = "telemetry"
		if status == "" {
			status = "recorded"
		}
	default:
		if kind == "" {
			kind = strings.TrimPrefix(event.EventType, workspaceEventHarnessEventPrefix)
		}
		if status == "" {
			status = "recorded"
		}
		if text == "" {
			text = anyString(inner["message"])
		}
	}
	itemID := strings.TrimSpace(anyString(inner["timeline_item_id"]))
	if itemID == "" {
		itemID = event.EventID
	}
	return TimelineItem{ID: itemID, WorkspaceID: event.WorkspaceID, RunID: sessionID, SessionID: sessionID, SourceKind: "harness_session", SourceID: sessionID, Kind: kind, Status: status, CreatedAt: event.CreatedAt.Format(time.RFC3339Nano), Text: text, Metadata: map[string]any{"event_type": event.EventType, "provider_kind": outer.ProviderKind, "harness_event_id": outer.HarnessEventID}}
}

func harnessSessionTimelineItemFromEvent(event globaldb.WorkspaceEvent) TimelineItem {
	payload := workspaceEventStringPayload(event.PayloadJSON)
	status := strings.TrimSpace(payload["status"])
	if status == "" {
		status = strings.TrimPrefix(event.EventType, "session.")
	}
	return TimelineItem{ID: event.EventID, WorkspaceID: event.WorkspaceID, RunID: event.SubjectID, SessionID: event.SubjectID, SourceKind: "harness_session", SourceID: event.SubjectID, Kind: "lifecycle", Status: status, CreatedAt: event.CreatedAt.Format(time.RFC3339Nano), Text: payload["harness"], Metadata: map[string]any{"event_type": event.EventType, "final_response_id": finalResponseIDFromWorkspaceEventRef(event.PayloadRefJSON)}}
}

func genericTimelineItemFromEvent(event globaldb.WorkspaceEvent) TimelineItem {
	payload := workspaceEventStringPayload(event.PayloadJSON)
	status := strings.TrimSpace(payload["status"])
	if status == "" {
		status = "recorded"
	}
	text := strings.TrimSpace(payload["summary"])
	if text == "" {
		text = strings.TrimSpace(payload["message"])
	}
	return TimelineItem{ID: event.EventID, WorkspaceID: event.WorkspaceID, SourceKind: event.SubjectType, SourceID: event.SubjectID, Kind: event.EventType, Status: status, CreatedAt: event.CreatedAt.Format(time.RFC3339Nano), Text: text, Metadata: map[string]any{"event_type": event.EventType, "correlation_id": event.CorrelationID, "causation_id": event.CausationID}}
}

func workspaceEventJSONPayload(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return map[string]any{}
	}
	return payload
}

func anyString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return ""
	}
}

func (d *Daemon) executorTimelineItems(workspaceID string) []TimelineItem {
	d.executorMu.RLock()
	out := make([]TimelineItem, 0)
	for _, runItems := range d.executorItems {
		for _, item := range runItems {
			if item.WorkspaceID != workspaceID {
				continue
			}
			out = append(out, item)
		}
	}
	d.executorMu.RUnlock()
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt < out[j].CreatedAt
		}
		if out[i].RunID != out[j].RunID {
			return out[i].RunID < out[j].RunID
		}
		if out[i].Sequence != out[j].Sequence {
			return out[i].Sequence < out[j].Sequence
		}
		return out[i].ID < out[j].ID
	})
	return out
}

type ExecutorStartRequest struct {
	WorkspaceID     string
	RunID           string
	SessionID       string
	ContextPacket   string
	SourceProfileID string
	Model           string
	Prompt          string
	AuthSlotID      string
	AuthProjection  HarnessAuthProjectionPlan
	InvocationClass HarnessInvocationClass
	Options         []HarnessOption
}

type ExecutorRun struct {
	RunID             string
	SessionID         string
	Executor          string
	ProviderSessionID string
	ProviderRunID     string
	ProviderThreadID  string
	PID               int
	ExitCode          *int
	ProcessSample     *ProcessMetricsSample
	CapabilityNames   []string
	// Persistence, ResumeMode, and ResumeCursor are the adapter-reported
	// session-ref facts. Adapters that leave them empty surface as unknown.
	Persistence  HarnessSessionPersistence
	ResumeMode   HarnessResumeMode
	ResumeCursor json.RawMessage
}

type Executor interface {
	Start(context.Context, ExecutorStartRequest) (ExecutorRun, error)
	Items(context.Context, string) ([]TimelineItem, error)
	Stop(context.Context, string) error
}

type PTYExecutor struct {
	command string
	args    []string
	dir     string
	sink    func(string, []TimelineItem)
	mu      sync.Mutex
	runs    map[string][]TimelineItem
}

func (e *PTYExecutor) Descriptor() HarnessAdapterDescriptor {
	return HarnessAdapterDescriptor{Name: "pty", Capabilities: []HarnessCapability{HarnessCapabilityHarnessSessionFromContext, HarnessCapabilityContextPacket, HarnessCapabilityTimelineItems}}
}

func NewPTYExecutor(command string, args []string, dir string) *PTYExecutor {
	return &PTYExecutor{command: strings.TrimSpace(command), args: append([]string(nil), args...), dir: strings.TrimSpace(dir), runs: map[string][]TimelineItem{}}
}

func NewPTYExecutorWithSink(command string, args []string, dir string, sink func(string, []TimelineItem)) *PTYExecutor {
	executor := NewPTYExecutor(command, args, dir)
	executor.sink = sink
	return executor
}

func (e *PTYExecutor) Start(ctx context.Context, req ExecutorStartRequest) (ExecutorRun, error) {
	if ctx == nil {
		return ExecutorRun{}, fmt.Errorf("context is required")
	}
	if e == nil {
		return ExecutorRun{}, fmt.Errorf("executor is required")
	}
	if strings.TrimSpace(req.WorkspaceID) == "" {
		return ExecutorRun{}, fmt.Errorf("workspace id is required")
	}
	if e.command == "" {
		return ExecutorRun{}, &HarnessValidationError{Message: "command is required", Field: "command"}
	}
	proc, err := process.New(e.command, e.args, process.Options{Dir: e.dir})
	if err != nil {
		return ExecutorRun{}, err
	}
	if err := proc.Start(); err != nil {
		return ExecutorRun{}, err
	}
	runID := fmt.Sprintf("pty-run-%d", time.Now().UnixNano())
	ariRunID := strings.TrimSpace(req.RunID)
	if ariRunID == "" {
		ariRunID = runID
	}
	item := TimelineItem{ID: runID + ":lifecycle", WorkspaceID: req.WorkspaceID, RunID: runID, SourceKind: "executor", SourceID: runID, Kind: "lifecycle", Status: "running", Sequence: 1, Text: e.command}
	e.mu.Lock()
	e.runs[runID] = []TimelineItem{item}
	e.mu.Unlock()
	go func() {
		result, waitErr := proc.Wait()
		output := strings.TrimSpace(string(proc.OutputSnapshot()))
		status := "completed"
		if waitErr != nil || result.Signaled || result.ExitCode != 0 {
			status = "failed"
		}
		exitItem := TimelineItem{ID: runID + ":output", WorkspaceID: req.WorkspaceID, RunID: runID, SourceKind: "executor", SourceID: runID, Kind: "terminal_output", Status: status, Sequence: 2, Text: output}
		e.mu.Lock()
		e.runs[runID] = append(e.runs[runID], exitItem)
		e.mu.Unlock()
		if e.sink != nil {
			sinkItem := exitItem
			sinkItem.ID = ariRunID + ":output"
			sinkItem.RunID = ariRunID
			sinkItem.SessionID = ariRunID
			sinkItem.SourceID = ariRunID
			e.sink(ariRunID, []TimelineItem{sinkItem})
		}
	}()
	return ExecutorRun{RunID: runID, SessionID: runID, Executor: "pty", ProviderSessionID: runID, ProviderRunID: runID, PID: proc.PID(), CapabilityNames: []string{"timeline", "pty"}, Persistence: HarnessSessionEphemeral, ResumeMode: HarnessResumeNone}, nil
}

func (e *PTYExecutor) Items(ctx context.Context, runID string) ([]TimelineItem, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}
	if e == nil {
		return nil, fmt.Errorf("executor is required")
	}
	e.mu.Lock()
	items, ok := e.runs[strings.TrimSpace(runID)]
	e.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("run %q not found", runID)
	}
	return append([]TimelineItem(nil), items...), nil
}

func (e *PTYExecutor) Stop(ctx context.Context, runID string) error {
	if ctx == nil {
		return fmt.Errorf("context is required")
	}
	if e == nil {
		return fmt.Errorf("executor is required")
	}
	if strings.TrimSpace(runID) == "" {
		return fmt.Errorf("run id is required")
	}
	return nil
}
