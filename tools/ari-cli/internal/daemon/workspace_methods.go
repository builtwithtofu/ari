package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/vcs"
)

type WorkspaceCreateRequest struct {
	Name          string `json:"name"`
	Folder        string `json:"folder"`
	OriginRoot    string `json:"origin_root"`
	CleanupPolicy string `json:"cleanup_policy"`
	VCSPreference string `json:"vcs_preference"`
}

type WorkspaceCreateResponse struct {
	WorkspaceID   string `json:"workspace_id"`
	Name          string `json:"name"`
	Status        string `json:"status"`
	Folder        string `json:"folder"`
	VCSType       string `json:"vcs_type"`
	IsPrimary     bool   `json:"is_primary"`
	OriginRoot    string `json:"origin_root"`
	VCSPreference string `json:"vcs_preference"`
}

type WorkspaceSetupExistingRequest struct {
	Name          string `json:"name"`
	Folder        string `json:"folder"`
	VCSPreference string `json:"vcs_preference"`
}

type WorkspaceSetupExistingResponse struct {
	WorkspaceID     string `json:"workspace_id"`
	Name            string `json:"name"`
	Folder          string `json:"folder"`
	VCSType         string `json:"vcs_type"`
	ActiveWorkspace string `json:"active_workspace"`
	RollbackPointID string `json:"rollback_point_id"`
}

type WorkspaceListRequest struct{}

type WorkspaceSummary struct {
	WorkspaceID   string `json:"workspace_id"`
	Name          string `json:"name"`
	Status        string `json:"status"`
	PrimaryFolder string `json:"primary_folder"`
	FolderCount   int    `json:"folder_count"`
	CreatedAt     string `json:"created_at"`
}

type WorkspaceListResponse struct {
	Workspaces []WorkspaceSummary `json:"workspaces"`
}

type WorkspaceGetRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

type WorkspaceFolderInfo struct {
	Path      string `json:"path"`
	VCSType   string `json:"vcs_type"`
	IsPrimary bool   `json:"is_primary"`
	AddedAt   string `json:"added_at"`
	HasConfig bool   `json:"has_config"`
}

type WorkspaceGetResponse struct {
	WorkspaceID   string                `json:"workspace_id"`
	Name          string                `json:"name"`
	Status        string                `json:"status"`
	VCSPreference string                `json:"vcs_preference"`
	OriginRoot    string                `json:"origin_root"`
	CleanupPolicy string                `json:"cleanup_policy"`
	CreatedAt     string                `json:"created_at"`
	UpdatedAt     string                `json:"updated_at"`
	Folders       []WorkspaceFolderInfo `json:"folders"`
}

type WorkspaceSuspendRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

type WorkspaceSuspendResponse struct {
	Status string `json:"status"`
}

type WorkspaceResumeRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

type WorkspaceResumeResponse struct {
	Status string `json:"status"`
}

type WorkspaceAddFolderRequest struct {
	WorkspaceID string `json:"workspace_id"`
	FolderPath  string `json:"folder_path"`
}

type WorkspaceAddFolderResponse struct {
	FolderPath string `json:"folder_path"`
	VCSType    string `json:"vcs_type"`
}

type WorkspaceRemoveFolderRequest struct {
	WorkspaceID string `json:"workspace_id"`
	FolderPath  string `json:"folder_path"`
}

type WorkspaceRemoveFolderResponse struct{}

