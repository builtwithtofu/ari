package daemon

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestCommandEnvWithProjectionIsChildScoped(t *testing.T) {
	t.Setenv("ARI_TEST_DAEMON_ENV", "daemon")
	before := os.Getenv("ARI_TEST_PROJECTION_ONLY")

	env := commandEnvWithProjection(HarnessAuthProjectionPlan{Env: map[string]string{"ARI_TEST_PROJECTION_ONLY": "child", "ARI_TEST_DAEMON_ENV": "child-override"}})

	if os.Getenv("ARI_TEST_PROJECTION_ONLY") != before {
		t.Fatalf("daemon env was mutated: ARI_TEST_PROJECTION_ONLY=%q before=%q", os.Getenv("ARI_TEST_PROJECTION_ONLY"), before)
	}
	if os.Getenv("ARI_TEST_DAEMON_ENV") != "daemon" {
		t.Fatalf("daemon env was mutated: ARI_TEST_DAEMON_ENV=%q", os.Getenv("ARI_TEST_DAEMON_ENV"))
	}
	if !slices.Contains(env, "ARI_TEST_PROJECTION_ONLY=child") || !slices.Contains(env, "ARI_TEST_DAEMON_ENV=child-override") {
		t.Fatalf("env = %#v, want projected child-only values", env)
	}
}

func TestDefaultProjectionLeavesImplicitProcessEnvironment(t *testing.T) {
	if env := commandEnvWithProjection(HarnessAuthProjectionPlan{}); env != nil {
		t.Fatalf("env = %#v, want nil so default harness runs keep existing inheritance", env)
	}
}

func TestClaudeStartCarriesAuthProjectionToCommandOptions(t *testing.T) {
	runner := &fakeClaudeRunner{output: []byte(`{"result":"Done","session_id":"sess_123"}`)}
	executor := NewClaudeExecutorForTest(claudeExecutorOptions{Executable: "claude", Cwd: "/repo", RunCommand: runner.Run})
	projection := HarnessAuthProjectionPlan{Owner: HarnessAuthProjectionOwnerNative, Kind: HarnessAuthProjectionConfigRoot, Env: map[string]string{"CLAUDE_CONFIG_DIR": "/tmp/ari/claude-work"}}

	_, err := executor.Start(context.Background(), ExecutorStartRequest{WorkspaceID: "ws-1", ContextPacket: `{"context_packet_id":"ctx_123"}`, Options: []HarnessOption{WithInvocationMode(HarnessInvocationModeHeadless)}, AuthProjection: projection})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if runner.authProjection.Env["CLAUDE_CONFIG_DIR"] != "/tmp/ari/claude-work" || runner.authProjection.Kind != HarnessAuthProjectionConfigRoot {
		t.Fatalf("projection = %#v, want Claude config-root projection on command options", runner.authProjection)
	}
}

func TestOpenCodeStartCarriesAuthProjectionToCommandOptions(t *testing.T) {
	runner := &fakeOpenCodeRunner{output: []byte(strings.Join([]string{`{"type":"session.status","properties":{"sessionID":"sess_123","status":{"type":"busy"}}}`, `{"type":"session.status","properties":{"sessionID":"sess_123","status":{"type":"idle"}}}`}, "\n"))}
	executor := NewOpenCodeExecutorForTest(opencodeExecutorOptions{Executable: "opencode", Cwd: "/repo", RunCommand: runner.Run})
	projection := HarnessAuthProjectionPlan{Owner: HarnessAuthProjectionOwnerAri, Kind: HarnessAuthProjectionAuthContent, Env: map[string]string{"OPENCODE_AUTH_CONTENT": `{"provider":"test"}`}}

	_, err := executor.Start(context.Background(), ExecutorStartRequest{WorkspaceID: "ws-1", ContextPacket: `{"context_packet_id":"ctx_123"}`, AuthProjection: projection})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if runner.authProjection.Env["OPENCODE_AUTH_CONTENT"] == "" || runner.authProjection.Kind != HarnessAuthProjectionAuthContent {
		t.Fatalf("projection = %#v, want OpenCode auth-content projection on command options", runner.authProjection)
	}
}

