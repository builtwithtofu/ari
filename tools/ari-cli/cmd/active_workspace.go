package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/config"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/spf13/cobra"
)

var workflowContextResolver = &WorkflowContextResolver{store: activeWorkspaceConfigStore{}, resolveTarget: resolveWorkspaceTarget}

type activeWorkspacePersistence interface {
	Read() (string, error)
	Write(string) error
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
		var err error
		workspaceRef, err = resolver.store.Read()
		if err != nil {
			return WorkflowContext{}, err
		}
		workspaceRef = strings.TrimSpace(workspaceRef)
		if workspaceRef == "" {
			return WorkflowContext{}, userFacingError{message: "No active workspace is set"}
		}
		source = WorkflowContextSourceActiveWorkspace
		if strings.TrimSpace(os.Getenv("ARI_ACTIVE_WORKSPACE")) != "" {
			source = WorkflowContextSourceEnvironmentActive
		}
	}

	target, err := resolver.resolveTarget(ctx, socketPath, workspaceRef)
	if err != nil {
		return WorkflowContext{}, err
	}
	return WorkflowContext{WorkspaceID: target.WorkspaceID, Workspace: target.Workspace, Source: source}, nil
}

func (resolver *WorkflowContextResolver) ActiveWorkspaceID() (string, error) {
	workspaceID, err := resolver.store.Read()
	if err != nil {
		return "", err
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return "", userFacingError{message: "No active workspace is set"}
	}
	return workspaceID, nil
}

func (resolver *WorkflowContextResolver) PersistDefault(workspaceID string) error {
	if strings.TrimSpace(workspaceID) == "" {
		return userFacingError{message: "Workspace identifier is required"}
	}
	return resolver.store.Write(workspaceID)
}

func (resolver *WorkflowContextResolver) ClearDefault() error {
	return resolver.store.Write("")
}

type activeWorkspaceStoreFunc struct {
	read  func() (string, error)
	write func(string) error
}

func (store activeWorkspaceStoreFunc) Read() (string, error) {
	return store.read()
}

func (store activeWorkspaceStoreFunc) Write(workspaceID string) error {
	if store.write == nil {
		return config.WriteActiveWorkspace(workspaceID)
	}
	return store.write(workspaceID)
}

type activeWorkspaceConfigStore struct{}

func (activeWorkspaceConfigStore) Read() (string, error) {
	return config.ReadActiveWorkspace()
}

func (activeWorkspaceConfigStore) Write(workspaceID string) error {
	return config.WriteActiveWorkspace(workspaceID)
}

func readActiveWorkspaceID() (string, error) {
	return workflowContextResolver.ActiveWorkspaceID()
}

func writeAndReportActiveWorkspace(cmd *cobra.Command, workspaceID string) error {
	if cmd == nil {
		return fmt.Errorf("active workspace write: command is required")
	}
	if strings.TrimSpace(workspaceID) == "" {
		return userFacingError{message: "Workspace identifier is required"}
	}
	if err := workflowContextResolver.PersistDefault(workspaceID); err != nil {
		return err
	}
	if strings.TrimSpace(os.Getenv("ARI_ACTIVE_WORKSPACE")) != "" {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "Persisted active workspace set: %s; ARI_ACTIVE_WORKSPACE still overrides it in this shell\n", workspaceID)
		return err
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "Active workspace set: %s\n", workspaceID)
	return err
}

func clearAndReportActiveWorkspace(cmd *cobra.Command) error {
	if cmd == nil {
		return fmt.Errorf("active workspace clear: command is required")
	}
	if err := workflowContextResolver.ClearDefault(); err != nil {
		return err
	}
	if strings.TrimSpace(os.Getenv("ARI_ACTIVE_WORKSPACE")) != "" {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "Cleared persisted active workspace; ARI_ACTIVE_WORKSPACE still overrides it in this shell")
		return err
	}
	_, err := fmt.Fprintln(cmd.OutOrStdout(), "Cleared active workspace")
	return err
}
