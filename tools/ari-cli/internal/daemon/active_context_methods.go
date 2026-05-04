package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

const activeContextMetaKey = "active_workspace_context"

type ContextGetRequest struct{}

type ContextGetResponse struct {
	Current ActiveWorkspaceContext `json:"current"`
}

type ContextSetRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

type ContextSetResponse struct {
	Previous ActiveWorkspaceContext `json:"previous"`
	Current  ActiveWorkspaceContext `json:"current"`
}

type ActiveWorkspaceContext struct {
	WorkspaceID string `json:"workspace_id,omitempty"`
	Version     string `json:"version,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

type WorkspaceMembershipsForPathRequest struct {
	Path string `json:"path"`
}

type WorkspaceMembershipsForPathResponse struct {
	Path        string                `json:"path"`
	Memberships []WorkspaceMembership `json:"memberships"`
}

type WorkspaceMembership struct {
	WorkspaceID   string `json:"workspace_id"`
	Name          string `json:"name"`
	Status        string `json:"status"`
	FolderPath    string `json:"folder_path"`
	PrimaryFolder bool   `json:"primary_folder"`
	Active        bool   `json:"active"`
}

type DashboardGetRequest struct {
	WorkspaceID            string `json:"workspace_id,omitempty"`
	CWD                    string `json:"cwd,omitempty"`
	ObservedContextVersion string `json:"observed_context_version,omitempty"`
}

type DashboardGetResponse struct {
	ActiveContext        ActiveWorkspaceContext  `json:"active_context"`
	EffectiveWorkspaceID string                  `json:"effective_workspace_id"`
	Status               WorkspaceStatusResponse `json:"status"`
	ResumeActions        []ResumeAction          `json:"resume_actions"`
	CWDMemberships       []WorkspaceMembership   `json:"cwd_memberships"`
}

type ResumeAction struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	WorkspaceID string `json:"workspace_id"`
	SourceID    string `json:"source_id"`
	Label       string `json:"label"`
}

type ResumeActionRequest struct {
	ActionID               string `json:"action_id"`
	WorkspaceID            string `json:"workspace_id,omitempty"`
	ObservedContextVersion string `json:"observed_context_version,omitempty"`
}

type ResumeActionResponse struct {
	Action ResumeAction `json:"action"`
}

func (d *Daemon) registerActiveContextMethods(registry *rpc.MethodRegistry, store *globaldb.Store) error {
	if registry == nil {
		return fmt.Errorf("method registry is required")
	}
	if store == nil {
		return fmt.Errorf("globaldb store is required")
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[ContextGetRequest, ContextGetResponse]{
		Name:        "context.get",
		Description: "Get daemon-owned active workspace context",
		Handler: func(ctx context.Context, req ContextGetRequest) (ContextGetResponse, error) {
			_ = req
			current, err := readActiveWorkspaceContext(ctx, store)
			if err != nil {
				return ContextGetResponse{}, err
			}
			return ContextGetResponse{Current: current}, nil
		},
	}); err != nil {
		return fmt.Errorf("register context.get: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[ContextSetRequest, ContextSetResponse]{
		Name:        "context.set",
		Description: "Set daemon-owned active workspace context",
		Handler: func(ctx context.Context, req ContextSetRequest) (ContextSetResponse, error) {
			return setActiveWorkspaceContext(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register context.set: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[WorkspaceMembershipsForPathRequest, WorkspaceMembershipsForPathResponse]{
		Name:        "workspace.memberships_for_path",
		Description: "List workspaces containing a filesystem path without changing active context",
		Handler: func(ctx context.Context, req WorkspaceMembershipsForPathRequest) (WorkspaceMembershipsForPathResponse, error) {
			return workspaceMembershipsForPath(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register workspace.memberships_for_path: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[DashboardGetRequest, DashboardGetResponse]{
		Name:        "dashboard.get",
		Description: "Project daemon-owned dashboard data for the active or explicit workspace context",
		Handler: func(ctx context.Context, req DashboardGetRequest) (DashboardGetResponse, error) {
			return d.dashboardGet(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register dashboard.get: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[ResumeActionRequest, ResumeActionResponse]{
		Name:        "resume.action",
		Description: "Resolve a daemon-owned dashboard resume action",
		Handler: func(ctx context.Context, req ResumeActionRequest) (ResumeActionResponse, error) {
			return d.resumeAction(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register resume.action: %w", err)
	}

	return nil
}

func (d *Daemon) resumeAction(ctx context.Context, store *globaldb.Store, req ResumeActionRequest) (ResumeActionResponse, error) {
	actionID := strings.TrimSpace(req.ActionID)
	if actionID == "" {
		return ResumeActionResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "action_id is required", nil)
	}
	dashboard, err := d.dashboardGet(ctx, store, DashboardGetRequest{WorkspaceID: req.WorkspaceID, ObservedContextVersion: req.ObservedContextVersion})
	if err != nil {
		return ResumeActionResponse{}, err
	}
	for _, action := range dashboard.ResumeActions {
		if action.ID == actionID {
			return ResumeActionResponse{Action: action}, nil
		}
	}
	return ResumeActionResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "resume action is not available", map[string]any{"reason": "resume_action_not_available", "action_id": actionID, "workspace_id": dashboard.EffectiveWorkspaceID})
}

func (d *Daemon) dashboardGet(ctx context.Context, store *globaldb.Store, req DashboardGetRequest) (DashboardGetResponse, error) {
	activeContext, err := readActiveWorkspaceContext(ctx, store)
	if err != nil {
		return DashboardGetResponse{}, err
	}
	effectiveWorkspaceID := strings.TrimSpace(req.WorkspaceID)
	if effectiveWorkspaceID == "" {
		if err := rejectStaleObservedContextVersion(req.ObservedContextVersion, activeContext); err != nil {
			return DashboardGetResponse{}, err
		}
		effectiveWorkspaceID = activeContext.WorkspaceID
	}
	if effectiveWorkspaceID == "" {
		return DashboardGetResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "active workspace context is not set", map[string]any{"reason": "missing_active_context"})
	}
	status, err := d.workspaceStatus(ctx, store, effectiveWorkspaceID)
	if err != nil {
		return DashboardGetResponse{}, err
	}
	memberships := []WorkspaceMembership{}
	if strings.TrimSpace(req.CWD) != "" {
		membershipResp, err := workspaceMembershipsForPath(ctx, store, WorkspaceMembershipsForPathRequest{Path: req.CWD})
		if err != nil {
			return DashboardGetResponse{}, err
		}
		memberships = membershipResp.Memberships
	}
	return DashboardGetResponse{ActiveContext: activeContext, EffectiveWorkspaceID: status.WorkspaceID, Status: status, ResumeActions: resumeActionsForStatus(status), CWDMemberships: memberships}, nil
}

func resumeActionsForStatus(status WorkspaceStatusResponse) []ResumeAction {
	actions := make([]ResumeAction, 0)
	for _, agent := range status.Sessions {
		if agent.Status != "running" {
			continue
		}
		if agent.Usage == "ephemeral" {
			continue
		}
		label := strings.TrimSpace(agent.Name)
		if label == "" {
			label = strings.TrimSpace(agent.Executor)
		}
		actions = append(actions, ResumeAction{ID: "resume:session:" + agent.ID, Kind: "resume_session", WorkspaceID: status.WorkspaceID, SourceID: agent.ID, Label: label})
	}
	return actions
}

func rejectStaleObservedContextVersion(observedVersion string, current ActiveWorkspaceContext) error {
	observedVersion = strings.TrimSpace(observedVersion)
	if observedVersion == "" || observedVersion == current.Version {
		return nil
	}
	return rpc.NewHandlerError(rpc.InvalidParams, "active workspace context changed", map[string]any{"reason": "context_changed", "observed_version": observedVersion, "current_version": current.Version, "current_workspace_id": current.WorkspaceID})
}

func workspaceMembershipsForPath(ctx context.Context, store *globaldb.Store, req WorkspaceMembershipsForPathRequest) (WorkspaceMembershipsForPathResponse, error) {
	path := strings.TrimSpace(req.Path)
	if path == "" {
		return WorkspaceMembershipsForPathResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "path is required", nil)
	}
	normalized, err := filepath.Abs(path)
	if err != nil {
		return WorkspaceMembershipsForPathResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
	}
	current, err := readActiveWorkspaceContext(ctx, store)
	if err != nil {
		return WorkspaceMembershipsForPathResponse{}, err
	}
	workspaces, err := store.ListSessions(ctx)
	if err != nil {
		return WorkspaceMembershipsForPathResponse{}, err
	}
	memberships := make([]WorkspaceMembership, 0)
	membershipByWorkspace := map[string]int{}
	for _, workspace := range workspaces {
		folders, err := store.ListFolders(ctx, workspace.ID)
		if err != nil {
			return WorkspaceMembershipsForPathResponse{}, mapWorkspaceStoreError(err, workspace.ID)
		}
		for _, folder := range folders {
			folderPath, err := filepath.Abs(folder.FolderPath)
			if err != nil {
				return WorkspaceMembershipsForPathResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
			}
			if !pathContains(folderPath, normalized) {
				continue
			}
			membership := WorkspaceMembership{WorkspaceID: workspace.ID, Name: workspace.Name, Status: workspace.Status, FolderPath: folderPath, PrimaryFolder: folder.IsPrimary, Active: workspace.ID == current.WorkspaceID}
			if existingIndex, ok := membershipByWorkspace[workspace.ID]; ok {
				if pathDepth(membership.FolderPath) > pathDepth(memberships[existingIndex].FolderPath) {
					memberships[existingIndex] = membership
				}
				continue
			}
			membershipByWorkspace[workspace.ID] = len(memberships)
			memberships = append(memberships, membership)
		}
	}
	sort.SliceStable(memberships, func(i, j int) bool {
		if memberships[i].Active != memberships[j].Active {
			return memberships[i].Active
		}
		if memberships[i].Name != memberships[j].Name {
			return memberships[i].Name < memberships[j].Name
		}
		return memberships[i].WorkspaceID < memberships[j].WorkspaceID
	})
	return WorkspaceMembershipsForPathResponse{Path: normalized, Memberships: memberships}, nil
}

func pathDepth(path string) int {
	path = filepath.Clean(path)
	if path == string(filepath.Separator) || path == "." {
		return 0
	}
	return len(strings.Split(path, string(filepath.Separator)))
}

func pathContains(root, target string) bool {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	if root == target {
		return true
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."
}

func setActiveWorkspaceContext(ctx context.Context, store *globaldb.Store, req ContextSetRequest) (ContextSetResponse, error) {
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" {
		return ContextSetResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "workspace_id is required", nil)
	}
	workspace, err := store.GetSession(ctx, workspaceID)
	if err != nil {
		return ContextSetResponse{}, mapWorkspaceStoreError(err, workspaceID)
	}
	previous, err := readActiveWorkspaceContext(ctx, store)
	if err != nil {
		return ContextSetResponse{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	current := ActiveWorkspaceContext{WorkspaceID: workspace.ID, Version: now, UpdatedAt: now}
	encoded, err := json.Marshal(current)
	if err != nil {
		return ContextSetResponse{}, fmt.Errorf("encode active workspace context: %w", err)
	}
	if err := store.SetMeta(ctx, activeContextMetaKey, string(encoded)); err != nil {
		return ContextSetResponse{}, err
	}
	return ContextSetResponse{Previous: previous, Current: current}, nil
}

func readActiveWorkspaceContext(ctx context.Context, store *globaldb.Store) (ActiveWorkspaceContext, error) {
	encoded, err := store.GetMeta(ctx, activeContextMetaKey)
	if err != nil {
		if errors.Is(err, globaldb.ErrNotFound) {
			return ActiveWorkspaceContext{}, nil
		}
		return ActiveWorkspaceContext{}, err
	}
	var current ActiveWorkspaceContext
	if err := json.Unmarshal([]byte(encoded), &current); err != nil {
		return ActiveWorkspaceContext{}, fmt.Errorf("decode active workspace context: %w", err)
	}
	current.WorkspaceID = strings.TrimSpace(current.WorkspaceID)
	current.Version = strings.TrimSpace(current.Version)
	current.UpdatedAt = strings.TrimSpace(current.UpdatedAt)
	return current, nil
}
