package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/spf13/cobra"
)

var (
	commandResolveWorkspaceTarget = resolveWorkspaceTarget
	commandResolveWorkflowContext = func(ctx context.Context, socketPath, workspaceOverride string) (WorkflowContext, error) {
		resolver := &WorkflowContextResolver{store: workflowContextResolver.store, resolveTarget: commandResolveWorkspaceTarget}
		return resolver.Resolve(ctx, socketPath, workspaceOverride)
	}
	commandEnsureDaemonRunning  = ensureDaemonRunning
	commandEnsureWorkspaceScope = func(ctx context.Context, workspace *daemon.WorkspaceGetResponse, workspaceOverride string) error {
		return enforceActiveWorkspaceScope(ctx, workspace, workspaceOverride)
	}
	commandRunRPC = func(ctx context.Context, socketPath string, req daemon.CommandRunRequest) (daemon.CommandRunResponse, error) {
		return callDaemonRPC[daemon.CommandRunResponse](ctx, socketPath, "command.run", req)
	}
	commandListRPC = func(ctx context.Context, socketPath, workspaceID string) (daemon.CommandListResponse, error) {
		return callDaemonRPC[daemon.CommandListResponse](ctx, socketPath, "command.list", daemon.CommandListRequest{WorkspaceID: workspaceID})
	}
	commandGetRPC = func(ctx context.Context, socketPath, workspaceID, commandID string) (daemon.CommandGetResponse, error) {
		return callDaemonRPC[daemon.CommandGetResponse](ctx, socketPath, "command.get", daemon.CommandGetRequest{WorkspaceID: workspaceID, CommandID: commandID})
	}
	commandOutputRPC = func(ctx context.Context, socketPath, workspaceID, commandID string) (daemon.CommandOutputResponse, error) {
		return callDaemonRPC[daemon.CommandOutputResponse](ctx, socketPath, "command.output", daemon.CommandOutputRequest{WorkspaceID: workspaceID, CommandID: commandID})
	}
	commandStopRPC = func(ctx context.Context, socketPath, workspaceID, commandID string) (daemon.CommandStopResponse, error) {
		return callDaemonRPC[daemon.CommandStopResponse](ctx, socketPath, "command.stop", daemon.CommandStopRequest{WorkspaceID: workspaceID, CommandID: commandID})
	}
)

func NewExecCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "exec", Short: "Run and manage workspace command executions", Hidden: true}
	cmd.AddCommand(newCommandRunCmd())
	cmd.AddCommand(newCommandListCmd())
	cmd.AddCommand(newCommandShowCmd())
	cmd.AddCommand(newCommandOutputCmd())
	cmd.AddCommand(newCommandStopCmd())
	return cmd
}

func newCommandRunCmd() *cobra.Command {
	var workspaceRef string
	cmd := &cobra.Command{
		Use:   "run [--workspace <id-or-name>] -- <command> [args...]",
		Short: "Run command in workspace",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) < 1 {
				return userFacingError{message: "Usage: ari exec run [--workspace <id-or-name>] -- <command> [args...]"}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			workflow, err := startExecWorkflow(cmd, workspaceRef)
			if err != nil {
				return err
			}
			defer workflow.cancel()

			return workflow.runOneOffCommand(cmd, args[0], args[1:])
		},
	}
	cmd.Flags().StringVar(&workspaceRef, "workspace", "", "Target workspace id or name (defaults to active workspace)")
	return cmd
}

func newCommandListCmd() *cobra.Command {
	var workspaceRef string
	cmd := &cobra.Command{
		Use:   "list [--workspace <id-or-name>]",
		Short: "List commands for a workspace",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			workflow, err := startExecWorkflow(cmd, workspaceRef)
			if err != nil {
				return err
			}
			defer workflow.cancel()

			resp, err := workflow.listCommands()
			if err != nil {
				return err
			}

			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "ID       STATUS     STARTED                COMMAND"); err != nil {
				return err
			}
			for _, item := range resp.Commands {
				shortID := item.CommandID
				if len(shortID) > 8 {
					shortID = shortID[:8]
				}
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%-8s %-10s %-22s %s\n", shortID, item.Status, item.StartedAt, item.Command); err != nil {
					return err
				}
			}

			return nil
		},
	}
	cmd.Flags().StringVar(&workspaceRef, "workspace", "", "Target workspace id or name (defaults to active workspace)")
	return cmd
}

func newCommandShowCmd() *cobra.Command {
	var workspaceRef string
	cmd := &cobra.Command{
		Use:   "show <command-id> [--workspace <id-or-name>]",
		Short: "Show command details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			workflow, err := startExecWorkflow(cmd, workspaceRef)
			if err != nil {
				return err
			}
			defer workflow.cancel()

			resp, err := workflow.getCommand(strings.TrimSpace(args[0]))
			if err != nil {
				return err
			}

			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Command: %s (%s)\n", resp.CommandID, resp.Command); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Status: %s\n", resp.Status); err != nil {
				return err
			}
			if resp.ExitCode != nil {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Exit Code: %d\n", *resp.ExitCode); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Started: %s\n", resp.StartedAt); err != nil {
				return err
			}
			if resp.FinishedAt != "" {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Finished: %s\n", resp.FinishedAt); err != nil {
					return err
				}
			}

			return nil
		},
	}
	cmd.Flags().StringVar(&workspaceRef, "workspace", "", "Target workspace id or name (defaults to active workspace)")
	return cmd
}

func newCommandOutputCmd() *cobra.Command {
	var workspaceRef string
	cmd := &cobra.Command{
		Use:   "output <command-id> [--workspace <id-or-name>]",
		Short: "Show command output snapshot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			workflow, err := startExecWorkflow(cmd, workspaceRef)
			if err != nil {
				return err
			}
			defer workflow.cancel()

			resp, err := workflow.commandOutput(strings.TrimSpace(args[0]))
			if err != nil {
				return err
			}

			_, err = fmt.Fprint(cmd.OutOrStdout(), resp.Output)
			return err
		},
	}
	cmd.Flags().StringVar(&workspaceRef, "workspace", "", "Target workspace id or name (defaults to active workspace)")
	return cmd
}

func newCommandStopCmd() *cobra.Command {
	var workspaceRef string
	cmd := &cobra.Command{
		Use:   "stop <command-id> [--workspace <id-or-name>]",
		Short: "Stop a running command",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			workflow, err := startExecWorkflow(cmd, workspaceRef)
			if err != nil {
				return err
			}
			defer workflow.cancel()

			resp, err := workflow.stopCommand(strings.TrimSpace(args[0]))
			if err != nil {
				return err
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Command stop: %s\n", resp.Status)
			return err
		},
	}
	cmd.Flags().StringVar(&workspaceRef, "workspace", "", "Target workspace id or name (defaults to active workspace)")
	return cmd
}
