package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/config"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
	"github.com/sourcegraph/jsonrpc2"
	"github.com/spf13/cobra"
)

var workspaceAttachResolveWorkspaceFromCWD = resolveWorkspaceFromCWD

func newWorkspaceAttachCmd() *cobra.Command {
	var workspaceRef string

	cmd := &cobra.Command{
		Use:   "attach [agent-id]",
		Short: "Attach to an agent in the workspace",
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

	agentID := strings.TrimSpace(strings.Join(args, ""))
	if agentID == "" {
		agentID, err = resolveDefaultWorkspaceAttachAgent(runCtx, cfg, target.WorkspaceID)
		if err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Attaching to agent %q in workspace %s. Detach: Ctrl-\\.\n", agentID, target.WorkspaceID); err != nil {
		return err
	}

	return runAgentAttachFlow(cmd, cfg, runCtx, restoreTerminal, target.WorkspaceID, agentID)
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

	workspaceRef := ""
	if cmd.Flags().Lookup("workspace") != nil {
		flagWorkspaceRef, flagErr := cmd.Flags().GetString("workspace")
		if flagErr != nil {
			return resolvedSessionTarget{}, flagErr
		}
		workspaceRef = flagWorkspaceRef
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
	workspace, err := workspaceAttachResolveWorkspaceFromCWD(rpcCtx, cfg.Daemon.SocketPath, cwd)
	if err != nil {
		return resolvedSessionTarget{}, err
	}
	resolved := workspace
	return resolvedSessionTarget{WorkspaceID: workspace.WorkspaceID, Session: &resolved}, nil
}

func resolveDefaultWorkspaceAttachAgent(runCtx context.Context, cfg *config.Config, workspaceID string) (string, error) {
	if runCtx == nil {
		return "", fmt.Errorf("workspace attach: run context is required")
	}
	if cfg == nil {
		return "", fmt.Errorf("workspace attach: config is required")
	}
	if strings.TrimSpace(workspaceID) == "" {
		return "", userFacingError{message: "Workspace not found"}
	}

	lookupCtx, cancel := context.WithTimeout(runCtx, 5*time.Second)
	defer cancel()

	list, err := agentListRPC(lookupCtx, cfg.Daemon.SocketPath, workspaceID)
	if err != nil {
		return "", mapAgentRPCError(err)
	}

	defaultHarness := strings.TrimSpace(cfg.DefaultHarness)
	runningAgentIDs := make([]string, 0)
	runningDefaultHarnessAgentIDs := make([]string, 0)
	for _, summary := range list.Agents {
		agentID := strings.TrimSpace(summary.AgentID)
		if agentID == "" {
			continue
		}

		details, getErr := agentGetRPC(lookupCtx, cfg.Daemon.SocketPath, workspaceID, agentID)
		if getErr != nil {
			if isAgentNotFoundRPCError(getErr) {
				continue
			}
			return "", mapAgentRPCError(getErr)
		}
		if !strings.EqualFold(strings.TrimSpace(details.Status), "running") {
			continue
		}

		runningAgentIDs = append(runningAgentIDs, agentID)
		if defaultHarness != "" && strings.EqualFold(strings.TrimSpace(details.Harness), defaultHarness) {
			runningDefaultHarnessAgentIDs = append(runningDefaultHarnessAgentIDs, agentID)
		}
	}

	if len(runningAgentIDs) == 1 {
		return runningAgentIDs[0], nil
	}

	if len(runningDefaultHarnessAgentIDs) > 0 {
		if len(runningDefaultHarnessAgentIDs) > 1 {
			sort.Strings(runningDefaultHarnessAgentIDs)
			return "", userFacingError{message: "Multiple running agents match default_harness; run `ari workspace attach <agent-id>`"}
		}
		return runningDefaultHarnessAgentIDs[0], nil
	}

	if len(runningAgentIDs) > 1 {
		sort.Strings(runningAgentIDs)
		return "", userFacingError{message: "Multiple running agents found; run `ari workspace attach <agent-id>`"}
	}

	if defaultHarness != "" {
		spawnResp, spawnErr := agentSpawnRPC(lookupCtx, cfg.Daemon.SocketPath, daemon.AgentSpawnRequest{
			WorkspaceID: workspaceID,
			Harness:     defaultHarness,
		})
		if spawnErr != nil {
			return "", mapAgentRPCError(spawnErr)
		}
		if strings.TrimSpace(spawnResp.AgentID) == "" {
			return "", userFacingError{message: "Default agent did not return an id"}
		}
		return strings.TrimSpace(spawnResp.AgentID), nil
	}

	return "", userFacingError{message: "No running agents found and no default_harness configured"}
}

func isAgentNotFoundRPCError(err error) bool {
	if err == nil {
		return false
	}
	var rpcErr *jsonrpc2.Error
	if !errors.As(err, &rpcErr) {
		return false
	}
	return rpcErr.Code == int64(rpc.AgentNotFound)
}
