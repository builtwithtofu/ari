package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/fakeharness"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
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

	run, items, err := StartExecutorRun(context.Background(), executor, packet, Profile{Name: "builder", Model: "sonnet", Prompt: "Build it", InvocationClass: HarnessInvocationSticky})
	if err != nil {
		t.Fatalf("StartExecutorRun returned error: %v", err)
	}
	if run.Executor != HarnessNameOpenCode || run.ProviderRunID != "sess_123" || run.HarnessSessionID == run.ProviderRunID || !isULID(run.HarnessSessionID) {
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
		t.Fatalf("opencode prompt = %q, want profile prompt and context packet", runner.prompt)
	}
}

func TestOpenCodeExecutorIncludesProfilePromptInVisibleRunInput(t *testing.T) {
	runner := &fakeOpenCodeRunner{output: []byte(strings.Join([]string{
		`{"type":"session.status","properties":{"sessionID":"sess_123","status":{"type":"busy"}}}`,
		`{"type":"session.status","properties":{"sessionID":"sess_123","status":{"type":"idle"}}}`,
	}, "\n"))}
	executor := NewOpenCodeExecutorForTest(opencodeExecutorOptions{Executable: "opencode", Cwd: "/repo", RunCommand: runner.Run})
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}

	_, _, err := StartExecutorRun(context.Background(), executor, packet, Profile{Name: "builder", Model: "sonnet", Prompt: "Use builder behavior", InvocationClass: HarnessInvocationSticky})
	if err != nil {
		t.Fatalf("StartExecutorRun returned error: %v", err)
	}

	if !strings.Contains(runner.prompt, "Use builder behavior") {
		t.Fatalf("opencode prompt = %q, want profile behavior in visible run input", runner.prompt)
	}
	if !strings.Contains(runner.prompt, "ctx_123") {
		t.Fatalf("opencode prompt = %q, want context packet visible in user payload", runner.prompt)
	}
}

func TestOpenCodeExecutorAttemptsServerPromptDeliveryAgainstFakeHandler(t *testing.T) {
	var recorded fakeharness.OpenCodePromptDelivery
	server := httptest.NewServer(fakeharness.OpenCodeDeliveryHandler(func(delivery fakeharness.OpenCodePromptDelivery) {
		recorded = delivery
	}))
	t.Cleanup(server.Close)
	executor := NewOpenCodeExecutorForTest(opencodeExecutorOptions{DeliveryServerURL: server.URL})

	result, err := executor.AttemptWorkspaceDelivery(context.Background(), WorkspaceDeliveryAttempt{Delivery: globaldb.PendingDelivery{DeliveryID: "pd-opencode", WorkspaceID: "ws-1", SubscriptionID: "sub-1", TargetType: "harness_session", TargetID: "sess_123", EventIDs: []string{"we-1"}, Status: "attempted", Attempts: 1}})
	if err != nil {
		t.Fatalf("AttemptWorkspaceDelivery returned error: %v", err)
	}
	if result.Status != WorkspaceDeliveryAttemptCompleted || result.LastError != "" {
		t.Fatalf("delivery result = %#v, want completed fake server prompt delivery", result)
	}
	if recorded.SessionID != "sess_123" || recorded.IdempotencyKey != "pd-opencode" || recorded.Delivery != "queue" || recorded.TextHash == "" {
		t.Fatalf("recorded prompt = %#v, want queued idempotent prompt for target session", recorded)
	}
	text := opencodeWorkspaceDeliveryText(WorkspaceDeliveryAttempt{Delivery: globaldb.PendingDelivery{DeliveryID: "pd-opencode", WorkspaceID: "ws-1", SubscriptionID: "sub-1", EventIDs: []string{"we-1"}}})
	if strings.Contains(text, "pd-opencode") || strings.Contains(text, "we-1") || strings.Contains(text, "delivery_id") || strings.Contains(text, "event_ids") {
		t.Fatalf("OpenCode delivery text leaked durable ids: %s", text)
	}
	if !strings.Contains(text, `"event_count":1`) {
		t.Fatalf("OpenCode delivery text = %s, want redacted event_count", text)
	}
}

