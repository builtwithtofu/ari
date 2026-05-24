package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/config"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
	"github.com/sourcegraph/jsonrpc2"
)

func TestSessionStartCallsPublicSessionStartRPC(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	originalResolver := workflowContextResolver
	originalEnsure := sessionEnsureDaemonRunning
	originalStart := sessionStartRPC
	workflowContextResolver = &WorkflowContextResolver{store: activeWorkspaceStoreFunc{read: func() (string, error) { return "ws-1", nil }}, resolveTarget: func(context.Context, string, string) (resolvedWorkspaceTarget, error) {
		return resolvedWorkspaceTarget{WorkspaceID: "ws-1", Workspace: &daemon.WorkspaceGetResponse{WorkspaceID: "ws-1"}}, nil
	}}
	sessionEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	sessionStartRPC = func(_ context.Context, _ string, req daemon.HarnessSessionStartRequest) (daemon.HarnessSessionStartResponse, error) {
		if req.WorkspaceID != "ws-1" || req.Profile != "executor" || req.SessionID != "executor-main" || req.Message != "Start phase 1" || req.Prompt != "replacement behavior" {
			t.Fatalf("session.start request = %#v", req)
		}
		return daemon.HarnessSessionStartResponse{Run: daemon.HarnessSession{HarnessSessionID: req.SessionID, SessionID: req.SessionID, WorkspaceID: req.WorkspaceID, Status: "waiting"}}, nil
	}
	t.Cleanup(func() {
		workflowContextResolver = originalResolver
		sessionEnsureDaemonRunning = originalEnsure
		sessionStartRPC = originalStart
	})

	out, err := executeRootCommand("session", "start", "executor", "--session", "executor-main", "--message", "Start phase 1", "--prompt", "replacement behavior")
	if err != nil {
		t.Fatalf("session start returned error: %v", err)
	}
	if !strings.Contains(out, "Session started: executor-main") {
		t.Fatalf("session start output = %q, want stable session id", out)
	}
}

func TestSessionStartResolvesWorkspaceNameBeforeRPC(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	originalEnsure := sessionEnsureDaemonRunning
	originalStart := sessionStartRPC
	originalGet := workspaceGetRPC
	originalList := workspaceListRPC
	originalResolve := workspaceResolveRPC
	sessionEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	workspaceResolveRPC = func(context.Context, string, daemon.WorkspaceResolveRequest) (daemon.WorkspaceResolveResponse, error) {
		return daemon.WorkspaceResolveResponse{Workspace: daemon.WorkspaceGetResponse{WorkspaceID: "ws-1", Name: "app"}}, nil
	}
	workspaceGetRPC = func(context.Context, string, string) (daemon.WorkspaceGetResponse, error) {
		return daemon.WorkspaceGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "session not found"}
	}
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{Workspaces: []daemon.WorkspaceSummary{{WorkspaceID: "ws-1", Name: "app"}}}, nil
	}
	sessionStartRPC = func(_ context.Context, _ string, req daemon.HarnessSessionStartRequest) (daemon.HarnessSessionStartResponse, error) {
		if req.WorkspaceID != "ws-1" {
			t.Fatalf("session.start workspace_id = %q, want resolved id ws-1", req.WorkspaceID)
		}
		return daemon.HarnessSessionStartResponse{Run: daemon.HarnessSession{HarnessSessionID: "executor-main", SessionID: "executor-main", WorkspaceID: req.WorkspaceID, Status: "waiting"}}, nil
	}
	t.Cleanup(func() {
		sessionEnsureDaemonRunning = originalEnsure
		sessionStartRPC = originalStart
		workspaceGetRPC = originalGet
		workspaceListRPC = originalList
		workspaceResolveRPC = originalResolve
	})

	if _, err := executeRootCommand("session", "start", "executor", "--workspace", "app", "--session", "executor-main"); err != nil {
		t.Fatalf("session start returned error: %v", err)
	}
}

