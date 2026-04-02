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

type SessionCreateRequest struct {
	Name          string `json:"name"`
	Folder        string `json:"folder"`
	OriginRoot    string `json:"origin_root"`
	CleanupPolicy string `json:"cleanup_policy"`
	VCSPreference string `json:"vcs_preference"`
}

type SessionCreateResponse struct {
	SessionID     string `json:"session_id"`
	Name          string `json:"name"`
	Status        string `json:"status"`
	Folder        string `json:"folder"`
	VCSType       string `json:"vcs_type"`
	IsPrimary     bool   `json:"is_primary"`
	OriginRoot    string `json:"origin_root"`
	VCSPreference string `json:"vcs_preference"`
}

type SessionListRequest struct{}

type SessionSummary struct {
	SessionID     string `json:"session_id"`
	Name          string `json:"name"`
	Status        string `json:"status"`
	PrimaryFolder string `json:"primary_folder"`
	FolderCount   int    `json:"folder_count"`
	CreatedAt     string `json:"created_at"`
}

type SessionListResponse struct {
	Sessions []SessionSummary `json:"sessions"`
}

type SessionGetRequest struct {
	SessionID string `json:"session_id"`
}

type SessionFolderInfo struct {
	Path      string `json:"path"`
	VCSType   string `json:"vcs_type"`
	IsPrimary bool   `json:"is_primary"`
	AddedAt   string `json:"added_at"`
	HasConfig bool   `json:"has_config"`
}

type SessionGetResponse struct {
	SessionID     string              `json:"session_id"`
	Name          string              `json:"name"`
	Status        string              `json:"status"`
	VCSPreference string              `json:"vcs_preference"`
	OriginRoot    string              `json:"origin_root"`
	CleanupPolicy string              `json:"cleanup_policy"`
	CreatedAt     string              `json:"created_at"`
	UpdatedAt     string              `json:"updated_at"`
	Folders       []SessionFolderInfo `json:"folders"`
}

type SessionCloseRequest struct {
	SessionID string `json:"session_id"`
}

type SessionCloseResponse struct {
	Status string `json:"status"`
}

type SessionSuspendRequest struct {
	SessionID string `json:"session_id"`
}

type SessionSuspendResponse struct {
	Status string `json:"status"`
}

type SessionResumeRequest struct {
	SessionID string `json:"session_id"`
}

type SessionResumeResponse struct {
	Status string `json:"status"`
}

type SessionAddFolderRequest struct {
	SessionID  string `json:"session_id"`
	FolderPath string `json:"folder_path"`
}

type SessionAddFolderResponse struct {
	FolderPath string `json:"folder_path"`
	VCSType    string `json:"vcs_type"`
}

type SessionRemoveFolderRequest struct {
	SessionID  string `json:"session_id"`
	FolderPath string `json:"folder_path"`
}

type SessionRemoveFolderResponse struct{}

