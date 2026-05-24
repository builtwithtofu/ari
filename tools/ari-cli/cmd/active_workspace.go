package cmd

import (
	"context"
	"os"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
)

var workflowContextResolver = &WorkflowContextResolver{store: activeWorkspaceConfigStore{}, resolveTarget: resolveWorkspaceTarget}

type activeWorkspacePersistence interface {
	Read() (string, error)
}

type WorkflowContextSource string

const (
	WorkflowContextSourceExplicit          WorkflowContextSource = "explicit"
	WorkflowContextSourceActiveWorkspace   WorkflowContextSource = "active_workspace"
	WorkflowContextSourceEnvironmentActive WorkflowContextSource = "environment_active_workspace"
)

type WorkflowContext struct {
	WorkspaceID string
	Workspace   *daemon.WorkspaceGetResponse
	Source      WorkflowContextSource
}

type WorkflowContextResolver struct {
	store         activeWorkspacePersistence
	resolveTarget func(context.Context, string, string) (resolvedWorkspaceTarget, error)
}

func (resolver *WorkflowContextResolver) Resolve(ctx context.Context, socketPath, workspaceOverride string) (WorkflowContext, error) {
	workspaceRef := strings.TrimSpace(workspaceOverride)
	source := WorkflowContextSourceExplicit
	if workspaceRef == "" {
		workspaceRef = strings.TrimSpace(os.Getenv("ARI_ACTIVE_WORKSPACE"))
		if workspaceRef != "" {
			source = WorkflowContextSourceEnvironmentActive
		} else {
			if resolver.store != nil {
				stored, err := resolver.store.Read()
				if err != nil {
					return WorkflowContext{}, err
				}
				workspaceRef = strings.TrimSpace(stored)
			}
			if workspaceRef != "" {
				source = WorkflowContextSourceActiveWorkspace
			} else {
				current, err := workspaceContextGetRPC(ctx, socketPath)
				if err != nil {
					return WorkflowContext{}, err
				}
				workspaceRef = strings.TrimSpace(current.Current.WorkspaceID)
				if workspaceRef == "" {
					return WorkflowContext{}, userFacingError{message: "No active workspace is set"}
				}
				source = WorkflowContextSourceActiveWorkspace
			}
		}
	}

	target, err := resolver.resolveTarget(ctx, socketPath, workspaceRef)
	if err != nil {
		return WorkflowContext{}, err
	}
	return WorkflowContext{WorkspaceID: target.WorkspaceID, Workspace: target.Workspace, Source: source}, nil
}

type activeWorkspaceStoreFunc struct {
	read func() (string, error)
}

func (store activeWorkspaceStoreFunc) Read() (string, error) {
	return store.read()
}

type activeWorkspaceConfigStore struct{}

func (activeWorkspaceConfigStore) Read() (string, error) { return "", nil }
