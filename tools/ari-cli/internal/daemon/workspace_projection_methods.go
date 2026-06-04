package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/vcs"
)

type WorkspaceStatusRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

type WorkspaceStatusResponse struct {
	WorkspaceID      string                   `json:"workspace_id"`
	WorkspaceName    string                   `json:"workspace_name"`
	VCS              DiffSummary              `json:"vcs"`
	ActiveTaskID     string                   `json:"active_task_id,omitempty"`
	Attention        AttentionSummary         `json:"attention"`
	Processes        []ProcessActivity        `json:"processes"`
	Sessions         []SessionActivity        `json:"sessions"`
	Proofs           []ProofResultSummary     `json:"proofs"`
	ContextExcerpts  []ContextExcerptActivity `json:"context_excerpts"`
	AgentMessages    []AgentMessageActivity   `json:"agent_messages"`
	FanoutMembers    []FanoutMemberActivity   `json:"fanout_members"`
	StickyInbox      []StickyInboxActivity    `json:"sticky_inbox"`
	WorkspaceRoots   []string                 `json:"workspace_roots"`
	RecentOperations []OperationActivity      `json:"recent_operations"`
	RecentTimeline   []TimelineItem           `json:"recent_timeline"`
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

type SessionActivity struct {
	ID              string `json:"id"`
	Name            string `json:"name,omitempty"`
	Status          string `json:"status"`
	Executor        string `json:"executor"`
	WorkspaceID     string `json:"workspace_id"`
	ActiveTaskID    string `json:"active_task_id,omitempty"`
	StartedAt       string `json:"started_at"`
	LastActivityAt  string `json:"last_activity_at,omitempty"`
	OutputSummary   string `json:"output_summary,omitempty"`
	Usage           string `json:"usage,omitempty"`
	SourceSessionID string `json:"source_session_id,omitempty"`
	SourceAgentID   string `json:"source_agent_id,omitempty"`
}

type AgentActivity = SessionActivity

type ContextExcerptActivity struct {
	ContextExcerptID string `json:"context_excerpt_id"`
	SourceSessionID  string `json:"source_session_id"`
	SourceAgentID    string `json:"source_agent_id"`
	TargetAgentID    string `json:"target_agent_id"`
	SelectorType     string `json:"selector_type"`
	ItemCount        int    `json:"item_count"`
}

type AgentMessageActivity struct {
	AgentMessageID      string `json:"agent_message_id"`
	SourceSessionID     string `json:"source_session_id"`
	SourceAgentID       string `json:"source_agent_id"`
	TargetSessionID     string `json:"target_session_id"`
	TargetAgentID       string `json:"target_agent_id"`
	Status              string `json:"status"`
	ContextExcerptCount int    `json:"context_excerpt_count"`
}

type FanoutMemberActivity struct {
	FanoutMemberID        string `json:"fanout_member_id"`
	FanoutGroupID         string `json:"fanout_group_id"`
	WorkerSessionID       string `json:"worker_session_id"`
	TargetProfileID       string `json:"target_profile_id"`
	RequestAgentMessageID string `json:"request_agent_message_id,omitempty"`
	ReplyAgentMessageID   string `json:"reply_agent_message_id,omitempty"`
	FinalResponseID       string `json:"final_response_id,omitempty"`
	Status                string `json:"status"`
}

type StickyInboxActivity struct {
	InboxItemID       string `json:"inbox_item_id"`
	TargetSessionID   string `json:"target_session_id"`
	WorkspaceEventID  string `json:"workspace_event_id,omitempty"`
	EventType         string `json:"event_type,omitempty"`
	FanoutGroupID     string `json:"fanout_group_id,omitempty"`
	FanoutMemberID    string `json:"fanout_member_id,omitempty"`
	WorkerSessionID   string `json:"worker_session_id,omitempty"`
	FinalResponseID   string `json:"final_response_id,omitempty"`
	Kind              string `json:"kind"`
	Status            string `json:"status"`
	AttentionRequired bool   `json:"attention_required"`
	Summary           string `json:"summary,omitempty"`
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

type OperationActivity struct {
	OperationID     string `json:"operation_id"`
	OperationType   string `json:"operation_type"`
	Source          string `json:"source"`
	Status          string `json:"status"`
	RequestSummary  string `json:"request_summary"`
	RollbackPointID string `json:"rollback_point_id,omitempty"`
	CreatedAt       string `json:"created_at"`
}

func (d *Daemon) registerWorkspaceProjectionMethods(registry *rpc.MethodRegistry, store *globaldb.Store) error {
	if registry == nil {
		return fmt.Errorf("method registry is required")
	}
	if store == nil {
		return fmt.Errorf("globaldb store is required")
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[WorkspaceStatusRequest, WorkspaceStatusResponse]{
		Name:        "workspace.status",
		Description: "Project workspace orchestration status for control-plane clients",
		Handler: func(ctx context.Context, req WorkspaceStatusRequest) (WorkspaceStatusResponse, error) {
			return d.workspaceStatus(ctx, store, req.WorkspaceID)
		},
	}); err != nil {
		return fmt.Errorf("register workspace.status: %w", err)
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

func (d *Daemon) workspaceStatus(ctx context.Context, store *globaldb.Store, rawWorkspaceID string) (WorkspaceStatusResponse, error) {
	workspaceID, roots, err := requireWorkspaceRoots(ctx, store, rawWorkspaceID)
	if err != nil {
		return WorkspaceStatusResponse{}, err
	}
	session, err := store.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return WorkspaceStatusResponse{}, mapWorkspaceStoreError(err, workspaceID)
	}
	processes, err := d.workspaceProcessActivity(ctx, store, workspaceID)
	if err != nil {
		return WorkspaceStatusResponse{}, err
	}
	sessions, err := d.workspaceSessionActivity(ctx, store, workspaceID)
	if err != nil {
		return WorkspaceStatusResponse{}, err
	}
	proofs, err := d.workspaceProofs(ctx, store, workspaceID)
	if err != nil {
		return WorkspaceStatusResponse{}, err
	}
	authSlots, err := workspaceAuthSlots(ctx, store, workspaceID)
	if err != nil {
		return WorkspaceStatusResponse{}, err
	}
	contextExcerpts, err := workspaceContextExcerpts(ctx, store, workspaceID)
	if err != nil {
		return WorkspaceStatusResponse{}, err
	}
	agentMessages, err := agentSessionConfigMessages(ctx, store, workspaceID)
	if err != nil {
		return WorkspaceStatusResponse{}, err
	}
	fanoutMembers, err := fanoutMemberActivity(ctx, store, workspaceID)
	if err != nil {
		return WorkspaceStatusResponse{}, err
	}
	stickyInbox, err := stickyInboxActivity(ctx, store, workspaceID, sessions)
	if err != nil {
		return WorkspaceStatusResponse{}, err
	}
	recentOperations, err := workspaceOperationActivity(ctx, store, workspaceID)
	if err != nil {
		return WorkspaceStatusResponse{}, err
	}

	attention := attentionFromActivity(proofs, sessions, authSlots)
	recentTimeline, err := workspaceStateTimeline(ctx, store, session, roots, attention)
	if err != nil {
		return WorkspaceStatusResponse{}, err
	}

	return WorkspaceStatusResponse{
		WorkspaceID:      workspaceID,
		WorkspaceName:    session.Name,
		VCS:              buildDiffSummary(roots),
		Attention:        attention,
		Processes:        processes,
		Sessions:         sessions,
		Proofs:           proofs,
		ContextExcerpts:  contextExcerpts,
		AgentMessages:    agentMessages,
		FanoutMembers:    fanoutMembers,
		StickyInbox:      stickyInbox,
		WorkspaceRoots:   roots,
		RecentOperations: recentOperations,
		RecentTimeline:   recentTimeline,
	}, nil
}

func workspaceOperationActivity(ctx context.Context, store *globaldb.Store, workspaceID string) ([]OperationActivity, error) {
	records, err := store.ListOperationRecords(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if workspaceID != "" {
		globalRecords, err := store.ListOperationRecords(ctx, "")
		if err != nil {
			return nil, err
		}
		for _, record := range globalRecords {
			if record.WorkspaceID == "" && operationRecordHomeWorkspaceID(record) == workspaceID {
				records = append(records, record)
			}
		}
	}
	out := make([]OperationActivity, 0, len(records))
	seen := map[string]bool{}
	for _, record := range records {
		if seen[record.OperationID] || record.OperationType == daemonOperationTypeRollbackCheckpoint {
			continue
		}
		seen[record.OperationID] = true
		out = append(out, OperationActivity{OperationID: record.OperationID, OperationType: record.OperationType, Source: record.Source, Status: operationRecordStatus(record), RequestSummary: record.RequestSummary, RollbackPointID: record.RollbackPointID, CreatedAt: record.CreatedAt.Format(time.RFC3339Nano)})
	}
	return out, nil
}

func operationRecordHomeWorkspaceID(record globaldb.OperationRecord) string {
	var snapshot map[string]string
	if err := json.Unmarshal([]byte(record.PayloadSnapshotJSON), &snapshot); err != nil {
		return ""
	}
	return strings.TrimSpace(snapshot["home_workspace_id"])
}

func operationRecordStatus(record globaldb.OperationRecord) string {
	if strings.HasPrefix(record.Result, "failed:") {
		return "failed"
	}
	return record.Result
}

func workspaceStateTimeline(ctx context.Context, store *globaldb.Store, session *globaldb.Workspace, roots []string, attention AttentionSummary) ([]TimelineItem, error) {
	profiles, err := store.ListProfiles(ctx, session.ID)
	if err != nil {
		return nil, err
	}
	agentSessions, err := store.ListHarnessSessions(ctx, session.ID)
	if err != nil {
		return nil, err
	}
	return []TimelineItem{{
		ID:          session.ID + ":workspace_state",
		WorkspaceID: session.ID,
		SourceKind:  "workspace",
		SourceID:    session.ID,
		Kind:        "workspace_state",
		Status:      session.Status,
		Sequence:    1,
		CreatedAt:   session.UpdatedAt,
		Text:        session.Name,
		Metadata: map[string]any{
			"workspace_name":  session.Name,
			"root_folders":    roots,
			"profile_count":   len(profiles),
			"session_count":   len(agentSessions),
			"attention_level": attention.Level,
		},
	}}, nil
}

func workspaceContextExcerpts(ctx context.Context, store *globaldb.Store, workspaceID string) ([]ContextExcerptActivity, error) {
	excerpts, err := store.ListContextExcerpts(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]ContextExcerptActivity, 0, len(excerpts))
	for _, excerpt := range excerpts {
		out = append(out, ContextExcerptActivity{ContextExcerptID: excerpt.ContextExcerptID, SourceSessionID: excerpt.SourceSessionID, SourceAgentID: excerpt.SourceAgentID, TargetAgentID: excerpt.TargetAgentID, SelectorType: excerpt.SelectorType, ItemCount: len(excerpt.Items)})
	}
	return out, nil
}

func agentSessionConfigMessages(ctx context.Context, store *globaldb.Store, workspaceID string) ([]AgentMessageActivity, error) {
	messages, err := store.ListAgentMessages(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]AgentMessageActivity, 0, len(messages))
	for _, dm := range messages {
		out = append(out, AgentMessageActivity{AgentMessageID: dm.AgentMessageID, SourceSessionID: dm.SourceSessionID, SourceAgentID: dm.SourceAgentID, TargetSessionID: dm.TargetSessionID, TargetAgentID: dm.TargetAgentID, Status: dm.Status, ContextExcerptCount: len(dm.ContextExcerptIDs)})
	}
	return out, nil
}

func fanoutMemberActivity(ctx context.Context, store *globaldb.Store, workspaceID string) ([]FanoutMemberActivity, error) {
	members, err := fanoutMemberProjectionForWorkspace(ctx, store, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]FanoutMemberActivity, 0, len(members))
	for _, member := range members {
		out = append(out, FanoutMemberActivity{FanoutMemberID: member.FanoutMemberID, FanoutGroupID: member.FanoutGroupID, WorkerSessionID: member.WorkerSessionID, TargetProfileID: member.TargetProfileID, RequestAgentMessageID: member.RequestAgentMessageID, ReplyAgentMessageID: member.ReplyAgentMessageID, FinalResponseID: member.FinalResponseID, Status: member.Status})
	}
	return out, nil
}

func stickyInboxActivity(ctx context.Context, store *globaldb.Store, workspaceID string, sessions []SessionActivity) ([]StickyInboxActivity, error) {
	out := make([]StickyInboxActivity, 0)
	for _, session := range sessions {
		if session.Usage != globaldb.HarnessSessionUsageSticky {
			continue
		}
		items, err := store.ListInboxItems(ctx, workspaceID, session.ID)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			out = append(out, StickyInboxActivity{InboxItemID: item.InboxItemID, TargetSessionID: item.SourceSessionID, WorkspaceEventID: item.WorkspaceEventID, EventType: item.EventType, FanoutGroupID: item.FanoutGroupID, FanoutMemberID: item.FanoutMemberID, WorkerSessionID: item.WorkerSessionID, FinalResponseID: item.FinalResponseID, Kind: item.Kind, Status: item.Status, AttentionRequired: item.AttentionRequired, Summary: item.Summary})
		}
	}
	return out, nil
}

