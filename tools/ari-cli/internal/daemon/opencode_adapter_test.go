package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestOpenCodeExecutorMapsJSONEvents(t *testing.T) {
	runner := &fakeOpenCodeRunner{output: []byte(strings.Join([]string{
		`{"type":"session.status","properties":{"sessionID":"sess_123","status":{"type":"busy"}}}`,
		`{"type":"message.updated","properties":{"info":{"id":"msg_123","sessionID":"sess_123","role":"assistant","cost":0.0123,"tokens":{"input":1200,"output":300,"reasoning":0,"cache":{"read":0,"write":0}}}}}`,
		`{"type":"message.part.updated","properties":{"part":{"id":"part_1","sessionID":"sess_123","messageID":"msg_123","type":"text","text":"hello world"}}}`,
		`{"type":"session.status","properties":{"sessionID":"sess_123","status":{"type":"idle"}}}`,
	}, "\n"))}
	executor := NewOpenCodeExecutorForTest(opencodeExecutorOptions{Executable: "opencode", Cwd: "/repo", RunCommand: runner.Run})
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}

	run, items, err := StartExecutorRun(context.Background(), executor, packet, AgentProfile{Name: "builder", Model: "sonnet", Prompt: "Build it", InvocationClass: HarnessInvocationAgent})
	if err != nil {
		t.Fatalf("StartExecutorRun returned error: %v", err)
	}
	if run.Executor != HarnessNameOpenCode || run.ProviderRunID != "sess_123" || run.AgentRunID == run.ProviderRunID || !isULID(run.AgentRunID) {
		t.Fatalf("run = %#v, want Ari run id with OpenCode provider session", run)
	}
	if len(items) != 4 {
		t.Fatalf("items len = %d, want lifecycle/text/telemetry/completed items: %#v", len(items), items)
	}
	if items[1].Kind != "agent_text" || items[1].Text != "hello world" {
		t.Fatalf("message item = %#v, want OpenCode text part", items[1])
	}
	if items[2].Kind != "telemetry" || items[2].Metadata["input_tokens"] != "1200" || items[2].Metadata["output_tokens"] != "300" {
		t.Fatalf("telemetry item = %#v, want OpenCode token metadata", items[2])
	}
	if got := strings.Join(runner.args, " "); !strings.Contains(got, "run") || !strings.Contains(got, "--format json") || !strings.Contains(got, "--model sonnet") {
		t.Fatalf("opencode args = %q, want run json model invocation", got)
	}
	if !strings.Contains(runner.prompt, "Build it") || !strings.Contains(runner.prompt, "ctx_123") {
		t.Fatalf("opencode prompt = %q, want profile prompt plus context packet", runner.prompt)
	}
}

func TestOpenCodeExecutorParsesLargeJSONLEvent(t *testing.T) {
	largeText := strings.Repeat("x", 128*1024)
	line, err := json.Marshal(map[string]any{"type": "message.part.updated", "properties": map[string]any{"part": map[string]string{"id": "part_1", "sessionID": "sess_123", "messageID": "msg_123", "type": "text", "text": largeText}}})
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	runner := &fakeOpenCodeRunner{output: []byte(strings.Join([]string{
		`{"type":"session.status","properties":{"sessionID":"sess_123","status":{"type":"busy"}}}`,
		string(line),
		`{"type":"session.status","properties":{"sessionID":"sess_123","status":{"type":"idle"}}}`,
	}, "\n"))}
	executor := NewOpenCodeExecutorForTest(opencodeExecutorOptions{Executable: "opencode", Cwd: "/repo", RunCommand: runner.Run})
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}

	_, items, err := StartExecutorRun(context.Background(), executor, packet)
	if err != nil {
		t.Fatalf("StartExecutorRun returned error: %v", err)
	}
	if len(items) != 3 || items[1].Kind != "agent_text" || items[1].Text != largeText {
		t.Fatalf("items = %#v, want large OpenCode text event preserved", items)
	}
}

func TestOpenCodeExecutorReportsMissingExecutableBeforeStart(t *testing.T) {
	executor := NewOpenCodeExecutorForTest(opencodeExecutorOptions{Executable: "missing-opencode", Cwd: "/repo", RunCommand: func(ctx context.Context, opts opencodeExecutorOptions, prompt string) (commandRunResult, error) {
		return commandRunResult{}, &HarnessUnavailableError{Harness: HarnessNameOpenCode, Reason: "missing_executable", Executable: opts.Executable, Probe: opts.Executable + " --version", RequiredCapability: HarnessCapabilityAgentRunFromContext, StartInvoked: false}
	}})
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}

	_, _, err := StartExecutorRun(context.Background(), executor, packet)
	unavailable := &HarnessUnavailableError{}
	if !errors.As(err, &unavailable) {
		t.Fatalf("error = %T %[1]v, want HarnessUnavailableError", err)
	}
	if unavailable.StartInvoked || unavailable.Executable != "missing-opencode" || unavailable.RequiredCapability != HarnessCapabilityAgentRunFromContext {
		t.Fatalf("unavailable = %#v, want pre-start missing executable", unavailable)
	}
}

