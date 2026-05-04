package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/client"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/config"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/spf13/cobra"
)

var (
	sessionReadActiveWorkspace = config.ReadActiveWorkspace
	sessionEnsureDaemonRunning = ensureDaemonRunning
	sessionStartRPC            = func(ctx context.Context, socketPath string, req daemon.AgentSessionStartRequest) (daemon.AgentSessionStartResponse, error) {
		rpcClient := client.New(socketPath)
		var resp daemon.AgentSessionStartResponse
		if err := rpcClient.Call(ctx, "session.start", req, &resp); err != nil {
			return daemon.AgentSessionStartResponse{}, err
		}
		return resp, nil
	}
	sessionListRPC = func(ctx context.Context, socketPath string, req daemon.SessionListRequest) (daemon.SessionListResponse, error) {
		rpcClient := client.New(socketPath)
		var resp daemon.SessionListResponse
		if err := rpcClient.Call(ctx, "session.list", req, &resp); err != nil {
			return daemon.SessionListResponse{}, err
		}
		return resp, nil
	}
	sessionGetRPC = func(ctx context.Context, socketPath string, req daemon.SessionGetRequest) (daemon.SessionGetResponse, error) {
		rpcClient := client.New(socketPath)
		var resp daemon.SessionGetResponse
		if err := rpcClient.Call(ctx, "session.get", req, &resp); err != nil {
			return daemon.SessionGetResponse{}, err
		}
		return resp, nil
	}
	sessionMessageSendRPC = func(ctx context.Context, socketPath string, req daemon.AgentMessageSendRequest) (daemon.AgentMessageSendResponse, error) {
		rpcClient := client.New(socketPath)
		var resp daemon.AgentMessageSendResponse
		if err := rpcClient.Call(ctx, "session.message.send", req, &resp); err != nil {
			return daemon.AgentMessageSendResponse{}, err
		}
		return resp, nil
	}
	sessionCallRPC = func(ctx context.Context, socketPath string, req daemon.EphemeralAgentCallRequest) (daemon.EphemeralAgentCallResponse, error) {
		rpcClient := client.New(socketPath)
		var resp daemon.EphemeralAgentCallResponse
		if err := rpcClient.Call(ctx, "session.call.ephemeral", req, &resp); err != nil {
			return daemon.EphemeralAgentCallResponse{}, err
		}
		return resp, nil
	}
	sessionFanoutRPC = func(ctx context.Context, socketPath string, req daemon.AgentMessageSendRequest) (daemon.AgentMessageSendResponse, error) {
		rpcClient := client.New(socketPath)
		var resp daemon.AgentMessageSendResponse
		if err := rpcClient.Call(ctx, "session.fanout", req, &resp); err != nil {
			return daemon.AgentMessageSendResponse{}, err
		}
		return resp, nil
	}
)

func NewSessionCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "session", Short: "Manage workspace sessions, messages, calls, and fan-out"}
	cmd.AddCommand(newSessionStartCmd(), newSessionListCmd(), newSessionShowCmd(), newSessionMessageCmd(), newSessionCallCmd(), newSessionFanoutCmd())
	return cmd
}

func newSessionStartCmd() *cobra.Command {
	var sessionID, message, prompt, promptFile, workspaceID string
	cmd := &cobra.Command{Use: "start <profile>", Short: "Start a sticky workspace session from a profile", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := configuredDaemonConfig()
		if err != nil {
			return err
		}
		if err := sessionEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
			return err
		}
		workspaceID = strings.TrimSpace(workspaceID)
		if workspaceID == "" {
			workspaceID, err = sessionReadActiveWorkspace()
			if err != nil {
				return err
			}
		}
		workspaceID = strings.TrimSpace(workspaceID)
		if workspaceID == "" {
			return userFacingError{message: "No active workspace is set"}
		}
		if strings.TrimSpace(prompt) != "" && strings.TrimSpace(promptFile) != "" {
			return userFacingError{message: "Use either --prompt or --prompt-file, not both"}
		}
		if strings.TrimSpace(promptFile) != "" {
			data, err := os.ReadFile(promptFile)
			if err != nil {
				return err
			}
			prompt = string(data)
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		resp, err := sessionStartRPC(ctx, cfg.Daemon.SocketPath, daemon.AgentSessionStartRequest{WorkspaceID: workspaceID, Profile: strings.TrimSpace(args[0]), SessionID: strings.TrimSpace(sessionID), Message: strings.TrimSpace(message), Prompt: prompt})
		if err != nil {
			return mapSessionRPCError(err)
		}
		id := resp.Run.SessionID
		if strings.TrimSpace(id) == "" {
			id = resp.Run.AgentSessionID
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Session started: %s\n", id)
		return err
	}}
	cmd.Flags().StringVar(&sessionID, "session", "", "Stable session id or name to create or attach")
	cmd.Flags().StringVar(&message, "message", "", "Visible task message for the session")
	cmd.Flags().StringVar(&prompt, "prompt", "", "Session-specific replacement prompt")
	cmd.Flags().StringVar(&promptFile, "prompt-file", "", "File containing a session-specific replacement prompt")
	cmd.Flags().StringVar(&workspaceID, "workspace", "", "Target workspace id or name (defaults to active workspace)")
	return cmd
}