func (d *Daemon) registerWorkspaceMethods(registry *rpc.MethodRegistry, store *globaldb.Store) error {
	if registry == nil {
		return fmt.Errorf("method registry is required")
	}
	if store == nil {
		return fmt.Errorf("globaldb store is required")
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[WorkspaceCreateRequest, WorkspaceCreateResponse]{
		Name:        "workspace.create",
		Description: "Create a workspace",
		Handler: func(ctx context.Context, req WorkspaceCreateRequest) (WorkspaceCreateResponse, error) {
			return d.createWorkspace(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register workspace.create: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[WorkspaceSetupExistingRequest, WorkspaceSetupExistingResponse]{
		Name:        "workspace.setup_existing",
		Description: "Create and select a project workspace from an existing folder",
		Handler: func(ctx context.Context, req WorkspaceSetupExistingRequest) (WorkspaceSetupExistingResponse, error) {
			return d.workspaceSetupExisting(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register workspace.setup_existing: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[WorkspaceListRequest, WorkspaceListResponse]{
		Name:        "workspace.list",
		Description: "List workspaces",
		Handler: func(ctx context.Context, req WorkspaceListRequest) (WorkspaceListResponse, error) {
			_ = req
			sessions, err := store.ListWorkspaces(ctx)
			if err != nil {
				return WorkspaceListResponse{}, err
			}

			out := make([]WorkspaceSummary, 0, len(sessions))
			for _, session := range sessions {
				folders, err := store.ListFolders(ctx, session.ID)
				if err != nil {
					return WorkspaceListResponse{}, mapWorkspaceStoreError(err, session.ID)
				}

				primary := ""
				for _, folder := range folders {
					if folder.IsPrimary {
						primary = folder.FolderPath
						break
					}
				}

				out = append(out, WorkspaceSummary{
					WorkspaceID:   session.ID,
					Name:          session.Name,
					Status:        session.Status,
					PrimaryFolder: primary,
					FolderCount:   len(folders),
					CreatedAt:     session.CreatedAt,
				})
			}

			return WorkspaceListResponse{Workspaces: out}, nil
		},
	}); err != nil {
		return fmt.Errorf("register workspace.list: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[WorkspaceGetRequest, WorkspaceGetResponse]{
		Name:        "workspace.get",
		Description: "Get workspace details",
		Handler: func(ctx context.Context, req WorkspaceGetRequest) (WorkspaceGetResponse, error) {
			sessionID := strings.TrimSpace(req.WorkspaceID)
			if sessionID == "" {
				return WorkspaceGetResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "workspace_id is required", nil)
			}

			session, err := store.GetWorkspace(ctx, sessionID)
			if err != nil {
				if errors.Is(err, globaldb.ErrNotFound) {
					sessionByName, lookupErr := store.GetWorkspaceByName(ctx, sessionID)
					if lookupErr != nil {
						return WorkspaceGetResponse{}, mapWorkspaceStoreError(lookupErr, sessionID)
					}
					session = sessionByName
				} else {
					return WorkspaceGetResponse{}, mapWorkspaceStoreError(err, sessionID)
				}
			}

			folders, err := store.ListFolders(ctx, session.ID)
			if err != nil {
				return WorkspaceGetResponse{}, mapWorkspaceStoreError(err, session.ID)
			}

			folderInfo := make([]WorkspaceFolderInfo, 0, len(folders))
			for _, folder := range folders {
				folderInfo = append(folderInfo, WorkspaceFolderInfo{
					Path:      folder.FolderPath,
					VCSType:   folder.VCSType,
					IsPrimary: folder.IsPrimary,
					AddedAt:   folder.AddedAt,
					HasConfig: hasAriConfig(folder.FolderPath),
				})
			}

			return WorkspaceGetResponse{
				WorkspaceID:   session.ID,
				Name:          session.Name,
				Status:        session.Status,
				VCSPreference: session.VCSPreference,
				OriginRoot:    session.OriginRoot,
				CleanupPolicy: session.CleanupPolicy,
				CreatedAt:     session.CreatedAt,
				UpdatedAt:     session.UpdatedAt,
				Folders:       folderInfo,
			}, nil
		},
	}); err != nil {
		return fmt.Errorf("register workspace.get: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[WorkspaceSuspendRequest, WorkspaceSuspendResponse]{
		Name:        "workspace.suspend",
		Description: "Suspend a workspace",
		Handler: func(ctx context.Context, req WorkspaceSuspendRequest) (WorkspaceSuspendResponse, error) {
			sessionID := strings.TrimSpace(req.WorkspaceID)
			if sessionID == "" {
				return WorkspaceSuspendResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "workspace_id is required", nil)
			}
			if err := store.UpdateWorkspaceStatus(ctx, sessionID, "suspended"); err != nil {
				return WorkspaceSuspendResponse{}, mapWorkspaceStoreError(err, sessionID)
			}
			return WorkspaceSuspendResponse{Status: "suspended"}, nil
		},
	}); err != nil {
		return fmt.Errorf("register workspace.suspend: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[WorkspaceResumeRequest, WorkspaceResumeResponse]{
		Name:        "workspace.resume",
		Description: "Resume a workspace",
		Handler: func(ctx context.Context, req WorkspaceResumeRequest) (WorkspaceResumeResponse, error) {
			sessionID := strings.TrimSpace(req.WorkspaceID)
			if sessionID == "" {
				return WorkspaceResumeResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "workspace_id is required", nil)
			}
			if err := store.UpdateWorkspaceStatus(ctx, sessionID, "active"); err != nil {
				return WorkspaceResumeResponse{}, mapWorkspaceStoreError(err, sessionID)
			}
			return WorkspaceResumeResponse{Status: "active"}, nil
		},
	}); err != nil {
		return fmt.Errorf("register workspace.resume: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[WorkspaceAddFolderRequest, WorkspaceAddFolderResponse]{
		Name:        "workspace.add_folder",
		Description: "Add folder to a workspace",
		Handler: func(ctx context.Context, req WorkspaceAddFolderRequest) (WorkspaceAddFolderResponse, error) {
			sessionID := strings.TrimSpace(req.WorkspaceID)
			if sessionID == "" {
				return WorkspaceAddFolderResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "workspace_id is required", nil)
			}

			session, err := store.GetWorkspace(ctx, sessionID)
			if err != nil {
				return WorkspaceAddFolderResponse{}, mapWorkspaceStoreError(err, sessionID)
			}

			folderPath, vcsType, err := normalizeAndValidateVCSRoot(req.FolderPath, normalizeVCSPreference(session.VCSPreference))
			if err != nil {
				return WorkspaceAddFolderResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
			}

			if err := store.AddFolder(ctx, sessionID, folderPath, vcsType, false); err != nil {
				return WorkspaceAddFolderResponse{}, mapWorkspaceStoreError(err, sessionID)
			}

			return WorkspaceAddFolderResponse{FolderPath: folderPath, VCSType: vcsType}, nil
		},
	}); err != nil {
		return fmt.Errorf("register workspace.add_folder: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[WorkspaceRemoveFolderRequest, WorkspaceRemoveFolderResponse]{
		Name:        "workspace.remove_folder",
		Description: "Remove folder from a workspace",
		Handler: func(ctx context.Context, req WorkspaceRemoveFolderRequest) (WorkspaceRemoveFolderResponse, error) {
			sessionID := strings.TrimSpace(req.WorkspaceID)
			if sessionID == "" {
				return WorkspaceRemoveFolderResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "workspace_id is required", nil)
			}

			if _, err := store.GetWorkspace(ctx, sessionID); err != nil {
				return WorkspaceRemoveFolderResponse{}, mapWorkspaceStoreError(err, sessionID)
			}

			folderPath, err := normalizePath(req.FolderPath)
			if err != nil {
				return WorkspaceRemoveFolderResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
			}

			if err := store.RemoveFolder(ctx, sessionID, folderPath); err != nil {
				if errors.Is(err, globaldb.ErrNotFound) {
					return WorkspaceRemoveFolderResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "folder not found in workspace", sessionID)
				}
				return WorkspaceRemoveFolderResponse{}, mapWorkspaceStoreError(err, sessionID)
			}

			return WorkspaceRemoveFolderResponse{}, nil
		},
	}); err != nil {
		return fmt.Errorf("register workspace.remove_folder: %w", err)
	}

	return nil
}

