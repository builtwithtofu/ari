package cmd

import (
	"context"
	"errors"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
	"github.com/sourcegraph/jsonrpc2"
	"github.com/spf13/cobra"
)

func TestAgentAttachDetachViaCtrlBackslash(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolve := commandResolveSessionIdentifier
	originalAttach := agentAttachRPC
	originalSize := agentAttachTerminalSize

	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	agentAttachRPC = func(_ context.Context, _ string, _ daemon.AgentAttachRequest) (daemon.AgentAttachResponse, error) {
		return daemon.AgentAttachResponse{Token: "tok-1", Status: "pending"}, nil
	}
	agentAttachTerminalSize = func(_ *cobra.Command) (uint16, uint16) {
		return 120, 40
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		agentAttachRPC = originalAttach
		agentAttachTerminalSize = originalSize
	})

	out, err := executeRootCommandWithInput(string([]byte{0x1c}), "agent", "attach", "alpha", "claude")
	if err != nil {
		t.Fatalf("execute agent attach: %v", err)
	}

	if out != "Detached from agent \"claude\".\n" {
		t.Fatalf("attach output = %q, want %q", out, "Detached from agent \"claude\".\n")
	}
}

func TestAgentAttachDaemonDisconnectMessage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolve := commandResolveSessionIdentifier
	originalAttach := agentAttachRPC
	originalSize := agentAttachTerminalSize

	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	agentAttachRPC = func(_ context.Context, _ string, _ daemon.AgentAttachRequest) (daemon.AgentAttachResponse, error) {
		return daemon.AgentAttachResponse{}, errors.New("EOF")
	}
	agentAttachTerminalSize = func(_ *cobra.Command) (uint16, uint16) {
		return 120, 40
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		agentAttachRPC = originalAttach
		agentAttachTerminalSize = originalSize
	})

	_, err := executeRootCommand("agent", "attach", "alpha", "claude")
	if err == nil {
		t.Fatal("agent attach returned nil error on disconnect")
	}

	if err.Error() != "Daemon disconnected. Agent may still be running." {
		t.Fatalf("agent attach error = %q, want %q", err.Error(), "Daemon disconnected. Agent may still be running.")
	}
}

func TestAgentAttachStoppedAgentError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolve := commandResolveSessionIdentifier
	originalAttach := agentAttachRPC
	originalSize := agentAttachTerminalSize

	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	agentAttachRPC = func(_ context.Context, _ string, _ daemon.AgentAttachRequest) (daemon.AgentAttachResponse, error) {
		return daemon.AgentAttachResponse{}, &jsonrpc2.Error{Code: int64(rpc.AgentNotRunning), Message: "agent is not running"}
	}
	agentAttachTerminalSize = func(_ *cobra.Command) (uint16, uint16) {
		return 120, 40
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		agentAttachRPC = originalAttach
		agentAttachTerminalSize = originalSize
	})

	_, err := executeRootCommand("agent", "attach", "alpha", "claude")
	if err == nil {
		t.Fatal("agent attach returned nil error for stopped agent")
	}
	if err.Error() != "Agent is not running" {
		t.Fatalf("agent attach error = %q, want %q", err.Error(), "Agent is not running")
	}
}

func TestAgentAttachActiveWriterError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolve := commandResolveSessionIdentifier
	originalAttach := agentAttachRPC
	originalSize := agentAttachTerminalSize

	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	agentAttachRPC = func(_ context.Context, _ string, _ daemon.AgentAttachRequest) (daemon.AgentAttachResponse, error) {
		return daemon.AgentAttachResponse{}, &jsonrpc2.Error{Code: int64(rpc.AgentAlreadyAttached), Message: "agent already has an active attach session"}
	}
	agentAttachTerminalSize = func(_ *cobra.Command) (uint16, uint16) {
		return 120, 40
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		agentAttachRPC = originalAttach
		agentAttachTerminalSize = originalSize
	})

	_, err := executeRootCommand("agent", "attach", "alpha", "claude")
	if err == nil {
		t.Fatal("agent attach returned nil error for active writer")
	}
	if err.Error() != "Agent already has an active attach session" {
		t.Fatalf("agent attach error = %q, want %q", err.Error(), "Agent already has an active attach session")
	}
}
