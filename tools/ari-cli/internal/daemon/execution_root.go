package daemon

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

func validateWorkspaceExecutionRootPath(ctx context.Context, store *globaldb.Store, workspaceID, executionRootPath string) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("context is required")
	}
	if store == nil {
		return "", fmt.Errorf("globaldb store is required")
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return "", rpc.NewHandlerError(rpc.InvalidParams, "workspace_id is required", nil)
	}
	executionRootPath = strings.TrimSpace(executionRootPath)
	if executionRootPath == "" {
		return "", nil
	}
	if !filepath.IsAbs(executionRootPath) {
		return "", rpc.NewHandlerError(rpc.InvalidParams, "execution_root_path must be absolute", workspaceID)
	}

	normalizedRoot, err := normalizePath(executionRootPath)
	if err != nil {
		return "", rpc.NewHandlerError(rpc.InvalidParams, err.Error(), workspaceID)
	}
	folders, err := store.ListFolders(ctx, workspaceID)
	if err != nil {
		return "", mapWorkspaceStoreError(err, workspaceID)
	}
	for _, folder := range folders {
		if folder.FolderPath == normalizedRoot {
			return normalizedRoot, nil
		}
	}

	return "", rpc.NewHandlerError(rpc.InvalidParams, "execution_root_path is not a workspace folder", workspaceID)
}
