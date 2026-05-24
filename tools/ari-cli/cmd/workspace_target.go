package cmd

import (
	"context"
	"errors"
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

	cwd, err := os.Getwd()
	if err != nil {
		return resolvedWorkspaceTarget{}, err
	}

	response, err := workspaceResolveRPC(ctx, socketPath, daemon.WorkspaceResolveRequest{Identifier: idOrName, CWD: cwd})
	if err != nil {
		return resolvedWorkspaceTarget{}, mapWorkspaceRPCError(err)
	}
	resolved := response.Workspace
	return resolvedWorkspaceTarget{WorkspaceID: resolved.WorkspaceID, Workspace: &resolved}, nil
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
