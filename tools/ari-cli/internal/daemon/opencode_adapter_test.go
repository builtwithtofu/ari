package daemon

import (
	"context"
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

func TestOpenCodeExecutorReportsMissingExecutableBeforeStart(t *testing.T) {
	executor := NewOpenCodeExecutorForTest(opencodeExecutorOptions{Executable: "missing-opencode", Cwd: "/repo", RunCommand: func(ctx context.Context, opts opencodeExecutorOptions, prompt string) ([]byte, error) {
		return nil, &HarnessUnavailableError{Harness: HarnessNameOpenCode, Reason: "missing_executable", Executable: opts.Executable, Probe: opts.Executable + " --version", RequiredCapability: HarnessCapabilityAgentRunFromContext, StartInvoked: false}
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

func TestOpenCodeExecutorRejectsMissingSessionID(t *testing.T) {
	executor := NewOpenCodeExecutorForTest(opencodeExecutorOptions{Executable: "opencode", Cwd: "/repo", RunCommand: func(ctx context.Context, opts opencodeExecutorOptions, prompt string) ([]byte, error) {
		return []byte(`{"type":"message.part.updated","properties":{"part":{"type":"text","text":"orphan"}}}`), nil
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

func (r *fakeOpenCodeRunner) Run(ctx context.Context, opts opencodeExecutorOptions, prompt string) ([]byte, error) {
	_ = ctx
	r.args = opencodeArgs(opts, prompt)
	r.prompt = prompt
	return append([]byte(nil), r.output...), nil
}
