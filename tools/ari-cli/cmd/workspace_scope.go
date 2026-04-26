package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
		return userFacingError{message: "Active workspace details are unavailable; use --workspace <id-or-name> to target a workspace explicitly"}
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
		return userFacingError{message: "Active workspace belongs to a different workspace; use --workspace <id-or-name> to target a workspace explicitly"}
	}
	return nil
}

func workspaceMatchesSession(cwd string, session daemon.WorkspaceGetResponse) (bool, error) {
	_, matched, err := workspaceMatchScore(cwd, session)
	if err != nil {
		return false, err
	}

	return matched, nil
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

func resolveWorkspaceFromCWD(ctx context.Context, socketPath, cwd string) (daemon.WorkspaceGetResponse, error) {
	if ctx == nil {
		return daemon.WorkspaceGetResponse{}, fmt.Errorf("context is required")
	}
	if strings.TrimSpace(socketPath) == "" {
		return daemon.WorkspaceGetResponse{}, fmt.Errorf("socket path is required")
	}

	listResponse, err := workspaceListRPC(ctx, socketPath)
	if err != nil {
		return daemon.WorkspaceGetResponse{}, mapSessionRPCError(err)
	}

	candidates, err := loadLiveWorkspaceCandidates(ctx, socketPath, listResponse.Workspaces)
	if err != nil {
		return daemon.WorkspaceGetResponse{}, err
	}

	workspaceID, err := resolveWorkspaceByCWD(cwd, candidates)
	if err != nil {
		return daemon.WorkspaceGetResponse{}, err
	}

	for _, candidate := range candidates {
		if candidate.WorkspaceID == workspaceID {
			return candidate, nil
		}
	}

	return daemon.WorkspaceGetResponse{}, userFacingError{message: "Workspace not found"}
}

func loadLiveWorkspaceCandidates(ctx context.Context, socketPath string, summaries []daemon.WorkspaceSummary) ([]daemon.WorkspaceGetResponse, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}
	if strings.TrimSpace(socketPath) == "" {
		return nil, fmt.Errorf("socket path is required")
	}

	candidates := make([]daemon.WorkspaceGetResponse, 0, len(summaries))
	for _, summary := range summaries {
		workspaceID := strings.TrimSpace(summary.WorkspaceID)
		if workspaceID == "" {
			continue
		}

		workspace, getErr := workspaceGetRPC(ctx, socketPath, workspaceID)
		if getErr != nil {
			if isSessionNotFoundError(getErr) {
				continue
			}
			return nil, mapSessionRPCError(getErr)
		}
		if strings.EqualFold(strings.TrimSpace(workspace.Status), "closed") {
			continue
		}
		candidates = append(candidates, workspace)
	}

	return candidates, nil
}

func resolveWorkspaceByCWD(cwd string, workspaces []daemon.WorkspaceGetResponse) (string, error) {
	if strings.TrimSpace(cwd) == "" {
		return "", fmt.Errorf("cwd is required")
	}

	if len(workspaces) == 0 {
		return "", workspaceCWDResolutionError{reason: workspaceCWDReasonNoMatch}
	}

	type workspaceMatch struct {
		workspaceID string
		score       int
	}

	matches := make([]workspaceMatch, 0, len(workspaces))
	for _, workspace := range workspaces {
		workspaceID := strings.TrimSpace(workspace.WorkspaceID)
		if workspaceID == "" {
			continue
		}

		score, matched, matchErr := workspaceMatchScore(cwd, workspace)
		if matchErr != nil {
			return "", matchErr
		}
		if matched {
			matches = append(matches, workspaceMatch{workspaceID: workspaceID, score: score})
		}
	}

	if len(matches) == 0 {
		return "", workspaceCWDResolutionError{reason: workspaceCWDReasonNoMatch}
	}

	sort.Slice(matches, func(i int, j int) bool {
		if matches[i].score == matches[j].score {
			return matches[i].workspaceID < matches[j].workspaceID
		}
		return matches[i].score > matches[j].score
	})

	if len(matches) > 1 && matches[0].score == matches[1].score {
		return "", workspaceCWDResolutionError{reason: workspaceCWDReasonAmbiguous}
	}

	return matches[0].workspaceID, nil
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
		return "current directory matches multiple workspaces; run `ari workspace set <id-or-name>` to choose one"
	default:
		return "workspace resolution from current directory failed"
	}
}

func isWorkspaceCWDNoMatch(err error) bool {
	if err == nil {
		return false
	}

	target := workspaceCWDResolutionError{}
	if !errors.As(err, &target) {
		return false
	}
	return target.reason == workspaceCWDReasonNoMatch
}

func workspaceMatchScore(cwd string, workspace daemon.WorkspaceGetResponse) (int, bool, error) {
	if strings.TrimSpace(cwd) == "" {
		return 0, false, fmt.Errorf("cwd is required")
	}

	normalizedCWD, err := normalizeWorkspacePath(cwd)
	if err != nil {
		return 0, false, err
	}

	workspaceRoots := workspaceRootCandidates(workspace)

	bestScore := 0
	matched := false
	for _, root := range workspaceRoots {
		normalizedRoot, err := normalizeWorkspacePath(root)
		if err != nil {
			return 0, false, err
		}

		within, err := pathWithinRoot(normalizedCWD, normalizedRoot)
		if err != nil {
			return 0, false, err
		}
		if !within {
			continue
		}

		score := workspacePathSegmentCount(normalizedRoot)
		if !matched || score > bestScore {
			bestScore = score
			matched = true
		}
	}

	return bestScore, matched, nil
}

func workspacePathSegmentCount(path string) int {
	cleanPath := filepath.Clean(path)
	if cleanPath == "." || cleanPath == string(os.PathSeparator) {
		return 0
	}
	trimmed := strings.Trim(cleanPath, string(os.PathSeparator))
	if trimmed == "" {
		return 0
	}
	return len(strings.Split(trimmed, string(os.PathSeparator)))
}

func workspaceRootCandidates(workspace daemon.WorkspaceGetResponse) []string {
	workspaceRoots := make([]string, 0, len(workspace.Folders)+1)
	if strings.TrimSpace(workspace.OriginRoot) != "" {
		workspaceRoots = append(workspaceRoots, workspace.OriginRoot)
	}
	for _, folder := range workspace.Folders {
		if strings.TrimSpace(folder.Path) == "" {
			continue
		}
		workspaceRoots = append(workspaceRoots, folder.Path)
	}

	return workspaceRoots
}
