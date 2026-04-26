package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/vcs"
)

type WorkspaceActivityRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

type WorkspaceActivityResponse struct {
	WorkspaceID    string               `json:"workspace_id"`
	WorkspaceName  string               `json:"workspace_name"`
	VCS            DiffSummary          `json:"vcs"`
	ActiveTaskID   string               `json:"active_task_id,omitempty"`
	Attention      AttentionSummary     `json:"attention"`
	Processes      []ProcessActivity    `json:"processes"`
	Agents         []AgentActivity      `json:"agents"`
	Proofs         []ProofResultSummary `json:"proofs"`
	WorkspaceRoots []string             `json:"workspace_roots"`
}

type WorkspaceDiffRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

type WorkspaceDiffResponse struct {
	Diff DiffSummary `json:"diff"`
}

type WorkspaceProofsRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

type WorkspaceProofsResponse struct {
	WorkspaceID string               `json:"workspace_id"`
	Proofs      []ProofResultSummary `json:"proofs"`
}

type AttentionSummary struct {
	Level string          `json:"level"`
	Items []AttentionItem `json:"items"`
}

type AttentionItem struct {
	Kind     string `json:"kind"`
	SourceID string `json:"source_id"`
	Message  string `json:"message"`
}

type DiffSummary struct {
	Backend      string   `json:"backend"`
	Root         string   `json:"root"`
	ChangedFiles int      `json:"changed_files"`
	Files        []string `json:"files"`
	Error        string   `json:"error,omitempty"`
}

type ProcessActivity struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	Status        string `json:"status"`
	ExitCode      *int   `json:"exit_code,omitempty"`
	Label         string `json:"label"`
	WorkspaceID   string `json:"workspace_id"`
	StartedAt     string `json:"started_at"`
	FinishedAt    string `json:"finished_at,omitempty"`
	LastOutputAt  string `json:"last_output_at,omitempty"`
	OutputSummary string `json:"output_summary,omitempty"`
}

type AgentActivity struct {
	ID             string `json:"id"`
	Name           string `json:"name,omitempty"`
	Status         string `json:"status"`
	Executor       string `json:"executor"`
	WorkspaceID    string `json:"workspace_id"`
	ActiveTaskID   string `json:"active_task_id,omitempty"`
	StartedAt      string `json:"started_at"`
	LastActivityAt string `json:"last_activity_at,omitempty"`
	OutputSummary  string `json:"output_summary,omitempty"`
}

