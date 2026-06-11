package daemon

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/fakeharness"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
)

type fakePiRunner struct {
	output []byte
	args   []string
	input  string
}

func (r *fakePiRunner) Run(ctx context.Context, opts piExecutorOptions, args []string, input string) (commandRunResult, error) {
	_ = ctx
	_ = opts
	r.args = append([]string(nil), args...)
	r.input = input
	return commandRunResult{Output: append([]byte(nil), r.output...)}, nil
}

func TestPiExecutorMapsJSONEventsToTimelineItems(t *testing.T) {
	runner := &fakePiRunner{output: []byte(strings.Join([]string{
		`{"type":"extension_ui_request","id":"x","method":"setStatus","statusText":"ignored"}`,
		`{"type":"agent_start"}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"streaming"}}`,
		`{"type":"tool_execution_end","toolCallId":"call_1","toolName":"bash","isError":false}`,
		`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"hello from pi"}],"usage":{"input":12,"output":7},"stopReason":"stop"}}`,
		`{"type":"agent_end","messages":[]}`,
	}, "\n"))}
	executor := NewPiExecutorForTest(piExecutorOptions{Executable: "pi", Cwd: "/repo", RunCommand: runner.Run})
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}

	result, err := StartExecutorRunResult(context.Background(), executor, packet, "", Profile{Name: "builder", Harness: HarnessNamePi, Model: "anthropic/claude-sonnet", Prompt: "Build", InvocationClass: HarnessInvocationSticky})
	if err != nil {
		t.Fatalf("StartExecutorRunResult returned error: %v", err)
	}
	run := result.HarnessSession
	if run.Executor != HarnessNamePi || !isULID(run.HarnessSessionID) || run.ProviderSessionID != run.HarnessSessionID {
		t.Fatalf("run = %#v, want pi run keyed by ari session id", run)
	}
	kinds := make([]string, 0, len(result.Items))
	for _, item := range result.Items {
		kinds = append(kinds, item.Kind)
	}
	if got := strings.Join(kinds, ","); got != "lifecycle,tool,agent_text,telemetry,lifecycle" {
		t.Fatalf("item kinds = %q, want lifecycle/tool/agent_text/telemetry/lifecycle", got)
	}
	if result.FinalResponse == nil || result.FinalResponse.Text != "hello from pi" {
		t.Fatalf("final response = %#v, want pi text", result.FinalResponse)
	}
	if !result.Telemetry.MeasuredTokenTelemetry || *result.Telemetry.InputTokens != 12 || *result.Telemetry.OutputTokens != 7 {
		t.Fatalf("telemetry = %#v, want measured usage from message_end", result.Telemetry)
	}
	if result.SessionRef.Persistence != HarnessSessionPersistent || result.SessionRef.ResumeMode != HarnessResumeJSONRPC {
		t.Fatalf("session ref = %#v, want persistent json_rpc default server mode", result.SessionRef)
	}
	args := strings.Join(runner.args, " ")
	if !strings.Contains(args, "--mode rpc") || !strings.Contains(args, "--session-id "+run.ProviderSessionID) || !strings.Contains(args, "--model anthropic/claude-sonnet") || !strings.Contains(args, "--system-prompt Build") {
		t.Fatalf("pi args = %q, want rpc mode with ari session id, model, and system prompt", args)
	}
	if !strings.Contains(runner.input, `"type":"prompt"`) || !strings.Contains(runner.input, "ctx_123") {
		t.Fatalf("pi rpc input = %q, want prompt command carrying context packet", runner.input)
	}
}

func TestPiExecutorHeadlessModeUsesPrintJSONArgs(t *testing.T) {
	runner := &fakePiRunner{output: []byte(strings.Join([]string{
		`{"type":"agent_start"}`,
		`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"done"}],"usage":{"input":1,"output":1},"stopReason":"stop"}}`,
		`{"type":"agent_end","messages":[]}`,
	}, "\n"))}
	executor := NewPiExecutorForTest(piExecutorOptions{Executable: "pi", Cwd: "/repo", RunCommand: runner.Run})
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}

	result, err := StartExecutorRunResult(context.Background(), executor, packet, "", Profile{Name: "worker", Harness: HarnessNamePi, InvocationClass: HarnessInvocationEphemeral, Defaults: map[string]any{"invocation_mode": "headless"}})
	if err != nil {
		t.Fatalf("StartExecutorRunResult returned error: %v", err)
	}
	args := strings.Join(runner.args, " ")
	if !strings.Contains(args, "-p --mode json") || strings.Contains(args, "--mode rpc") {
		t.Fatalf("pi args = %q, want headless print json mode", args)
	}
	if result.SessionRef.ResumeMode != HarnessResumeCLIFlag {
		t.Fatalf("session ref = %#v, want cli_flag resume for headless runs", result.SessionRef)
	}
}

func TestPiExecutorRejectsBackgroundInvocationMode(t *testing.T) {
	executor := NewPiExecutorForTest(piExecutorOptions{Executable: "pi", Cwd: "/repo", RunCommand: (&fakePiRunner{}).Run})
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}

	_, err := StartExecutorRunResult(context.Background(), executor, packet, "", Profile{Name: "worker", Harness: HarnessNamePi, InvocationClass: HarnessInvocationSticky, Defaults: map[string]any{"invocation_mode": "background"}})
	var validation *HarnessValidationError
	if err == nil || !errors.As(err, &validation) || validation.Field != "invocation_mode" {
		t.Fatalf("error = %v, want invocation_mode validation error", err)
	}
}