func newSessionListCmd() *cobra.Command {
	var workspaceID string
	cmd := &cobra.Command{Use: "list", Short: "List workspace sessions", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := configuredDaemonConfig()
		if err != nil {
			return err
		}
		if err := sessionEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
			return err
		}
		workspaceID = strings.TrimSpace(workspaceID)
		if workspaceID == "" {
			workspaceID, err = sessionReadActiveWorkspace()
			if err != nil {
				return err
			}
		}
		workspaceID = strings.TrimSpace(workspaceID)
		if workspaceID == "" {
			return userFacingError{message: "No active workspace is set"}
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		resp, err := sessionListRPC(ctx, cfg.Daemon.SocketPath, daemon.SessionListRequest{WorkspaceID: workspaceID})
		if err != nil {
			return mapSessionRPCError(err)
		}
		for _, session := range resp.Sessions {
			id := strings.TrimSpace(session.SessionID)
			if id == "" {
				id = strings.TrimSpace(session.AgentSessionID)
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", id, session.Status, session.Executor); err != nil {
				return err
			}
		}
		return nil
	}}
	cmd.Flags().StringVar(&workspaceID, "workspace", "", "Target workspace id or name (defaults to active workspace)")
	return cmd
}

func newSessionShowCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "show <session>", Short: "Show workspace session details", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := configuredDaemonConfig()
		if err != nil {
			return err
		}
		if err := sessionEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		resp, err := sessionGetRPC(ctx, cfg.Daemon.SocketPath, daemon.SessionGetRequest{SessionID: strings.TrimSpace(args[0])})
		if err != nil {
			return mapSessionRPCError(err)
		}
		session := resp.Session
		id := strings.TrimSpace(session.SessionID)
		if id == "" {
			id = strings.TrimSpace(session.AgentSessionID)
		}
		for _, line := range []string{
			"Session: " + id,
			"Status: " + session.Status,
			"Executor: " + session.Executor,
			"Workspace: " + session.WorkspaceID,
		} {
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), line); err != nil {
				return err
			}
		}
		return nil
	}}
	return cmd
}

func newSessionMessageCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "message", Short: "Send visible messages between workspace sessions"}
	var fromSessionID, targetSessionID, messageBody string
	var excerptIDs []string
	send := &cobra.Command{Use: "send", Short: "Send a visible message to a session", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := configuredDaemonConfig()
		if err != nil {
			return err
		}
		if err := sessionEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
			return err
		}
		fromSessionID = strings.TrimSpace(fromSessionID)
		targetSessionID = strings.TrimSpace(targetSessionID)
		messageBody = strings.TrimSpace(messageBody)
		if fromSessionID == "" || targetSessionID == "" || messageBody == "" {
			return userFacingError{message: "--from, --to, and --message are required"}
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		messageID := fmt.Sprintf("dm-%d", time.Now().UnixNano())
		resp, err := sessionMessageSendRPC(ctx, cfg.Daemon.SocketPath, daemon.AgentMessageSendRequest{AgentMessageID: messageID, SourceSessionID: fromSessionID, TargetSessionID: targetSessionID, Body: messageBody, ContextExcerptIDs: excerptIDs})
		if err != nil {
			return mapSessionRPCError(err)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Message sent: %s\n", resp.AgentMessage.AgentMessageID)
		return err
	}}
	send.Flags().StringVar(&fromSessionID, "from", "", "Source session id or name")
	send.Flags().StringVar(&targetSessionID, "to", "", "Target session id")
	send.Flags().StringArrayVar(&excerptIDs, "excerpt", nil, "Context excerpt id to attach as visible context")
	send.Flags().StringVar(&messageBody, "message", "", "Visible message body")
	cmd.AddCommand(send)
	return cmd
}

