package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
)

func enforceActiveWorkspaceScope(session *daemon.WorkspaceGetResponse, sessionOverride string) error {
	if strings.TrimSpace(sessionOverride) != "" {
		return nil
	}
	if strings.TrimSpace(os.Getenv("ARI_ACTIVE_WORKSPACE")) != "" {
		return nil
	}
	if session == nil {
		return userFacingError{message: "Active workspace details are unavailable; use --workspace <id-or-name> to override"}
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	matches, err := workspaceMatchesSession(cwd, *session)
	if err != nil {
		return err
	}
	if !matches {
		return userFacingError{message: "Active workspace belongs to a different workspace; use --workspace <id-or-name> to override"}
	}
	return nil
}

func workspaceMatchesSession(cwd string, session daemon.WorkspaceGetResponse) (bool, error) {
	normalizedCWD, err := normalizeWorkspacePath(cwd)
	if err != nil {
		return false, err
	}
	workspaceRoots := make([]string, 0, len(session.Folders)+1)
	if strings.TrimSpace(session.OriginRoot) != "" {
		workspaceRoots = append(workspaceRoots, session.OriginRoot)
	}
	for _, folder := range session.Folders {
		if strings.TrimSpace(folder.Path) == "" {
			continue
		}
		workspaceRoots = append(workspaceRoots, folder.Path)
	}

	for _, root := range workspaceRoots {
		normalizedRoot, err := normalizeWorkspacePath(root)
		if err != nil {
			return false, err
		}
		within, err := pathWithinRoot(normalizedCWD, normalizedRoot)
		if err != nil {
			return false, err
		}
		if within {
			return true, nil
		}
	}

	return false, nil
}

func normalizeWorkspacePath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", fmt.Errorf("workspace path is required")
	}
	absPath, err := filepath.Abs(trimmed)
	if err != nil {
		return "", err
	}
	resolvedPath, err := filepath.EvalSymlinks(absPath)
	if err == nil {
		return filepath.Clean(resolvedPath), nil
	}
	return filepath.Clean(absPath), nil
}

func pathWithinRoot(path, root string) (bool, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false, err
	}
	if rel == "." {
		return true, nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false, nil
	}
	return true, nil
}