func TestSessionStartPromptFileSuppliesReplacementPrompt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	promptPath := filepath.Join(t.TempDir(), "prompt.md")
	if err := os.WriteFile(promptPath, []byte("file behavior\n"), 0o644); err != nil {
		t.Fatalf("write prompt file: %v", err)
	}
	originalResolver := workflowContextResolver
	originalEnsure := sessionEnsureDaemonRunning
	originalStart := sessionStartRPC
	workflowContextResolver = &WorkflowContextResolver{store: activeWorkspaceStoreFunc{read: func() (string, error) { return "ws-1", nil }}, resolveTarget: func(context.Context, string, string) (resolvedWorkspaceTarget, error) {
		return resolvedWorkspaceTarget{WorkspaceID: "ws-1", Workspace: &daemon.WorkspaceGetResponse{WorkspaceID: "ws-1"}}, nil
	}}
	sessionEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	sessionStartRPC = func(_ context.Context, _ string, req daemon.HarnessSessionStartRequest) (daemon.HarnessSessionStartResponse, error) {
		if req.Prompt != "file behavior\n" {
			t.Fatalf("session.start prompt = %q, want file contents", req.Prompt)
		}
		return daemon.HarnessSessionStartResponse{Run: daemon.HarnessSession{HarnessSessionID: "executor-main", SessionID: "executor-main", WorkspaceID: req.WorkspaceID, Status: "waiting"}}, nil
	}
	t.Cleanup(func() {
		workflowContextResolver = originalResolver
		sessionEnsureDaemonRunning = originalEnsure
		sessionStartRPC = originalStart
	})

	if _, err := executeRootCommand("session", "start", "executor", "--session", "executor-main", "--prompt-file", promptPath); err != nil {
		t.Fatalf("session start returned error: %v", err)
	}
}

func TestSessionListCallsPublicSessionListRPC(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	originalResolver := workflowContextResolver
	originalEnsure := sessionEnsureDaemonRunning
	originalList := sessionListRPC
	workflowContextResolver = &WorkflowContextResolver{store: activeWorkspaceStoreFunc{read: func() (string, error) { return "ws-1", nil }}, resolveTarget: func(context.Context, string, string) (resolvedWorkspaceTarget, error) {
		return resolvedWorkspaceTarget{WorkspaceID: "ws-1", Workspace: &daemon.WorkspaceGetResponse{WorkspaceID: "ws-1"}}, nil
	}}
	sessionEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	sessionListRPC = func(_ context.Context, _ string, req daemon.SessionListRequest) (daemon.SessionListResponse, error) {
		if req.WorkspaceID != "ws-1" {
			t.Fatalf("session.list request = %#v", req)
		}
		return daemon.SessionListResponse{Sessions: []daemon.HarnessSession{{SessionID: "executor-main", Status: "running", Executor: "codex"}}}, nil
	}
	t.Cleanup(func() {
		workflowContextResolver = originalResolver
		sessionEnsureDaemonRunning = originalEnsure
		sessionListRPC = originalList
	})

	out, err := executeRootCommand("session", "list")
	if err != nil {
		t.Fatalf("session list returned error: %v", err)
	}
	if !strings.Contains(out, "executor-main") || !strings.Contains(out, "running") {
		t.Fatalf("session list output = %q, want listed session id and status", out)
	}
}

func TestSessionListResolvesWorkspaceNameBeforeRPC(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	originalEnsure := sessionEnsureDaemonRunning
	originalListSessions := sessionListRPC
	originalGet := workspaceGetRPC
	originalListWorkspaces := workspaceListRPC
	originalResolve := workspaceResolveRPC
	sessionEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	workspaceResolveRPC = func(context.Context, string, daemon.WorkspaceResolveRequest) (daemon.WorkspaceResolveResponse, error) {
		return daemon.WorkspaceResolveResponse{Workspace: daemon.WorkspaceGetResponse{WorkspaceID: "ws-1", Name: "app"}}, nil
	}
	workspaceGetRPC = func(context.Context, string, string) (daemon.WorkspaceGetResponse, error) {
		return daemon.WorkspaceGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "session not found"}
	}
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{Workspaces: []daemon.WorkspaceSummary{{WorkspaceID: "ws-1", Name: "app"}}}, nil
	}
	sessionListRPC = func(_ context.Context, _ string, req daemon.SessionListRequest) (daemon.SessionListResponse, error) {
		if req.WorkspaceID != "ws-1" {
			t.Fatalf("session.list workspace_id = %q, want resolved id ws-1", req.WorkspaceID)
		}
		return daemon.SessionListResponse{Sessions: []daemon.HarnessSession{{SessionID: "executor-main", Status: "running", Executor: "codex"}}}, nil
	}
	t.Cleanup(func() {
		sessionEnsureDaemonRunning = originalEnsure
		sessionListRPC = originalListSessions
		workspaceGetRPC = originalGet
		workspaceListRPC = originalListWorkspaces
		workspaceResolveRPC = originalResolve
	})

	if _, err := executeRootCommand("session", "list", "--workspace", "app"); err != nil {
		t.Fatalf("session list returned error: %v", err)
	}
}

