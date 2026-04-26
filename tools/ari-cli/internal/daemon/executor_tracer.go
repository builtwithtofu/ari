package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

type AgentRunStartRequest struct {
	Executor  string         `json:"executor"`
	Packet    ContextPacket  `json:"packet"`
	Command   string         `json:"command,omitempty"`
	Args      []string       `json:"args,omitempty"`
	FakeItems []TimelineItem `json:"fake_items,omitempty"`
}

type AgentRunStartResponse struct {
	Run   AgentRun       `json:"run"`
	Items []TimelineItem `json:"items"`
}

type AgentRun struct {
	AgentRunID      string   `json:"agent_run_id"`
	WorkspaceID     string   `json:"workspace_id"`
	TaskID          string   `json:"task_id"`
	Executor        string   `json:"executor"`
	ProviderRunID   string   `json:"provider_run_id"`
	Status          string   `json:"status"`
	ContextPacketID string   `json:"context_packet_id"`
	StartedAt       string   `json:"started_at"`
	Capabilities    []string `json:"capabilities"`
}

func (d *Daemon) registerExecutorMethods(registry *rpc.MethodRegistry, store *globaldb.Store) error {
	if registry == nil {
		return fmt.Errorf("method registry is required")
	}
	if store == nil {
		return fmt.Errorf("globaldb store is required")
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[AgentRunStartRequest, AgentRunStartResponse]{
		Name:        "agent.run",
		Description: "Start an executor-backed agent run from a context packet",
		Handler: func(ctx context.Context, req AgentRunStartRequest) (AgentRunStartResponse, error) {
			executorName := strings.TrimSpace(req.Executor)
			if executorName == "" {
				return AgentRunStartResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "executor is required", nil)
			}
			primaryFolder, err := lookupPrimaryFolder(ctx, store, req.Packet.WorkspaceID)
			if err != nil {
				return AgentRunStartResponse{}, mapWorkspaceStoreError(err, req.Packet.WorkspaceID)
			}
			var executor Executor
			switch executorName {
			case "fake":
				executor = NewFakeExecutor(executorName, req.FakeItems)
			case "pty":
				executor = NewPTYExecutorWithSink(req.Command, req.Args, primaryFolder, d.appendExecutorItems)
			default:
				return AgentRunStartResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "executor is not available", executorName)
			}
			if _, err := store.GetSession(ctx, req.Packet.WorkspaceID); err != nil {
				return AgentRunStartResponse{}, mapWorkspaceStoreError(err, req.Packet.WorkspaceID)
			}
			run, items, err := StartExecutorRun(ctx, executor, req.Packet)
			if err != nil {
				return AgentRunStartResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
			}
			d.recordExecutorRun(run, items)
			return AgentRunStartResponse{Run: run, Items: items}, nil
		},
	}); err != nil {
		return fmt.Errorf("register agent.run: %w", err)
	}
	return nil
}

func (d *Daemon) appendExecutorItems(runID string, items []TimelineItem) {
	if d == nil || strings.TrimSpace(runID) == "" || len(items) == 0 {
		return
	}
	d.executorMu.Lock()
	d.executorItems[runID] = append(d.executorItems[runID], items...)
	d.updateExecutorRunStatusLocked(runID, items)
	d.executorMu.Unlock()
}

func StartExecutorRun(ctx context.Context, executor Executor, packet ContextPacket) (AgentRun, []TimelineItem, error) {
	if ctx == nil {
		return AgentRun{}, nil, fmt.Errorf("context is required")
	}
	if executor == nil {
		return AgentRun{}, nil, fmt.Errorf("executor is required")
	}
	if strings.TrimSpace(packet.ID) == "" {
		return AgentRun{}, nil, fmt.Errorf("context packet id is required")
	}
	if strings.TrimSpace(packet.WorkspaceID) == "" {
		return AgentRun{}, nil, fmt.Errorf("workspace id is required")
	}
	if strings.TrimSpace(packet.TaskID) == "" {
		return AgentRun{}, nil, fmt.Errorf("task id is required")
	}
	providerRun, err := executor.Start(ctx, ExecutorStartRequest{WorkspaceID: packet.WorkspaceID, ContextPacket: renderContextPacket(packet)})
	if err != nil {
		return AgentRun{}, nil, err
	}
	items, err := executor.Items(ctx, providerRun.RunID)
	if err != nil {
		return AgentRun{}, nil, err
	}
	agentRun := AgentRun{
		AgentRunID:      providerRun.RunID,
		WorkspaceID:     packet.WorkspaceID,
		TaskID:          packet.TaskID,
		Executor:        providerRun.Executor,
		ProviderRunID:   providerRun.ProviderRunID,
		Status:          executorRunStatusFromItems(items),
		ContextPacketID: packet.ID,
		StartedAt:       time.Now().UTC().Format(time.RFC3339Nano),
		Capabilities:    append([]string(nil), providerRun.CapabilityNames...),
	}
	for i := range items {
		items[i].RunID = agentRun.AgentRunID
		items[i].WorkspaceID = agentRun.WorkspaceID
	}
	return agentRun, items, nil
}

func (d *Daemon) recordExecutorRun(run AgentRun, items []TimelineItem) {
	if d == nil || strings.TrimSpace(run.AgentRunID) == "" {
		return
	}
	d.executorMu.Lock()
	d.executorRuns[run.AgentRunID] = run
	d.executorItems[run.AgentRunID] = append([]TimelineItem(nil), items...)
	d.executorMu.Unlock()
}

func (d *Daemon) updateExecutorRunStatusLocked(runID string, items []TimelineItem) {
	run, ok := d.executorRuns[runID]
	if !ok {
		return
	}
	status := executorRunStatusFromItems(items)
	if status == "running" {
		return
	}
	run.Status = status
	d.executorRuns[runID] = run
}

func executorRunStatusFromItems(items []TimelineItem) string {
	if len(items) == 0 {
		return "running"
	}
	for _, item := range items {
		switch strings.TrimSpace(item.Status) {
		case "failed":
			return "failed"
		case "completed":
			return "completed"
		}
	}
	return "running"
}

func renderContextPacket(packet ContextPacket) string {
	encoded, err := json.Marshal(packet)
	if err != nil {
		panic(fmt.Sprintf("render context packet: %v", err))
	}
	return string(encoded)
}
