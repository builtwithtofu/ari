package daemon

import (
	"context"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

func requireWorkspaceCanStartRuntime(ctx context.Context, store *globaldb.Store, workspaceID string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return rpc.NewHandlerError(rpc.InvalidParams, "workspace_id is required", nil)
	}
	workspace, err := store.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return mapWorkspaceStoreError(err, workspaceID)
	}
	if workspace.Status == "suspended" {
		return rpc.NewHandlerError(rpc.InvalidParams, "workspace is suspended", map[string]any{"reason": "workspace_suspended", "workspace_id": workspaceID, "start_invoked": false})
	}
	return nil
}
