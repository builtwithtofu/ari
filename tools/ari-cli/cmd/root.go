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
	dashboardRPC            func(context.Context, string, string) (daemon.DashboardGetResponse, error)
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
	dashboardRPC: func(ctx context.Context, socketPath, cwd string) (daemon.DashboardGetResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.DashboardGetResponse
		if err := rpcClient.Call(ctx, "dashboard.get", daemon.DashboardGetRequest{CWD: cwd}, &response); err != nil {
			return daemon.DashboardGetResponse{}, err
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

	dashboard, err := rootDeps.dashboardRPC(ctx, cfg.Daemon.SocketPath, cwd)
	if err != nil {
		return mapSessionRPCError(err)
	}
	return renderDashboard(cmd, dashboard)
}

func renderDashboard(cmd *cobra.Command, dashboard daemon.DashboardGetResponse) error {
	activity := dashboard.Activity
	if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Ari workspace dashboard"); err != nil {
		return err
	}
	workspaceName := activity.WorkspaceName
	if workspaceName == "" {
		workspaceName = activity.WorkspaceID
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Active workspace: %s\n", workspaceName); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "ID: %s\n", activity.WorkspaceID); err != nil {
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
	for _, action := range dashboard.ResumeActions {
		label := action.Label
		if label == "" {
			label = action.SourceID
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Resume: %s %s\n", action.Kind, label); err != nil {
			return err
		}
	}
	for _, membership := range dashboard.CWDMemberships {
		if membership.Active {
			continue
		}
		name := membership.Name
		if name == "" {
			name = membership.WorkspaceID
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "CWD workspace: %s\n", name); err != nil {
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
	rootCmd.AddCommand(NewInitCmd())
	rootCmd.AddCommand(NewWorkspaceCmd())
	rootCmd.AddCommand(NewCommandCmd())
	rootCmd.AddCommand(NewExecCmd())
	rootCmd.AddCommand(NewAgentCmd())
	rootCmd.AddCommand(NewProfileCmd())
	rootCmd.AddCommand(NewFinalResponseCmd())
	rootCmd.AddCommand(NewTelemetryCmd())
	rootCmd.AddCommand(NewAuthCmd())
	rootCmd.AddCommand(newStatusCmd())
	rootCmd.AddCommand(NewAPICmd())

	return rootCmd
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show Ari dashboard status",
		Args:  cobra.NoArgs,
		RunE:  rootRunNonInteractive,
	}
}

func Execute() {
	if err := NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
