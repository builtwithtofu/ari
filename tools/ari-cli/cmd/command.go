package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/client"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/config"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
	"github.com/sourcegraph/jsonrpc2"
	"github.com/spf13/cobra"
)

var (
	commandResolveSessionIdentifier = resolveSessionIdentifier
	commandReadActiveSession        = config.ReadActiveSession
	commandEnsureDaemonRunning      = ensureDaemonRunning
	commandEnsureWorkspaceScope     = enforceActiveWorkspaceScope
	commandRunRPC                   = func(ctx context.Context, socketPath string, req daemon.CommandRunRequest) (daemon.CommandRunResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.CommandRunResponse
		if err := rpcClient.Call(ctx, "command.run", req, &response); err != nil {
			return daemon.CommandRunResponse{}, err
		}
		return response, nil
	}
	commandListRPC = func(ctx context.Context, socketPath, sessionID string) (daemon.CommandListResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.CommandListResponse
		if err := rpcClient.Call(ctx, "command.list", daemon.CommandListRequest{SessionID: sessionID}, &response); err != nil {
			return daemon.CommandListResponse{}, err
		}
		return response, nil
	}
	commandGetRPC = func(ctx context.Context, socketPath, sessionID, commandID string) (daemon.CommandGetResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.CommandGetResponse
		if err := rpcClient.Call(ctx, "command.get", daemon.CommandGetRequest{SessionID: sessionID, CommandID: commandID}, &response); err != nil {
			return daemon.CommandGetResponse{}, err
		}
		return response, nil
	}
	commandOutputRPC = func(ctx context.Context, socketPath, sessionID, commandID string) (daemon.CommandOutputResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.CommandOutputResponse
		if err := rpcClient.Call(ctx, "command.output", daemon.CommandOutputRequest{SessionID: sessionID, CommandID: commandID}, &response); err != nil {
			return daemon.CommandOutputResponse{}, err
		}
		return response, nil
	}
	commandStopRPC = func(ctx context.Context, socketPath, sessionID, commandID string) (daemon.CommandStopResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.CommandStopResponse
		if err := rpcClient.Call(ctx, "command.stop", daemon.CommandStopRequest{SessionID: sessionID, CommandID: commandID}, &response); err != nil {
			return daemon.CommandStopResponse{}, err
		}
		return response, nil
	}
)

func NewCommandCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "command", Short: "Manage session commands"}
	cmd.AddCommand(newCommandRunCmd())
	cmd.AddCommand(newCommandListCmd())
	cmd.AddCommand(newCommandShowCmd())
	cmd.AddCommand(newCommandOutputCmd())
	cmd.AddCommand(newCommandStopCmd())
	return cmd
}

func newCommandRunCmd() *cobra.Command {
	var sessionRef string
	cmd := &cobra.Command{
		Use:   "run [--session <id-or-name>] -- <command> [args...]",
		Short: "Run command in session",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) < 1 {
				return userFacingError{message: "Usage: ari command run [--session <id-or-name>] -- <command> [args...]"}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			sessionReference, err := commandSessionReference(sessionRef)
			if err != nil {
				return err
			}
			if err := commandEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			sessionID, err := commandResolveSessionIdentifier(ctx, cfg.Daemon.SocketPath, sessionReference)
			if err != nil {
				return err
			}
			if err := commandEnsureWorkspaceScope(ctx, cfg.Daemon.SocketPath, sessionID, sessionRef); err != nil {
				return err
			}

			resp, err := commandRunRPC(ctx, cfg.Daemon.SocketPath, daemon.CommandRunRequest{
				SessionID: sessionID,
				Command:   args[0],
				Args:      args[1:],
			})
			if err != nil {
				return mapCommandRPCError(err)
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Command started: %s\n", resp.CommandID)
			return err
		},
	}
	cmd.Flags().StringVar(&sessionRef, "session", "", "Session id or name override (defaults to active workspace session)")
	return cmd
}

