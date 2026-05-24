package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/config"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/spf13/cobra"
)

var (
	workspaceCommandEnsureScope = func(ctx context.Context, workspace *daemon.WorkspaceGetResponse, workspaceOverride string) error {
		return enforceActiveWorkspaceScope(ctx, workspace, workspaceOverride)
	}
	workspaceCommandCreateRPC = func(ctx context.Context, socketPath string, req daemon.WorkspaceCommandCreateRequest) (daemon.WorkspaceCommandCreateResponse, error) {
		return callDaemonRPC[daemon.WorkspaceCommandCreateResponse](ctx, socketPath, "workspace.command.create", req)
	}
	workspaceCommandListRPC = func(ctx context.Context, socketPath string, req daemon.WorkspaceCommandListRequest) (daemon.WorkspaceCommandListResponse, error) {
		return callDaemonRPC[daemon.WorkspaceCommandListResponse](ctx, socketPath, "workspace.command.list", req)
	}
	workspaceCommandGetRPC = func(ctx context.Context, socketPath string, req daemon.WorkspaceCommandGetRequest) (daemon.WorkspaceCommandGetResponse, error) {
		return callDaemonRPC[daemon.WorkspaceCommandGetResponse](ctx, socketPath, "workspace.command.get", req)
	}
	workspaceCommandRemoveRPC = func(ctx context.Context, socketPath string, req daemon.WorkspaceCommandRemoveRequest) (daemon.WorkspaceCommandRemoveResponse, error) {
		return callDaemonRPC[daemon.WorkspaceCommandRemoveResponse](ctx, socketPath, "workspace.command.remove", req)
	}
)

func NewCommandCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "command", Short: "Manage workspace command definitions", Hidden: true}
	cmd.AddCommand(newWorkspaceCommandCreateCmd())
	cmd.AddCommand(newWorkspaceCommandListCmd())
	cmd.AddCommand(newWorkspaceCommandShowCmd())
	cmd.AddCommand(newWorkspaceCommandRunCmd())
	cmd.AddCommand(newWorkspaceCommandRemoveCmd())
	return cmd
}

func newWorkspaceCommandCreateCmd() *cobra.Command {
	var workspaceRef string
	cmd := &cobra.Command{
		Use:   "create <name> <command> [args...]",
		Short: "Create a workspace command definition",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if err := workspaceEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			target, err := resolveWorkspaceCommandTarget(ctx, cfg, workspaceRef)
			if err != nil {
				return err
			}

			resp, err := workspaceCommandCreateRPC(ctx, cfg.Daemon.SocketPath, daemon.WorkspaceCommandCreateRequest{
				WorkspaceID: target.WorkspaceID,
				Name:        strings.TrimSpace(args[0]),
				Command:     strings.TrimSpace(args[1]),
				Args:        args[2:],
			})
			if err != nil {
				return mapCommandRPCError(err)
			}

			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Command created: %s (%s)\n", resp.CommandID, resp.Name); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Command: %s %s\n", resp.Command, strings.Join(resp.Args, " "))
			return err
		},
	}
	cmd.Flags().SetInterspersed(false)
	cmd.Flags().StringVar(&workspaceRef, "workspace", "", "Target workspace id or name (defaults to active workspace)")
	return cmd
}

func newWorkspaceCommandListCmd() *cobra.Command {
	var workspaceRef string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List workspace command definitions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if err := workspaceEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			target, err := resolveWorkspaceCommandTarget(ctx, cfg, workspaceRef)
			if err != nil {
				return err
			}

			resp, err := workspaceCommandListRPC(ctx, cfg.Daemon.SocketPath, daemon.WorkspaceCommandListRequest{WorkspaceID: target.WorkspaceID})
			if err != nil {
				return mapCommandRPCError(err)
			}

			if len(resp.Commands) == 0 {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), "No commands found")
				return err
			}

			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "COMMAND ID          NAME       COMMAND"); err != nil {
				return err
			}
			for _, item := range resp.Commands {
				commandLine := strings.TrimSpace(item.Command + " " + strings.Join(item.Args, " "))
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%-18s %-10s %s\n", item.CommandID, item.Name, commandLine); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&workspaceRef, "workspace", "", "Target workspace id or name (defaults to active workspace)")
	return cmd
}

