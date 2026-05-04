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
)

func TestSessionStartCallsPublicSessionStartRPC(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	originalReadActive := sessionReadActiveWorkspace
	originalEnsure := sessionEnsureDaemonRunning
	originalStart := sessionStartRPC
	sessionReadActiveWorkspace = func() (string, error) { return "ws-1", nil }
	sessionEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	sessionStartRPC = func(_ context.Context, _ string, req daemon.AgentSessionStartRequest) (daemon.AgentSessionStartResponse, error) {
		if req.WorkspaceID != "ws-1" || req.Profile != "executor" || req.SessionID != "executor-main" || req.Message != "Start phase 1" || req.Prompt != "replacement behavior" {
			t.Fatalf("session.start request = %#v", req)
		}
		return daemon.AgentSessionStartResponse{Run: daemon.AgentSession{AgentSessionID: req.SessionID, SessionID: req.SessionID, WorkspaceID: req.WorkspaceID, Status: "waiting"}}, nil
	}
	t.Cleanup(func() {
		sessionReadActiveWorkspace = originalReadActive
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

func TestSessionStartPromptFileSuppliesReplacementPrompt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	promptPath := filepath.Join(t.TempDir(), "prompt.md")
	if err := os.WriteFile(promptPath, []byte("file behavior\n"), 0o644); err != nil {
		t.Fatalf("write prompt file: %v", err)
	}
	originalReadActive := sessionReadActiveWorkspace
	originalEnsure := sessionEnsureDaemonRunning
	originalStart := sessionStartRPC
	sessionReadActiveWorkspace = func() (string, error) { return "ws-1", nil }
	sessionEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	sessionStartRPC = func(_ context.Context, _ string, req daemon.AgentSessionStartRequest) (daemon.AgentSessionStartResponse, error) {
		if req.Prompt != "file behavior\n" {
			t.Fatalf("session.start prompt = %q, want file contents", req.Prompt)
		}
		return daemon.AgentSessionStartResponse{Run: daemon.AgentSession{AgentSessionID: "executor-main", SessionID: "executor-main", WorkspaceID: req.WorkspaceID, Status: "waiting"}}, nil
	}
	t.Cleanup(func() {
		sessionReadActiveWorkspace = originalReadActive
		sessionEnsureDaemonRunning = originalEnsure
		sessionStartRPC = originalStart
	})

	if _, err := executeRootCommand("session", "start", "executor", "--session", "executor-main", "--prompt-file", promptPath); err != nil {
		t.Fatalf("session start returned error: %v", err)
	}
}

func TestSessionListCallsPublicSessionListRPC(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	originalReadActive := sessionReadActiveWorkspace
	originalEnsure := sessionEnsureDaemonRunning
	originalList := sessionListRPC
	sessionReadActiveWorkspace = func() (string, error) { return "ws-1", nil }
	sessionEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	sessionListRPC = func(_ context.Context, _ string, req daemon.SessionListRequest) (daemon.SessionListResponse, error) {
		if req.WorkspaceID != "ws-1" {
			t.Fatalf("session.list request = %#v", req)
		}
		return daemon.SessionListResponse{Sessions: []daemon.AgentSession{{SessionID: "executor-main", Status: "running", Executor: "codex"}}}, nil
	}
	t.Cleanup(func() {
		sessionReadActiveWorkspace = originalReadActive
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

func TestSessionShowCallsPublicSessionGetRPC(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	originalEnsure := sessionEnsureDaemonRunning
	originalGet := sessionGetRPC
	sessionEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	sessionGetRPC = func(_ context.Context, _ string, req daemon.SessionGetRequest) (daemon.SessionGetResponse, error) {
		if req.SessionID != "executor-main" {
			t.Fatalf("session.get request = %#v", req)
		}
		return daemon.SessionGetResponse{Session: daemon.AgentSession{SessionID: "executor-main", Status: "running", Executor: "codex", WorkspaceID: "ws-1"}}, nil
	}
	t.Cleanup(func() {
		sessionEnsureDaemonRunning = originalEnsure
		sessionGetRPC = originalGet
	})

	out, err := executeRootCommand("session", "show", "executor-main")
	if err != nil {
		t.Fatalf("session show returned error: %v", err)
	}
	for _, want := range []string{"executor-main", "running", "codex", "ws-1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("session show output = %q, want %q", out, want)
		}
	}
}

func TestSessionMessageSendCallsPublicRPC(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	originalEnsure := sessionEnsureDaemonRunning
	originalSend := sessionMessageSendRPC
	sessionEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	sessionMessageSendRPC = func(_ context.Context, _ string, req daemon.AgentMessageSendRequest) (daemon.AgentMessageSendResponse, error) {
		if req.SourceSessionID != "planner-main" || req.TargetSessionID != "executor-main" || req.TargetAgentID != "" || strings.TrimSpace(req.AgentMessageID) == "" || req.Body != "Begin phase 1" || len(req.ContextExcerptIDs) != 1 || req.ContextExcerptIDs[0] != "plan-tail" {
			t.Fatalf("session.message.send request = %#v", req)
		}
		return daemon.AgentMessageSendResponse{AgentMessage: daemon.AgentMessageResponse{AgentMessageID: "dm-1", Status: "delivered", TargetSessionID: "executor-main"}}, nil
	}
	t.Cleanup(func() {
		sessionEnsureDaemonRunning = originalEnsure
		sessionMessageSendRPC = originalSend
	})

	out, err := executeRootCommand("session", "message", "send", "--from", "planner-main", "--to", "executor-main", "--excerpt", "plan-tail", "--message", "Begin phase 1")
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
	sessionEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	sessionCallRPC = func(_ context.Context, _ string, req daemon.EphemeralAgentCallRequest) (daemon.EphemeralAgentCallResponse, error) {
		if req.SourceSessionID != "planner-main" || req.TargetAgentID != "reviewer" || req.Body != "Please review" || len(req.ContextExcerptIDs) != 1 || req.ContextExcerptIDs[0] != "plan-tail" || strings.TrimSpace(req.CallID) == "" {
			t.Fatalf("session.call.ephemeral request = %#v", req)
		}
		return daemon.EphemeralAgentCallResponse{Run: globaldb.AgentSession{SessionID: "call-1-run", Status: "completed"}}, nil
	}
	t.Cleanup(func() {
		sessionEnsureDaemonRunning = originalEnsure
		sessionCallRPC = originalCall
	})

	out, err := executeRootCommand("session", "call", "--from", "planner-main", "--profile", "reviewer", "--excerpt", "plan-tail", "--message", "Please review")
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
	sessionEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	sessionFanoutRPC = func(_ context.Context, _ string, req daemon.AgentMessageSendRequest) (daemon.AgentMessageSendResponse, error) {
		if req.SourceSessionID != "planner-main" || req.TargetSessionID != "executor-main" || strings.TrimSpace(req.AgentMessageID) == "" || req.Body != "Please execute" || len(req.ContextExcerptIDs) != 1 || req.ContextExcerptIDs[0] != "plan-tail" || req.TargetAgentID != "" {
			t.Fatalf("session.fanout request = %#v", req)
		}
		return daemon.AgentMessageSendResponse{AgentMessage: daemon.AgentMessageResponse{AgentMessageID: "fanout-1", Status: "delivered", TargetSessionID: "executor-main"}}, nil
	}
	t.Cleanup(func() {
		sessionEnsureDaemonRunning = originalEnsure
		sessionFanoutRPC = originalFanout
	})

	out, err := executeRootCommand("session", "fanout", "--from", "planner-main", "--to-session", "executor-main", "--excerpt", "plan-tail", "--message", "Please execute")
	if err != nil {
		t.Fatalf("session fanout returned error: %v", err)
	}
	if !strings.Contains(out, "Fanout message: fanout-1") {
		t.Fatalf("session fanout output = %q, want stable fanout message id", out)
	}
}