func (d *Daemon) workspaceSetupExisting(ctx context.Context, store *globaldb.Store, req WorkspaceSetupExistingRequest) (WorkspaceSetupExistingResponse, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return WorkspaceSetupExistingResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "workspace name is required", nil)
	}
	vcsPreference, err := parseVCSPreference(req.VCSPreference)
	if err != nil {
		return WorkspaceSetupExistingResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
	}
	folderPath, vcsType, err := normalizeAndValidateVCSRoot(req.Folder, vcsPreference)
	if err != nil {
		return WorkspaceSetupExistingResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
	}
	previousContext, err := readActiveWorkspaceContext(ctx, store)
	if err != nil {
		return WorkspaceSetupExistingResponse{}, err
	}
	payload := map[string]string{"name": name, "folder": folderPath, "vcs_type": vcsType, "previous_workspace_id": previousContext.WorkspaceID}
	checkpoint, err := createDaemonOperationCheckpoint(ctx, store, daemonOperationCheckpointOptions{Actor: "user", Source: daemonOperationSourceCLI, Scope: globaldb.OperationScopeGlobal, RequestSummary: "set up project workspace", PayloadSnapshot: payload})
	if err != nil {
		return WorkspaceSetupExistingResponse{}, err
	}

	var response WorkspaceSetupExistingResponse
	created, err := d.createWorkspace(ctx, store, WorkspaceCreateRequest{Name: name, Folder: folderPath, OriginRoot: folderPath, CleanupPolicy: "manual", VCSPreference: vcsPreference})
	if err != nil {
		return WorkspaceSetupExistingResponse{}, err
	}
	payload["workspace_id"] = created.WorkspaceID
	contextResp, err := setActiveWorkspaceContext(ctx, store, ContextSetRequest{WorkspaceID: created.WorkspaceID})
	if err != nil {
		if rollbackErr := store.DeleteWorkspace(ctx, created.WorkspaceID); rollbackErr != nil && !errors.Is(rollbackErr, globaldb.ErrNotFound) {
			return WorkspaceSetupExistingResponse{}, fmt.Errorf("rollback workspace setup after active context failure: %w", rollbackErr)
		}
		return WorkspaceSetupExistingResponse{}, err
	}
	if err := patchJSONConfigStrings(d.configPath, map[string]string{"active_workspace": created.WorkspaceID}); err != nil {
		if previousContext.WorkspaceID != "" {
			if _, rollbackErr := setActiveWorkspaceContext(ctx, store, ContextSetRequest{WorkspaceID: previousContext.WorkspaceID}); rollbackErr != nil {
				return WorkspaceSetupExistingResponse{}, fmt.Errorf("restore previous active workspace after config failure: %w", rollbackErr)
			}
		} else if rollbackErr := store.SetMeta(ctx, activeContextMetaKey, `{}`); rollbackErr != nil {
			return WorkspaceSetupExistingResponse{}, fmt.Errorf("clear active workspace after config failure: %w", rollbackErr)
		}
		if rollbackErr := store.DeleteWorkspace(ctx, created.WorkspaceID); rollbackErr != nil && !errors.Is(rollbackErr, globaldb.ErrNotFound) {
			return WorkspaceSetupExistingResponse{}, fmt.Errorf("rollback workspace setup after config failure: %w", rollbackErr)
		}
		return WorkspaceSetupExistingResponse{}, err
	}
	if _, err := appendDaemonOperationRecord(ctx, store, daemonOperationRecordOptions{WorkspaceID: created.WorkspaceID, OperationType: "workspace_project_setup", OperationKind: daemonOperationKindMutating, Actor: "user", Source: daemonOperationSourceCLI, Scope: globaldb.OperationScopeWorkspace, RequestSummary: "create and select project workspace", ParentOperationID: checkpoint.OperationID, CheckpointOperationID: checkpoint.OperationID, RollbackPointID: checkpoint.OperationID, RollbackData: map[string]string{"workspace_id": created.WorkspaceID, "previous_workspace_id": previousContext.WorkspaceID, "scope": "ari_owned_state_only"}, PayloadSnapshot: payload}, daemonOperationResultSucceeded); err != nil {
		if previousContext.WorkspaceID != "" {
			if _, rollbackErr := setActiveWorkspaceContext(ctx, store, ContextSetRequest{WorkspaceID: previousContext.WorkspaceID}); rollbackErr != nil {
				return WorkspaceSetupExistingResponse{}, fmt.Errorf("restore previous active workspace after operation record failure: %w", rollbackErr)
			}
			if rollbackErr := patchJSONConfigStrings(d.configPath, map[string]string{"active_workspace": previousContext.WorkspaceID}); rollbackErr != nil {
				return WorkspaceSetupExistingResponse{}, fmt.Errorf("restore persisted active workspace after operation record failure: %w", rollbackErr)
			}
		} else {
			if rollbackErr := store.SetMeta(ctx, activeContextMetaKey, `{}`); rollbackErr != nil {
				return WorkspaceSetupExistingResponse{}, fmt.Errorf("clear active workspace after operation record failure: %w", rollbackErr)
			}
			if rollbackErr := patchJSONConfigStrings(d.configPath, map[string]string{"active_workspace": ""}); rollbackErr != nil {
				return WorkspaceSetupExistingResponse{}, fmt.Errorf("clear persisted active workspace after operation record failure: %w", rollbackErr)
			}
		}
		if rollbackErr := store.DeleteWorkspace(ctx, created.WorkspaceID); rollbackErr != nil && !errors.Is(rollbackErr, globaldb.ErrNotFound) {
			return WorkspaceSetupExistingResponse{}, fmt.Errorf("rollback workspace setup after operation record failure: %w", rollbackErr)
		}
		return WorkspaceSetupExistingResponse{}, err
	}
	response = WorkspaceSetupExistingResponse{WorkspaceID: created.WorkspaceID, Name: created.Name, Folder: created.Folder, VCSType: created.VCSType, ActiveWorkspace: contextResp.Current.WorkspaceID, RollbackPointID: checkpoint.OperationID}
	return response, nil
}

