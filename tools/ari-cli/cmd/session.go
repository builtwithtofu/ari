package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/spf13/cobra"
)

var (
	sessionEnsureDaemonRunning = ensureDaemonRunning
	sessionStartRPC            = func(ctx context.Context, socketPath string, req daemon.HarnessSessionStartRequest) (daemon.HarnessSessionStartResponse, error) {
		return callDaemonRPC[daemon.HarnessSessionStartResponse](ctx, socketPath, "session.start", req)
	}
	sessionListRPC = func(ctx context.Context, socketPath string, req daemon.SessionListRequest) (daemon.SessionListResponse, error) {
		return callDaemonRPC[daemon.SessionListResponse](ctx, socketPath, "session.list", req)
	}
	sessionGetRPC = func(ctx context.Context, socketPath string, req daemon.SessionGetRequest) (daemon.SessionGetResponse, error) {
		return callDaemonRPC[daemon.SessionGetResponse](ctx, socketPath, "session.get", req)
	}
	sessionMessageSendRPC = func(ctx context.Context, socketPath string, req daemon.AgentMessageSendRequest) (daemon.AgentMessageSendResponse, error) {
		return callDaemonRPC[daemon.AgentMessageSendResponse](ctx, socketPath, "session.message.send", req)
	}
	sessionCallRPC = func(ctx context.Context, socketPath string, req daemon.EphemeralCallRequest) (daemon.EphemeralCallResponse, error) {
		return callDaemonRPC[daemon.EphemeralCallResponse](ctx, socketPath, "session.call.ephemeral", req)
	}
	sessionFanoutRPC = func(ctx context.Context, socketPath string, req daemon.AgentMessageSendRequest) (daemon.AgentMessageSendResponse, error) {
		return callDaemonRPC[daemon.AgentMessageSendResponse](ctx, socketPath, "session.fanout", req)
	}
	sessionLogsRPC = func(ctx context.Context, socketPath string, req daemon.SessionLogsRequest) (daemon.SessionLogsResponse, error) {
		return callDaemonRPC[daemon.SessionLogsResponse](ctx, socketPath, "session.logs", req)
	}
	sessionAttachRPC = func(ctx context.Context, socketPath string, req daemon.SessionAttachRequest) (daemon.SessionAttachResponse, error) {
		return callDaemonRPC[daemon.SessionAttachResponse](ctx, socketPath, "session.attach", req)
	}
)

func NewSessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Manage workspace sessions, messages, calls, and fan-out",
		Long:  "Manage workspace sessions, messages, calls, and fan-out. Claude Code sessions default to subscription-backed background mode; headless claude -p is opt-in API-credit automation.",
	}
	cmd.AddCommand(newSessionStartCmd(), newSessionListCmd(), newSessionShowCmd(), newSessionLogsCmd(), newSessionAttachCmd(), newSessionMessageCmd(), newSessionCallCmd(), newSessionFanoutCmd())
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
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		workflowCtx, err := workflowContextResolver.Resolve(ctx, cfg.Daemon.SocketPath, workspaceID)
		if err != nil {
			return err
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
		resp, err := sessionStartRPC(ctx, cfg.Daemon.SocketPath, daemon.HarnessSessionStartRequest{WorkspaceID: workflowCtx.WorkspaceID, Profile: strings.TrimSpace(args[0]), SessionID: strings.TrimSpace(sessionID), Message: strings.TrimSpace(message), Prompt: prompt})
		if err != nil {
			return mapWorkspaceRPCError(err)
		}
		id := resp.Run.SessionID
		if strings.TrimSpace(id) == "" {
			id = resp.Run.HarnessSessionID
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
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		workflowCtx, err := workflowContextResolver.Resolve(ctx, cfg.Daemon.SocketPath, workspaceID)
		if err != nil {
			return err
		}
		resp, err := sessionListRPC(ctx, cfg.Daemon.SocketPath, daemon.SessionListRequest{WorkspaceID: workflowCtx.WorkspaceID})
		if err != nil {
			return mapWorkspaceRPCError(err)
		}
		for _, session := range resp.Sessions {
			id := strings.TrimSpace(session.SessionID)
			if id == "" {
				id = strings.TrimSpace(session.HarnessSessionID)
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", id, presentationStatusLabel(session.Presentation, session.Status), presentationLabel(session.Presentation, session.Executor)); err != nil {
				return err
			}
		}
		return nil
	}}
	cmd.Flags().StringVar(&workspaceID, "workspace", "", "Target workspace id or name (defaults to active workspace)")
	return cmd
}

func newSessionShowCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "show <session>", Short: "Show workspace session details by global session id", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
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
			return mapWorkspaceRPCError(err)
		}
		session := resp.Session
		id := strings.TrimSpace(session.SessionID)
		if id == "" {
			id = strings.TrimSpace(session.HarnessSessionID)
		}
		for _, line := range []string{
			"Session: " + id,
			"Status: " + presentationStatusLabel(session.Presentation, session.Status),
			"Harness: " + presentationLabel(session.Presentation, session.Executor),
			"Workspace: " + session.WorkspaceID,
			"Native provider session: " + session.ProviderSessionID,
			"Native invocation mode: " + nativeSessionField(session.Presentation, "invocation_mode", session.InvocationMode),
			"Native usage bucket: " + nativeSessionField(session.Presentation, "usage_bucket", session.UsageBucket),
		} {
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), line); err != nil {
				return err
			}
		}
		return nil
	}}
	return cmd
}

func newSessionLogsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "logs <session>", Short: "Show native harness session logs by global session id", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := configuredDaemonConfig()
		if err != nil {
			return err
		}
		if err := sessionEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		resp, err := sessionLogsRPC(ctx, cfg.Daemon.SocketPath, daemon.SessionLogsRequest{SessionID: strings.TrimSpace(args[0])})
		if err != nil {
			return mapWorkspaceRPCError(err)
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Command: %s\n", strings.Join(resp.Command, " ")); err != nil {
			return err
		}
		if strings.TrimSpace(resp.Output) == "" {
			return nil
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), resp.Output)
		return err
	}}
	return cmd
}

func newSessionAttachCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "attach-command <session>", Short: "Print the native harness attach command by global session id", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := configuredDaemonConfig()
		if err != nil {
			return err
		}
		if err := sessionEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		resp, err := sessionAttachRPC(ctx, cfg.Daemon.SocketPath, daemon.SessionAttachRequest{SessionID: strings.TrimSpace(args[0])})
		if err != nil {
			return mapWorkspaceRPCError(err)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s\n", strings.Join(resp.Command, " "))
		return err
	}}
	return cmd
}

func newSessionMessageCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "message", Short: "Send visible messages between workspace sessions"}
	var fromSessionID, targetSessionID, messageBody, workspaceID string
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
		workflowCtx, err := workflowContextResolver.Resolve(ctx, cfg.Daemon.SocketPath, workspaceID)
		if err != nil {
			return err
		}
		resp, err := sessionMessageSendRPC(ctx, cfg.Daemon.SocketPath, daemon.AgentMessageSendRequest{WorkspaceID: workflowCtx.WorkspaceID, SourceSessionID: fromSessionID, TargetSessionID: targetSessionID, Body: messageBody, ContextExcerptIDs: excerptIDs})
		if err != nil {
			return mapWorkspaceRPCError(err)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Message sent: %s\n", resp.AgentMessage.AgentMessageID)
		return err
	}}
	send.Flags().StringVar(&fromSessionID, "from", "", "Source session id or name")
	send.Flags().StringVar(&targetSessionID, "to", "", "Target session id")
	send.Flags().StringVar(&workspaceID, "workspace", "", "Target workspace id or name (defaults to active workspace)")
	send.Flags().StringArrayVar(&excerptIDs, "excerpt", nil, "Context excerpt id to attach as visible context")
	send.Flags().StringVar(&messageBody, "message", "", "Visible message body")
	cmd.AddCommand(send)
	return cmd
}

