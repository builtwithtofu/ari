package daemon

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestClaudeExecutorMapsJSONResult(t *testing.T) {
	runner := &fakeClaudeRunner{output: []byte(`{"result":"Done","session_id":"550e8400-e29b-41d4-a716-446655440000","usage":{"input_tokens":12,"output_tokens":34},"total_cost_usd":0.0123}`)}
	executor := NewClaudeExecutorForTest(claudeExecutorOptions{Executable: "claude", Cwd: "/repo", RunCommand: runner.Run})
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}

	run, items, err := StartExecutorRun(context.Background(), executor, packet, AgentProfile{Name: "reviewer", Model: "opus", Prompt: "Review it", InvocationClass: HarnessInvocationAgent})
	if err != nil {
		t.Fatalf("StartExecutorRun returned error: %v", err)
	}
	if run.Executor != HarnessNameClaude || run.ProviderRunID != "550e8400-e29b-41d4-a716-446655440000" || run.AgentSessionID == run.ProviderRunID || !isULID(run.AgentSessionID) {
		t.Fatalf("run = %#v, want Ari run id with Claude provider session", run)
	}
	if len(items) != 4 {
		t.Fatalf("items len = %d, want lifecycle/text/telemetry/completed items: %#v", len(items), items)
	}
	if items[1].Kind != "agent_text" || items[1].Text != "Done" {
		t.Fatalf("message item = %#v, want Claude result text", items[1])
	}
	if items[2].Kind != "telemetry" || items[2].Metadata["input_tokens"] != "12" || items[2].Metadata["output_tokens"] != "34" {
		t.Fatalf("telemetry item = %#v, want Claude usage metadata", items[2])
	}
	if got := strings.Join(runner.args, " "); !strings.Contains(got, "--bare") || !strings.Contains(got, "--output-format json") || !strings.Contains(got, "--model opus") {
		t.Fatalf("claude args = %q, want bare json model invocation", got)
	}
	if !strings.Contains(runner.prompt, "Review it") || !strings.Contains(runner.prompt, "ctx_123") {
		t.Fatalf("claude prompt = %q, want profile prompt plus context packet", runner.prompt)
	}
}

func TestClaudeExecutorReportsMissingExecutableBeforeStart(t *testing.T) {
	executor := NewClaudeExecutorForTest(claudeExecutorOptions{Executable: "missing-claude", Cwd: "/repo", RunCommand: func(ctx context.Context, opts claudeExecutorOptions, prompt string) (commandRunResult, error) {
		return commandRunResult{}, &HarnessUnavailableError{Harness: HarnessNameClaude, Reason: "missing_executable", Executable: opts.Executable, Probe: opts.Executable + " --version", RequiredCapability: HarnessCapabilityAgentSessionFromContext, StartInvoked: false}
	}})
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}

	_, _, err := StartExecutorRun(context.Background(), executor, packet)
	unavailable := &HarnessUnavailableError{}
	if !errors.As(err, &unavailable) {
		t.Fatalf("error = %T %[1]v, want HarnessUnavailableError", err)
	}
	if unavailable.StartInvoked || unavailable.Executable != "missing-claude" || unavailable.RequiredCapability != HarnessCapabilityAgentSessionFromContext {
		t.Fatalf("unavailable = %#v, want pre-start missing executable", unavailable)
	}
}

func TestClaudeAuthStatusNormalizesProviderOwnedReadiness(t *testing.T) {
	exitCode := 0
	executor := NewClaudeExecutorForTest(claudeExecutorOptions{Executable: "claude", Cwd: "/repo", RunAuthCommand: func(ctx context.Context, opts claudeExecutorOptions, args []string) (commandRunResult, error) {
		_ = ctx
		_ = opts
		if strings.Join(args, " ") != "auth status --json" {
			t.Fatalf("args = %q, want auth status --json", strings.Join(args, " "))
		}
		return commandRunResult{Output: []byte(`{"authenticated":true}`), ExitCode: &exitCode}, nil
	}})

	status, err := executor.AuthStatus(context.Background(), HarnessAuthSlot{AuthSlotID: "claude-default", Harness: HarnessNameClaude})
	if err != nil {
		t.Fatalf("AuthStatus returned error: %v", err)
	}
	if status.Status != HarnessAuthAuthenticated || status.AuthSlotID != "claude-default" || status.AriSecretStorage != HarnessAriSecretStorageNone {
		t.Fatalf("status = %#v, want authenticated Claude slot without Ari secrets", status)
	}
}