func (d *Daemon) createWorkspace(ctx context.Context, store *globaldb.Store, req WorkspaceCreateRequest) (WorkspaceCreateResponse, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return WorkspaceCreateResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "workspace name is required", nil)
	}
	vcsPreference, err := parseVCSPreference(req.VCSPreference)
	if err != nil {
		return WorkspaceCreateResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
	}
	folderPath, vcsType, err := normalizeAndValidateVCSRoot(req.Folder, vcsPreference)
	if err != nil {
		return WorkspaceCreateResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
	}
	originRoot, err := normalizePath(req.OriginRoot)
	if err != nil {
		return WorkspaceCreateResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
	}
	cleanupPolicy := strings.TrimSpace(req.CleanupPolicy)
	if cleanupPolicy == "" {
		cleanupPolicy = "manual"
	}
	sessionID, err := newWorkspaceID()
	if err != nil {
		return WorkspaceCreateResponse{}, fmt.Errorf("generate workspace id: %w", err)
	}
	if err := store.CreateWorkspace(ctx, sessionID, name, originRoot, cleanupPolicy, vcsPreference); err != nil {
		return WorkspaceCreateResponse{}, mapWorkspaceStoreError(err, sessionID)
	}
	if err := store.AddFolder(ctx, sessionID, folderPath, vcsType, true); err != nil {
		if rollbackErr := store.DeleteWorkspace(ctx, sessionID); rollbackErr != nil && !errors.Is(rollbackErr, globaldb.ErrNotFound) {
			return WorkspaceCreateResponse{}, fmt.Errorf("rollback workspace create after folder add failure: %w", rollbackErr)
		}
		return WorkspaceCreateResponse{}, mapWorkspaceStoreError(err, sessionID)
	}
	helperHarness, err := d.readConfiguredDefaultHarness()
	if err != nil {
		if rollbackErr := store.DeleteWorkspace(ctx, sessionID); rollbackErr != nil && !errors.Is(rollbackErr, globaldb.ErrNotFound) {
			return WorkspaceCreateResponse{}, fmt.Errorf("rollback workspace create after helper harness read failure: %w", rollbackErr)
		}
		return WorkspaceCreateResponse{}, err
	}
	if helperHarness != "" {
		if _, err := store.EnsureDefaultHelperProfile(ctx, sessionID, helperHarness, helperPrompt()); err != nil {
			if rollbackErr := store.DeleteWorkspace(ctx, sessionID); rollbackErr != nil && !errors.Is(rollbackErr, globaldb.ErrNotFound) {
				return WorkspaceCreateResponse{}, fmt.Errorf("rollback workspace create after helper ensure failure: %w", rollbackErr)
			}
			return WorkspaceCreateResponse{}, err
		}
	}
	session, err := store.GetWorkspace(ctx, sessionID)
	if err != nil {
		return WorkspaceCreateResponse{}, mapWorkspaceStoreError(err, sessionID)
	}
	return WorkspaceCreateResponse{WorkspaceID: session.ID, Name: session.Name, Status: session.Status, Folder: folderPath, VCSType: vcsType, IsPrimary: true, OriginRoot: session.OriginRoot, VCSPreference: session.VCSPreference}, nil
}

