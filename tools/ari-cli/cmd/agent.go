package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/client"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/frame"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
	termio "github.com/builtwithtofu/ari/tools/ari-cli/internal/terminal"
	"github.com/sourcegraph/jsonrpc2"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type attachSessionOutcome struct {
	Detached bool
	ExitCode *int
}

type attachFrameSession interface {
	ReadFrame() (frame.Frame, error)
	SendData(payload []byte) error
	SendDetach() error
	SendResize(cols, rows uint16) error
	Close() error
}

type agentExitedFramePayload struct {
	ExitCode int `json:"exit_code"`
}

func agentAttachPrepareTerminal(cmd *cobra.Command, ctx context.Context) (func(), error) {
	if cmd == nil {
		return nil, fmt.Errorf("attach terminal setup: command is required")
	}
	if ctx == nil {
		return nil, fmt.Errorf("attach terminal setup: context is required")
	}

	inputFile, ok := cmd.InOrStdin().(*os.File)
	if !ok {
		return func() {}, nil
	}
	fd := int(inputFile.Fd())
	if !term.IsTerminal(fd) {
		return func() {}, nil
	}

	manager := termio.NewStateManager(fd, cmd.OutOrStdout())
	if err := manager.EnterRaw(); err != nil {
		return nil, err
	}
	if err := manager.EnterAltScreen(); err != nil {
		_ = manager.Restore()
		return nil, err
	}

	return func() {
		_ = manager.Restore()
	}, nil
}

func runAttachResizeLoop(ctx context.Context, session attachFrameSession, resizeSignals <-chan os.Signal, sizeProvider func() (uint16, uint16)) func() {
	if ctx == nil || session == nil || resizeSignals == nil || sizeProvider == nil {
		return func() {}
	}

	stopCh := make(chan struct{})
	var stopOnce sync.Once

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-stopCh:
				return
			case <-resizeSignals:
				cols, rows := sizeProvider()
				if cols == 0 || rows == 0 {
					continue
				}
				_ = session.SendResize(cols, rows)
			}
		}
	}()

	return func() {
		stopOnce.Do(func() {
			close(stopCh)
		})
	}
}