func TestOpenCodeExecutorDeliversThroughManagedServeProcess(t *testing.T) {
	// No DeliveryServerURL configured: the adapter must start a bounded fake
	// `opencode serve` process itself, deliver, and stop it afterwards.
	fake := buildFakeHarnessExecutable(t)
	t.Setenv(fakeharness.EnvHarness, "opencode")
	t.Setenv(fakeharness.EnvMode, "authenticated")
	executor := NewOpenCodeExecutorForTest(opencodeExecutorOptions{Executable: fake, Cwd: t.TempDir()})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	result, err := executor.AttemptWorkspaceDelivery(ctx, WorkspaceDeliveryAttempt{Delivery: globaldb.PendingDelivery{DeliveryID: "pd-opencode", WorkspaceID: "ws-1", SubscriptionID: "sub-1", TargetType: "harness_session", TargetID: "sess_123", EventIDs: []string{"we-1"}, Status: "attempted", Attempts: 1}})
	if err != nil {
		t.Fatalf("AttemptWorkspaceDelivery returned error: %v", err)
	}
	if result.Status != WorkspaceDeliveryAttemptCompleted || result.LastError != "" {
		t.Fatalf("delivery result = %#v, want completed delivery through managed serve process", result)
	}
}

func TestOpenCodeDeliveryHTTPClientHasTimeout(t *testing.T) {
	if openCodeDeliveryHTTPClient == nil || openCodeDeliveryHTTPClient.Timeout <= 0 {
		t.Fatalf("openCodeDeliveryHTTPClient = %#v, want explicit timeout", openCodeDeliveryHTTPClient)
	}
}

