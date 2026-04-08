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

type WorkspaceCloseRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

type WorkspaceCloseResponse struct {
	Status string `json:"status"`
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

			if err := store.CreateSession(ctx, sessionID, name, originRoot, cleanupPolicy, vcsPreference); err != nil {
				return WorkspaceCreateResponse{}, mapWorkspaceStoreError(err, sessionID)
			}
			if err := store.AddFolder(ctx, sessionID, folderPath, vcsType, true); err != nil {
				if rollbackErr := store.DeleteSession(ctx, sessionID); rollbackErr != nil && !errors.Is(rollbackErr, globaldb.ErrNotFound) {
					return WorkspaceCreateResponse{}, fmt.Errorf("rollback workspace create after folder add failure: %w", rollbackErr)
				}
				return WorkspaceCreateResponse{}, mapWorkspaceStoreError(err, sessionID)
			}

			session, err := store.GetSession(ctx, sessionID)
			if err != nil {
				return WorkspaceCreateResponse{}, mapWorkspaceStoreError(err, sessionID)
			}

			return WorkspaceCreateResponse{
				WorkspaceID:   session.ID,
				Name:          session.Name,
				Status:        session.Status,
				Folder:        folderPath,
				VCSType:       vcsType,
				IsPrimary:     true,
				OriginRoot:    session.OriginRoot,
				VCSPreference: session.VCSPreference,
			}, nil
		},
	}); err != nil {
		return fmt.Errorf("register workspace.create: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[WorkspaceListRequest, WorkspaceListResponse]{
		Name:        "workspace.list",
		Description: "List workspaces",
		Handler: func(ctx context.Context, req WorkspaceListRequest) (WorkspaceListResponse, error) {
			_ = req
			sessions, err := store.ListSessions(ctx)
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

			session, err := store.GetSession(ctx, sessionID)
			if err != nil {
				if errors.Is(err, globaldb.ErrNotFound) {
					sessionByName, lookupErr := store.GetSessionByName(ctx, sessionID)
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

	if err := rpc.RegisterMethod(registry, rpc.Method[WorkspaceCloseRequest, WorkspaceCloseResponse]{
		Name:        "workspace.close",
		Description: "Close a workspace",
		Handler: func(ctx context.Context, req WorkspaceCloseRequest) (WorkspaceCloseResponse, error) {
			sessionID := strings.TrimSpace(req.WorkspaceID)
			if sessionID == "" {
				return WorkspaceCloseResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "workspace_id is required", nil)
			}
			if err := store.UpdateSessionStatus(ctx, sessionID, "closed"); err != nil {
				return WorkspaceCloseResponse{}, mapWorkspaceStoreError(err, sessionID)
			}
			return WorkspaceCloseResponse{Status: "closed"}, nil
		},
	}); err != nil {
		return fmt.Errorf("register workspace.close: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[WorkspaceSuspendRequest, WorkspaceSuspendResponse]{
		Name:        "workspace.suspend",
		Description: "Suspend a workspace",
		Handler: func(ctx context.Context, req WorkspaceSuspendRequest) (WorkspaceSuspendResponse, error) {
			sessionID := strings.TrimSpace(req.WorkspaceID)
			if sessionID == "" {
				return WorkspaceSuspendResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "workspace_id is required", nil)
			}
			if err := store.UpdateSessionStatus(ctx, sessionID, "suspended"); err != nil {
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
			if err := store.UpdateSessionStatus(ctx, sessionID, "active"); err != nil {
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

			session, err := store.GetSession(ctx, sessionID)
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

			if _, err := store.GetSession(ctx, sessionID); err != nil {
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

func mapWorkspaceStoreError(err error, sessionID string) error {
	if errors.Is(err, globaldb.ErrNotFound) {
		return rpc.NewHandlerError(rpc.SessionNotFound, "workspace not found", sessionID)
	}
	if errors.Is(err, globaldb.ErrSessionClosed) {
		return rpc.NewHandlerError(rpc.InvalidParams, "workspace is closed", sessionID)
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
