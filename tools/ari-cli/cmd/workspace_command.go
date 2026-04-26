package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/client"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/config"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/spf13/cobra"
)

var (
	workspaceCommandReadActiveSession = config.ReadActiveWorkspace
	workspaceCommandEnsureScope       = func(session *daemon.WorkspaceGetResponse, workspaceOverride string) error {
		return enforceActiveWorkspaceScope(session, workspaceOverride)
	}
	workspaceCommandCreateRPC = func(ctx context.Context, socketPath string, req daemon.WorkspaceCommandCreateRequest) (daemon.WorkspaceCommandCreateResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.WorkspaceCommandCreateResponse
		if err := rpcClient.Call(ctx, "workspace.command.create", req, &response); err != nil {
			return daemon.WorkspaceCommandCreateResponse{}, err
		}
		return response, nil
	}
	workspaceCommandListRPC = func(ctx context.Context, socketPath string, req daemon.WorkspaceCommandListRequest) (daemon.WorkspaceCommandListResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.WorkspaceCommandListResponse
		if err := rpcClient.Call(ctx, "workspace.command.list", req, &response); err != nil {
			return daemon.WorkspaceCommandListResponse{}, err
		}
		return response, nil
	}
	workspaceCommandGetRPC = func(ctx context.Context, socketPath string, req daemon.WorkspaceCommandGetRequest) (daemon.WorkspaceCommandGetResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.WorkspaceCommandGetResponse
		if err := rpcClient.Call(ctx, "workspace.command.get", req, &response); err != nil {
			return daemon.WorkspaceCommandGetResponse{}, err
		}
		return response, nil
	}
	workspaceCommandRemoveRPC = func(ctx context.Context, socketPath string, req daemon.WorkspaceCommandRemoveRequest) (daemon.WorkspaceCommandRemoveResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.WorkspaceCommandRemoveResponse
		if err := rpcClient.Call(ctx, "workspace.command.remove", req, &response); err != nil {
			return daemon.WorkspaceCommandRemoveResponse{}, err
		}
		return response, nil
	}
)

func NewCommandCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "command", Short: "Manage workspace command definitions"}
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
	var agentSelector string
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

			return runOneOffCommandAndForwardOutput(cmd, cfg, target.WorkspaceID, definition.Command, definition.Args, agentSelector)
		},
	}
	cmd.Flags().StringVar(&workspaceRef, "workspace", "", "Target workspace id or name (defaults to active workspace)")
	cmd.Flags().StringVar(&agentSelector, "agent", "0", "Target agent id/name/index for output forwarding (defaults to 0)")
	return cmd
}

func resolveWorkspaceCommandTarget(ctx context.Context, cfg *config.Config, workspaceOverride string) (resolvedSessionTarget, error) {
	if ctx == nil {
		return resolvedSessionTarget{}, fmt.Errorf("workspace command: context is required")
	}
	if cfg == nil {
		return resolvedSessionTarget{}, fmt.Errorf("workspace command: config is required")
	}

	workspaceRef, err := resolveWorkspaceReference(workspaceOverride, workspaceCommandReadActiveSession)
	if err != nil {
		return resolvedSessionTarget{}, err
	}

	target, err := resolveSessionTarget(ctx, cfg.Daemon.SocketPath, workspaceRef)
	if err != nil {
		return resolvedSessionTarget{}, err
	}
	if err := workspaceCommandEnsureScope(target.Session, workspaceOverride); err != nil {
		return resolvedSessionTarget{}, err
	}

	return target, nil
}
