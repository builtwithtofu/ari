package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/config"
	"github.com/spf13/cobra"
)

func newWorkspaceAttachCmd() *cobra.Command {
	var workspaceRef string

	cmd := &cobra.Command{
		Use:   "attach <agent-id>",
		Short: "Attach to a running agent in the workspace",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runWorkspaceAttachEntrypoint,
	}
	cmd.Flags().StringVar(&workspaceRef, "workspace", "", "Workspace id or name override (defaults to CWD match)")
	return cmd
}

func runWorkspaceAttachEntrypoint(cmd *cobra.Command, args []string) error {
	if cmd == nil {
		return fmt.Errorf("workspace attach: command is required")
	}
	if len(args) == 0 {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "Workspace attach needs an agent id; run `ari workspace attach <agent-id>` from the workspace folder or pass `--workspace <id-or-name>`.")
		return err
	}
	if len(args) > 1 {
		return userFacingError{message: "workspace attach accepts at most one agent id"}
	}

	cfg, err := configuredDaemonConfig()
	if err != nil {
		return err
	}
	if err := workspaceEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
		return err
	}

	runCtx := cmd.Context()
	if runCtx == nil {
		runCtx = context.Background()
	}
	runCtx, stopSignals := signal.NotifyContext(runCtx, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	terminalCleanup, err := agentAttachPrepareTerminalFn(cmd, runCtx)
	if err != nil {
		return err
	}
	terminalRestored := false
	restoreTerminal := func() {
		if terminalRestored {
			return
		}
		terminalRestored = true
		terminalCleanup()
	}
	defer restoreTerminal()

	target, err := resolveWorkspaceAttachTarget(runCtx, cfg, cmd)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Attaching to agent %q in workspace %s. Detach: Ctrl-\\.\n", strings.TrimSpace(args[0]), target.WorkspaceID); err != nil {
		return err
	}

	return runAgentAttachFlow(cmd, cfg, runCtx, restoreTerminal, target.WorkspaceID, strings.TrimSpace(args[0]))
}

func resolveWorkspaceAttachTarget(runCtx context.Context, cfg *config.Config, cmd *cobra.Command) (resolvedSessionTarget, error) {
	if runCtx == nil {
		return resolvedSessionTarget{}, fmt.Errorf("workspace attach: run context is required")
	}
	if cfg == nil {
		return resolvedSessionTarget{}, fmt.Errorf("workspace attach: config is required")
	}
	if cmd == nil {
		return resolvedSessionTarget{}, fmt.Errorf("workspace attach: command is required")
	}

	rpcCtx, cancel := context.WithTimeout(runCtx, 5*time.Second)
	defer cancel()

	workspaceRef, err := cmd.Flags().GetString("workspace")
	if err != nil {
		return resolvedSessionTarget{}, err
	}
	workspaceRef = strings.TrimSpace(workspaceRef)
	if workspaceRef != "" {
		target, err := commandResolveSessionTarget(rpcCtx, cfg.Daemon.SocketPath, workspaceRef)
		if err != nil {
			return resolvedSessionTarget{}, err
		}
		if err := agentEnsureWorkspaceScope(target.Session, workspaceRef); err != nil {
			return resolvedSessionTarget{}, err
		}
		return target, nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return resolvedSessionTarget{}, err
	}
	workspace, err := resolveWorkspaceFromCWD(rpcCtx, cfg.Daemon.SocketPath, cwd)
	if err != nil {
		return resolvedSessionTarget{}, err
	}
	resolved := workspace
	return resolvedSessionTarget{WorkspaceID: workspace.WorkspaceID, Session: &resolved}, nil
}