func TestSessionShowCallsPublicSessionGetRPC(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	originalEnsure := sessionEnsureDaemonRunning
	originalGet := sessionGetRPC
	sessionEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	sessionGetRPC = func(_ context.Context, _ string, req daemon.SessionGetRequest) (daemon.SessionGetResponse, error) {
		if req.SessionID != "executor-main" {
			t.Fatalf("session.get request = %#v", req)
		}
		return daemon.SessionGetResponse{Session: daemon.HarnessSession{SessionID: "executor-main", Status: "running", Executor: "claude", WorkspaceID: "ws-1", ProviderSessionID: "provider-1", InvocationMode: "background", UsageBucket: "subscription"}}, nil
	}
	t.Cleanup(func() {
		sessionEnsureDaemonRunning = originalEnsure
		sessionGetRPC = originalGet
	})

	out, err := executeRootCommand("session", "show", "executor-main")
	if err != nil {
		t.Fatalf("session show returned error: %v", err)
	}
	for _, want := range []string{"executor-main", "running", "claude", "ws-1", "provider-1", "background", "subscription"} {
		if !strings.Contains(out, want) {
			t.Fatalf("session show output = %q, want %q", out, want)
		}
	}
}

func TestSessionLogsAndAttachCommandsUseNeutralRPCs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	originalEnsure := sessionEnsureDaemonRunning
	originalLogs := sessionLogsRPC
	originalAttach := sessionAttachRPC
	sessionEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	sessionLogsRPC = func(_ context.Context, _ string, req daemon.SessionLogsRequest) (daemon.SessionLogsResponse, error) {
		if req.SessionID != "executor-main" {
			t.Fatalf("session.logs request = %#v", req)
		}
		return daemon.SessionLogsResponse{SessionID: req.SessionID, ProviderSessionID: "provider-1", Command: []string{"claude", "logs", "provider-1"}, Output: "log line"}, nil
	}
	sessionAttachRPC = func(_ context.Context, _ string, req daemon.SessionAttachRequest) (daemon.SessionAttachResponse, error) {
		if req.SessionID != "executor-main" {
			t.Fatalf("session.attach request = %#v", req)
		}
		return daemon.SessionAttachResponse{SessionID: req.SessionID, ProviderSessionID: "provider-1", Command: []string{"claude", "attach", "provider-1"}}, nil
	}
	t.Cleanup(func() {
		sessionEnsureDaemonRunning = originalEnsure
		sessionLogsRPC = originalLogs
		sessionAttachRPC = originalAttach
	})

	logsOut, err := executeRootCommand("session", "logs", "executor-main")
	if err != nil {
		t.Fatalf("session logs returned error: %v", err)
	}
	if !strings.Contains(logsOut, "claude logs provider-1") || !strings.Contains(logsOut, "log line") {
		t.Fatalf("session logs output = %q, want command and logs", logsOut)
	}
	attachOut, err := executeRootCommand("session", "attach-command", "executor-main")
	if err != nil {
		t.Fatalf("session attach-command returned error: %v", err)
	}
	if strings.TrimSpace(attachOut) != "claude attach provider-1" {
		t.Fatalf("session attach-command output = %q", attachOut)
	}
}

func TestSessionHelpDescribesClaudeBackgroundDefault(t *testing.T) {
	out, err := executeRootCommand("session", "--help")
	if err != nil {
		t.Fatalf("session help returned error: %v", err)
	}
	for _, want := range []string{"Claude Code", "subscription-backed background", "headless claude -p", "opt-in API-credit"} {
		if !strings.Contains(out, want) {
			t.Fatalf("session help = %q, want %q", out, want)
		}
	}
}

func TestSessionMessageSendCallsPublicRPC(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	originalEnsure := sessionEnsureDaemonRunning
	originalSend := sessionMessageSendRPC
	originalResolve := workspaceResolveRPC
	sessionEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	workspaceResolveRPC = func(context.Context, string, daemon.WorkspaceResolveRequest) (daemon.WorkspaceResolveResponse, error) {
		return daemon.WorkspaceResolveResponse{Workspace: daemon.WorkspaceGetResponse{WorkspaceID: "ws-1", Name: "alpha"}}, nil
	}
	sessionMessageSendRPC = func(_ context.Context, _ string, req daemon.AgentMessageSendRequest) (daemon.AgentMessageSendResponse, error) {
		if req.WorkspaceID != "ws-1" || req.SourceSessionID != "planner-main" || req.TargetSessionID != "executor-main" || req.TargetAgentID != "" || strings.TrimSpace(req.AgentMessageID) != "" || req.Body != "Begin phase 1" || len(req.ContextExcerptIDs) != 1 || req.ContextExcerptIDs[0] != "plan-tail" {
			t.Fatalf("session.message.send request = %#v", req)
		}
		return daemon.AgentMessageSendResponse{AgentMessage: daemon.AgentMessageResponse{AgentMessageID: "dm-1", Status: "delivered", TargetSessionID: "executor-main"}}, nil
	}
	t.Cleanup(func() {
		sessionEnsureDaemonRunning = originalEnsure
		sessionMessageSendRPC = originalSend
		workspaceResolveRPC = originalResolve
	})

	out, err := executeRootCommand("session", "message", "send", "--workspace", "alpha", "--from", "planner-main", "--to", "executor-main", "--excerpt", "plan-tail", "--message", "Begin phase 1")
	if err != nil {
		t.Fatalf("session message send returned error: %v", err)
	}
	if !strings.Contains(out, "Message sent: dm-1") {
		t.Fatalf("session message send output = %q, want stable message id", out)
	}
}

