package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/client"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/config"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/spf13/cobra"
)

var (
	agentsReadActiveWorkspace   = config.ReadActiveWorkspace
	agentsEnsureDaemonRunning   = ensureDaemonRunning
	agentSessionConfigCreateRPC = func(ctx context.Context, socketPath string, req daemon.AgentSessionConfigCreateRequest) (daemon.AgentSessionConfigCreateResponse, error) {
		rpcClient := client.New(socketPath)
		var resp daemon.AgentSessionConfigCreateResponse
		if err := rpcClient.Call(ctx, "workspace.agent.create", req, &resp); err != nil {
			return daemon.AgentSessionConfigCreateResponse{}, err
		}
		return resp, nil
	}
	agentSessionConfigListRPC = func(ctx context.Context, socketPath, workspaceID string) (daemon.AgentSessionConfigListResponse, error) {
		rpcClient := client.New(socketPath)
		var resp daemon.AgentSessionConfigListResponse
		if err := rpcClient.Call(ctx, "workspace.agent.list", daemon.AgentSessionConfigListRequest{WorkspaceID: workspaceID}, &resp); err != nil {
			return daemon.AgentSessionConfigListResponse{}, err
		}
		return resp, nil
	}
	agentSessionConfigSessionRPC = func(ctx context.Context, socketPath string, req daemon.AgentSessionConfigSessionRequest) (daemon.AgentSessionConfigSessionResponse, error) {
		rpcClient := client.New(socketPath)
		var resp daemon.AgentSessionConfigSessionResponse
		if err := rpcClient.Call(ctx, "workspace.agent.run", req, &resp); err != nil {
			return daemon.AgentSessionConfigSessionResponse{}, err
		}
		return resp, nil
	}
	agentMessageSendRPC = func(ctx context.Context, socketPath string, req daemon.AgentMessageSendRequest) (daemon.AgentMessageSendResponse, error) {
		rpcClient := client.New(socketPath)
		var resp daemon.AgentMessageSendResponse
		if err := rpcClient.Call(ctx, "agent.message.send", req, &resp); err != nil {
			return daemon.AgentMessageSendResponse{}, err
		}
		return resp, nil
	}
	ephemeralAgentCallRPC = func(ctx context.Context, socketPath string, req daemon.EphemeralAgentCallRequest) (daemon.EphemeralAgentCallResponse, error) {
		rpcClient := client.New(socketPath)
		var resp daemon.EphemeralAgentCallResponse
		if err := rpcClient.Call(ctx, "agent.call.ephemeral", req, &resp); err != nil {
			return daemon.EphemeralAgentCallResponse{}, err
		}
		return resp, nil
	}
)

func NewAgentsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "agents", Short: "Manage workspace agents, sessions, and messages"}
	cmd.AddCommand(newAgentsCreateCmd(), newAgentsListCmd(), newAgentsRunCmd(), newAgentsSendCmd(), newAgentsCallCmd())
	return cmd
}

func agentsContext(cmd *cobra.Command) (*config.Config, context.Context, context.CancelFunc, string, error) {
	cfg, err := configuredDaemonConfig()
	if err != nil {
		return nil, nil, nil, "", err
	}
	if err := agentsEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
		return nil, nil, nil, "", err
	}
	workspaceID, err := agentsReadActiveWorkspace()
	if err != nil {
		return nil, nil, nil, "", err
	}
	if strings.TrimSpace(workspaceID) == "" {
		return nil, nil, nil, "", userFacingError{message: "No active workspace is set"}
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
	return cfg, ctx, cancel, strings.TrimSpace(workspaceID), nil
}

func newAgentsCreateCmd() *cobra.Command {
	var name, harness, model, prompt string
	cmd := &cobra.Command{Use: "create <agent-id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		cfg, ctx, cancel, ws, err := agentsContext(cmd)
		if err != nil {
			return err
		}
		defer cancel()
		resp, err := agentSessionConfigCreateRPC(ctx, cfg.Daemon.SocketPath, daemon.AgentSessionConfigCreateRequest{AgentID: strings.TrimSpace(args[0]), WorkspaceID: ws, Name: strings.TrimSpace(name), Harness: strings.TrimSpace(harness), Model: strings.TrimSpace(model), Prompt: strings.TrimSpace(prompt)})
		if err != nil {
			return mapSessionRPCError(err)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Agent created: %s\n", resp.Agent.AgentID)
		return err
	}}
	cmd.Flags().StringVar(&name, "name", "", "Agent display name")
	cmd.Flags().StringVar(&harness, "harness", "", "Harness")
	cmd.Flags().StringVar(&model, "model", "", "Model")
	cmd.Flags().StringVar(&prompt, "prompt", "", "Prompt")
	return cmd
}