func mapWorkspaceStoreError(err error, sessionID string) error {
	if errors.Is(err, globaldb.ErrNotFound) {
		return rpc.NewHandlerError(rpc.SessionNotFound, "workspace not found", sessionID)
	}
	if errors.Is(err, globaldb.ErrLastFolder) {
		return rpc.NewHandlerError(rpc.InvalidParams, "cannot remove last folder", sessionID)
	}
	if errors.Is(err, globaldb.ErrInvalidInput) {
		return rpc.NewHandlerError(rpc.InvalidParams, err.Error(), sessionID)
	}
	return err
}

func normalizeAndValidateVCSRoot(path, preference string) (string, string, error) {
	normalizedPath, err := normalizePath(path)
	if err != nil {
		return "", "", err
	}

	backend, err := vcs.Detect(normalizedPath)
	if err != nil {
		return "", "", err
	}
	if backend.Name() == "none" {
		return "", "", fmt.Errorf("folder is not a VCS root: %s", normalizedPath)
	}

	backendRoot, err := normalizePath(backend.Root())
	if err != nil {
		return "", "", err
	}
	if backendRoot != normalizedPath {
		return "", "", fmt.Errorf("folder is not a VCS root: %s (detected root: %s)", normalizedPath, backendRoot)
	}

	vcsType, err := chooseVCSTypeForRoot(normalizedPath, normalizeVCSPreference(preference))
	if err != nil {
		return "", "", err
	}

	return normalizedPath, vcsType, nil
}

