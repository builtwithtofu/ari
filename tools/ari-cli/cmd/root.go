package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var rootIsInteractiveTerminal = func(cmd *cobra.Command) bool {
	return isInteractiveTerminalWithOutput(cmd)
}

var rootConfiguredDaemonConfig = configuredDaemonConfig

var rootEnsureDaemonRunning = ensureDaemonRunning

var rootResolveWorkspaceFromCWD = resolveWorkspaceFromCWD

var rootAgentListRPC = agentListRPC

var rootRunInteractive = func(cmd *cobra.Command, _ []string) error {
	if cmd == nil {
		return fmt.Errorf("root command is required")
	}

	return cmd.Help()
}

var rootRunNonInteractive = func(cmd *cobra.Command, _ []string) error {
	if cmd == nil {
		return fmt.Errorf("root command is required")
	}

	cfg, err := rootConfiguredDaemonConfig()
	if err != nil {
		return err
	}

	if err := rootEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
	defer cancel()

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	workspace, err := rootResolveWorkspaceFromCWD(ctx, cfg.Daemon.SocketPath, cwd)
	if err != nil {
		if isWorkspaceCWDNoMatch(err) {
			_, writeErr := fmt.Fprintln(cmd.OutOrStdout(), "No workspace matches current directory.")
			if writeErr != nil {
				return writeErr
			}
			_, writeErr = fmt.Fprintln(cmd.OutOrStdout(), "Hint: Run `ari workspace create <name>` in this project.")
			return writeErr
		}
		return err
	}

	agents, err := rootAgentListRPC(ctx, cfg.Daemon.SocketPath, workspace.WorkspaceID)
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
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Agents: %d\n", len(agents.Agents)); err != nil {
		return err
	}

	return nil
}

func NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "ari",
		Short: "Ari daemon CLI",
		RunE: func(cmd *cobra.Command, args []string) error {
			if rootIsInteractiveTerminal(cmd) {
				return rootRunInteractive(cmd, args)
			}
			return rootRunNonInteractive(cmd, args)
		},
	}

	rootCmd.AddCommand(NewDaemonCmd())
	rootCmd.AddCommand(NewWorkspaceCmd())
	rootCmd.AddCommand(NewCommandCmd())
	rootCmd.AddCommand(NewAgentCmd())

	return rootCmd
}

func Execute() {
	if err := NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
