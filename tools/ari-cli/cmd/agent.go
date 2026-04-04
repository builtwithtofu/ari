package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/client"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
	"github.com/sourcegraph/jsonrpc2"
	"github.com/spf13/cobra"
)

var (
	agentSpawnRPC = func(ctx context.Context, socketPath string, req daemon.AgentSpawnRequest) (daemon.AgentSpawnResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.AgentSpawnResponse
		if err := rpcClient.Call(ctx, "agent.spawn", req, &response); err != nil {
			return daemon.AgentSpawnResponse{}, err
		}
		return response, nil
	}
	agentListRPC = func(ctx context.Context, socketPath, sessionID string) (daemon.AgentListResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.AgentListResponse
		if err := rpcClient.Call(ctx, "agent.list", daemon.AgentListRequest{SessionID: sessionID}, &response); err != nil {
			return daemon.AgentListResponse{}, err
		}
		return response, nil
	}
	agentGetRPC = func(ctx context.Context, socketPath, sessionID, agentID string) (daemon.AgentGetResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.AgentGetResponse
		if err := rpcClient.Call(ctx, "agent.get", daemon.AgentGetRequest{SessionID: sessionID, AgentID: agentID}, &response); err != nil {
			return daemon.AgentGetResponse{}, err
		}
		return response, nil
	}
	agentSendRPC = func(ctx context.Context, socketPath string, req daemon.AgentSendRequest) (daemon.AgentSendResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.AgentSendResponse
		if err := rpcClient.Call(ctx, "agent.send", req, &response); err != nil {
			return daemon.AgentSendResponse{}, err
		}
		return response, nil
	}
	agentOutputRPC = func(ctx context.Context, socketPath, sessionID, agentID string) (daemon.AgentOutputResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.AgentOutputResponse
		if err := rpcClient.Call(ctx, "agent.output", daemon.AgentOutputRequest{SessionID: sessionID, AgentID: agentID}, &response); err != nil {
			return daemon.AgentOutputResponse{}, err
		}
		return response, nil
	}
	agentStopRPC = func(ctx context.Context, socketPath, sessionID, agentID string) (daemon.AgentStopResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.AgentStopResponse
		if err := rpcClient.Call(ctx, "agent.stop", daemon.AgentStopRequest{SessionID: sessionID, AgentID: agentID}, &response); err != nil {
			return daemon.AgentStopResponse{}, err
		}
		return response, nil
	}
)

func NewAgentCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "agent", Short: "Manage session agents"}
	cmd.AddCommand(newAgentSpawnCmd())
	cmd.AddCommand(newAgentListCmd())
	cmd.AddCommand(newAgentShowCmd())
	cmd.AddCommand(newAgentSendCmd())
	cmd.AddCommand(newAgentOutputCmd())
	cmd.AddCommand(newAgentStopCmd())
	return cmd
}

func newAgentSpawnCmd() *cobra.Command {
	var name string
	var harness string

	cmd := &cobra.Command{
		Use:   "spawn <session> [--name <name>] -- <command> [args...]",
		Short: "Spawn an agent in session",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) < 2 {
				return userFacingError{message: "Usage: ari agent spawn <session> [--name <name>] -- <command> [args...]"}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			sessionID, err := commandResolveSessionIdentifier(ctx, cfg.Daemon.SocketPath, args[0])
			if err != nil {
				return err
			}

			resp, err := agentSpawnRPC(ctx, cfg.Daemon.SocketPath, daemon.AgentSpawnRequest{
				SessionID: sessionID,
				Name:      strings.TrimSpace(name),
				Harness:   strings.TrimSpace(harness),
				Command:   args[1],
				Args:      args[2:],
			})
			if err != nil {
				return mapAgentRPCError(err)
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Agent started: %s\n", resp.AgentID)
			return err
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Optional agent name")
	cmd.Flags().StringVar(&harness, "harness", "", "Harness identity (claude-code|codex|opencode)")
	return cmd
}

func newAgentListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list <session>",
		Short: "List agents for a session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			sessionID, err := commandResolveSessionIdentifier(ctx, cfg.Daemon.SocketPath, args[0])
			if err != nil {
				return err
			}

			resp, err := agentListRPC(ctx, cfg.Daemon.SocketPath, sessionID)
			if err != nil {
				return mapAgentRPCError(err)
			}

			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "ID       NAME       STATUS     STARTED                COMMAND"); err != nil {
				return err
			}
			for _, item := range resp.Agents {
				shortID := item.AgentID
				if len(shortID) > 8 {
					shortID = shortID[:8]
				}
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%-8s %-10s %-10s %-22s %s\n", shortID, item.Name, item.Status, item.StartedAt, item.Command); err != nil {
					return err
				}
			}

			return nil
		},
	}
}

func newAgentShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <session> <id-or-name>",
		Short: "Show agent details",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			sessionID, err := commandResolveSessionIdentifier(ctx, cfg.Daemon.SocketPath, args[0])
			if err != nil {
				return err
			}

			resp, err := agentGetRPC(ctx, cfg.Daemon.SocketPath, sessionID, strings.TrimSpace(args[1]))
			if err != nil {
				return mapAgentRPCError(err)
			}

			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Agent: %s (%s)\n", resp.AgentID, resp.Command); err != nil {
				return err
			}
			if resp.Name != "" {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Name: %s\n", resp.Name); err != nil {
					return err
				}
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
			if resp.StoppedAt != "" {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Stopped: %s\n", resp.StoppedAt); err != nil {
					return err
				}
			}

			return nil
		},
	}
}

func newAgentSendCmd() *cobra.Command {
	var inputFlag string

	cmd := &cobra.Command{
		Use:   "send <session> <id-or-name> --input <text>",
		Short: "Send input to an agent",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}

			stdinText, hasStdin, err := readPipedStdin(cmd)
			if err != nil {
				return err
			}
			hasFlag := strings.TrimSpace(inputFlag) != ""
			if hasFlag && hasStdin {
				return userFacingError{message: "Provide input via --input or stdin pipe, not both"}
			}
			if !hasFlag && !hasStdin {
				return userFacingError{message: "Provide input via --input or stdin pipe"}
			}

			input := inputFlag
			if hasStdin {
				input = stdinText
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			sessionID, err := commandResolveSessionIdentifier(ctx, cfg.Daemon.SocketPath, args[0])
			if err != nil {
				return err
			}

			if _, err := agentSendRPC(ctx, cfg.Daemon.SocketPath, daemon.AgentSendRequest{
				SessionID: sessionID,
				AgentID:   strings.TrimSpace(args[1]),
				Input:     input,
			}); err != nil {
				return mapAgentRPCError(err)
			}

			_, err = fmt.Fprintln(cmd.OutOrStdout(), "Input sent")
			return err
		},
	}

	cmd.Flags().StringVar(&inputFlag, "input", "", "Input text to send")
	return cmd
}

func newAgentOutputCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "output <session> <id-or-name>",
		Short: "Show agent output snapshot",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			sessionID, err := commandResolveSessionIdentifier(ctx, cfg.Daemon.SocketPath, args[0])
			if err != nil {
				return err
			}

			resp, err := agentOutputRPC(ctx, cfg.Daemon.SocketPath, sessionID, strings.TrimSpace(args[1]))
			if err != nil {
				return mapAgentRPCError(err)
			}

			_, err = fmt.Fprint(cmd.OutOrStdout(), resp.Output)
			return err
		},
	}
}

func newAgentStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop <session> <id-or-name>",
		Short: "Stop a running agent",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			sessionID, err := commandResolveSessionIdentifier(ctx, cfg.Daemon.SocketPath, args[0])
			if err != nil {
				return err
			}

			resp, err := agentStopRPC(ctx, cfg.Daemon.SocketPath, sessionID, strings.TrimSpace(args[1]))
			if err != nil {
				return mapAgentRPCError(err)
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Agent stop: %s\n", resp.Status)
			return err
		},
	}
}

func readPipedStdin(cmd *cobra.Command) (string, bool, error) {
	stdin := cmd.InOrStdin()
	if statter, ok := stdin.(interface{ Stat() (os.FileInfo, error) }); ok {
		stat, err := statter.Stat()
		if err != nil {
			return "", false, err
		}
		if stat.Mode()&os.ModeCharDevice != 0 {
			return "", false, nil
		}
	}

	b, err := io.ReadAll(stdin)
	if err != nil {
		return "", false, err
	}
	if len(b) == 0 {
		return "", false, nil
	}
	return string(b), true, nil
}

func mapAgentRPCError(err error) error {
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
		case int64(rpc.AgentNotFound):
			return userFacingError{message: "Agent not found"}
		case int64(rpc.AgentNotRunning):
			return userFacingError{message: "Agent is not running"}
		case int64(rpc.InvalidParams):
			return userFacingError{message: rpcErr.Message}
		}
	}

	return err
}
