package daemon

import (
	"context"
	"fmt"
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

	agents, err := store.ListAgents(ctx, workspaceID)
	if err != nil {
		return nil, mapAgentStoreError(err, workspaceID)
	}
	for _, agent := range agents {
		if output := strings.TrimSpace(agentSummaryOutput(d, agent.AgentID)); output != "" {
			items = append(items, TimelineItem{
				ID:          agent.AgentID + ":output",
				WorkspaceID: agent.WorkspaceID,
				RunID:       agent.AgentID,
				SourceKind:  "agent",
				SourceID:    agent.AgentID,
				Kind:        "terminal_output",
				Status:      agent.Status,
				Sequence:    sequence,
				CreatedAt:   agent.StartedAt,
				Text:        output,
			})
			sequence++
		}
	}
	d.executorMu.RLock()
	for _, runItems := range d.executorItems {
		for _, item := range runItems {
			if item.WorkspaceID != workspaceID {
				continue
			}
			item.Sequence = sequence
			items = append(items, item)
			sequence++
		}
	}
	d.executorMu.RUnlock()
	return items, nil
}

type ExecutorStartRequest struct {
	WorkspaceID   string
	ContextPacket string
}

type ExecutorRun struct {
	RunID           string
	Executor        string
	ProviderRunID   string
	CapabilityNames []string
}

type Executor interface {
	Start(context.Context, ExecutorStartRequest) (ExecutorRun, error)
	Items(context.Context, string) ([]TimelineItem, error)
	Stop(context.Context, string) error
}

type FakeExecutor struct {
	mu                sync.Mutex
	name              string
	template          []TimelineItem
	runs              map[string][]TimelineItem
	lastContextPacket string
}

func NewFakeExecutor(name string, items []TimelineItem) *FakeExecutor {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "fake"
	}
	return &FakeExecutor{name: name, template: append([]TimelineItem(nil), items...), runs: map[string][]TimelineItem{}}
}

func (e *FakeExecutor) Start(ctx context.Context, req ExecutorStartRequest) (ExecutorRun, error) {
	if ctx == nil {
		return ExecutorRun{}, fmt.Errorf("context is required")
	}
	if e == nil {
		return ExecutorRun{}, fmt.Errorf("executor is required")
	}
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" {
		return ExecutorRun{}, fmt.Errorf("workspace id is required")
	}
	runID := fmt.Sprintf("%s-run-%d", e.name, time.Now().UnixNano())
	e.lastContextPacket = req.ContextPacket
	items := append([]TimelineItem(nil), e.template...)
	for i := range items {
		items[i].RunID = runID
		items[i].WorkspaceID = workspaceID
		items[i].SourceKind = "executor"
		if strings.TrimSpace(items[i].SourceID) == "" {
			items[i].SourceID = runID
		}
		if strings.TrimSpace(items[i].ID) == "" {
			items[i].ID = fmt.Sprintf("%s:item-%d", runID, i+1)
		}
		if items[i].Sequence == 0 {
			items[i].Sequence = i + 1
		}
		if strings.TrimSpace(items[i].Status) == "" {
			items[i].Status = "completed"
		}
	}
	e.mu.Lock()
	e.runs[runID] = items
	e.mu.Unlock()
	return ExecutorRun{RunID: runID, Executor: e.name, ProviderRunID: runID, CapabilityNames: []string{"timeline"}}, nil
}

func (e *FakeExecutor) Items(ctx context.Context, runID string) ([]TimelineItem, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}
	if e == nil {
		return nil, fmt.Errorf("executor is required")
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, fmt.Errorf("run id is required")
	}
	e.mu.Lock()
	items, ok := e.runs[runID]
	e.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("run %q not found", runID)
	}
	return append([]TimelineItem(nil), items...), nil
}

func (e *FakeExecutor) Stop(ctx context.Context, runID string) error {
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

type PTYExecutor struct {
	command string
	args    []string
	dir     string
	sink    func(string, []TimelineItem)
	mu      sync.Mutex
	runs    map[string][]TimelineItem
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
		return ExecutorRun{}, fmt.Errorf("command is required")
	}
	proc, err := process.New(e.command, e.args, process.Options{Dir: e.dir})
	if err != nil {
		return ExecutorRun{}, err
	}
	if err := proc.Start(); err != nil {
		return ExecutorRun{}, err
	}
	runID := fmt.Sprintf("pty-run-%d", time.Now().UnixNano())
	item := TimelineItem{ID: runID + ":lifecycle", WorkspaceID: req.WorkspaceID, RunID: runID, SourceKind: "executor", SourceID: runID, Kind: "lifecycle", Status: "running", Sequence: 1, Text: e.command}
	e.mu.Lock()
	e.runs[runID] = []TimelineItem{item}
	e.mu.Unlock()
	go func() {
		_, _ = proc.Wait()
		output := strings.TrimSpace(string(proc.OutputSnapshot()))
		exitItem := TimelineItem{ID: runID + ":output", WorkspaceID: req.WorkspaceID, RunID: runID, SourceKind: "executor", SourceID: runID, Kind: "terminal_output", Status: "completed", Sequence: 2, Text: output}
		e.mu.Lock()
		e.runs[runID] = append(e.runs[runID], exitItem)
		e.mu.Unlock()
		if e.sink != nil {
			e.sink(runID, []TimelineItem{exitItem})
		}
	}()
	return ExecutorRun{RunID: runID, Executor: "pty", ProviderRunID: runID, CapabilityNames: []string{"timeline", "pty"}}, nil
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