func TestSessionCallCallsPublicEphemeralRPC(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	originalEnsure := sessionEnsureDaemonRunning
	originalCall := sessionCallRPC
	originalResolve := workspaceResolveRPC
	sessionEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	workspaceResolveRPC = func(context.Context, string, daemon.WorkspaceResolveRequest) (daemon.WorkspaceResolveResponse, error) {
		return daemon.WorkspaceResolveResponse{Workspace: daemon.WorkspaceGetResponse{WorkspaceID: "ws-1", Name: "alpha"}}, nil
	}
	sessionCallRPC = func(_ context.Context, _ string, req daemon.EphemeralCallRequest) (daemon.EphemeralCallResponse, error) {
		if req.WorkspaceID != "ws-1" || req.SourceSessionID != "planner-main" || req.TargetAgentID != "reviewer" || req.Body != "Please review" || len(req.ContextExcerptIDs) != 1 || req.ContextExcerptIDs[0] != "plan-tail" || strings.TrimSpace(req.CallID) != "" {
			t.Fatalf("session.call.ephemeral request = %#v", req)
		}
		return daemon.EphemeralCallResponse{Run: globaldb.HarnessSession{SessionID: "call-1-run", Status: "completed"}}, nil
	}
	t.Cleanup(func() {
		sessionEnsureDaemonRunning = originalEnsure
		sessionCallRPC = originalCall
		workspaceResolveRPC = originalResolve
	})

	out, err := executeRootCommand("session", "call", "--workspace", "alpha", "--from", "planner-main", "--profile", "reviewer", "--excerpt", "plan-tail", "--message", "Please review")
	if err != nil {
		t.Fatalf("session call returned error: %v", err)
	}
	if !strings.Contains(out, "Ephemeral call run: call-1-run") {
		t.Fatalf("session call output = %q, want stable call run id", out)
	}
}

func TestSessionFanoutCallsPublicRPC(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	originalEnsure := sessionEnsureDaemonRunning
	originalFanout := sessionFanoutRPC
	originalResolve := workspaceResolveRPC
	sessionEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	workspaceResolveRPC = func(context.Context, string, daemon.WorkspaceResolveRequest) (daemon.WorkspaceResolveResponse, error) {
		return daemon.WorkspaceResolveResponse{Workspace: daemon.WorkspaceGetResponse{WorkspaceID: "ws-1", Name: "alpha"}}, nil
	}
	sessionFanoutRPC = func(_ context.Context, _ string, req daemon.AgentMessageSendRequest) (daemon.AgentMessageSendResponse, error) {
		if req.WorkspaceID != "ws-1" || req.SourceSessionID != "planner-main" || req.TargetSessionID != "executor-main" || strings.TrimSpace(req.AgentMessageID) != "" || req.Body != "Please execute" || len(req.ContextExcerptIDs) != 1 || req.ContextExcerptIDs[0] != "plan-tail" || req.TargetAgentID != "" {
			t.Fatalf("session.fanout request = %#v", req)
		}
		return daemon.AgentMessageSendResponse{AgentMessage: daemon.AgentMessageResponse{AgentMessageID: "fanout-1", Status: "delivered", TargetSessionID: "executor-main"}}, nil
	}
	t.Cleanup(func() {
		sessionEnsureDaemonRunning = originalEnsure
		sessionFanoutRPC = originalFanout
		workspaceResolveRPC = originalResolve
	})

	out, err := executeRootCommand("session", "fanout", "--workspace", "alpha", "--from", "planner-main", "--to-session", "executor-main", "--excerpt", "plan-tail", "--message", "Please execute")
	if err != nil {
		t.Fatalf("session fanout returned error: %v", err)
	}
	if !strings.Contains(out, "Fanout message: fanout-1") {
		t.Fatalf("session fanout output = %q, want stable fanout message id", out)
	}
}
