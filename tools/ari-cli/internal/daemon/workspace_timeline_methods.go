package daemon

import (
	"context"
	"encoding/json"
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
	ID           string         `json:"id"`
	WorkspaceID  string         `json:"workspace_id,omitempty"`
	RunID        string         `json:"run_id,omitempty"`
	SessionID    string         `json:"session_id,omitempty"`
	SourceKind   string         `json:"source_kind"`
	SourceID     string         `json:"source_id"`
	Kind         string         `json:"kind"`
	Status       string         `json:"status"`
	Sequence     int            `json:"sequence"`
	CreatedAt    string         `json:"created_at,omitempty"`
	Text         string         `json:"text,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	Presentation Presentation   `json:"presentation"`
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
	items, err := store.ListTimelineItems(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]TimelineItem, 0, len(items))
	for _, item := range items {
		out = append(out, presentTimelineItem(timelineItemFromGlobalDB(item)))
	}
	return out, nil
}

func timelineItemFromGlobalDB(item globaldb.TimelineItem) TimelineItem {
	return TimelineItem{
		ID:          item.ID,
		WorkspaceID: item.WorkspaceID,
		RunID:       item.RunID,
		SessionID:   item.SessionID,
		SourceKind:  item.SourceKind,
		SourceID:    item.SourceID,
		Kind:        item.Kind,
		Status:      item.Status,
		Sequence:    item.Sequence,
		CreatedAt:   item.CreatedAt,
		Text:        item.Text,
		Metadata:    item.Metadata,
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
