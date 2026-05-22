package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/config"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
	"github.com/sourcegraph/jsonrpc2"
	"github.com/spf13/cobra"
)

var (
	commandResolveWorkspaceIdentifier = resolveWorkspaceIdentifier
	commandResolveWorkspaceTarget     = resolveWorkspaceTarget
	commandResolveWorkflowContext     = func(ctx context.Context, socketPath, workspaceOverride string) (WorkflowContext, error) {
		workspaceRef := strings.TrimSpace(workspaceOverride)
		source := WorkflowContextSourceExplicit
		if workspaceRef == "" {
			var err error
			workspaceRef, err = workflowContextResolver.ActiveWorkspaceID()
			if err != nil {
				return WorkflowContext{}, err
			}
			source = WorkflowContextSourceActiveWorkspace
		}
		target, err := commandResolveWorkspaceTarget(ctx, socketPath, workspaceRef)
		if err != nil {
			return WorkflowContext{}, err
		}
		return WorkflowContext{WorkspaceID: target.WorkspaceID, Workspace: target.Workspace, Source: source}, nil
	}
	commandEnsureDaemonRunning  = ensureDaemonRunning
	commandEnsureWorkspaceScope = func(workspace *daemon.WorkspaceGetResponse, workspaceOverride string) error {
		return enforceActiveWorkspaceScope(workspace, workspaceOverride)
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
	oneOffCommandMaxDuration = 24 * time.Hour
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
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if strings.TrimSpace(workspaceRef) == "" {
				if _, err := workflowContextResolver.ActiveWorkspaceID(); err != nil {
					return err
				}
			}
			if err := commandEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			workflowCtx, err := commandResolveWorkflowContext(ctx, cfg.Daemon.SocketPath, workspaceRef)
			if err != nil {
				return err
			}
			if err := commandEnsureWorkspaceScope(workflowCtx.Workspace, workspaceRef); err != nil {
				return err
			}

			return runOneOffCommand(cmd, cfg, workflowCtx.WorkspaceID, args[0], args[1:])
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
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if strings.TrimSpace(workspaceRef) == "" {
				if _, err := workflowContextResolver.ActiveWorkspaceID(); err != nil {
					return err
				}
			}
			if err := commandEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			workflowCtx, err := commandResolveWorkflowContext(ctx, cfg.Daemon.SocketPath, workspaceRef)
			if err != nil {
				return err
			}
			if err := commandEnsureWorkspaceScope(workflowCtx.Workspace, workspaceRef); err != nil {
				return err
			}

			resp, err := commandListRPC(ctx, cfg.Daemon.SocketPath, workflowCtx.WorkspaceID)
			if err != nil {
				return mapCommandRPCError(err)
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
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if strings.TrimSpace(workspaceRef) == "" {
				if _, err := workflowContextResolver.ActiveWorkspaceID(); err != nil {
					return err
				}
			}
			if err := commandEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			workflowCtx, err := commandResolveWorkflowContext(ctx, cfg.Daemon.SocketPath, workspaceRef)
			if err != nil {
				return err
			}
			if err := commandEnsureWorkspaceScope(workflowCtx.Workspace, workspaceRef); err != nil {
				return err
			}

			resp, err := commandGetRPC(ctx, cfg.Daemon.SocketPath, workflowCtx.WorkspaceID, strings.TrimSpace(args[0]))
			if err != nil {
				return mapCommandRPCError(err)
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
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if strings.TrimSpace(workspaceRef) == "" {
				if _, err := workflowContextResolver.ActiveWorkspaceID(); err != nil {
					return err
				}
			}
			if err := commandEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			workflowCtx, err := commandResolveWorkflowContext(ctx, cfg.Daemon.SocketPath, workspaceRef)
			if err != nil {
				return err
			}
			if err := commandEnsureWorkspaceScope(workflowCtx.Workspace, workspaceRef); err != nil {
				return err
			}

			resp, err := commandOutputRPC(ctx, cfg.Daemon.SocketPath, workflowCtx.WorkspaceID, strings.TrimSpace(args[0]))
			if err != nil {
				return mapCommandRPCError(err)
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
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if strings.TrimSpace(workspaceRef) == "" {
				if _, err := workflowContextResolver.ActiveWorkspaceID(); err != nil {
					return err
				}
			}
			if err := commandEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			workflowCtx, err := commandResolveWorkflowContext(ctx, cfg.Daemon.SocketPath, workspaceRef)
			if err != nil {
				return err
			}
			if err := commandEnsureWorkspaceScope(workflowCtx.Workspace, workspaceRef); err != nil {
				return err
			}

			resp, err := commandStopRPC(ctx, cfg.Daemon.SocketPath, workflowCtx.WorkspaceID, strings.TrimSpace(args[0]))
			if err != nil {
				return mapCommandRPCError(err)
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Command stop: %s\n", resp.Status)
			return err
		},
	}
	cmd.Flags().StringVar(&workspaceRef, "workspace", "", "Target workspace id or name (defaults to active workspace)")
	return cmd
}

func runOneOffCommand(cmd *cobra.Command, cfg *config.Config, workspaceID, command string, args []string) error {
	if cmd == nil {
		return fmt.Errorf("exec run: command is required")
	}
	if cfg == nil {
		return fmt.Errorf("exec run: config is required")
	}
	if strings.TrimSpace(workspaceID) == "" {
		return userFacingError{message: "Workspace not found"}
	}
	if strings.TrimSpace(command) == "" {
		return userFacingError{message: "Command is required"}
	}

	parentCtx := cmd.Context()
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	runCtx, runCancel := context.WithTimeout(parentCtx, oneOffCommandMaxDuration)
	defer runCancel()

	startCtx, startCancel := context.WithTimeout(runCtx, 5*time.Second)
	defer startCancel()

	resp, err := commandRunRPC(startCtx, cfg.Daemon.SocketPath, daemon.CommandRunRequest{
		WorkspaceID: workspaceID,
		Command:     command,
		Args:        args,
	})
	if err != nil {
		return mapCommandRPCError(err)
	}

	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Command started: %s\n", resp.CommandID); err != nil {
		return err
	}

	terminalState, err := waitForCommandCompletion(runCtx, cfg.Daemon.SocketPath, workspaceID, resp.CommandID)
	if err != nil {
		return err
	}

	outputCtx, outputCancel := context.WithTimeout(runCtx, 5*time.Second)
	defer outputCancel()
	outputResp, err := commandOutputRPC(outputCtx, cfg.Daemon.SocketPath, workspaceID, resp.CommandID)
	if err != nil {
		return mapCommandRPCError(err)
	}
	if strings.TrimSpace(outputResp.Output) != "" {
		_, err = fmt.Fprint(cmd.OutOrStdout(), outputResp.Output)
	} else {
		_, err = fmt.Fprintln(cmd.OutOrStdout(), "Command produced no output.")
	}
	if err != nil {
		return err
	}
	return commandTerminalError(terminalState)
}

func waitForCommandCompletion(ctx context.Context, socketPath, workspaceID, commandID string) (daemon.CommandGetResponse, error) {
	if ctx == nil {
		return daemon.CommandGetResponse{}, fmt.Errorf("wait command completion: context is required")
	}
	if strings.TrimSpace(socketPath) == "" {
		return daemon.CommandGetResponse{}, fmt.Errorf("wait command completion: socket path is required")
	}
	if strings.TrimSpace(workspaceID) == "" {
		return daemon.CommandGetResponse{}, userFacingError{message: "Workspace not found"}
	}
	if strings.TrimSpace(commandID) == "" {
		return daemon.CommandGetResponse{}, userFacingError{message: "Command not found"}
	}

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		resp, err := commandGetRPC(ctx, socketPath, workspaceID, commandID)
		if err != nil {
			return daemon.CommandGetResponse{}, mapCommandRPCError(err)
		}
		if !strings.EqualFold(strings.TrimSpace(resp.Status), "running") {
			return resp, nil
		}

		select {
		case <-ctx.Done():
			return daemon.CommandGetResponse{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func commandTerminalError(state daemon.CommandGetResponse) error {
	status := strings.ToLower(strings.TrimSpace(state.Status))
	if status == "running" {
		return userFacingError{message: "Command is still running"}
	}
	if status == "lost" {
		return userFacingError{message: "Command ended unexpectedly"}
	}
	if state.ExitCode != nil && *state.ExitCode != 0 {
		return userFacingError{message: fmt.Sprintf("Command exited with code %d", *state.ExitCode)}
	}
	return nil
}

func mapCommandRPCError(err error) error {
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
		switch rpcErr.Code {
		case int64(rpc.SessionNotFound):
			return userFacingError{message: "Workspace not found"}
		case int64(rpc.CommandNotFound):
			return userFacingError{message: "Command not found"}
		case int64(rpc.InvalidParams):
			return userFacingError{message: rpcErr.Message}
		}
	}

	return err
}