func workspaceAuthSlots(ctx context.Context, store *globaldb.Store, workspaceID string) ([]globaldb.AuthSlot, error) {
	profiles, err := store.ListProfiles(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	slots := make([]globaldb.AuthSlot, 0)
	seen := map[string]bool{}
	for _, profile := range profiles {
		authSlotID := strings.TrimSpace(profile.AuthSlotID)
		if authSlotID == "" || seen[authSlotID] {
			continue
		}
		slot, err := store.GetAuthSlot(ctx, authSlotID)
		if err != nil {
			if errors.Is(err, globaldb.ErrNotFound) {
				continue
			}
			return nil, err
		}
		seen[authSlotID] = true
		slots = append(slots, slot)
	}
	return slots, nil
}

func attentionFromActivity(proofs []ProofResultSummary, sessions []SessionActivity, authSlots []globaldb.AuthSlot) AttentionSummary {
	items := make([]AttentionItem, 0)
	level := "none"
	for _, proof := range proofs {
		if proof.Status != "failed" {
			continue
		}
		items = append(items, AttentionItem{Kind: "proof_failed", SourceID: proof.ID, Message: proof.Command})
		level = "action-required"
	}

	for _, slot := range authSlots {
		if slot.Status != "auth_required" && slot.Status != "auth_failed" && slot.Status != "not_installed" {
			continue
		}
		kind := "auth_required"
		if slot.Status == "auth_failed" {
			kind = "auth_failed"
		}
		if slot.Status == "not_installed" {
			kind = "auth_not_installed"
		}
		message := strings.TrimSpace(slot.Harness)
		if strings.TrimSpace(slot.Label) != "" {
			message = message + " " + strings.TrimSpace(slot.Label)
		}
		items = append(items, AttentionItem{Kind: kind, SourceID: slot.AuthSlotID, Message: strings.TrimSpace(message)})
		if kind == "auth_failed" || kind == "auth_not_installed" {
			level = "action-required"
			continue
		}
		if level == "none" || level == "running" {
			level = "auth"
		}
	}

	for _, session := range sessions {
		if session.Status != "running" && session.Status != "waiting" {
			continue
		}
		message := strings.TrimSpace(session.Name)
		if message == "" {
			message = strings.TrimSpace(session.Executor)
		}
		kind := "session_running"
		if session.Status == "waiting" {
			kind = "session_waiting"
		}
		if session.Usage == "ephemeral" && session.Status == "running" {
			kind = "ephemeral_running"
		}
		items = append(items, AttentionItem{Kind: kind, SourceID: session.ID, Message: message})
		if level == "none" {
			level = "running"
		}
	}
	if len(items) == 0 {
		return AttentionSummary{Level: "none", Items: []AttentionItem{}}
	}
	return AttentionSummary{Level: level, Items: items}
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

func (d *Daemon) workspaceSessionActivity(ctx context.Context, store *globaldb.Store, workspaceID string) ([]SessionActivity, error) {
	out := make([]SessionActivity, 0)
	seen := make(map[string]bool)
	executorRuns := d.executorRunsForWorkspace(workspaceID)
	for _, run := range executorRuns {
		out = append(out, SessionActivity{ID: run.HarnessSessionID, Status: run.Status, Executor: run.Executor, WorkspaceID: run.WorkspaceID, ActiveTaskID: run.TaskID, StartedAt: run.StartedAt, LastActivityAt: run.StartedAt})
		seen[run.HarnessSessionID] = true
	}
	persistedRuns, err := store.ListHarnessSessions(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	for _, run := range persistedRuns {
		if seen[run.SessionID] {
			continue
		}
		out = append(out, SessionActivity{ID: run.SessionID, Status: run.Status, Executor: run.Harness, WorkspaceID: run.WorkspaceID, Usage: run.Usage, SourceSessionID: run.SourceSessionID, SourceAgentID: run.SourceAgentID})
	}
	return out, nil
}

func (d *Daemon) executorRunsForWorkspace(workspaceID string) []HarnessSession {
	d.executorMu.RLock()
	runs := make([]HarnessSession, 0, len(d.executorRuns))
	for _, run := range d.executorRuns {
		if run.WorkspaceID != workspaceID {
			continue
		}
		runs = append(runs, run)
	}
	d.executorMu.RUnlock()
	sort.Slice(runs, func(i int, j int) bool {
		if runs[i].StartedAt == runs[j].StartedAt {
			return runs[i].HarnessSessionID < runs[j].HarnessSessionID
		}
		return runs[i].StartedAt < runs[j].StartedAt
	})
	return runs
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
	session, err := store.GetWorkspace(ctx, workspaceID)
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