func newSessionCallCmd() *cobra.Command {
	var fromSessionID, targetProfile, messageBody, workspaceID string
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
		workflowCtx, err := workflowContextResolver.Resolve(ctx, cfg.Daemon.SocketPath, workspaceID)
		if err != nil {
			return err
		}
		resp, err := sessionCallRPC(ctx, cfg.Daemon.SocketPath, daemon.EphemeralCallRequest{WorkspaceID: workflowCtx.WorkspaceID, SourceSessionID: fromSessionID, TargetAgentID: targetProfile, Body: messageBody, ContextExcerptIDs: excerptIDs})
		if err != nil {
			return mapWorkspaceRPCError(err)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Ephemeral call run: %s\n", resp.Run.SessionID)
		return err
	}}
	cmd.Flags().StringVar(&fromSessionID, "from", "", "Source session id")
	cmd.Flags().StringVar(&targetProfile, "profile", "", "Target profile name")
	cmd.Flags().StringVar(&workspaceID, "workspace", "", "Target workspace id or name (defaults to active workspace)")
	cmd.Flags().StringArrayVar(&excerptIDs, "excerpt", nil, "Context excerpt id to attach as visible context")
	cmd.Flags().StringVar(&messageBody, "message", "", "Visible task message")
	return cmd
}

func newSessionFanoutCmd() *cobra.Command {
	var fromSessionID, messageBody, workspaceID string
	var targetSessionIDs, targetProfiles, excerptIDs []string
	cmd := &cobra.Command{Use: "fanout", Short: "Fan out messages or calls from a session", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		fromSessionID = strings.TrimSpace(fromSessionID)
		messageBody = strings.TrimSpace(messageBody)
		if fromSessionID == "" || messageBody == "" {
			return userFacingError{message: "--from and --message are required"}
		}
		trimmedProfiles := make([]string, 0, len(targetProfiles))
		for _, profile := range targetProfiles {
			if strings.TrimSpace(profile) != "" {
				trimmedProfiles = append(trimmedProfiles, strings.TrimSpace(profile))
			}
		}
		if len(targetSessionIDs) > 1 {
			return userFacingError{message: fmt.Sprintf("at most one --to-session, got %d", len(targetSessionIDs))}
		}
		if len(targetSessionIDs) > 0 && len(trimmedProfiles) > 0 {
			return userFacingError{message: "use either --to-session or --to-profile, not both"}
		}
		if len(trimmedProfiles) == 0 && len(targetSessionIDs) != 1 {
			return userFacingError{message: "provide one --to-session or one or more --to-profile"}
		}
		targetSessionID := ""
		if len(trimmedProfiles) == 0 {
			targetSessionID = strings.TrimSpace(targetSessionIDs[0])
			if targetSessionID == "" {
				return userFacingError{message: "--to-session must be non-empty"}
			}
		}
		cfg, err := configuredDaemonConfig()
		if err != nil {
			return err
		}
		if err := sessionEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		workflowCtx, err := workflowContextResolver.Resolve(ctx, cfg.Daemon.SocketPath, workspaceID)
		if err != nil {
			return err
		}
		resp, err := sessionFanoutRPC(ctx, cfg.Daemon.SocketPath, daemon.AgentMessageSendRequest{WorkspaceID: workflowCtx.WorkspaceID, SourceSessionID: fromSessionID, TargetSessionID: targetSessionID, TargetProfileIDs: trimmedProfiles, Body: messageBody, ContextExcerptIDs: excerptIDs})
		if err != nil {
			return mapWorkspaceRPCError(err)
		}
		if resp.FanoutGroupID != "" {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Fanout group: %s\n", resp.FanoutGroupID); err != nil {
				return err
			}
			for _, member := range resp.FanoutMembers {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Worker: %s\t%s\t%s\n", member.FanoutMemberID, member.TargetProfileID, member.Session.SessionID); err != nil {
					return err
				}
			}
			return nil
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Fanout message: %s\n", resp.AgentMessage.AgentMessageID)
		return err
	}}
	cmd.Flags().StringVar(&fromSessionID, "from", "", "Source session id")
	cmd.Flags().StringArrayVar(&targetSessionIDs, "to-session", nil, "Target session id")
	cmd.Flags().StringVar(&workspaceID, "workspace", "", "Target workspace id or name (defaults to active workspace)")
	cmd.Flags().StringArrayVar(&targetProfiles, "to-profile", nil, "Target profile name for an ephemeral call")
	cmd.Flags().StringArrayVar(&excerptIDs, "excerpt", nil, "Context excerpt id to attach as visible context")
	cmd.Flags().StringVar(&messageBody, "message", "", "Visible task message")
	return cmd
}