func newWorkspaceCommandShowCmd() *cobra.Command {
	var workspaceRef string
	cmd := &cobra.Command{
		Use:   "show <id-or-name>",
		Short: "Show a workspace command definition",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if err := workspaceEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			target, err := resolveWorkspaceCommandTarget(ctx, cfg, workspaceRef)
			if err != nil {
				return err
			}

			resp, err := workspaceCommandGetRPC(ctx, cfg.Daemon.SocketPath, daemon.WorkspaceCommandGetRequest{
				WorkspaceID:     target.WorkspaceID,
				CommandIDOrName: strings.TrimSpace(args[0]),
			})
			if err != nil {
				return mapCommandRPCError(err)
			}

			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Command ID: %s\n", resp.CommandID); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Name: %s\n", resp.Name); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Command: %s\n", resp.Command); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Args: %s\n", strings.Join(resp.Args, " ")); err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&workspaceRef, "workspace", "", "Target workspace id or name (defaults to active workspace)")
	return cmd
}

func newWorkspaceCommandRemoveCmd() *cobra.Command {
	var workspaceRef string
	cmd := &cobra.Command{
		Use:   "remove <id-or-name>",
		Short: "Remove a workspace command definition",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if err := workspaceEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			target, err := resolveWorkspaceCommandTarget(ctx, cfg, workspaceRef)
			if err != nil {
				return err
			}

			resp, err := workspaceCommandRemoveRPC(ctx, cfg.Daemon.SocketPath, daemon.WorkspaceCommandRemoveRequest{
				WorkspaceID:     target.WorkspaceID,
				CommandIDOrName: strings.TrimSpace(args[0]),
			})
			if err != nil {
				return mapCommandRPCError(err)
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Command remove: %s\n", resp.Status)
			return err
		},
	}
	cmd.Flags().StringVar(&workspaceRef, "workspace", "", "Target workspace id or name (defaults to active workspace)")
	return cmd
}

func newWorkspaceCommandRunCmd() *cobra.Command {
	var workspaceRef string
	cmd := &cobra.Command{
		Use:   "run <id-or-name>",
		Short: "Run a workspace command definition",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if err := workspaceEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			target, err := resolveWorkspaceCommandTarget(ctx, cfg, workspaceRef)
			if err != nil {
				return err
			}

			definition, err := workspaceCommandGetRPC(ctx, cfg.Daemon.SocketPath, daemon.WorkspaceCommandGetRequest{
				WorkspaceID:     target.WorkspaceID,
				CommandIDOrName: strings.TrimSpace(args[0]),
			})
			if err != nil {
				return mapCommandRPCError(err)
			}

			workflow := newExecWorkflow(cmd.Context(), cfg, target)
			return workflow.runOneOffCommand(cmd, definition.Command, definition.Args)
		},
	}
	cmd.Flags().StringVar(&workspaceRef, "workspace", "", "Target workspace id or name (defaults to active workspace)")
	return cmd
}

func resolveWorkspaceCommandTarget(ctx context.Context, cfg *config.Config, workspaceOverride string) (WorkflowContext, error) {
	if ctx == nil {
		return WorkflowContext{}, fmt.Errorf("workspace command: context is required")
	}
	if cfg == nil {
		return WorkflowContext{}, fmt.Errorf("workspace command: config is required")
	}

	workflowCtx, err := workflowContextResolver.Resolve(ctx, cfg.Daemon.SocketPath, workspaceOverride)
	if err != nil {
		return WorkflowContext{}, err
	}
	if err := workspaceCommandEnsureScope(ctx, workflowCtx.Workspace, workspaceOverride); err != nil {
		return WorkflowContext{}, err
	}

	return workflowCtx, nil
}