func TestPiExecutorMapsErrorEventsToFailure(t *testing.T) {
	runner := &fakePiRunner{output: []byte(strings.Join([]string{
		`{"type":"agent_start"}`,
		`{"type":"error","message":"provider exploded"}`,
	}, "\n"))}
	executor := NewPiExecutorForTest(piExecutorOptions{Executable: "pi", Cwd: "/repo", RunCommand: runner.Run})
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}

	result, err := StartExecutorRunResult(context.Background(), executor, packet, "", Profile{Name: "builder", Harness: HarnessNamePi, InvocationClass: HarnessInvocationSticky})
	if err != nil {
		t.Fatalf("StartExecutorRunResult returned error: %v", err)
	}
	if result.Status != HarnessCallFailed {
		t.Fatalf("status = %q, want failed call from error event", result.Status)
	}
	last := result.Items[len(result.Items)-1]
	if last.Kind != "lifecycle" || last.Status != "failed" || !strings.Contains(last.Text, "provider exploded") {
		t.Fatalf("last item = %#v, want failed lifecycle with provider error", last)
	}
}

func TestPiAuthStatusProbesProviderKeyPresence(t *testing.T) {
	missing := NewPiExecutorForTest(piExecutorOptions{Executable: "pi", LookupEnv: func(string) string { return "" }})
	status, err := missing.AuthStatus(context.Background(), HarnessAuthSlot{AuthSlotID: "pi-default", Harness: HarnessNamePi})
	if err != nil {
		t.Fatalf("AuthStatus returned error: %v", err)
	}
	if status.Status != HarnessAuthRequired || status.Remediation == nil || status.Remediation.Method != "provider_env_key" {
		t.Fatalf("status = %#v, want auth required with env key remediation", status)
	}

	present := NewPiExecutorForTest(piExecutorOptions{Executable: "pi", LookupEnv: func(key string) string {
		if key == "ANTHROPIC_API_KEY" {
			return "sk-fake"
		}
		return ""
	}})
	status, err = present.AuthStatus(context.Background(), HarnessAuthSlot{AuthSlotID: "pi-default", Harness: HarnessNamePi})
	if err != nil {
		t.Fatalf("AuthStatus returned error: %v", err)
	}
	if status.Status != HarnessAuthAuthenticated {
		t.Fatalf("status = %#v, want authenticated from env key presence", status)
	}
}

func TestPiNamedSlotsRequireAriEnvProjection(t *testing.T) {
	executor := NewPiExecutorForTest(piExecutorOptions{Executable: "pi", Cwd: "/repo", RunCommand: (&fakePiRunner{}).Run})
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}

	_, err := StartExecutorRunResult(context.Background(), executor, packet, "", Profile{Name: "builder", Harness: HarnessNamePi, AuthSlotID: "pi-work", InvocationClass: HarnessInvocationSticky})
	var unavailable *HarnessUnavailableError
	if err == nil || !errors.As(err, &unavailable) || unavailable.Reason != "auth_slot_projection_required" {
		t.Fatalf("error = %v, want projection-required failure for named slot without grant", err)
	}
	if unavailable.StartInvoked {
		t.Fatal("Start must not be invoked without a granted projection")
	}
}

func TestPiExecutorStartsAgainstFakeRPCEngine(t *testing.T) {
	fake := buildFakeHarnessExecutable(t)
	stateDir := t.TempDir()
	t.Setenv(fakeharness.EnvHarness, "pi")
	t.Setenv(fakeharness.EnvMode, "authenticated")
	t.Setenv(fakeharness.EnvStateDir, stateDir)
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}

	executor := NewPiExecutorForTest(piExecutorOptions{Executable: fake, Cwd: t.TempDir()})
	first, err := StartExecutorRunResult(context.Background(), executor, packet, "", Profile{Name: "builder", Harness: HarnessNamePi, InvocationClass: HarnessInvocationSticky})
	if err != nil {
		t.Fatalf("first StartExecutorRunResult returned error: %v", err)
	}
	if first.FinalResponse == nil || !strings.Contains(first.FinalResponse.Text, "fake pi response (turn 1)") {
		t.Fatalf("final response = %#v, want fake pi turn 1 text", first.FinalResponse)
	}

	// Delivery reattaches to the same pi session id and counts as turn 2.
	deliveryRecord := t.TempDir() + "/pi-delivery.jsonl"
	t.Setenv(fakeharness.EnvRecord, deliveryRecord)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	result, err := executor.AttemptWorkspaceDelivery(ctx, WorkspaceDeliveryAttempt{Delivery: globaldb.PendingDelivery{DeliveryID: "pd-pi", WorkspaceID: "ws-1", SubscriptionID: "sub-1", TargetType: "harness_session", TargetID: first.HarnessSession.ProviderSessionID, EventIDs: []string{"we-1"}, Status: "attempted", Attempts: 1}})
	if err != nil {
		t.Fatalf("AttemptWorkspaceDelivery returned error: %v", err)
	}
	if result.Status != WorkspaceDeliveryAttemptCompleted {
		t.Fatalf("delivery result = %#v, want completed pi rpc delivery", result)
	}
}

func TestPiWorkspaceDeliveryTurnRedactsDurableIDs(t *testing.T) {
	turn := piWorkspaceDeliveryTurn(WorkspaceDeliveryAttempt{Delivery: globaldb.PendingDelivery{DeliveryID: "pd-secret", WorkspaceID: "ws-1", SubscriptionID: "sub-1", EventIDs: []string{"we-secret"}}})
	if strings.Contains(turn, "pd-secret") || strings.Contains(turn, "we-secret") {
		t.Fatalf("pi delivery turn leaked durable ids: %s", turn)
	}
	if !strings.Contains(turn, `"event_count":1`) {
		t.Fatalf("pi delivery turn = %s, want redacted event_count", turn)
	}
}
