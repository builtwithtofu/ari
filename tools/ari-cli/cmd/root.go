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
	dashboardRPC            func(context.Context, string, string) (daemon.DashboardGetResponse, error)
	timelineRPC             func(context.Context, string, string) (daemon.WorkspaceTimelineResponse, error)
}

var rootDeps = rootRunDeps{
	isInteractiveTerminal: func(cmd *cobra.Command) bool {
		return isInteractiveTerminalWithOutput(cmd)
	},
	configuredDaemonConfig:  configuredDaemonConfig,
	ensureDaemonRunning:     ensureDaemonRunning,
	resolveWorkspaceFromCWD: resolveWorkspaceFromCWD,
	dashboardRPC: func(ctx context.Context, socketPath, cwd string) (daemon.DashboardGetResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.DashboardGetResponse
		if err := rpcClient.Call(ctx, "dashboard.get", daemon.DashboardGetRequest{CWD: cwd}, &response); err != nil {
			return daemon.DashboardGetResponse{}, err
		}
		return response, nil
	},
	timelineRPC: func(ctx context.Context, socketPath, workspaceID string) (daemon.WorkspaceTimelineResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.WorkspaceTimelineResponse
		if err := rpcClient.Call(ctx, "workspace.timeline", daemon.WorkspaceTimelineRequest{WorkspaceID: workspaceID}, &response); err != nil {
			return daemon.WorkspaceTimelineResponse{}, err
		}
		return response, nil
	},
}

var rootRunInteractive = func(cmd *cobra.Command, _ []string) error {
	if cmd == nil {
		return fmt.Errorf("root command is required")
	}

	return rootRunNonInteractive(cmd, nil)
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
	status := dashboard.Status
	if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Ari workspace dashboard"); err != nil {
		return err
	}
	workspaceName := status.WorkspaceName
	if workspaceName == "" {
		workspaceName = status.WorkspaceID
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Active workspace: %s\n", workspaceName); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "ID: %s\n", status.WorkspaceID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Sessions: %d\n", len(status.Sessions)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "VCS: %s (%d changed files)\n", status.VCS.Backend, status.VCS.ChangedFiles); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Processes: %d\n", len(status.Processes)); err != nil {
		return err
	}
	waitingSessions := 0
	runningEphemeral := 0
	for _, agent := range status.Sessions {
		if agent.Status == "waiting" {
			waitingSessions++
		}
		if agent.Usage == "ephemeral" && agent.Status == "running" {
			runningEphemeral++
		}
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Waiting sessions: %d\n", waitingSessions); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Running ephemeral calls: %d\n", runningEphemeral); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Context excerpts: %d\n", len(status.ContextExcerpts)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Session messages: %d\n", len(status.AgentMessages)); err != nil {
		return err
	}
	for _, excerpt := range status.ContextExcerpts {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Context excerpt: %s %s %d -> %s\n", excerpt.ContextExcerptID, excerpt.SelectorType, excerpt.ItemCount, excerpt.TargetAgentID); err != nil {
			return err
		}
	}
	for _, message := range status.AgentMessages {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Session message: %s %s %s -> %s\n", message.AgentMessageID, message.Status, message.SourceSessionID, message.TargetAgentID); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Attention: %s (%d items)\n", status.Attention.Level, len(status.Attention.Items)); err != nil {
		return err
	}
	if len(status.Proofs) > 0 {
		proof := status.Proofs[0]
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Latest proof: %s %s\n", proof.Status, proof.Command); err != nil {
			return err
		}
	}
	for _, action := range dashboard.ResumeActions {
		label := action.Label
		if label == "" {
			label = action.SourceID
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Resume session: %s\n", label); err != nil {
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
	rootCmd.AddCommand(NewSessionCmd())
	rootCmd.AddCommand(NewContextCmd())
	rootCmd.AddCommand(NewProfileCmd())
	rootCmd.AddCommand(NewFinalResponseCmd())
	rootCmd.AddCommand(NewTelemetryCmd())
	rootCmd.AddCommand(NewAuthCmd())
	rootCmd.AddCommand(newStatusCmd())
	rootCmd.AddCommand(newTimelineCmd())
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

func newTimelineCmd() *cobra.Command {
	var workspaceID string
	cmd := &cobra.Command{
		Use:   "timeline",
		Short: "Show Ari workspace timeline",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = args
			cfg, err := rootDeps.configuredDaemonConfig()
			if err != nil {
				return err
			}
			if err := rootDeps.ensureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			ws := workspaceID
			if ws == "" {
				dashboard, err := rootDeps.dashboardRPC(ctx, cfg.Daemon.SocketPath, "")
				if err != nil {
					return mapSessionRPCError(err)
				}
				ws = dashboard.EffectiveWorkspaceID
			}
			if ws == "" {
				return userFacingError{message: "No active workspace is set"}
			}
			resp, err := rootDeps.timelineRPC(ctx, cfg.Daemon.SocketPath, ws)
			if err != nil {
				return mapSessionRPCError(err)
			}
			for _, item := range resp.Items {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", item.ID, item.Kind, item.Status); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&workspaceID, "workspace", "", "Target workspace id or name (defaults to active workspace)")
	return cmd
}

func Execute() {
	if err := NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