func newCommandListCmd() *cobra.Command {
	var sessionRef string
	cmd := &cobra.Command{
		Use:   "list [--session <id-or-name>]",
		Short: "List commands for a session",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			sessionReference, err := commandSessionReference(sessionRef)
			if err != nil {
				return err
			}
			if err := commandEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			sessionID, err := commandResolveSessionIdentifier(ctx, cfg.Daemon.SocketPath, sessionReference)
			if err != nil {
				return err
			}
			if err := commandEnsureWorkspaceScope(ctx, cfg.Daemon.SocketPath, sessionID, sessionRef); err != nil {
				return err
			}

			resp, err := commandListRPC(ctx, cfg.Daemon.SocketPath, sessionID)
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
	cmd.Flags().StringVar(&sessionRef, "session", "", "Session id or name override (defaults to active workspace session)")
	return cmd
}

func newCommandShowCmd() *cobra.Command {
	var sessionRef string
	cmd := &cobra.Command{
		Use:   "show <command-id> [--session <id-or-name>]",
		Short: "Show command details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			sessionReference, err := commandSessionReference(sessionRef)
			if err != nil {
				return err
			}
			if err := commandEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			sessionID, err := commandResolveSessionIdentifier(ctx, cfg.Daemon.SocketPath, sessionReference)
			if err != nil {
				return err
			}
			if err := commandEnsureWorkspaceScope(ctx, cfg.Daemon.SocketPath, sessionID, sessionRef); err != nil {
				return err
			}

			resp, err := commandGetRPC(ctx, cfg.Daemon.SocketPath, sessionID, strings.TrimSpace(args[0]))
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
	cmd.Flags().StringVar(&sessionRef, "session", "", "Session id or name override (defaults to active workspace session)")
	return cmd
}

func newCommandOutputCmd() *cobra.Command {
	var sessionRef string
	cmd := &cobra.Command{
		Use:   "output <command-id> [--session <id-or-name>]",
		Short: "Show command output snapshot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			sessionReference, err := commandSessionReference(sessionRef)
			if err != nil {
				return err
			}
			if err := commandEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			sessionID, err := commandResolveSessionIdentifier(ctx, cfg.Daemon.SocketPath, sessionReference)
			if err != nil {
				return err
			}
			if err := commandEnsureWorkspaceScope(ctx, cfg.Daemon.SocketPath, sessionID, sessionRef); err != nil {
				return err
			}

			resp, err := commandOutputRPC(ctx, cfg.Daemon.SocketPath, sessionID, strings.TrimSpace(args[0]))
			if err != nil {
				return mapCommandRPCError(err)
			}

			_, err = fmt.Fprint(cmd.OutOrStdout(), resp.Output)
			return err
		},
	}
	cmd.Flags().StringVar(&sessionRef, "session", "", "Session id or name override (defaults to active workspace session)")
	return cmd
}

func newCommandStopCmd() *cobra.Command {
	var sessionRef string
	cmd := &cobra.Command{
		Use:   "stop <command-id> [--session <id-or-name>]",
		Short: "Stop a running command",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			sessionReference, err := commandSessionReference(sessionRef)
			if err != nil {
				return err
			}
			if err := commandEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			sessionID, err := commandResolveSessionIdentifier(ctx, cfg.Daemon.SocketPath, sessionReference)
			if err != nil {
				return err
			}
			if err := commandEnsureWorkspaceScope(ctx, cfg.Daemon.SocketPath, sessionID, sessionRef); err != nil {
				return err
			}

			resp, err := commandStopRPC(ctx, cfg.Daemon.SocketPath, sessionID, strings.TrimSpace(args[0]))
			if err != nil {
				return mapCommandRPCError(err)
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Command stop: %s\n", resp.Status)
			return err
		},
	}
	cmd.Flags().StringVar(&sessionRef, "session", "", "Session id or name override (defaults to active workspace session)")
	return cmd
}

func commandSessionReference(overrideSession string) (string, error) {
	return resolveWorkspaceSessionReference(overrideSession, commandReadActiveSession)
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
			return userFacingError{message: "Session not found"}
		case int64(rpc.CommandNotFound):
			return userFacingError{message: "Command not found"}
		case int64(rpc.InvalidParams):
			return userFacingError{message: rpcErr.Message}
		}
	}

	return err
}