func TestClaudeAuthStatusReturnsProviderConfigRemediation(t *testing.T) {
	exitCode := 1
	executor := NewClaudeExecutorForTest(claudeExecutorOptions{Executable: "claude", Cwd: "/repo", RunAuthCommand: func(ctx context.Context, opts claudeExecutorOptions, args []string) (commandRunResult, error) {
		_ = ctx
		_ = opts
		_ = args
		return commandRunResult{Output: []byte(`{"authenticated":false}`), ExitCode: &exitCode}, errors.New("not authenticated")
	}})

	status, err := executor.AuthStatus(context.Background(), HarnessAuthSlot{AuthSlotID: "claude-default", Harness: HarnessNameClaude})
	if err != nil {
		t.Fatalf("AuthStatus returned error: %v", err)
	}
	if status.Status != HarnessAuthRequired || status.Remediation == nil || status.Remediation.SecretOwnedBy != HarnessNameClaude || status.AriSecretStorage != HarnessAriSecretStorageNone {
		t.Fatalf("status = %#v, want provider-owned remediation", status)
	}
}

func TestClaudeAuthStatusTreatsEmptyOutputAsAuthRequired(t *testing.T) {
	exitCode := 0
	executor := NewClaudeExecutorForTest(claudeExecutorOptions{Executable: "claude", Cwd: "/repo", RunAuthCommand: func(ctx context.Context, opts claudeExecutorOptions, args []string) (commandRunResult, error) {
		_ = ctx
		_ = opts
		_ = args
		return commandRunResult{Output: nil, ExitCode: &exitCode}, nil
	}})

	status, err := executor.AuthStatus(context.Background(), HarnessAuthSlot{AuthSlotID: "claude-default", Harness: HarnessNameClaude})
	if err != nil {
		t.Fatalf("AuthStatus returned error: %v", err)
	}
	if status.Status != HarnessAuthRequired || status.AriSecretStorage != HarnessAriSecretStorageNone {
		t.Fatalf("status = %#v, want auth_required for empty provider output", status)
	}
}

func TestClaudeAuthLogoutRunsProviderLogout(t *testing.T) {
	exitCode := 0
	executor := NewClaudeExecutorForTest(claudeExecutorOptions{Executable: "claude", Cwd: "/repo", RunAuthCommand: func(ctx context.Context, opts claudeExecutorOptions, args []string) (commandRunResult, error) {
		_ = ctx
		_ = opts
		if strings.Join(args, " ") != "auth logout" {
			t.Fatalf("args = %q, want auth logout", strings.Join(args, " "))
		}
		return commandRunResult{ExitCode: &exitCode}, nil
	}})

	status, err := executor.AuthLogout(context.Background(), HarnessAuthSlot{AuthSlotID: "claude-default", Harness: HarnessNameClaude})
	if err != nil {
		t.Fatalf("AuthLogout returned error: %v", err)
	}
	if status.Status != HarnessAuthRequired || status.AriSecretStorage != HarnessAriSecretStorageNone {
		t.Fatalf("status = %#v, want auth_required after provider logout", status)
	}
}

func TestClaudeExecutorRejectsMissingSessionID(t *testing.T) {
	executor := NewClaudeExecutorForTest(claudeExecutorOptions{Executable: "claude", Cwd: "/repo", RunCommand: func(ctx context.Context, opts claudeExecutorOptions, prompt string) (commandRunResult, error) {
		return commandRunResult{Output: []byte(`{"result":"Done"}`)}, nil
	}})
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}

	_, _, err := StartExecutorRun(context.Background(), executor, packet)
	if err == nil || !strings.Contains(err.Error(), "claude session id is required") {
		t.Fatalf("StartExecutorRun error = %v, want missing session id error", err)
	}
}

type fakeClaudeRunner struct {
	output []byte
	args   []string
	prompt string
}

func (r *fakeClaudeRunner) Run(ctx context.Context, opts claudeExecutorOptions, prompt string) (commandRunResult, error) {
	_ = ctx
	r.args = claudeArgs(opts)
	r.prompt = prompt
	return commandRunResult{Output: append([]byte(nil), r.output...)}, nil
}
