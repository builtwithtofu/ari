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
	finalResponseEnsureDaemonRunning = ensureDaemonRunning
	finalResponseGetRPC              = func(ctx context.Context, socketPath string, req daemon.FinalResponseGetRequest) (daemon.FinalResponseResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.FinalResponseResponse
		if err := rpcClient.Call(ctx, "final_response.get", req, &response); err != nil {
			return daemon.FinalResponseResponse{}, err
		}
		return response, nil
	}
	finalResponseListRPC = func(ctx context.Context, socketPath string, req daemon.FinalResponseListRequest) (daemon.FinalResponseListResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.FinalResponseListResponse
		if err := rpcClient.Call(ctx, "final_response.list", req, &response); err != nil {
			return daemon.FinalResponseListResponse{}, err
		}
		return response, nil
	}
)

func NewFinalResponseCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "final-response", Short: "Read final response artifacts"}
	cmd.AddCommand(newFinalResponseShowCmd())
	cmd.AddCommand(newFinalResponseListCmd())
	cmd.AddCommand(newFinalResponseExportCmd())
	return cmd
}

func newFinalResponseShowCmd() *cobra.Command {
	var runID string
	cmd := &cobra.Command{
		Use:   "show <final-response-id>",
		Short: "Show a final response artifact",
		Args: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(runID) != "" && len(args) == 0 {
				return nil
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := finalResponseLookup(cmd, args, runID)
			if err != nil {
				return err
			}
			return printFinalResponse(cmd, resp)
		},
	}
	cmd.Flags().StringVar(&runID, "run-id", "", "Look up the final response by agent run id")
	return cmd
}

func newFinalResponseExportCmd() *cobra.Command {
	var runID string
	cmd := &cobra.Command{
		Use:   "export <final-response-id>",
		Short: "Print only final response text for sharing",
		Args: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(runID) != "" && len(args) == 0 {
				return nil
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := finalResponseLookup(cmd, args, runID)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), resp.Text)
			return err
		},
	}
	cmd.Flags().StringVar(&runID, "run-id", "", "Look up the final response by agent run id")
	return cmd
}

func newFinalResponseListCmd() *cobra.Command {
	var workspaceID string
	cmd := &cobra.Command{
		Use:   "list --workspace-id <workspace-id>",
		Short: "List final responses for a workspace",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = args
			workspaceID = strings.TrimSpace(workspaceID)
			if workspaceID == "" {
				return userFacingError{message: "Provide --workspace-id"}
			}
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if err := finalResponseEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			resp, err := finalResponseListRPC(ctx, cfg.Daemon.SocketPath, daemon.FinalResponseListRequest{WorkspaceID: workspaceID})
			if err != nil {
				return err
			}
			for _, response := range resp.FinalResponses {
				if err := printFinalResponse(cmd, response); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&workspaceID, "workspace-id", "", "Workspace id to list final responses for")
	return cmd
}

func finalResponseLookup(cmd *cobra.Command, args []string, runID string) (daemon.FinalResponseResponse, error) {
	cfg, err := configuredDaemonConfig()
	if err != nil {
		return daemon.FinalResponseResponse{}, err
	}
	if err := finalResponseEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
		return daemon.FinalResponseResponse{}, err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
	defer cancel()
	req := daemon.FinalResponseGetRequest{RunID: strings.TrimSpace(runID)}
	if req.RunID == "" && len(args) > 0 {
		req.FinalResponseID = strings.TrimSpace(args[0])
	}
	return finalResponseGetRPC(ctx, cfg.Daemon.SocketPath, req)
}

func printFinalResponse(cmd *cobra.Command, response daemon.FinalResponseResponse) error {
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "final_response\tid=%s\trun=%s\tstatus=%s\n", response.FinalResponseID, response.RunID, response.Status); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "workspace=%s\ttask=%s\tcontext_packet=%s\n", response.WorkspaceID, response.TaskID, response.ContextPacketID); err != nil {
		return err
	}
	if strings.TrimSpace(response.ProfileID) != "" {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "profile=%s\n", response.ProfileID); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "text=%s\n", response.Text); err != nil {
		return err
	}
	for _, link := range response.EvidenceLinks {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "evidence=%s:%s\n", link.Kind, link.ID); err != nil {
			return err
		}
	}
	return nil
}
