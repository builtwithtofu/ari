package daemon

import (
	"context"
	"fmt"
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

func (d *Daemon) workspaceTimeline(ctx context.Context, store *globaldb.Store, workspaceID string) ([]TimelineItem, error) {
	sequence := 1
	items := make([]TimelineItem, 0)
	commands, err := store.ListCommands(ctx, workspaceID)
	if err != nil {
		return nil, mapCommandStoreError(err, workspaceID)
	}
	for _, command := range commands {
		items = append(items, TimelineItem{
			ID:          command.CommandID + ":lifecycle",
			WorkspaceID: command.WorkspaceID,
			SourceKind:  "command",
			SourceID:    command.CommandID,
			Kind:        "lifecycle",
			Status:      command.Status,
			Sequence:    sequence,
			CreatedAt:   command.StartedAt,
			Text:        commandLabel(command.Command, command.Args),
		})
		sequence++
		if output := strings.TrimSpace(commandSummaryOutput(d, command.CommandID)); output != "" {
			items = append(items, TimelineItem{
				ID:          command.CommandID + ":output",
				WorkspaceID: command.WorkspaceID,
				SourceKind:  "command",
				SourceID:    command.CommandID,
				Kind:        "command_output",
				Status:      timelineOutputStatus(command.Status),
				Sequence:    sequence,
				CreatedAt:   bestTimelineTimestamp(command.FinishedAt, command.StartedAt),
				Text:        output,
			})
			sequence++
		}
		proof := ProofResultSummary{
			ID:         "proof_" + command.CommandID,
			SourceID:   command.CommandID,
			SourceKind: "command",
			Status:     proofStatusForCommand(command),
			Command:    commandLabel(command.Command, command.Args),
		}
		items = append(items, TimelineItem{
			ID:          proof.ID,
			WorkspaceID: command.WorkspaceID,
			SourceKind:  "proof",
			SourceID:    command.CommandID,
			Kind:        "proof_result",
			Status:      proof.Status,
			Sequence:    sequence,
			CreatedAt:   bestTimelineTimestamp(command.FinishedAt, command.StartedAt),
			Text:        proof.Command,
		})
		sequence++
	}

	excerpts, err := store.ListContextExcerpts(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	excerptsByID := make(map[string]globaldb.ContextExcerpt, len(excerpts))
	excerptOrder := make([]string, 0, len(excerpts))
	for _, excerpt := range excerpts {
		excerptsByID[excerpt.ContextExcerptID] = excerpt
		excerptOrder = append(excerptOrder, excerpt.ContextExcerptID)
	}
	emittedExcerpts := make(map[string]bool, len(excerpts))
	appendExcerpt := func(excerptID string) {
		if emittedExcerpts[excerptID] {
			return
		}
		excerpt, ok := excerptsByID[excerptID]
		if !ok {
			return
		}
		items = append(items, TimelineItem{
			ID:          excerpt.ContextExcerptID,
			WorkspaceID: workspaceID,
			RunID:       excerpt.SourceSessionID,
			SessionID:   excerpt.SourceSessionID,
			SourceKind:  "context_excerpt",
			SourceID:    excerpt.ContextExcerptID,
			Kind:        "context_excerpt",
			Status:      "captured",
			Sequence:    sequence,
			Metadata: map[string]any{
				"source_session_id": excerpt.SourceSessionID,
				"source_agent_id":   excerpt.SourceAgentID,
				"target_agent_id":   excerpt.TargetAgentID,
				"selector_type":     excerpt.SelectorType,
				"item_count":        len(excerpt.Items),
			},
		})
		emittedExcerpts[excerptID] = true
		sequence++
	}

	messages, err := store.ListAgentMessages(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	for _, msg := range messages {
		for _, excerptID := range msg.ContextExcerptIDs {
			appendExcerpt(excerptID)
		}
		items = append(items, TimelineItem{
			ID:          msg.AgentMessageID,
			WorkspaceID: workspaceID,
			RunID:       msg.SourceSessionID,
			SessionID:   msg.SourceSessionID,
			SourceKind:  "agent_message",
			SourceID:    msg.AgentMessageID,
			Kind:        "agent_message",
			Status:      msg.Status,
			Sequence:    sequence,
			Text:        msg.Body,
			Metadata: map[string]any{
				"source_session_id":     msg.SourceSessionID,
				"source_agent_id":       msg.SourceAgentID,
				"target_session_id":     msg.TargetSessionID,
				"target_agent_id":       msg.TargetAgentID,
				"context_excerpt_count": len(msg.ContextExcerptIDs),
			},
		})
		sequence++
	}
	for _, excerptID := range excerptOrder {
		appendExcerpt(excerptID)
	}

	for _, item := range d.executorTimelineItems(workspaceID) {
		item = normalizeAgentSessionTimelineItem(item)
		item.Sequence = sequence
		items = append(items, item)
		sequence++
	}
	return items, nil
}

func normalizeAgentSessionTimelineItem(item TimelineItem) TimelineItem {
	switch item.SourceKind {
	case "executor", "agent_session":
		item.SourceKind = "agent_session"
	}
	switch item.Kind {
	case "agent_text", "terminal_output":
		item.Kind = "run_log_message"
	}
	return item
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
	InvocationClass HarnessInvocationClass
}

type ExecutorRun struct {
	RunID             string
	SessionID         string
	Executor          string
	ProviderSessionID string
	ProviderRunID     string
	PID               int
	ExitCode          *int
	ProcessSample     *ProcessMetricsSample
	CapabilityNames   []string
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
	return HarnessAdapterDescriptor{Name: "pty", Capabilities: []HarnessCapability{HarnessCapabilityAgentSessionFromContext, HarnessCapabilityContextPacket, HarnessCapabilityTimelineItems}}
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
	return ExecutorRun{RunID: runID, SessionID: runID, Executor: "pty", ProviderSessionID: runID, ProviderRunID: runID, PID: proc.PID(), CapabilityNames: []string{"timeline", "pty"}}, nil
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

func timelineOutputStatus(status string) string {
	if strings.TrimSpace(status) == "running" {
		return "running"
	}
	return "completed"
}

func bestTimelineTimestamp(finishedAt *string, fallback string) string {
	if finishedAt != nil && strings.TrimSpace(*finishedAt) != "" {
		return *finishedAt
	}
	return fallback
}