func TestOpenCodeExecutorAlwaysAdvertisesPromptTurnDelivery(t *testing.T) {
	// Delivery no longer depends on a pre-configured server URL: attempts
	// without one start a bounded `opencode serve` process themselves.
	for _, options := range []opencodeExecutorOptions{{}, {DeliveryServerURL: "http://127.0.0.1:3000"}} {
		executor := NewOpenCodeExecutorForTest(options)
		if got := executor.Descriptor().DeliveryCapabilities; len(got) != 1 || got[0] != HarnessDeliveryVisiblePromptTurn {
			t.Fatalf("delivery capabilities (server url %q) = %#v, want visible prompt turn", options.DeliveryServerURL, got)
		}
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
		return commandRunResult{}, &HarnessUnavailableError{Harness: HarnessNameOpenCode, Reason: "missing_executable", Executable: opts.Executable, Probe: opts.Executable + " --version", RequiredCapability: HarnessCapabilityHarnessSessionFromContext, StartInvoked: false}
	}})
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}

	_, _, err := StartExecutorRun(context.Background(), executor, packet)
	unavailable := &HarnessUnavailableError{}
	if !errors.As(err, &unavailable) {
		t.Fatalf("error = %T %[1]v, want HarnessUnavailableError", err)
	}
	if unavailable.StartInvoked || unavailable.Executable != "missing-opencode" || unavailable.RequiredCapability != HarnessCapabilityHarnessSessionFromContext {
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

func TestOpenCodeAuthStatusUsesNamedProviderSlotHint(t *testing.T) {
	exitCode := 0
	executor := NewOpenCodeExecutorForTest(opencodeExecutorOptions{Executable: "opencode", Cwd: "/repo", RunAuthCommand: func(ctx context.Context, opts opencodeExecutorOptions, args []string) (commandRunResult, error) {
		_ = ctx
		_ = opts
		if strings.Join(args, " ") != "auth list" {
			t.Fatalf("args = %q, want auth list", strings.Join(args, " "))
		}
		return commandRunResult{Output: []byte("anthropic not authenticated\nopenrouter authenticated\n"), ExitCode: &exitCode}, nil
	}})

	status, err := executor.AuthStatus(context.Background(), HarnessAuthSlot{AuthSlotID: "opencode-openrouter", Harness: HarnessNameOpenCode, Label: "OpenRouter"})
	if err != nil {
		t.Fatalf("AuthStatus returned error: %v", err)
	}
	if status.Status != HarnessAuthAuthenticated || status.AuthSlotID != "opencode-openrouter" {
		t.Fatalf("status = %#v, want authenticated named OpenCode provider slot", status)
	}
}

func TestOpenCodeAuthStatusUsesProviderLabelHint(t *testing.T) {
	exitCode := 0
	executor := NewOpenCodeExecutorForTest(opencodeExecutorOptions{Executable: "opencode", Cwd: "/repo", RunAuthCommand: func(ctx context.Context, opts opencodeExecutorOptions, args []string) (commandRunResult, error) {
		_ = ctx
		_ = opts
		if strings.Join(args, " ") != "auth list" {
			t.Fatalf("args = %q, want auth list", strings.Join(args, " "))
		}
		return commandRunResult{Output: []byte("anthropic not authenticated\nopenai authenticated\n"), ExitCode: &exitCode}, nil
	}})

	status, err := executor.AuthStatus(context.Background(), HarnessAuthSlot{AuthSlotID: "opencode-default", Harness: HarnessNameOpenCode, Label: "default", ProviderLabel: "openai"})
	if err != nil {
		t.Fatalf("AuthStatus returned error: %v", err)
	}
	if status.Status != HarnessAuthAuthenticated || status.AuthSlotID != "opencode-default" {
		t.Fatalf("status = %#v, want ProviderLabel-selected OpenCode provider slot", status)
	}
}

func TestOpenCodeAuthLogoutRunsProviderLogout(t *testing.T) {
	exitCode := 0
	executor := NewOpenCodeExecutorForTest(opencodeExecutorOptions{Executable: "opencode", Cwd: "/repo", RunAuthCommand: func(ctx context.Context, opts opencodeExecutorOptions, args []string) (commandRunResult, error) {
		_ = ctx
		_ = opts
		if strings.Join(args, " ") != "auth logout --provider openrouter" {
			t.Fatalf("args = %q, want auth logout --provider openrouter", strings.Join(args, " "))
		}
		return commandRunResult{ExitCode: &exitCode}, nil
	}})

	status, err := executor.AuthLogout(context.Background(), HarnessAuthSlot{AuthSlotID: "opencode-openrouter", Harness: HarnessNameOpenCode, Label: "OpenRouter"})
	if err != nil {
		t.Fatalf("AuthLogout returned error: %v", err)
	}
	if status.Status != HarnessAuthRequired || status.AriSecretStorage != HarnessAriSecretStorageNone {
		t.Fatalf("status = %#v, want auth_required after provider logout", status)
	}
}

func TestOpenCodeAuthLogoutUsesProviderLabel(t *testing.T) {
	exitCode := 0
	executor := NewOpenCodeExecutorForTest(opencodeExecutorOptions{Executable: "opencode", Cwd: "/repo", RunAuthCommand: func(ctx context.Context, opts opencodeExecutorOptions, args []string) (commandRunResult, error) {
		_ = ctx
		_ = opts
		if strings.Join(args, " ") != "auth logout --provider openai" {
			t.Fatalf("args = %q, want auth logout --provider openai", strings.Join(args, " "))
		}
		return commandRunResult{ExitCode: &exitCode}, nil
	}})

	status, err := executor.AuthLogout(context.Background(), HarnessAuthSlot{AuthSlotID: "opencode-default", Harness: HarnessNameOpenCode, Label: "default", ProviderLabel: "openai"})
	if err != nil {
		t.Fatalf("AuthLogout returned error: %v", err)
	}
	if status.Status != HarnessAuthRequired || status.AriSecretStorage != HarnessAriSecretStorageNone {
		t.Fatalf("status = %#v, want auth_required after provider logout", status)
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

func TestOpenCodeStartRejectsNamedSlotWithoutAuthContentProjection(t *testing.T) {
	runner := &fakeOpenCodeRunner{output: []byte(`{"type":"session.status","properties":{"sessionID":"sess_123","status":{"type":"idle"}}}`)}
	executor := NewOpenCodeExecutorForTest(opencodeExecutorOptions{Executable: "opencode", Cwd: "/repo", RunCommand: runner.Run})

	_, err := executor.Start(context.Background(), ExecutorStartRequest{WorkspaceID: "ws-1", AuthSlotID: "opencode-work", ContextPacket: `{"context_packet_id":"ctx_123"}`})
	unavailable := &HarnessUnavailableError{}
	if !errors.As(err, &unavailable) {
		t.Fatalf("Start error = %T %[1]v, want HarnessUnavailableError", err)
	}
	if unavailable.Reason != "auth_slot_projection_required" || unavailable.StartInvoked {
		t.Fatalf("unavailable = %#v, want fail-closed before provider launch", unavailable)
	}
	if runner.args != nil {
		t.Fatalf("runner args = %#v, want provider not launched", runner.args)
	}
}

func TestOpenCodeStartAcceptsNamedSlotWithAuthContentProjection(t *testing.T) {
	runner := &fakeOpenCodeRunner{output: []byte(strings.Join([]string{`{"type":"session.status","properties":{"sessionID":"sess_123","status":{"type":"busy"}}}`, `{"type":"session.status","properties":{"sessionID":"sess_123","status":{"type":"idle"}}}`}, "\n"))}
	executor := NewOpenCodeExecutorForTest(opencodeExecutorOptions{Executable: "opencode", Cwd: "/repo", RunCommand: runner.Run})
	projection := HarnessAuthProjectionPlan{Owner: HarnessAuthProjectionOwnerAri, Kind: HarnessAuthProjectionAuthContent, Env: map[string]string{"OPENCODE_AUTH_CONTENT": `{"provider":"anthropic","type":"api"}`}, RiskLabels: []string{"env_projection_downgrade_risk"}}

	_, err := executor.Start(context.Background(), ExecutorStartRequest{WorkspaceID: "ws-1", AuthSlotID: "opencode-work", ContextPacket: `{"context_packet_id":"ctx_123"}`, AuthProjection: projection})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if runner.authProjection.Kind != HarnessAuthProjectionAuthContent || runner.authProjection.Env["OPENCODE_AUTH_CONTENT"] == "" {
		t.Fatalf("projection = %#v, want auth content projected to child command", runner.authProjection)
	}
}

func TestOpenCodeAuthProjectionPlanDoesNotSerializeEnvPayload(t *testing.T) {
	projection := HarnessAuthProjectionPlan{Owner: HarnessAuthProjectionOwnerAri, Kind: HarnessAuthProjectionAuthContent, Env: map[string]string{"OPENCODE_AUTH_CONTENT": `{"provider":"anthropic","access_token":"ari-secret-sentinel"}`}}
	encoded, err := json.Marshal(projection)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	if bytes.Contains(encoded, []byte("OPENCODE_AUTH_CONTENT")) || bytes.Contains(encoded, []byte("ari-secret-sentinel")) || bytes.Contains(encoded, []byte("access_token")) {
		t.Fatalf("projection JSON leaked env payload: %s", encoded)
	}
}

func TestOpenCodeExecutorAdvertisesCapabilityProofSurface(t *testing.T) {
	executor := NewOpenCodeExecutorForTest(opencodeExecutorOptions{Executable: "opencode", Cwd: "/repo"})
	capabilities := executor.Descriptor().Capabilities
	for _, required := range []HarnessCapability{HarnessCapabilityHarnessSessionFromContext, HarnessCapabilityContextPacket, HarnessCapabilityTimelineItems, HarnessCapabilityFinalResponse, HarnessCapabilityMeasuredTokenTelemetry} {
		if !harnessCapabilitiesContain(capabilities, required) {
			t.Fatalf("opencode capabilities = %#v, missing required %q", capabilities, required)
		}
	}
}

func TestReadOpenCodeServerURLStopsConsumingAfterMatch(t *testing.T) {
	reader, writer := io.Pipe()
	done := make(chan struct{})
	go func() {
		_, _ = writer.Write([]byte("server ready http://127.0.0.1:12345\n"))
		<-done
		_ = writer.Close()
	}()

	url, err := readOpenCodeServerURL(context.Background(), reader)
	if err != nil {
		t.Fatalf("readOpenCodeServerURL returned error: %v", err)
	}
	if url != "http://127.0.0.1:12345" {
		t.Fatalf("url = %q, want OpenCode server URL", url)
	}

	close(done)
	select {
	case <-time.After(100 * time.Millisecond):
		t.Fatal("readOpenCodeServerURL reader goroutine did not release pipe after match")
	default:
	}
}

type fakeOpenCodeRunner struct {
	output         []byte
	args           []string
	prompt         string
	authProjection HarnessAuthProjectionPlan
}

func (r *fakeOpenCodeRunner) Run(ctx context.Context, opts opencodeExecutorOptions, prompt string) (commandRunResult, error) {
	_ = ctx
	r.args = opencodeArgs(opts, prompt)
	r.prompt = prompt
	r.authProjection = opts.AuthProjection
	return commandRunResult{Output: append([]byte(nil), r.output...)}, nil
}