func isDaemonDisconnectError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) {
		return true
	}
	return strings.TrimSpace(err.Error()) == "EOF"
}

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
	agentAttachRPC = func(ctx context.Context, socketPath string, req daemon.AgentAttachRequest) (daemon.AgentAttachResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.AgentAttachResponse
		if err := rpcClient.Call(ctx, "agent.attach", req, &response); err != nil {
			return daemon.AgentAttachResponse{}, err
		}
		return response, nil
	}
	agentDetachRPC = func(ctx context.Context, socketPath string, req daemon.AgentDetachRequest) (daemon.AgentDetachResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.AgentDetachResponse
		if err := rpcClient.Call(ctx, "agent.detach", req, &response); err != nil {
			return daemon.AgentDetachResponse{}, err
		}
		return response, nil
	}
	agentAttachTerminalSize = func(cmd *cobra.Command) (uint16, uint16) {
		candidates := []any{cmd.InOrStdin(), cmd.ErrOrStderr(), cmd.OutOrStdout()}
		for _, candidate := range candidates {
			file, ok := candidate.(*os.File)
			if !ok {
				continue
			}

			cols, rows, err := term.GetSize(int(file.Fd()))
			if err == nil && cols > 0 && rows > 0 {
				return uint16(cols), uint16(rows)
			}
		}

		return 80, 24
	}
	agentAttachOpenSession = func(ctx context.Context, socketPath, token string, cols, rows uint16) (attachFrameSession, []byte, error) {
		return client.OpenAttachSession(ctx, socketPath, client.AttachConnectRequest{Token: token, Cols: cols, Rows: rows})
	}
	agentAttachRunSession = func(ctx context.Context, input io.Reader, output io.Writer, socketPath, token string, cols, rows uint16, resizeSignals <-chan os.Signal, sizeProvider func() (uint16, uint16)) (attachSessionOutcome, error) {
		session, snapshot, err := agentAttachOpenSession(ctx, socketPath, token, cols, rows)
		if err != nil {
			return attachSessionOutcome{}, err
		}
		defer func() {
			_ = session.Close()
		}()
		stopResizeLoop := runAttachResizeLoop(ctx, session, resizeSignals, sizeProvider)
		defer stopResizeLoop()

		if len(snapshot) > 0 {
			if _, err := output.Write(snapshot); err != nil {
				return attachSessionOutcome{}, err
			}
		}

		scanner := termio.NewDetachScanner()
		buf := make([]byte, 1024)
		inputResultCh := make(chan attachSessionOutcome, 1)
		errCh := make(chan error, 2)
		detachRequestedCh := make(chan struct{}, 1)

		go func() {
			for {
				n, readErr := input.Read(buf)
				if n > 0 {
					passthrough, shouldDetach := scanner.Scan(buf[:n])
					if len(passthrough) > 0 {
						if err := session.SendData(passthrough); err != nil {
							errCh <- err
							return
						}
					}
					if shouldDetach {
						if err := session.SendDetach(); err != nil {
							errCh <- err
							return
						}
						select {
						case detachRequestedCh <- struct{}{}:
						default:
						}
						_ = session.Close()
						inputResultCh <- attachSessionOutcome{Detached: true}
						return
					}
				}

				if readErr != nil {
					if errors.Is(readErr, io.EOF) {
						return
					}
					errCh <- readErr
					return
				}
			}
		}()

		for {
			select {
			case <-ctx.Done():
				return attachSessionOutcome{}, ctx.Err()
			case outcome := <-inputResultCh:
				return outcome, nil
			case runErr := <-errCh:
				return attachSessionOutcome{}, runErr
			default:
			}

			msg, err := session.ReadFrame()
			if err != nil {
				select {
				case <-detachRequestedCh:
					return attachSessionOutcome{Detached: true}, nil
				default:
				}
				return attachSessionOutcome{}, err
			}

			switch msg.Type {
			case frame.TypeDataServerToClient:
				if len(msg.Payload) == 0 {
					continue
				}
				if _, err := output.Write(msg.Payload); err != nil {
					return attachSessionOutcome{}, err
				}
			case frame.TypeAgentExited:
				var payload agentExitedFramePayload
				if err := json.Unmarshal(msg.Payload, &payload); err != nil {
					return attachSessionOutcome{}, fmt.Errorf("decode agent exited payload: %w", err)
				}
				return attachSessionOutcome{ExitCode: &payload.ExitCode}, nil
			case frame.TypeError:
				return attachSessionOutcome{}, fmt.Errorf("attach protocol error: %s", string(msg.Payload))
			default:
				return attachSessionOutcome{}, fmt.Errorf("attach protocol error: unsupported frame type 0x%02x", byte(msg.Type))
			}
		}
	}
)

func NewAgentCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "agent", Short: "Manage session agents"}
	cmd.AddCommand(newAgentSpawnCmd())
	cmd.AddCommand(newAgentListCmd())
	cmd.AddCommand(newAgentShowCmd())
	cmd.AddCommand(newAgentAttachCmd())
	cmd.AddCommand(newAgentDetachCmd())
	cmd.AddCommand(newAgentSendCmd())
	cmd.AddCommand(newAgentOutputCmd())
	cmd.AddCommand(newAgentStopCmd())
	return cmd
}

func newAgentAttachCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "attach <session> <id-or-name>",
		Short: "Attach to a running agent terminal",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}

			runCtx := cmd.Context()
			if runCtx == nil {
				runCtx = context.Background()
			}
			runCtx, stopSignals := signal.NotifyContext(runCtx, os.Interrupt, syscall.SIGTERM)
			defer stopSignals()

			rpcCtx, cancel := context.WithTimeout(runCtx, 5*time.Second)
			defer cancel()

			terminalCleanup, err := agentAttachPrepareTerminal(cmd, runCtx)
			if err != nil {
				return err
			}
			defer terminalCleanup()

			sessionID, err := commandResolveSessionIdentifier(rpcCtx, cfg.Daemon.SocketPath, args[0])
			if err != nil {
				return err
			}

			cols, rows := agentAttachTerminalSize(cmd)

			resp, err := agentAttachRPC(rpcCtx, cfg.Daemon.SocketPath, daemon.AgentAttachRequest{
				SessionID:   sessionID,
				AgentID:     strings.TrimSpace(args[1]),
				InitialCols: cols,
				InitialRows: rows,
			})
			if err != nil {
				if isDaemonDisconnectError(err) {
					return userFacingError{message: "Daemon disconnected. Agent may still be running."}
				}
				return mapAgentRPCError(err)
			}

			resizeSignalCh := make(chan os.Signal, 1)
			signal.Notify(resizeSignalCh, syscall.SIGWINCH)
			defer signal.Stop(resizeSignalCh)

			outcome, err := agentAttachRunSession(
				runCtx,
				cmd.InOrStdin(),
				cmd.OutOrStdout(),
				cfg.Daemon.SocketPath,
				resp.Token,
				cols,
				rows,
				resizeSignalCh,
				func() (uint16, uint16) { return agentAttachTerminalSize(cmd) },
			)
			if err != nil {
				if isDaemonDisconnectError(err) {
					return userFacingError{message: "Daemon disconnected. Agent may still be running."}
				}
				return err
			}
			if outcome.ExitCode != nil {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "Agent exited (code %d).\n", *outcome.ExitCode)
				return err
			}
			if !outcome.Detached {
				return userFacingError{message: "Attach session ended unexpectedly"}
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Detached from agent %q.\n", strings.TrimSpace(args[1]))
			return err
		},
	}
}

func newAgentDetachCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "detach <session> <id-or-name>",
		Short: "Detach any active attach session for an agent",
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

			resp, err := agentDetachRPC(ctx, cfg.Daemon.SocketPath, daemon.AgentDetachRequest{
				SessionID: sessionID,
				AgentID:   strings.TrimSpace(args[1]),
			})
			if err != nil {
				return mapAgentRPCError(err)
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Agent detach: %s\n", resp.Status)
			return err
		},
	}
}

func newAgentSpawnCmd() *cobra.Command {
	var name string
	var harness string

	cmd := &cobra.Command{
		Use:   "spawn <session> [--name <name>] [--harness <harness>] [-- <command> [args...]]",
		Short: "Spawn an agent in session",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) < 1 {
				return userFacingError{message: "Usage: ari agent spawn <session> [--name <name>] [--harness <harness>] [-- <command> [args...]]"}
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

			command := ""
			commandArgs := make([]string, 0)
			if len(args) > 1 {
				extra := args[1:]
				if strings.TrimSpace(harness) != "" {
					commandArgs = append(commandArgs, extra...)
				} else {
					command = extra[0]
					commandArgs = append(commandArgs, extra[1:]...)
				}
			}
			if strings.TrimSpace(harness) == "" && strings.TrimSpace(command) == "" {
				return userFacingError{message: "Provide --harness or an explicit command"}
			}

			resp, err := agentSpawnRPC(ctx, cfg.Daemon.SocketPath, daemon.AgentSpawnRequest{
				SessionID: sessionID,
				Name:      strings.TrimSpace(name),
				Harness:   strings.TrimSpace(harness),
				Command:   command,
				Args:      commandArgs,
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
		case int64(rpc.AgentAlreadyAttached):
			return userFacingError{message: "Agent already has an active attach session"}
		case int64(rpc.InvalidParams):
			return userFacingError{message: rpcErr.Message}
		}
	}

	return err
}