func newSessionCallCmd() *cobra.Command {
	var fromSessionID, targetProfile, messageBody string
	var excerptIDs []string
	cmd := &cobra.Command{Use: "call", Short: "Start an ephemeral profile call from a session", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := configuredDaemonConfig()
		if err != nil {
			return err
		}
		if err := sessionEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
			return err
		}
		fromSessionID = strings.TrimSpace(fromSessionID)
		targetProfile = strings.TrimSpace(targetProfile)
		messageBody = strings.TrimSpace(messageBody)
		if fromSessionID == "" || targetProfile == "" || messageBody == "" {
			return userFacingError{message: "--from, --profile, and --message are required"}
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		callID := fmt.Sprintf("call-%d", time.Now().UnixNano())
		resp, err := sessionCallRPC(ctx, cfg.Daemon.SocketPath, daemon.EphemeralAgentCallRequest{CallID: callID, SourceSessionID: fromSessionID, TargetAgentID: targetProfile, Body: messageBody, ContextExcerptIDs: excerptIDs})
		if err != nil {
			return mapSessionRPCError(err)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Ephemeral call run: %s\n", resp.Run.SessionID)
		return err
	}}
	cmd.Flags().StringVar(&fromSessionID, "from", "", "Source session id")
	cmd.Flags().StringVar(&targetProfile, "profile", "", "Target profile name")
	cmd.Flags().StringArrayVar(&excerptIDs, "excerpt", nil, "Context excerpt id to attach as visible context")
	cmd.Flags().StringVar(&messageBody, "message", "", "Visible task message")
	return cmd
}

func newSessionFanoutCmd() *cobra.Command {
	var fromSessionID, messageBody string
	var targetSessionIDs, targetProfiles, excerptIDs []string
	cmd := &cobra.Command{Use: "fanout", Short: "Fan out messages or calls from a session", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := configuredDaemonConfig()
		if err != nil {
			return err
		}
		if err := sessionEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
			return err
		}
		fromSessionID = strings.TrimSpace(fromSessionID)
		messageBody = strings.TrimSpace(messageBody)
		if fromSessionID == "" || messageBody == "" {
			return userFacingError{message: "--from and --message are required"}
		}
		if len(targetProfiles) > 0 {
			return userFacingError{message: "--to-profile is not implemented yet"}
		}
		if len(targetSessionIDs) != 1 {
			return userFacingError{message: "exactly one --to-session is required in this phase"}
		}
		targetSessionID := strings.TrimSpace(targetSessionIDs[0])
		if targetSessionID == "" {
			return userFacingError{message: "--to-session must be non-empty"}
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		resp, err := sessionFanoutRPC(ctx, cfg.Daemon.SocketPath, daemon.AgentMessageSendRequest{SourceSessionID: fromSessionID, TargetSessionID: targetSessionID, Body: messageBody, ContextExcerptIDs: excerptIDs})
		if err != nil {
			return mapSessionRPCError(err)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Fanout message: %s\n", resp.AgentMessage.AgentMessageID)
		return err
	}}
	cmd.Flags().StringVar(&fromSessionID, "from", "", "Source session id")
	cmd.Flags().StringArrayVar(&targetSessionIDs, "to-session", nil, "Target session id")
	cmd.Flags().StringArrayVar(&targetProfiles, "to-profile", nil, "Target profile name for an ephemeral call")
	cmd.Flags().StringArrayVar(&excerptIDs, "excerpt", nil, "Context excerpt id to attach as visible context")
	cmd.Flags().StringVar(&messageBody, "message", "", "Visible task message")
	return cmd
}
