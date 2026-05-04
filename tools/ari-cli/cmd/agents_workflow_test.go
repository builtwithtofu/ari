package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/config"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
)

func TestAgentsWorkflowCommandsCallDaemonAPIs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	originalReadActive := agentsReadActiveWorkspace
	originalEnsure := agentsEnsureDaemonRunning
	originalCreate := agentSessionConfigCreateRPC
	originalList := agentSessionConfigListRPC
	originalRun := agentSessionConfigSessionRPC
	originalSend := agentMessageSendRPC
	originalCall := ephemeralAgentCallRPC
	agentsReadActiveWorkspace = func() (string, error) { return "ws-1", nil }
	agentsEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	calls := []string{}
	agentSessionConfigCreateRPC = func(_ context.Context, _ string, req daemon.AgentSessionConfigCreateRequest) (daemon.AgentSessionConfigCreateResponse, error) {
		calls = append(calls, "workspace.agent.create")
		if req.WorkspaceID != "ws-1" || req.AgentID != "planner" || req.Name != "Planner" || req.Harness != "codex" || req.Model != "gpt-5" || req.Prompt != "plan" {
			t.Fatalf("create request = %#v", req)
		}
		return daemon.AgentSessionConfigCreateResponse{Agent: daemon.AgentSessionConfigResponse{AgentID: req.AgentID, Name: req.Name}}, nil
	}
	agentSessionConfigListRPC = func(_ context.Context, _ string, workspaceID string) (daemon.AgentSessionConfigListResponse, error) {
		calls = append(calls, "workspace.agent.list")
		if workspaceID != "ws-1" {
			t.Fatalf("list workspace = %q", workspaceID)
		}
		return daemon.AgentSessionConfigListResponse{Agents: []daemon.AgentSessionConfigResponse{{AgentID: "planner", Name: "Planner", Harness: "codex"}}}, nil
	}
	agentSessionConfigSessionRPC = func(_ context.Context, _ string, req daemon.AgentSessionConfigSessionRequest) (daemon.AgentSessionConfigSessionResponse, error) {
		calls = append(calls, "workspace.agent.run")
		if req.AgentID != "planner" || req.SessionID != "run-1" {
			t.Fatalf("run request = %#v", req)
		}
		return daemon.AgentSessionConfigSessionResponse{Run: globaldb.AgentSession{SessionID: req.SessionID, AgentID: req.AgentID, Status: "waiting"}}, nil
	}
	agentMessageSendRPC = func(_ context.Context, _ string, req daemon.AgentMessageSendRequest) (daemon.AgentMessageSendResponse, error) {
		calls = append(calls, "agent.message.send")
		if req.SourceSessionID != "run-1" || req.TargetAgentID != "reviewer" || req.Body != "review" || req.StartSessionID != "review-run" || len(req.ContextExcerptIDs) != 0 {
			t.Fatalf("send request = %#v", req)
		}
		return daemon.AgentMessageSendResponse{AgentMessage: daemon.AgentMessageResponse{AgentMessageID: req.AgentMessageID, Status: "delivered"}}, nil
	}
	ephemeralAgentCallRPC = func(_ context.Context, _ string, req daemon.EphemeralAgentCallRequest) (daemon.EphemeralAgentCallResponse, error) {
		calls = append(calls, "agent.call.ephemeral")
		if req.SourceSessionID != "run-1" || req.TargetAgentID != "librarian" || req.Body != "research" || len(req.ContextExcerptIDs) != 0 {
			t.Fatalf("call request = %#v", req)
		}
		return daemon.EphemeralAgentCallResponse{Run: globaldb.AgentSession{SessionID: "call-1-run", Usage: "ephemeral"}}, nil
	}
	t.Cleanup(func() {
		agentsReadActiveWorkspace = originalReadActive
		agentsEnsureDaemonRunning = originalEnsure
		agentSessionConfigCreateRPC = originalCreate
		agentSessionConfigListRPC = originalList
		agentSessionConfigSessionRPC = originalRun
		agentMessageSendRPC = originalSend
		ephemeralAgentCallRPC = originalCall
	})

	commands := [][]string{
		{"agents", "create", "planner", "--name", "Planner", "--harness", "codex", "--model", "gpt-5", "--prompt", "plan"},
		{"agents", "list"},
		{"agents", "run", "planner", "--run-id", "run-1"},
		{"agents", "send", "run-1", "reviewer", "--message", "review", "--message-id", "dm-1", "--start-run-id", "review-run"},
		{"agents", "call", "run-1", "librarian", "--message", "research", "--call-id", "call-1"},
	}
	for _, args := range commands {
		out, err := executeRootCommand(args...)
		if err != nil {
			t.Fatalf("execute %v: %v", args, err)
		}
		if strings.TrimSpace(out) == "" {
			t.Fatalf("execute %v produced empty output", args)
		}
	}
	for _, want := range []string{"workspace.agent.create", "workspace.agent.list", "workspace.agent.run", "agent.message.send", "agent.call.ephemeral"} {
		if !containsAgentWorkflowString(calls, want) {
			t.Fatalf("calls = %#v, want %s", calls, want)
		}
	}
}

func TestAgentsHelpUsesAgentMessageTerminology(t *testing.T) {
	for _, args := range [][]string{{"agents", "send", "--help"}, {"agents", "call", "--help"}} {
		out, err := executeRootCommand(args...)
		if err != nil {
			t.Fatalf("%v returned error: %v", args, err)
		}
		if strings.Contains(out, "Message excerpt") || strings.Contains(out, "message excerpt") || strings.Contains(out, "Excerpt id") || strings.Contains(out, "excerpt") || strings.Contains(out, "attach") {
			t.Fatalf("%v output = %q, want no excerpt/context-excerpt or attach terminology", args, out)
		}
	}
}

func TestAgentsHelpDoesNotExposeExcerptCommand(t *testing.T) {
	out, err := executeRootCommand("agents", "--help")
	if err != nil {
		t.Fatalf("agents --help returned error: %v", err)
	}
	if strings.Contains(out, "excerpt") || strings.Contains(out, "Excerpt") {
		t.Fatalf("agents help = %q, want no excerpt command terminology", out)
	}
}

func containsAgentWorkflowString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
