package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/config"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
	"github.com/sourcegraph/jsonrpc2"
	"github.com/spf13/cobra"
)

var oneOffCommandMaxDuration = 24 * time.Hour

type execWorkflow struct {
	ctx         context.Context
	cancel      context.CancelFunc
	cfg         *config.Config
	workflowCtx WorkflowContext
}

func startExecWorkflow(cmd *cobra.Command, workspaceRef string) (*execWorkflow, error) {
	if cmd == nil {
		return nil, fmt.Errorf("exec workflow: command is required")
	}

	cfg, err := configuredDaemonConfig()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(workspaceRef) == "" {
		if _, err := workflowContextResolver.ActiveWorkspaceID(); err != nil {
			return nil, err
		}
	}
	if err := commandEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
	workflowCtx, err := commandResolveWorkflowContext(ctx, cfg.Daemon.SocketPath, workspaceRef)
	if err != nil {
		cancel()
		return nil, err
	}
	if err := commandEnsureWorkspaceScope(workflowCtx.Workspace, workspaceRef); err != nil {
		cancel()
		return nil, err
	}

	return &execWorkflow{ctx: ctx, cancel: cancel, cfg: cfg, workflowCtx: workflowCtx}, nil
}

func (workflow *execWorkflow) listCommands() (daemon.CommandListResponse, error) {
	resp, err := commandListRPC(workflow.ctx, workflow.cfg.Daemon.SocketPath, workflow.workflowCtx.WorkspaceID)
	if err != nil {
		return daemon.CommandListResponse{}, mapCommandRPCError(err)
	}
	return resp, nil
}

func (workflow *execWorkflow) getCommand(commandID string) (daemon.CommandGetResponse, error) {
	resp, err := commandGetRPC(workflow.ctx, workflow.cfg.Daemon.SocketPath, workflow.workflowCtx.WorkspaceID, commandID)
	if err != nil {
		return daemon.CommandGetResponse{}, mapCommandRPCError(err)
	}
	return resp, nil
}

func (workflow *execWorkflow) commandOutput(commandID string) (daemon.CommandOutputResponse, error) {
	resp, err := commandOutputRPC(workflow.ctx, workflow.cfg.Daemon.SocketPath, workflow.workflowCtx.WorkspaceID, commandID)
	if err != nil {
		return daemon.CommandOutputResponse{}, mapCommandRPCError(err)
	}
	return resp, nil
}

func (workflow *execWorkflow) stopCommand(commandID string) (daemon.CommandStopResponse, error) {
	resp, err := commandStopRPC(workflow.ctx, workflow.cfg.Daemon.SocketPath, workflow.workflowCtx.WorkspaceID, commandID)
	if err != nil {
		return daemon.CommandStopResponse{}, mapCommandRPCError(err)
	}
	return resp, nil
}

func (workflow *execWorkflow) runOneOffCommand(cmd *cobra.Command, command string, args []string) error {
	if cmd == nil {
		return fmt.Errorf("exec run: command is required")
	}
	if workflow == nil || workflow.cfg == nil {
		return fmt.Errorf("exec run: workflow is required")
	}
	if strings.TrimSpace(workflow.workflowCtx.WorkspaceID) == "" {
		return userFacingError{message: "Workspace not found"}
	}
	if strings.TrimSpace(command) == "" {
		return userFacingError{message: "Command is required"}
	}

	parentCtx := cmd.Context()
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	runCtx, runCancel := context.WithTimeout(parentCtx, oneOffCommandMaxDuration)
	defer runCancel()

	startCtx, startCancel := context.WithTimeout(runCtx, 5*time.Second)
	defer startCancel()

	resp, err := commandRunRPC(startCtx, workflow.cfg.Daemon.SocketPath, daemon.CommandRunRequest{
		WorkspaceID: workflow.workflowCtx.WorkspaceID,
		Command:     command,
		Args:        args,
	})
	if err != nil {
		return mapCommandRPCError(err)
	}

	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Command started: %s\n", resp.CommandID); err != nil {
		return err
	}

	terminalState, err := workflow.waitForCommandCompletion(runCtx, resp.CommandID)
	if err != nil {
		return err
	}

	outputCtx, outputCancel := context.WithTimeout(runCtx, 5*time.Second)
	defer outputCancel()
	outputResp, err := commandOutputRPC(outputCtx, workflow.cfg.Daemon.SocketPath, workflow.workflowCtx.WorkspaceID, resp.CommandID)
	if err != nil {
		return mapCommandRPCError(err)
	}
	if strings.TrimSpace(outputResp.Output) != "" {
		_, err = fmt.Fprint(cmd.OutOrStdout(), outputResp.Output)
	} else {
		_, err = fmt.Fprintln(cmd.OutOrStdout(), "Command produced no output.")
	}
	if err != nil {
		return err
	}
	return commandTerminalError(terminalState)
}

func (workflow *execWorkflow) waitForCommandCompletion(ctx context.Context, commandID string) (daemon.CommandGetResponse, error) {
	if ctx == nil {
		return daemon.CommandGetResponse{}, fmt.Errorf("wait command completion: context is required")
	}
	if workflow == nil || workflow.cfg == nil {
		return daemon.CommandGetResponse{}, fmt.Errorf("wait command completion: workflow is required")
	}
	if strings.TrimSpace(workflow.cfg.Daemon.SocketPath) == "" {
		return daemon.CommandGetResponse{}, fmt.Errorf("wait command completion: socket path is required")
	}
	if strings.TrimSpace(workflow.workflowCtx.WorkspaceID) == "" {
		return daemon.CommandGetResponse{}, userFacingError{message: "Workspace not found"}
	}
	if strings.TrimSpace(commandID) == "" {
		return daemon.CommandGetResponse{}, userFacingError{message: "Command not found"}
	}

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		resp, err := commandGetRPC(ctx, workflow.cfg.Daemon.SocketPath, workflow.workflowCtx.WorkspaceID, commandID)
		if err != nil {
			return daemon.CommandGetResponse{}, mapCommandRPCError(err)
		}
		if !strings.EqualFold(strings.TrimSpace(resp.Status), "running") {
			return resp, nil
		}

		select {
		case <-ctx.Done():
			return daemon.CommandGetResponse{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func commandTerminalError(state daemon.CommandGetResponse) error {
	status := strings.ToLower(strings.TrimSpace(state.Status))
	if status == "running" {
		return userFacingError{message: "Command is still running"}
	}
	if status == "lost" {
		return userFacingError{message: "Command ended unexpectedly"}
	}
	if state.ExitCode != nil && *state.ExitCode != 0 {
		return userFacingError{message: fmt.Sprintf("Command exited with code %d", *state.ExitCode)}
	}
	return nil
}

func mapCommandRPCError(err error) error {
	if err == nil {
		return nil
	}
	if isDaemonUnavailable(err) {
		return userFacingError{message: notRunningMessage()}
	}
	if isPermissionDenied(err) {
		cfg, cfgErr := configuredDaemonConfig()
		if cfgErr != nil {
			return err
		}
		return socketPermissionError(cfg.Daemon.SocketPath)
	}
	if isTimeoutError(err) {
		return timeoutError()
	}

	var rpcErr *jsonrpc2.Error
	if errors.As(err, &rpcErr) {
		switch rpcErr.Code {
		case int64(rpc.SessionNotFound):
			return userFacingError{message: "Workspace not found"}
		case int64(rpc.CommandNotFound):
			return userFacingError{message: "Command not found"}
		case int64(rpc.InvalidParams):
			return userFacingError{message: rpcErr.Message}
		}
	}

	return err
}
