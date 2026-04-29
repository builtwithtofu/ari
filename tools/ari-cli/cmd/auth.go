package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/client"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/spf13/cobra"
)

var (
	authEnsureDaemonRunning = ensureDaemonRunning
	authStatusRPC           = func(ctx context.Context, socketPath string, req daemon.HarnessAuthStatusRequest) (daemon.HarnessAuthStatusResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.HarnessAuthStatusResponse
		if err := rpcClient.Call(ctx, "auth.status", req, &response); err != nil {
			return daemon.HarnessAuthStatusResponse{}, err
		}
		return response, nil
	}
)

func NewAuthCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "Inspect provider-owned harness auth"}
	cmd.AddCommand(newAuthStatusCmd())
	return cmd
}

func newAuthStatusCmd() *cobra.Command {
	var workspaceID string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Summarize harness auth readiness without reading secrets",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = args
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if err := authEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()
			resp, err := authStatusRPC(ctx, cfg.Daemon.SocketPath, daemon.HarnessAuthStatusRequest{WorkspaceID: workspaceID})
			if err != nil {
				return err
			}
			for _, status := range resp.Statuses {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\tslot=%s\tsecrets=provider-owned\n", status.Harness, status.Status, status.AuthSlotID); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&workspaceID, "workspace-id", "", "Workspace id for provider-home context")
	return cmd
}
