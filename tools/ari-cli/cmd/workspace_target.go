package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
	"github.com/sourcegraph/jsonrpc2"
)

type resolvedWorkspaceTarget struct {
	WorkspaceID string
	Workspace   *daemon.WorkspaceGetResponse
}

func resolveWorkspaceIdentifier(ctx context.Context, socketPath, idOrName string) (string, error) {
	target, err := resolveWorkspaceTarget(ctx, socketPath, idOrName)
	if err != nil {
		return "", err
	}
	return target.WorkspaceID, nil
}

func resolveWorkspaceTarget(ctx context.Context, socketPath, idOrName string) (resolvedWorkspaceTarget, error) {
	idOrName = strings.TrimSpace(idOrName)
	if idOrName == "" {
		return resolvedWorkspaceTarget{}, userFacingError{message: "Workspace identifier is required"}
	}

	var directNameLookup *daemon.WorkspaceGetResponse

	if workspace, err := workspaceGetRPC(ctx, socketPath, idOrName); err == nil {
		workspaceID := strings.TrimSpace(workspace.WorkspaceID)
		if workspaceID == idOrName {
			resolved := workspace
			return resolvedWorkspaceTarget{WorkspaceID: workspace.WorkspaceID, Workspace: &resolved}, nil
		}
		resolved := workspace
		directNameLookup = &resolved
	} else if !isWorkspaceNotFoundError(err) {
		return resolvedWorkspaceTarget{}, mapWorkspaceRPCError(err)
	}

	list, err := workspaceListRPC(ctx, socketPath)
	if err != nil {
		if directNameLookup != nil {
			return resolvedWorkspaceTarget{WorkspaceID: directNameLookup.WorkspaceID, Workspace: directNameLookup}, nil
		}
		return resolvedWorkspaceTarget{}, mapWorkspaceRPCError(err)
	}

	exactIDMatches := make([]daemon.WorkspaceSummary, 0)
	nameMatches := make([]daemon.WorkspaceSummary, 0)
	prefixMatches := make([]string, 0)
	for _, workspace := range list.Workspaces {
		if workspace.WorkspaceID == idOrName {
			exactIDMatches = append(exactIDMatches, workspace)
		}
		if workspace.Name == idOrName {
			nameMatches = append(nameMatches, workspace)
		}
		if strings.HasPrefix(workspace.WorkspaceID, idOrName) {
			prefixMatches = append(prefixMatches, workspace.WorkspaceID)
		}
	}
	if len(exactIDMatches) == 1 {
		workspace, err := workspaceGetRPC(ctx, socketPath, exactIDMatches[0].WorkspaceID)
		if err != nil {
			return resolvedWorkspaceTarget{}, mapWorkspaceRPCError(err)
		}
		resolved := workspace
		return resolvedWorkspaceTarget{WorkspaceID: workspace.WorkspaceID, Workspace: &resolved}, nil
	}
	if len(exactIDMatches) > 1 {
		return resolvedWorkspaceTarget{}, userFacingError{message: "Workspace ID prefix is ambiguous"}
	}
	if len(nameMatches) == 1 {
		resolved := resolvedWorkspaceTarget{WorkspaceID: nameMatches[0].WorkspaceID}
		if directNameLookup != nil && strings.TrimSpace(directNameLookup.WorkspaceID) == nameMatches[0].WorkspaceID {
			resolved.Workspace = directNameLookup
		}
		return resolved, nil
	}
	if len(nameMatches) > 1 {
		workspaceID, err := resolveNameCollisionByCWD(ctx, socketPath, nameMatches)
		if err != nil {
			return resolvedWorkspaceTarget{}, err
		}
		return resolvedWorkspaceTarget{WorkspaceID: workspaceID}, nil
	}
	if len(prefixMatches) == 1 {
		return resolvedWorkspaceTarget{WorkspaceID: prefixMatches[0]}, nil
	}
	if len(prefixMatches) > 1 {
		return resolvedWorkspaceTarget{}, userFacingError{message: "Workspace ID prefix is ambiguous"}
	}

	if directNameLookup != nil {
		return resolvedWorkspaceTarget{WorkspaceID: directNameLookup.WorkspaceID, Workspace: directNameLookup}, nil
	}

	return resolvedWorkspaceTarget{}, userFacingError{message: "Workspace not found"}
}

func resolveNameCollisionByCWD(ctx context.Context, socketPath string, nameMatches []daemon.WorkspaceSummary) (string, error) {
	if len(nameMatches) < 2 {
		return "", fmt.Errorf("name collision requires at least two matches")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	candidates, err := loadLiveWorkspaceCandidates(ctx, socketPath, nameMatches)
	if err != nil {
		return "", err
	}

	if len(candidates) == 0 {
		return "", userFacingError{message: "Workspace not found"}
	}

	if len(candidates) == 1 {
		return candidates[0].WorkspaceID, nil
	}

	workspaceID, resolveErr := resolveWorkspaceByCWD(cwd, candidates)
	if resolveErr == nil {
		return workspaceID, nil
	}

	if isWorkspaceCWDNoMatch(resolveErr) {
		return "", userFacingError{message: "Workspace name is ambiguous; run `ari workspace set <id-or-name>` to choose one"}
	}

	return "", resolveErr
}

func mapWorkspaceRPCError(err error) error {
	if err == nil {
		return nil
	}
	if isDaemonUnavailable(err) {
		return userFacingError{message: notRunningMessage()}
	}
	if isPermissionDenied(err) {
		cfg, cfgErr := configuredDaemonConfig()
		if cfgErr != nil {
			return err
		}
		return socketPermissionError(cfg.Daemon.SocketPath)
	}
	if isTimeoutError(err) {
		return timeoutError()
	}

	var rpcErr *jsonrpc2.Error
	if errors.As(err, &rpcErr) {
		if rpcErr.Code == int64(rpc.SessionNotFound) {
			return userFacingError{message: "Workspace not found"}
		}
		if rpcErr.Code == int64(rpc.InvalidParams) {
			return userFacingError{message: rpcErr.Message}
		}
	}

	return err
}

func isWorkspaceNotFoundError(err error) bool {
	var rpcErr *jsonrpc2.Error
	if !errors.As(err, &rpcErr) {
		return false
	}
	return rpcErr.Code == int64(rpc.SessionNotFound)
}

func mapSessionRPCError(err error) error {
	return mapWorkspaceRPCError(err)
}

func isSessionNotFoundError(err error) bool {
	return isWorkspaceNotFoundError(err)
}
