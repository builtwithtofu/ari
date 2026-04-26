package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/client"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/config"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/spf13/cobra"
)

type rootRunDeps struct {
	isInteractiveTerminal   func(*cobra.Command) bool
	configuredDaemonConfig  func() (*config.Config, error)
	ensureDaemonRunning     func(context.Context, *config.Config) error
	resolveWorkspaceFromCWD func(context.Context, string, string) (daemon.WorkspaceGetResponse, error)
	agentListRPC            func(context.Context, string, string) (daemon.AgentListResponse, error)
	workspaceActivityRPC    func(context.Context, string, string) (daemon.WorkspaceActivityResponse, error)
	runWorkspaceAttach      func(*cobra.Command, []string) error
}

var rootDeps = rootRunDeps{
	isInteractiveTerminal: func(cmd *cobra.Command) bool {
		return isInteractiveTerminalWithOutput(cmd)
	},
	configuredDaemonConfig:  configuredDaemonConfig,
	ensureDaemonRunning:     ensureDaemonRunning,
	resolveWorkspaceFromCWD: resolveWorkspaceFromCWD,
	agentListRPC:            agentListRPC,
	workspaceActivityRPC: func(ctx context.Context, socketPath, workspaceID string) (daemon.WorkspaceActivityResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.WorkspaceActivityResponse
		if err := rpcClient.Call(ctx, "workspace.activity", daemon.WorkspaceActivityRequest{WorkspaceID: workspaceID}, &response); err != nil {
			return daemon.WorkspaceActivityResponse{}, err
		}
		return response, nil
	},
	runWorkspaceAttach: runWorkspaceAttachEntrypoint,
}

var rootRunInteractive = func(cmd *cobra.Command, _ []string) error {
	if cmd == nil {
		return fmt.Errorf("root command is required")
	}

	return rootDeps.runWorkspaceAttach(cmd, nil)
}

var rootRunNonInteractive = func(cmd *cobra.Command, _ []string) error {
	if cmd == nil {
		return fmt.Errorf("root command is required")
	}

	cfg, err := rootDeps.configuredDaemonConfig()
	if err != nil {
		return err
	}

	if err := rootDeps.ensureDaemonRunning(cmd.Context(), cfg); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
	defer cancel()

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	workspace, err := rootDeps.resolveWorkspaceFromCWD(ctx, cfg.Daemon.SocketPath, cwd)
	if err != nil {
		if isWorkspaceCWDNoMatch(err) {
			_, writeErr := fmt.Fprintln(cmd.OutOrStdout(), "No workspace matches current directory.")
			if writeErr != nil {
				return writeErr
			}
			_, writeErr = fmt.Fprintln(cmd.OutOrStdout(), "Hint: Run `ari workspace create <name>` in this project.")
			if writeErr != nil {
				return writeErr
			}
			return userFacingError{message: "No workspace matches current directory"}
		}
		return err
	}

	activity, err := rootDeps.workspaceActivityRPC(ctx, cfg.Daemon.SocketPath, workspace.WorkspaceID)
	if err != nil {
		return mapSessionRPCError(err)
	}

	if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Ari workspace dashboard"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Workspace: %s\n", workspace.Name); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "ID: %s\n", workspace.WorkspaceID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Status: %s\n", workspace.Status); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Origin: %s\n", workspace.OriginRoot); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Agents: %d\n", len(activity.Agents)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "VCS: %s (%d changed files)\n", activity.VCS.Backend, activity.VCS.ChangedFiles); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Processes: %d\n", len(activity.Processes)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Attention: %s (%d items)\n", activity.Attention.Level, len(activity.Attention.Items)); err != nil {
		return err
	}
	if len(activity.Proofs) > 0 {
		proof := activity.Proofs[0]
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Latest proof: %s %s\n", proof.Status, proof.Command); err != nil {
			return err
		}
	}

	return nil
}

func NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "ari",
		Short: "Ari daemon CLI",
		RunE: func(cmd *cobra.Command, args []string) error {
			if rootDeps.isInteractiveTerminal(cmd) {
				return rootRunInteractive(cmd, args)
			}
			return rootRunNonInteractive(cmd, args)
		},
	}

	rootCmd.AddCommand(NewDaemonCmd())
	rootCmd.AddCommand(NewWorkspaceCmd())
	rootCmd.AddCommand(NewCommandCmd())
	rootCmd.AddCommand(NewExecCmd())
	rootCmd.AddCommand(NewAgentCmd())
	rootCmd.AddCommand(NewProfileCmd())
	rootCmd.AddCommand(NewFinalResponseCmd())

	return rootCmd
}

func Execute() {
	if err := NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
