package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/client"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/spf13/cobra"
)

var (
	contextEnsureDaemonRunning = ensureDaemonRunning
	contextTailRPC             = func(ctx context.Context, socketPath string, req daemon.ContextExcerptCreateFromTailRequest) (daemon.ContextExcerptResponse, error) {
		rpcClient := client.New(socketPath)
		var resp daemon.ContextExcerptResponse
		if err := rpcClient.Call(ctx, "context.excerpt.create_from_tail", req, &resp); err != nil {
			return daemon.ContextExcerptResponse{}, err
		}
		return resp, nil
	}
	contextGetRPC = func(ctx context.Context, socketPath string, req daemon.ContextExcerptGetRequest) (daemon.ContextExcerptResponse, error) {
		rpcClient := client.New(socketPath)
		var resp daemon.ContextExcerptResponse
		if err := rpcClient.Call(ctx, "context.excerpt.get", req, &resp); err != nil {
			return daemon.ContextExcerptResponse{}, err
		}
		return resp, nil
	}
	contextRangeRPC = func(ctx context.Context, socketPath string, req daemon.ContextExcerptCreateFromRangeRequest) (daemon.ContextExcerptResponse, error) {
		rpcClient := client.New(socketPath)
		var resp daemon.ContextExcerptResponse
		if err := rpcClient.Call(ctx, "context.excerpt.create_from_range", req, &resp); err != nil {
			return daemon.ContextExcerptResponse{}, err
		}
		return resp, nil
	}
	contextMessagesRPC = func(ctx context.Context, socketPath string, req daemon.ContextExcerptCreateFromExplicitIDsRequest) (daemon.ContextExcerptResponse, error) {
		rpcClient := client.New(socketPath)
		var resp daemon.ContextExcerptResponse
		if err := rpcClient.Call(ctx, "context.excerpt.create_from_explicit_ids", req, &resp); err != nil {
			return daemon.ContextExcerptResponse{}, err
		}
		return resp, nil
	}
)

func NewContextCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "context", Short: "Manage reusable session context excerpts"}
	cmd.AddCommand(newContextExcerptCmd())
	return cmd
}

func newContextExcerptCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "excerpt", Short: "Create and inspect bounded context excerpts"}

	var sourceSessionID, excerptID string
	var count int
	tail := &cobra.Command{Use: "tail", Short: "Create excerpt from last N run messages", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := configuredDaemonConfig()
		if err != nil {
			return err
		}
		if err := contextEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
			return err
		}
		sourceSessionID = strings.TrimSpace(sourceSessionID)
		excerptID = strings.TrimSpace(excerptID)
		if sourceSessionID == "" || excerptID == "" || count <= 0 {
			return userFacingError{message: "--session, --id, and --last (>0) are required"}
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		resp, err := contextTailRPC(ctx, cfg.Daemon.SocketPath, daemon.ContextExcerptCreateFromTailRequest{ContextExcerptID: excerptID, SourceSessionID: sourceSessionID, Count: count})
		if err != nil {
			return mapSessionRPCError(err)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Context excerpt created: %s\n", resp.ContextExcerptID)
		return err
	}}
	tail.Flags().StringVar(&sourceSessionID, "session", "", "Source session id or name")
	tail.Flags().IntVar(&count, "last", 0, "Number of most recent messages to include")
	tail.Flags().StringVar(&excerptID, "id", "", "Stable context excerpt id")

	var rangeSourceSessionID, rangeExcerptID string
	var startSequence, endSequence int
	rangeCmd := &cobra.Command{Use: "range", Short: "Create excerpt from inclusive run message range", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := configuredDaemonConfig()
		if err != nil {
			return err
		}
		if err := contextEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
			return err
		}
		rangeSourceSessionID = strings.TrimSpace(rangeSourceSessionID)
		rangeExcerptID = strings.TrimSpace(rangeExcerptID)
		if rangeSourceSessionID == "" || rangeExcerptID == "" || startSequence <= 0 || endSequence <= 0 {
			return userFacingError{message: "--session, --id, --start (>0), and --end (>0) are required"}
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		resp, err := contextRangeRPC(ctx, cfg.Daemon.SocketPath, daemon.ContextExcerptCreateFromRangeRequest{ContextExcerptID: rangeExcerptID, SourceSessionID: rangeSourceSessionID, StartSequence: startSequence, EndSequence: endSequence})
		if err != nil {
			return mapSessionRPCError(err)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Context excerpt created: %s\n", resp.ContextExcerptID)
		return err
	}}
	rangeCmd.Flags().StringVar(&rangeSourceSessionID, "session", "", "Source session id or name")
	rangeCmd.Flags().IntVar(&startSequence, "start", 0, "Inclusive starting message sequence")
	rangeCmd.Flags().IntVar(&endSequence, "end", 0, "Inclusive ending message sequence")
	rangeCmd.Flags().StringVar(&rangeExcerptID, "id", "", "Stable context excerpt id")

	var messageSourceSessionID, messageExcerptID string
	var messageIDs []string
	messages := &cobra.Command{Use: "messages", Short: "Create excerpt from explicit run message ids", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := configuredDaemonConfig()
		if err != nil {
			return err
		}
		if err := contextEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
			return err
		}
		messageSourceSessionID = strings.TrimSpace(messageSourceSessionID)
		messageExcerptID = strings.TrimSpace(messageExcerptID)
		if messageSourceSessionID == "" || messageExcerptID == "" || len(messageIDs) == 0 {
			return userFacingError{message: "--session, --id, and at least one --message-id are required"}
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		resp, err := contextMessagesRPC(ctx, cfg.Daemon.SocketPath, daemon.ContextExcerptCreateFromExplicitIDsRequest{ContextExcerptID: messageExcerptID, SourceSessionID: messageSourceSessionID, MessageIDs: messageIDs})
		if err != nil {
			return mapSessionRPCError(err)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Context excerpt created: %s\n", resp.ContextExcerptID)
		return err
	}}
	messages.Flags().StringVar(&messageSourceSessionID, "session", "", "Source session id or name")
	messages.Flags().StringArrayVar(&messageIDs, "message-id", nil, "Run message id to include")
	messages.Flags().StringVar(&messageExcerptID, "id", "", "Stable context excerpt id")

	show := &cobra.Command{Use: "show <excerpt>", Short: "Show context excerpt details", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := configuredDaemonConfig()
		if err != nil {
			return err
		}
		if err := contextEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		resp, err := contextGetRPC(ctx, cfg.Daemon.SocketPath, daemon.ContextExcerptGetRequest{ContextExcerptID: strings.TrimSpace(args[0])})
		if err != nil {
			return mapSessionRPCError(err)
		}
		for _, line := range []string{
			"Context excerpt: " + resp.ContextExcerptID,
			"Source session: " + resp.SourceSessionID,
			"Selector: " + resp.SelectorType,
			"Message: " + resp.AppendedMessage,
		} {
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), line); err != nil {
				return err
			}
		}
		return nil
	}}

	cmd.AddCommand(tail, rangeCmd, messages, show)
	return cmd
}