func (d *Daemon) registerSessionMethods(registry *rpc.MethodRegistry, store *globaldb.Store) error {
	if registry == nil {
		return fmt.Errorf("method registry is required")
	}
	if store == nil {
		return fmt.Errorf("globaldb store is required")
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[SessionCreateRequest, SessionCreateResponse]{
		Name:        "session.create",
		Description: "Create a session",
		Handler: func(ctx context.Context, req SessionCreateRequest) (SessionCreateResponse, error) {
			name := strings.TrimSpace(req.Name)
			if name == "" {
				return SessionCreateResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "session name is required", nil)
			}

			vcsPreference, err := parseVCSPreference(req.VCSPreference)
			if err != nil {
				return SessionCreateResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
			}
			folderPath, vcsType, err := normalizeAndValidateVCSRoot(req.Folder, vcsPreference)
			if err != nil {
				return SessionCreateResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
			}

			originRoot, err := normalizePath(req.OriginRoot)
			if err != nil {
				return SessionCreateResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
			}

			cleanupPolicy := strings.TrimSpace(req.CleanupPolicy)
			if cleanupPolicy == "" {
				cleanupPolicy = "manual"
			}

			sessionID, err := newSessionID()
			if err != nil {
				return SessionCreateResponse{}, fmt.Errorf("generate session id: %w", err)
			}

			if err := store.CreateSession(ctx, sessionID, name, originRoot, cleanupPolicy, vcsPreference); err != nil {
				return SessionCreateResponse{}, mapSessionStoreError(err, sessionID)
			}
			if err := store.AddFolder(ctx, sessionID, folderPath, vcsType, true); err != nil {
				if rollbackErr := store.DeleteSession(ctx, sessionID); rollbackErr != nil && !errors.Is(rollbackErr, globaldb.ErrNotFound) {
					return SessionCreateResponse{}, fmt.Errorf("rollback session create after folder add failure: %w", rollbackErr)
				}
				return SessionCreateResponse{}, mapSessionStoreError(err, sessionID)
			}

			session, err := store.GetSession(ctx, sessionID)
			if err != nil {
				return SessionCreateResponse{}, mapSessionStoreError(err, sessionID)
			}

			return SessionCreateResponse{
				SessionID:     session.ID,
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
		return fmt.Errorf("register session.create: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[SessionListRequest, SessionListResponse]{
		Name:        "session.list",
		Description: "List sessions",
		Handler: func(ctx context.Context, req SessionListRequest) (SessionListResponse, error) {
			_ = req
			sessions, err := store.ListSessions(ctx)
			if err != nil {
				return SessionListResponse{}, err
			}

			out := make([]SessionSummary, 0, len(sessions))
			for _, session := range sessions {
				folders, err := store.ListFolders(ctx, session.ID)
				if err != nil {
					return SessionListResponse{}, mapSessionStoreError(err, session.ID)
				}

				primary := ""
				for _, folder := range folders {
					if folder.IsPrimary {
						primary = folder.FolderPath
						break
					}
				}

				out = append(out, SessionSummary{
					SessionID:     session.ID,
					Name:          session.Name,
					Status:        session.Status,
					PrimaryFolder: primary,
					FolderCount:   len(folders),
					CreatedAt:     session.CreatedAt,
				})
			}

			return SessionListResponse{Sessions: out}, nil
		},
	}); err != nil {
		return fmt.Errorf("register session.list: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[SessionGetRequest, SessionGetResponse]{
		Name:        "session.get",
		Description: "Get session details",
		Handler: func(ctx context.Context, req SessionGetRequest) (SessionGetResponse, error) {
			sessionID := strings.TrimSpace(req.SessionID)
			if sessionID == "" {
				return SessionGetResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "session_id is required", nil)
			}

			session, err := store.GetSession(ctx, sessionID)
			if err != nil {
				if errors.Is(err, globaldb.ErrNotFound) {
					sessionByName, lookupErr := store.GetSessionByName(ctx, sessionID)
					if lookupErr != nil {
						return SessionGetResponse{}, mapSessionStoreError(lookupErr, sessionID)
					}
					session = sessionByName
				} else {
					return SessionGetResponse{}, mapSessionStoreError(err, sessionID)
				}
			}

			folders, err := store.ListFolders(ctx, session.ID)
			if err != nil {
				return SessionGetResponse{}, mapSessionStoreError(err, session.ID)
			}

			folderInfo := make([]SessionFolderInfo, 0, len(folders))
			for _, folder := range folders {
				folderInfo = append(folderInfo, SessionFolderInfo{
					Path:      folder.FolderPath,
					VCSType:   folder.VCSType,
					IsPrimary: folder.IsPrimary,
					AddedAt:   folder.AddedAt,
					HasConfig: hasAriConfig(folder.FolderPath),
				})
			}

			return SessionGetResponse{
				SessionID:     session.ID,
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
		return fmt.Errorf("register session.get: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[SessionCloseRequest, SessionCloseResponse]{
		Name:        "session.close",
		Description: "Close a session",
		Handler: func(ctx context.Context, req SessionCloseRequest) (SessionCloseResponse, error) {
			sessionID := strings.TrimSpace(req.SessionID)
			if sessionID == "" {
				return SessionCloseResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "session_id is required", nil)
			}
			if err := store.UpdateSessionStatus(ctx, sessionID, "closed"); err != nil {
				return SessionCloseResponse{}, mapSessionStoreError(err, sessionID)
			}
			return SessionCloseResponse{Status: "closed"}, nil
		},
	}); err != nil {
		return fmt.Errorf("register session.close: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[SessionSuspendRequest, SessionSuspendResponse]{
		Name:        "session.suspend",
		Description: "Suspend a session",
		Handler: func(ctx context.Context, req SessionSuspendRequest) (SessionSuspendResponse, error) {
			sessionID := strings.TrimSpace(req.SessionID)
			if sessionID == "" {
				return SessionSuspendResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "session_id is required", nil)
			}
			if err := store.UpdateSessionStatus(ctx, sessionID, "suspended"); err != nil {
				return SessionSuspendResponse{}, mapSessionStoreError(err, sessionID)
			}
			return SessionSuspendResponse{Status: "suspended"}, nil
		},
	}); err != nil {
		return fmt.Errorf("register session.suspend: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[SessionResumeRequest, SessionResumeResponse]{
		Name:        "session.resume",
		Description: "Resume a session",
		Handler: func(ctx context.Context, req SessionResumeRequest) (SessionResumeResponse, error) {
			sessionID := strings.TrimSpace(req.SessionID)
			if sessionID == "" {
				return SessionResumeResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "session_id is required", nil)
			}
			if err := store.UpdateSessionStatus(ctx, sessionID, "active"); err != nil {
				return SessionResumeResponse{}, mapSessionStoreError(err, sessionID)
			}
			return SessionResumeResponse{Status: "active"}, nil
		},
	}); err != nil {
		return fmt.Errorf("register session.resume: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[SessionAddFolderRequest, SessionAddFolderResponse]{
		Name:        "session.add_folder",
		Description: "Add folder to a session",
		Handler: func(ctx context.Context, req SessionAddFolderRequest) (SessionAddFolderResponse, error) {
			sessionID := strings.TrimSpace(req.SessionID)
			if sessionID == "" {
				return SessionAddFolderResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "session_id is required", nil)
			}

			session, err := store.GetSession(ctx, sessionID)
			if err != nil {
				return SessionAddFolderResponse{}, mapSessionStoreError(err, sessionID)
			}

			folderPath, vcsType, err := normalizeAndValidateVCSRoot(req.FolderPath, normalizeVCSPreference(session.VCSPreference))
			if err != nil {
				return SessionAddFolderResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
			}

			if err := store.AddFolder(ctx, sessionID, folderPath, vcsType, false); err != nil {
				return SessionAddFolderResponse{}, mapSessionStoreError(err, sessionID)
			}

			return SessionAddFolderResponse{FolderPath: folderPath, VCSType: vcsType}, nil
		},
	}); err != nil {
		return fmt.Errorf("register session.add_folder: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[SessionRemoveFolderRequest, SessionRemoveFolderResponse]{
		Name:        "session.remove_folder",
		Description: "Remove folder from a session",
		Handler: func(ctx context.Context, req SessionRemoveFolderRequest) (SessionRemoveFolderResponse, error) {
			sessionID := strings.TrimSpace(req.SessionID)
			if sessionID == "" {
				return SessionRemoveFolderResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "session_id is required", nil)
			}

			if _, err := store.GetSession(ctx, sessionID); err != nil {
				return SessionRemoveFolderResponse{}, mapSessionStoreError(err, sessionID)
			}

			folderPath, err := normalizePath(req.FolderPath)
			if err != nil {
				return SessionRemoveFolderResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
			}

			if err := store.RemoveFolder(ctx, sessionID, folderPath); err != nil {
				if errors.Is(err, globaldb.ErrNotFound) {
					return SessionRemoveFolderResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "folder not found in session", sessionID)
				}
				return SessionRemoveFolderResponse{}, mapSessionStoreError(err, sessionID)
			}

			return SessionRemoveFolderResponse{}, nil
		},
	}); err != nil {
		return fmt.Errorf("register session.remove_folder: %w", err)
	}

	return nil
}

func mapSessionStoreError(err error, sessionID string) error {
	if errors.Is(err, globaldb.ErrNotFound) {
		return rpc.NewHandlerError(rpc.SessionNotFound, "session not found", sessionID)
	}
	if errors.Is(err, globaldb.ErrSessionClosed) {
		return rpc.NewHandlerError(rpc.InvalidParams, "session is closed", sessionID)
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

func newSessionID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(buf)

	return fmt.Sprintf("%s-%s-%s-%s-%s", encoded[0:8], encoded[8:12], encoded[12:16], encoded[16:20], encoded[20:32]), nil
}