func TestOpenCodeAuthStatusNormalizesProviderOwnedReadiness(t *testing.T) {
	exitCode := 0
	executor := NewOpenCodeExecutorForTest(opencodeExecutorOptions{Executable: "opencode", Cwd: "/repo", RunAuthCommand: func(ctx context.Context, opts opencodeExecutorOptions, args []string) (commandRunResult, error) {
		_ = ctx
		_ = opts
		if strings.Join(args, " ") != "auth list" {
			t.Fatalf("args = %q, want auth list", strings.Join(args, " "))
		}
		return commandRunResult{Output: []byte("openrouter authenticated\n"), ExitCode: &exitCode}, nil
	}})

	status, err := executor.AuthStatus(context.Background(), HarnessAuthSlot{AuthSlotID: "opencode-default", Harness: HarnessNameOpenCode, Label: "openrouter"})
	if err != nil {
		t.Fatalf("AuthStatus returned error: %v", err)
	}
	if status.Status != HarnessAuthAuthenticated || status.AuthSlotID != "opencode-default" || status.AriSecretStorage != HarnessAriSecretStorageNone {
		t.Fatalf("status = %#v, want authenticated OpenCode slot without Ari secrets", status)
	}
}

func TestOpenCodeAuthStatusReturnsProviderLoginRemediation(t *testing.T) {
	exitCode := 1
	executor := NewOpenCodeExecutorForTest(opencodeExecutorOptions{Executable: "opencode", Cwd: "/repo", RunAuthCommand: func(ctx context.Context, opts opencodeExecutorOptions, args []string) (commandRunResult, error) {
		_ = ctx
		_ = opts
		_ = args
		return commandRunResult{Output: []byte("not authenticated\n"), ExitCode: &exitCode}, errors.New("not authenticated")
	}})

	status, err := executor.AuthStatus(context.Background(), HarnessAuthSlot{AuthSlotID: "opencode-default", Harness: HarnessNameOpenCode})
	if err != nil {
		t.Fatalf("AuthStatus returned error: %v", err)
	}
	if status.Status != HarnessAuthRequired || status.Remediation == nil || status.Remediation.SecretOwnedBy != HarnessNameOpenCode || status.AriSecretStorage != HarnessAriSecretStorageNone {
		t.Fatalf("status = %#v, want provider-owned remediation", status)
	}
}

func TestOpenCodeAuthStatusFailsClosedWithoutSlotHint(t *testing.T) {
	exitCode := 0
	executor := NewOpenCodeExecutorForTest(opencodeExecutorOptions{Executable: "opencode", Cwd: "/repo", RunAuthCommand: func(ctx context.Context, opts opencodeExecutorOptions, args []string) (commandRunResult, error) {
		_ = ctx
		_ = opts
		_ = args
		return commandRunResult{Output: []byte("openrouter authenticated\n"), ExitCode: &exitCode}, nil
	}})

	status, err := executor.AuthStatus(context.Background(), HarnessAuthSlot{AuthSlotID: "opencode-work", Harness: HarnessNameOpenCode})
	if err != nil {
		t.Fatalf("AuthStatus returned error: %v", err)
	}
	if status.Status != HarnessAuthRequired {
		t.Fatalf("status = %#v, want fail-closed auth_required without selected slot source hint", status)
	}
}

func TestOpenCodeExecutorRejectsMissingSessionID(t *testing.T) {
	executor := NewOpenCodeExecutorForTest(opencodeExecutorOptions{Executable: "opencode", Cwd: "/repo", RunCommand: func(ctx context.Context, opts opencodeExecutorOptions, prompt string) (commandRunResult, error) {
		return commandRunResult{Output: []byte(`{"type":"message.part.updated","properties":{"part":{"type":"text","text":"orphan"}}}`)}, nil
	}})
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}

	_, _, err := StartExecutorRun(context.Background(), executor, packet)
	if err == nil || !strings.Contains(err.Error(), "opencode session id is required") {
		t.Fatalf("StartExecutorRun error = %v, want missing session id error", err)
	}
}

type fakeOpenCodeRunner struct {
	output []byte
	args   []string
	prompt string
}

func (r *fakeOpenCodeRunner) Run(ctx context.Context, opts opencodeExecutorOptions, prompt string) (commandRunResult, error) {
	_ = ctx
	r.args = opencodeArgs(opts, prompt)
	r.prompt = prompt
	return commandRunResult{Output: append([]byte(nil), r.output...)}, nil
}