func TestHarnessCallPassesAuthProjectionToExecutorStart(t *testing.T) {
	runner := &fakeOpenCodeRunner{output: []byte(strings.Join([]string{`{"type":"session.status","properties":{"sessionID":"sess_123","status":{"type":"busy"}}}`, `{"type":"session.status","properties":{"sessionID":"sess_123","status":{"type":"idle"}}}`}, "\n"))}
	executor := NewOpenCodeExecutorForTest(opencodeExecutorOptions{Executable: "opencode", Cwd: "/repo", RunCommand: runner.Run})
	projection := HarnessAuthProjectionPlan{Owner: HarnessAuthProjectionOwnerAri, Kind: HarnessAuthProjectionAuthContent, Env: map[string]string{"OPENCODE_AUTH_CONTENT": `{"provider":"test"}`}}

	call, err := NewHarnessSessionHarnessCall(ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1"}, nil)
	if err != nil {
		t.Fatalf("NewHarnessSessionHarnessCall returned error: %v", err)
	}
	call.AuthSlotID = "opencode-work"
	call.AuthProjection = projection
	call.Input = []byte(`{"context_packet_id":"ctx_123"}`)

	_, err = StartHarnessCallResult(context.Background(), executor, call)
	if err != nil {
		t.Fatalf("StartHarnessCallResult returned error: %v", err)
	}
	if runner.authProjection.Kind != HarnessAuthProjectionAuthContent || runner.authProjection.Env["OPENCODE_AUTH_CONTENT"] == "" {
		t.Fatalf("projection = %#v, want harness call projection passed to OpenCode executor", runner.authProjection)
	}
}

func TestGrokStartCarriesAuthProjectionToCommandOptions(t *testing.T) {
	runner := &fakeGrokRunner{output: []byte(`{"type":"end","stopReason":"EndTurn","sessionId":"grok-sess-1","requestId":"req-1"}`)}
	executor := NewGrokExecutorForTest(grokExecutorOptions{Executable: "grok", Cwd: "/repo", RunCommand: runner.Run})
	projection := HarnessAuthProjectionPlan{Owner: HarnessAuthProjectionOwnerNative, Kind: HarnessAuthProjectionConfigRoot, Env: map[string]string{"GROK_HOME": "/tmp/ari/grok-work"}}

	_, err := executor.Start(context.Background(), ExecutorStartRequest{WorkspaceID: "ws-1", ContextPacket: `{"context_packet_id":"ctx_123"}`, AuthProjection: projection})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if runner.authProjection.Env["GROK_HOME"] != "/tmp/ari/grok-work" || runner.authProjection.Kind != HarnessAuthProjectionConfigRoot {
		t.Fatalf("projection = %#v, want Grok home projection on command options", runner.authProjection)
	}
}

func TestCodexStartCarriesAuthProjectionToTransportOptions(t *testing.T) {
	transport := newFakeCodexTransport([]codexNotification{{Method: "turn/completed", Params: mustRawJSON(t, `{"threadId":"thr_123","turn":{"id":"turn_456","status":"completed"}}`)}})
	var captured codexExecutorOptions
	executor := NewCodexExecutorForTest(codexExecutorOptions{Executable: "codex", Cwd: "/repo", StartTransport: func(ctx context.Context, opts codexExecutorOptions) (codexTransport, error) {
		_ = ctx
		captured = opts
		return transport, nil
	}})
	projection := HarnessAuthProjectionPlan{Owner: HarnessAuthProjectionOwnerNative, Kind: HarnessAuthProjectionConfigRoot, Env: map[string]string{"CODEX_HOME": "/tmp/ari/codex-work"}}

	_, err := executor.Start(context.Background(), ExecutorStartRequest{WorkspaceID: "ws-1", ContextPacket: `{"context_packet_id":"ctx_123"}`, AuthProjection: projection})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if captured.AuthProjection.Env["CODEX_HOME"] != "/tmp/ari/codex-work" || captured.AuthProjection.Kind != HarnessAuthProjectionConfigRoot {
		t.Fatalf("projection = %#v, want Codex config-root projection on transport options", captured.AuthProjection)
	}
}