func normalizeVCSPreference(preference string) string {
	preference = strings.ToLower(strings.TrimSpace(preference))
	if preference == "jj" || preference == "git" {
		return preference
	}
	return "auto"
}

func parseVCSPreference(raw string) (string, error) {
	preference := strings.ToLower(strings.TrimSpace(raw))
	switch preference {
	case "", "auto":
		return "auto", nil
	case "jj", "git":
		return preference, nil
	default:
		return "", fmt.Errorf("vcs_preference must be one of auto, jj, git")
	}
}

func chooseVCSTypeForRoot(rootPath, preference string) (string, error) {
	hasJJ := hasVCSMarker(rootPath, ".jj")
	hasGit := hasVCSMarker(rootPath, ".git")
	if !hasJJ && !hasGit {
		return "", fmt.Errorf("folder is not a VCS root: %s", rootPath)
	}

	switch normalizeVCSPreference(preference) {
	case "jj":
		if hasJJ {
			return "jj", nil
		}
		if hasGit {
			return "git", nil
		}
	case "git":
		if hasGit {
			return "git", nil
		}
		if hasJJ {
			return "jj", nil
		}
	default:
		if hasJJ {
			return "jj", nil
		}
		if hasGit {
			return "git", nil
		}
	}

	return "", fmt.Errorf("folder is not a VCS root: %s", rootPath)
}

func hasVCSMarker(rootPath, marker string) bool {
	_, err := os.Stat(filepath.Join(rootPath, marker))
	return err == nil
}

func normalizePath(path string) (string, error) {
	if path = strings.TrimSpace(path); path == "" {
		return "", fmt.Errorf("path is required")
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}

	return absPath, nil
}

func hasAriConfig(folderPath string) bool {
	_, err := os.Stat(filepath.Join(folderPath, "ari.json"))
	return err == nil
}

func newWorkspaceID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(buf)

	return fmt.Sprintf("%s-%s-%s-%s-%s", encoded[0:8], encoded[8:12], encoded[12:16], encoded[16:20], encoded[20:32]), nil
}