type ProofResultSummary struct {
	ID          string `json:"id"`
	SourceID    string `json:"source_id"`
	SourceKind  string `json:"source_kind"`
	Status      string `json:"status"`
	Command     string `json:"command,omitempty"`
	StartedAt   string `json:"started_at,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
	LogSummary  string `json:"log_summary,omitempty"`
}

func (d *Daemon) registerWorkspaceProjectionMethods(registry *rpc.MethodRegistry, store *globaldb.Store) error {
	if registry == nil {
		return fmt.Errorf("method registry is required")
	}
	if store == nil {
		return fmt.Errorf("globaldb store is required")
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[WorkspaceActivityRequest, WorkspaceActivityResponse]{
		Name:        "workspace.activity",
		Description: "Project workspace activity for control-plane clients",
		Handler: func(ctx context.Context, req WorkspaceActivityRequest) (WorkspaceActivityResponse, error) {
			return d.workspaceActivity(ctx, store, req.WorkspaceID)
		},
	}); err != nil {
		return fmt.Errorf("register workspace.activity: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[WorkspaceDiffRequest, WorkspaceDiffResponse]{
		Name:        "workspace.diff",
		Description: "Project workspace VCS diff summary",
		Handler: func(ctx context.Context, req WorkspaceDiffRequest) (WorkspaceDiffResponse, error) {
			workspaceID, roots, err := requireWorkspaceRoots(ctx, store, req.WorkspaceID)
			if err != nil {
				return WorkspaceDiffResponse{}, err
			}
			_ = workspaceID
			return WorkspaceDiffResponse{Diff: buildDiffSummary(roots)}, nil
		},
	}); err != nil {
		return fmt.Errorf("register workspace.diff: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[WorkspaceProofsRequest, WorkspaceProofsResponse]{
		Name:        "workspace.proofs",
		Description: "Project workspace proof/result summaries",
		Handler: func(ctx context.Context, req WorkspaceProofsRequest) (WorkspaceProofsResponse, error) {
			workspaceID, _, err := requireWorkspaceRoots(ctx, store, req.WorkspaceID)
			if err != nil {
				return WorkspaceProofsResponse{}, err
			}
			proofs, err := d.workspaceProofs(ctx, store, workspaceID)
			if err != nil {
				return WorkspaceProofsResponse{}, err
			}
			return WorkspaceProofsResponse{WorkspaceID: workspaceID, Proofs: proofs}, nil
		},
	}); err != nil {
		return fmt.Errorf("register workspace.proofs: %w", err)
	}

	return nil
}

func (d *Daemon) workspaceActivity(ctx context.Context, store *globaldb.Store, rawWorkspaceID string) (WorkspaceActivityResponse, error) {
	workspaceID, roots, err := requireWorkspaceRoots(ctx, store, rawWorkspaceID)
	if err != nil {
		return WorkspaceActivityResponse{}, err
	}
	session, err := store.GetSession(ctx, workspaceID)
	if err != nil {
		return WorkspaceActivityResponse{}, mapWorkspaceStoreError(err, workspaceID)
	}
	processes, err := d.workspaceProcessActivity(ctx, store, workspaceID)
	if err != nil {
		return WorkspaceActivityResponse{}, err
	}
	agents, err := d.workspaceAgentActivity(ctx, store, workspaceID)
	if err != nil {
		return WorkspaceActivityResponse{}, err
	}
	proofs, err := d.workspaceProofs(ctx, store, workspaceID)
	if err != nil {
		return WorkspaceActivityResponse{}, err
	}

	return WorkspaceActivityResponse{
		WorkspaceID:    workspaceID,
		WorkspaceName:  session.Name,
		VCS:            buildDiffSummary(roots),
		Attention:      attentionFromProofs(proofs),
		Processes:      processes,
		Agents:         agents,
		Proofs:         proofs,
		WorkspaceRoots: roots,
	}, nil
}

func attentionFromProofs(proofs []ProofResultSummary) AttentionSummary {
	items := make([]AttentionItem, 0)
	for _, proof := range proofs {
		if proof.Status != "failed" {
			continue
		}
		items = append(items, AttentionItem{Kind: "proof_failed", SourceID: proof.ID, Message: proof.Command})
	}
	if len(items) == 0 {
		return AttentionSummary{Level: "none", Items: []AttentionItem{}}
	}
	return AttentionSummary{Level: "action-required", Items: items}
}

func (d *Daemon) workspaceProcessActivity(ctx context.Context, store *globaldb.Store, workspaceID string) ([]ProcessActivity, error) {
	commands, err := store.ListCommands(ctx, workspaceID)
	if err != nil {
		return nil, mapCommandStoreError(err, workspaceID)
	}
	out := make([]ProcessActivity, 0, len(commands))
	for _, command := range commands {
		status := command.Status
		if proc, ok := d.getCommandProcess(command.CommandID); ok {
			status = string(proc.State())
		}
		finishedAt := ""
		if command.FinishedAt != nil {
			finishedAt = *command.FinishedAt
		}
		outputSummary := firstOutputLine(commandSummaryOutput(d, command.CommandID))
		item := ProcessActivity{
			ID:            command.CommandID,
			Kind:          "command",
			Status:        status,
			ExitCode:      command.ExitCode,
			Label:         commandLabel(command.Command, command.Args),
			WorkspaceID:   command.WorkspaceID,
			StartedAt:     command.StartedAt,
			FinishedAt:    finishedAt,
			LastOutputAt:  bestTimestamp(finishedAt, command.StartedAt),
			OutputSummary: outputSummary,
		}
		out = append(out, item)
	}
	return out, nil
}

func (d *Daemon) workspaceAgentActivity(ctx context.Context, store *globaldb.Store, workspaceID string) ([]AgentActivity, error) {
	agents, err := store.ListAgents(ctx, workspaceID)
	if err != nil {
		return nil, mapAgentStoreError(err, workspaceID)
	}
	out := make([]AgentActivity, 0, len(agents))
	for _, agent := range agents {
		status := agent.Status
		if proc, ok := d.getAgentProcess(agent.AgentID); ok {
			status = string(proc.State())
		}
		executor := strings.TrimSpace(agent.Command)
		if agent.Harness != nil && strings.TrimSpace(*agent.Harness) != "" {
			executor = strings.TrimSpace(*agent.Harness)
		}
		item := AgentActivity{
			ID:             agent.AgentID,
			Status:         status,
			Executor:       executor,
			WorkspaceID:    agent.WorkspaceID,
			StartedAt:      agent.StartedAt,
			LastActivityAt: agent.StartedAt,
			OutputSummary:  firstOutputLine(agentSummaryOutput(d, agent.AgentID)),
		}
		if agent.Name != nil {
			item.Name = *agent.Name
		}
		out = append(out, item)
	}
	d.executorMu.RLock()
	for _, run := range d.executorRuns {
		if run.WorkspaceID != workspaceID {
			continue
		}
		out = append(out, AgentActivity{ID: run.AgentRunID, Status: run.Status, Executor: run.Executor, WorkspaceID: run.WorkspaceID, ActiveTaskID: run.TaskID, StartedAt: run.StartedAt, LastActivityAt: run.StartedAt})
	}
	d.executorMu.RUnlock()
	return out, nil
}

func (d *Daemon) workspaceProofs(ctx context.Context, store *globaldb.Store, workspaceID string) ([]ProofResultSummary, error) {
	commands, err := store.ListCommands(ctx, workspaceID)
	if err != nil {
		return nil, mapCommandStoreError(err, workspaceID)
	}
	out := make([]ProofResultSummary, 0, len(commands))
	for _, command := range commands {
		completedAt := ""
		if command.FinishedAt != nil {
			completedAt = *command.FinishedAt
		}
		out = append(out, ProofResultSummary{
			ID:          "proof_" + command.CommandID,
			SourceID:    command.CommandID,
			SourceKind:  "command",
			Status:      proofStatusForCommand(command),
			Command:     commandLabel(command.Command, command.Args),
			StartedAt:   command.StartedAt,
			CompletedAt: completedAt,
			LogSummary:  firstOutputLine(commandSummaryOutput(d, command.CommandID)),
		})
	}
	return out, nil
}

func requireWorkspaceRoots(ctx context.Context, store *globaldb.Store, rawWorkspaceID string) (string, []string, error) {
	workspaceID := strings.TrimSpace(rawWorkspaceID)
	if workspaceID == "" {
		return "", nil, rpc.NewHandlerError(rpc.InvalidParams, "workspace_id is required", nil)
	}
	session, err := store.GetSession(ctx, workspaceID)
	if err != nil {
		return "", nil, mapWorkspaceStoreError(err, workspaceID)
	}
	folders, err := store.ListFolders(ctx, session.ID)
	if err != nil {
		return "", nil, mapWorkspaceStoreError(err, session.ID)
	}
	roots := make([]string, 0, len(folders))
	primaryRoots := make([]string, 0, 1)
	for _, folder := range folders {
		if strings.TrimSpace(folder.FolderPath) == "" {
			continue
		}
		if folder.IsPrimary {
			primaryRoots = append(primaryRoots, folder.FolderPath)
			continue
		}
		roots = append(roots, folder.FolderPath)
	}
	roots = append(primaryRoots, roots...)
	return session.ID, roots, nil
}

func buildDiffSummary(roots []string) DiffSummary {
	if len(roots) == 0 {
		return DiffSummary{Backend: "none", Files: []string{}}
	}
	backend, err := vcs.Detect(roots[0])
	if err != nil {
		return DiffSummary{Backend: "none", Root: roots[0], Files: []string{}, Error: err.Error()}
	}
	summary := DiffSummary{Backend: backend.Name(), Root: backend.Root(), Files: []string{}}
	files, err := backend.ChangedFiles()
	if err != nil {
		if !errors.Is(err, vcs.ErrNotSupported) {
			summary.Error = err.Error()
		}
		return summary
	}
	summary.Files = files
	summary.ChangedFiles = len(files)
	return summary
}

func proofStatusForCommand(command globaldb.Command) string {
	switch command.Status {
	case "running":
		return "running"
	case "lost":
		return "unknown"
	case "exited":
		if command.ExitCode != nil && *command.ExitCode == 0 {
			return "passed"
		}
		return "failed"
	default:
		return "unknown"
	}
}

func commandLabel(command, rawArgs string) string {
	command = strings.TrimSpace(command)
	args := strings.TrimSpace(rawArgs)
	if args == "" || args == "[]" {
		return command
	}
	var decoded []string
	if err := json.Unmarshal([]byte(args), &decoded); err != nil {
		return command
	}
	if len(decoded) == 0 {
		return command
	}
	return strings.TrimSpace(command + " " + strings.Join(decoded, " "))
}

func commandSummaryOutput(d *Daemon, commandID string) string {
	if output, ok := d.getCommandOutput(commandID); ok {
		return output
	}
	if proc, ok := d.getCommandProcess(commandID); ok {
		return string(proc.OutputSnapshot())
	}
	return ""
}

func agentSummaryOutput(d *Daemon, agentID string) string {
	if output, ok := d.getAgentOutput(agentID); ok {
		return output
	}
	if proc, ok := d.getAgentProcess(agentID); ok {
		return string(proc.OutputSnapshot())
	}
	return ""
}

func firstOutputLine(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	line, _, _ := strings.Cut(output, "\n")
	return strings.TrimSpace(line)
}

func bestTimestamp(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	return fallback
}