func newAgentsListCmd() *cobra.Command {
	return &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		cfg, ctx, cancel, ws, err := agentsContext(cmd)
		if err != nil {
			return err
		}
		defer cancel()
		resp, err := agentSessionConfigListRPC(ctx, cfg.Daemon.SocketPath, ws)
		if err != nil {
			return mapSessionRPCError(err)
		}
		for _, a := range resp.Agents {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", a.AgentID, a.Name, a.Harness); err != nil {
				return err
			}
		}
		return nil
	}}
}

func newAgentsRunCmd() *cobra.Command {
	var sessionID, cwd string
	cmd := &cobra.Command{Use: "run <agent-id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		cfg, ctx, cancel, _, err := agentsContext(cmd)
		if err != nil {
			return err
		}
		defer cancel()
		resp, err := agentSessionConfigSessionRPC(ctx, cfg.Daemon.SocketPath, daemon.AgentSessionConfigSessionRequest{AgentID: strings.TrimSpace(args[0]), SessionID: strings.TrimSpace(sessionID), CWD: strings.TrimSpace(cwd)})
		if err != nil {
			return mapSessionRPCError(err)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Run record created: %s\n", resp.Run.SessionID)
		return err
	}}
	cmd.Flags().StringVar(&sessionID, "run-id", "", "Run id")
	cmd.Flags().StringVar(&cwd, "cwd", "", "Working directory")
	return cmd
}

func newAgentsSendCmd() *cobra.Command {
	var msg, id, targetSessionID, startSessionID string
	cmd := &cobra.Command{Use: "send <source-run-id> <target-agent-id>", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		cfg, ctx, cancel, _, err := agentsContext(cmd)
		if err != nil {
			return err
		}
		defer cancel()
		resp, err := agentMessageSendRPC(ctx, cfg.Daemon.SocketPath, daemon.AgentMessageSendRequest{AgentMessageID: strings.TrimSpace(id), SourceSessionID: strings.TrimSpace(args[0]), TargetAgentID: strings.TrimSpace(args[1]), TargetSessionID: strings.TrimSpace(targetSessionID), StartSessionID: strings.TrimSpace(startSessionID), Body: strings.TrimSpace(msg)})
		if err != nil {
			return mapSessionRPCError(err)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Message delivered: %s\n", resp.AgentMessage.AgentMessageID)
		return err
	}}
	cmd.Flags().StringVar(&msg, "message", "", "Message body")
	cmd.Flags().StringVar(&id, "message-id", "", "Message id")
	cmd.Flags().StringVar(&targetSessionID, "target-run-id", "", "Existing target run id")
	cmd.Flags().StringVar(&startSessionID, "start-run-id", "", "New target run id to create")
	return cmd
}

func newAgentsCallCmd() *cobra.Command {
	var msg, id string
	cmd := &cobra.Command{Use: "call <source-run-id> <target-agent-id>", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		cfg, ctx, cancel, _, err := agentsContext(cmd)
		if err != nil {
			return err
		}
		defer cancel()
		resp, err := ephemeralAgentCallRPC(ctx, cfg.Daemon.SocketPath, daemon.EphemeralAgentCallRequest{CallID: strings.TrimSpace(id), SourceSessionID: strings.TrimSpace(args[0]), TargetAgentID: strings.TrimSpace(args[1]), Body: strings.TrimSpace(msg)})
		if err != nil {
			return mapSessionRPCError(err)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Ephemeral run: %s\n", resp.Run.SessionID)
		return err
	}}
	cmd.Flags().StringVar(&msg, "message", "", "Message body")
	cmd.Flags().StringVar(&id, "call-id", "", "Call id")
	return cmd
}
