package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
)

func enforceActiveWorkspaceScope(workspace *daemon.WorkspaceGetResponse, workspaceOverride string) error {
	if strings.TrimSpace(workspaceOverride) != "" {
		return nil
	}
	if strings.TrimSpace(os.Getenv("ARI_ACTIVE_WORKSPACE")) != "" {
		return nil
	}
	if workspace == nil {
		return userFacingError{message: "Active workspace details are unavailable; use --workspace <id-or-name> to target a workspace explicitly"}
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg, err := configuredDaemonConfig()
	if err != nil {
		return err
	}
	matches, err := workspaceMatchesWorkspace(context.Background(), cfg.Daemon.SocketPath, cwd, *workspace)
	if err != nil {
		return err
	}
	if !matches {
		return userFacingError{message: "Active workspace belongs to a different workspace; use --workspace <id-or-name> to target a workspace explicitly"}
	}
	return nil
}

func workspaceMatchesWorkspace(ctx context.Context, socketPath, cwd string, workspace daemon.WorkspaceGetResponse) (bool, error) {
	resolved, err := resolveWorkspaceFromCWD(ctx, socketPath, cwd)
	if err != nil {
		if isWorkspaceCWDNoMatch(err) {
			return false, nil
		}
		if fallback, fallbackErr := workspaceContainsPath(cwd, workspace); fallbackErr == nil {
			return fallback, nil
		}
		return false, err
	}
	return strings.TrimSpace(resolved.WorkspaceID) == strings.TrimSpace(workspace.WorkspaceID), nil
}

func workspaceContainsPath(cwd string, workspace daemon.WorkspaceGetResponse) (bool, error) {
	path, err := filepath.Abs(strings.TrimSpace(cwd))
	if err != nil {
		return false, err
	}
	roots := make([]string, 0, len(workspace.Folders)+1)
	if strings.TrimSpace(workspace.OriginRoot) != "" {
		roots = append(roots, workspace.OriginRoot)
	}
	for _, folder := range workspace.Folders {
		if strings.TrimSpace(folder.Path) != "" {
			roots = append(roots, folder.Path)
		}
	}
	for _, root := range roots {
		absRoot, err := filepath.Abs(root)
		if err != nil {
			return false, err
		}
		if path == absRoot {
			return true, nil
		}
		rel, err := filepath.Rel(absRoot, path)
		if err != nil {
			return false, err
		}
		if rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return true, nil
		}
	}
	return false, nil
}

func resolveWorkspaceFromCWD(ctx context.Context, socketPath, cwd string) (daemon.WorkspaceGetResponse, error) {
	if ctx == nil {
		return daemon.WorkspaceGetResponse{}, fmt.Errorf("context is required")
	}
	response, err := workspaceResolveRPC(ctx, socketPath, daemon.WorkspaceResolveRequest{CWD: cwd})
	if err != nil {
		return daemon.WorkspaceGetResponse{}, mapWorkspaceRPCError(err)
	}
	return response.Workspace, nil
}

type workspaceCWDReason string

const (
	workspaceCWDReasonNoMatch   workspaceCWDReason = "no_match"
	workspaceCWDReasonAmbiguous workspaceCWDReason = "ambiguous"
)

type workspaceCWDResolutionError struct {
	reason workspaceCWDReason
}

func (e workspaceCWDResolutionError) Error() string {
	switch e.reason {
	case workspaceCWDReasonNoMatch:
		return "No workspace matches current directory"
	case workspaceCWDReasonAmbiguous:
		return "current directory matches multiple workspaces; run `ari workspace use <id-or-name>` to choose one"
	default:
		return "workspace resolution from current directory failed"
	}
}

func isWorkspaceCWDNoMatch(err error) bool {
	if err == nil {
		return false
	}

	target := workspaceCWDResolutionError{}
	if errors.As(err, &target) {
		return target.reason == workspaceCWDReasonNoMatch
	}
	return err.Error() == "No workspace matches current directory"
}
