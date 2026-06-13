package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/fakeharness"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
)

type fakeGrokRunner struct {
	output         []byte
	args           []string
	authProjection HarnessAuthProjectionPlan
	err            error
}

func (r *fakeGrokRunner) Run(ctx context.Context, opts grokExecutorOptions, args []string) (commandRunResult, error) {
	_ = ctx
	r.authProjection = opts.AuthProjection
	r.args = append([]string(nil), args...)
	exitCode := 0
	return commandRunResult{Output: append([]byte(nil), r.output...), ExitCode: &exitCode}, r.err
}

func TestGrokExecutorMapsStreamingJSONEvents(t *testing.T) {
	runner := &fakeGrokRunner{output: []byte(strings.Join([]string{
		`{"type":"text","data":"hello "}`,
		`{"type":"text","data":"from grok"}`,
		`{"type":"thought","data":"reasoning ignored"}`,
		`{"type":"end","stopReason":"EndTurn","sessionId":"grok-sess-1","requestId":"req-1"}`,
	}, "\n"))}
	executor := NewGrokExecutorForTest(grokExecutorOptions{Executable: "grok", Cwd: "/repo", RunCommand: runner.Run})
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}

	result, err := StartExecutorRunResult(context.Background(), executor, packet, "", Profile{Name: "builder", Harness: HarnessNameGrok, Model: "grok-build", Prompt: "Build", InvocationClass: HarnessInvocationSticky})
	if err != nil {
		t.Fatalf("StartExecutorRunResult returned error: %v", err)
	}
	run := result.HarnessSession
	if run.Executor != HarnessNameGrok || run.ProviderSessionID != "grok-sess-1" || run.ProviderRunID != "req-1" || !isULID(run.HarnessSessionID) {
		t.Fatalf("run = %#v, want grok provider session and request ids under an ari session", run)
	}
	if result.FinalResponse == nil || result.FinalResponse.Text != "hello from grok" {
		t.Fatalf("final response = %#v, want aggregated text chunks", result.FinalResponse)
	}
	if result.Telemetry.MeasuredTokenTelemetry {
		t.Fatalf("telemetry = %#v, grok headless output has no usage and must not claim measurement", result.Telemetry)
	}
	if result.SessionRef.Persistence != HarnessSessionPersistent || result.SessionRef.ResumeMode != HarnessResumeCLIFlag || !strings.Contains(string(result.SessionRef.ResumeCursor), "grok-sess-1") {
		t.Fatalf("session ref = %#v, want persistent cli_flag resume with grok session cursor", result.SessionRef)
	}
	args := strings.Join(runner.args, " ")
	if !strings.Contains(args, "--output-format streaming-json") || !strings.Contains(args, "--no-auto-update") || !strings.Contains(args, "-m grok-build") || !strings.Contains(args, "--rules Build") {
		t.Fatalf("grok args = %q, want streaming-json headless invocation with model and rules", args)
	}
	if !strings.Contains(args, "ctx_123") {
		t.Fatalf("grok args = %q, want context packet as the -p prompt", args)
	}
}

func TestGrokExecutorMapsJSONObjectOutput(t *testing.T) {
	runner := &fakeGrokRunner{output: []byte(`{"text":"single object","stopReason":"EndTurn","sessionId":"grok-sess-2","requestId":"req-2"}`)}
	executor := NewGrokExecutorForTest(grokExecutorOptions{Executable: "grok", Cwd: "/repo", RunCommand: runner.Run})
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}

	result, err := StartExecutorRunResult(context.Background(), executor, packet, "", Profile{Name: "builder", Harness: HarnessNameGrok, InvocationClass: HarnessInvocationSticky})
	if err != nil {
		t.Fatalf("StartExecutorRunResult returned error: %v", err)
	}
	if result.HarnessSession.ProviderSessionID != "grok-sess-2" || result.FinalResponse == nil || result.FinalResponse.Text != "single object" {
		t.Fatalf("result = %#v, want session and text from json object output", result.HarnessSession)
	}
}

func TestGrokExecutorMapsErrorEventsToFailure(t *testing.T) {
	runner := &fakeGrokRunner{output: []byte(strings.Join([]string{
		`{"type":"text","data":"partial"}`,
		`{"type":"error","message":"rate limited"}`,
		`{"type":"end","stopReason":"Error","sessionId":"grok-sess-3","requestId":"req-3"}`,
	}, "\n"))}
	executor := NewGrokExecutorForTest(grokExecutorOptions{Executable: "grok", Cwd: "/repo", RunCommand: runner.Run})
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}

	result, err := StartExecutorRunResult(context.Background(), executor, packet, "", Profile{Name: "builder", Harness: HarnessNameGrok, InvocationClass: HarnessInvocationSticky})
	if err != nil {
		t.Fatalf("StartExecutorRunResult returned error: %v", err)
	}
	if result.Status != HarnessCallFailed {
		t.Fatalf("status = %q, want failed call from error event", result.Status)
	}
	last := result.Items[len(result.Items)-1]
	if last.Kind != "lifecycle" || last.Status != "failed" || !strings.Contains(last.Text, "rate limited") {
		t.Fatalf("last item = %#v, want failed lifecycle with grok error", last)
	}
}

func TestGrokExecutorRejectsNonTerminalStreamingOutput(t *testing.T) {
	runner := &fakeGrokRunner{output: []byte(`{"type":"text","data":"partial"}`)}
	executor := NewGrokExecutorForTest(grokExecutorOptions{Executable: "grok", Cwd: "/repo", RunCommand: runner.Run})
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}

	_, err := StartExecutorRunResult(context.Background(), executor, packet, "", Profile{Name: "builder", Harness: HarnessNameGrok, InvocationClass: HarnessInvocationSticky})
	if err == nil || !strings.Contains(err.Error(), "terminal end event") {
		t.Fatalf("StartExecutorRunResult error = %v, want missing end event error", err)
	}
}

func TestGrokNamedSlotDoesNotUseAmbientAPIKey(t *testing.T) {
	executor := NewGrokExecutorForTest(grokExecutorOptions{Executable: "grok", AuthHomeRoot: t.TempDir(), LookupEnv: func(key string) string {
		if key == "XAI_API_KEY" {
			return "global-key"
		}
		return ""
	}})

	status, err := executor.AuthStatus(context.Background(), HarnessAuthSlot{AuthSlotID: "grok-work", Harness: HarnessNameGrok})
	if err != nil {
		t.Fatalf("AuthStatus returned error: %v", err)
	}
	if status.Status != HarnessAuthRequired {
		t.Fatalf("status = %#v, want named slot to ignore ambient API key", status)
	}
}

func TestGrokAuthStatusRecognizesDocumentedGrokAPIKeyEnv(t *testing.T) {
	executor := NewGrokExecutorForTest(grokExecutorOptions{Executable: "grok", AuthHomeRoot: t.TempDir(), LookupEnv: func(key string) string {
		if key == "GROK_CODE_XAI_API_KEY" {
			return "xai-fake"
		}
		return ""
	}})

	status, err := executor.AuthStatus(context.Background(), HarnessAuthSlot{AuthSlotID: "grok-default", Harness: HarnessNameGrok})
	if err != nil {
		t.Fatalf("AuthStatus returned error: %v", err)
	}
	if status.Status != HarnessAuthAuthenticated {
		t.Fatalf("status = %#v, want authenticated from GROK_CODE_XAI_API_KEY", status)
	}
}

func TestGrokStartReportsProviderErrorBeforeMissingSessionID(t *testing.T) {
	runner := &fakeGrokRunner{output: []byte(`{"type":"error","message":"quota exceeded"}`)}
	executor := NewGrokExecutorForTest(grokExecutorOptions{Executable: "grok", Cwd: "/repo", RunCommand: runner.Run})
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}

	_, err := StartExecutorRunResult(context.Background(), executor, packet, "", Profile{Name: "builder", Harness: HarnessNameGrok, InvocationClass: HarnessInvocationSticky})
	if err == nil || !strings.Contains(err.Error(), "quota exceeded") {
		t.Fatalf("StartExecutorRunResult error = %v, want provider error", err)
	}
}

func TestGrokExecutorRejectsServerInvocationMode(t *testing.T) {
	executor := NewGrokExecutorForTest(grokExecutorOptions{Executable: "grok", Cwd: "/repo", RunCommand: (&fakeGrokRunner{}).Run})
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}

	_, err := StartExecutorRunResult(context.Background(), executor, packet, "", Profile{Name: "builder", Harness: HarnessNameGrok, InvocationClass: HarnessInvocationSticky, Defaults: map[string]any{"invocation_mode": "server"}})
	var validation *HarnessValidationError
	if err == nil || !errors.As(err, &validation) || validation.Field != "invocation_mode" {
		t.Fatalf("error = %v, want invocation_mode validation error", err)
	}
}

func TestGrokAuthStatusUsesAuthJSONPresence(t *testing.T) {
	home := t.TempDir()
	executor := NewGrokExecutorForTest(grokExecutorOptions{Executable: "grok", AuthHomeRoot: t.TempDir(), LookupEnv: func(key string) string {
		if key == "GROK_HOME" {
			return home
		}
		return ""
	}})

	status, err := executor.AuthStatus(context.Background(), HarnessAuthSlot{AuthSlotID: "grok-default", Harness: HarnessNameGrok})
	if err != nil {
		t.Fatalf("AuthStatus returned error: %v", err)
	}
	if status.Status != HarnessAuthRequired || status.Remediation == nil || status.Remediation.Method != "device_code" {
		t.Fatalf("status = %#v, want auth required with device code remediation", status)
	}

	if err := os.WriteFile(filepath.Join(home, "auth.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write auth.json: %v", err)
	}
	status, err = executor.AuthStatus(context.Background(), HarnessAuthSlot{AuthSlotID: "grok-default", Harness: HarnessNameGrok})
	if err != nil {
		t.Fatalf("AuthStatus returned error: %v", err)
	}
	if status.Status != HarnessAuthAuthenticated {
		t.Fatalf("status = %#v, want authenticated from auth.json presence", status)
	}
}

func TestGrokNamedSlotProjectionUsesPerSlotGrokHome(t *testing.T) {
	root := t.TempDir()
	options, err := grokExecutorOptions{Executable: "grok", AuthHomeRoot: root}.withGrokAuthSlotProjection("grok-work")
	if err != nil {
		t.Fatalf("withGrokAuthSlotProjection returned error: %v", err)
	}
	home := options.AuthProjection.Env["GROK_HOME"]
	if options.AuthProjection.Kind != HarnessAuthProjectionConfigRoot || !strings.HasPrefix(home, root) || !strings.Contains(home, "grok-work") {
		t.Fatalf("projection = %#v, want per-slot GROK_HOME under root", options.AuthProjection)
	}
}

func TestGrokAuthStartRelaysDeviceCodeWithoutSecrets(t *testing.T) {
	runner := &fakeGrokRunner{output: []byte("Open https://example.invalid/device and enter code FAKE-CODE\n")}
	executor := NewGrokExecutorForTest(grokExecutorOptions{Executable: "grok", RunAuthCommand: runner.Run, AuthHomeRoot: t.TempDir()})

	status, err := executor.AuthStart(context.Background(), HarnessAuthSlot{AuthSlotID: "grok-default", Harness: HarnessNameGrok}, "device_code")
	if err != nil {
		t.Fatalf("AuthStart returned error: %v", err)
	}
	if status.Status != HarnessAuthInProgress || status.Remediation == nil || status.Remediation.VerificationURL == "" || status.Remediation.UserCode != "FAKE-CODE" {
		t.Fatalf("status = %#v, want in-progress device flow with verification url and user code", status)
	}
	if got := strings.Join(runner.args, " "); got != "login --device-auth" {
		t.Fatalf("auth args = %q, want grok login --device-auth", got)
	}
}

func TestGrokAuthLogoutReportsProviderFailureAfterWaitError(t *testing.T) {
	exitCode := 7
	executor := NewGrokExecutorForTest(grokExecutorOptions{Executable: "grok", AuthHomeRoot: t.TempDir(), RunAuthCommand: func(ctx context.Context, opts grokExecutorOptions, args []string) (commandRunResult, error) {
		_ = ctx
		_ = opts
		if strings.Join(args, " ") != "logout" {
			t.Fatalf("args = %q, want logout", strings.Join(args, " "))
		}
		return commandRunResult{ExitCode: &exitCode}, errors.New("exit status 7")
	}})

	_, err := executor.AuthLogout(context.Background(), HarnessAuthSlot{AuthSlotID: "grok-default", Harness: HarnessNameGrok})
	var unavailable *HarnessUnavailableError
	if !errors.As(err, &unavailable) || unavailable.Reason != "auth_logout_failed" || !unavailable.StartInvoked {
		t.Fatalf("err = %#v, want invoked auth_logout_failed unavailability", err)
	}
}

func TestGrokExecutorStartsAndDeliversAgainstFakeBinary(t *testing.T) {
	fake := buildFakeHarnessExecutable(t)
	stateDir := t.TempDir()
	t.Setenv(fakeharness.EnvHarness, "grok")
	t.Setenv(fakeharness.EnvMode, "authenticated")
	t.Setenv(fakeharness.EnvStateDir, stateDir)
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}

	executor := NewGrokExecutorForTest(grokExecutorOptions{Executable: fake, Cwd: t.TempDir()})
	first, err := StartExecutorRunResult(context.Background(), executor, packet, "", Profile{Name: "builder", Harness: HarnessNameGrok, InvocationClass: HarnessInvocationSticky})
	if err != nil {
		t.Fatalf("StartExecutorRunResult returned error: %v", err)
	}
	if first.HarnessSession.ProviderSessionID != "fake-grok-session" || first.FinalResponse == nil || !strings.Contains(first.FinalResponse.Text, "(turn 1)") {
		t.Fatalf("first run = %#v final = %#v, want fake grok session at turn 1", first.HarnessSession, first.FinalResponse)
	}

	// Delivery resumes the captured session id with -r and counts as turn 2.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	result, err := executor.AttemptWorkspaceDelivery(ctx, WorkspaceDeliveryAttempt{Delivery: globaldb.PendingDelivery{DeliveryID: "pd-grok", WorkspaceID: "ws-1", SubscriptionID: "sub-1", TargetType: "harness_session", TargetID: first.HarnessSession.ProviderSessionID, EventIDs: []string{"we-1"}, Status: "attempted", Attempts: 1}})
	if err != nil {
		t.Fatalf("AttemptWorkspaceDelivery returned error: %v", err)
	}
	if result.Status != WorkspaceDeliveryAttemptCompleted {
		t.Fatalf("delivery result = %#v, want completed grok resume delivery", result)
	}
}

func TestGrokDeliveryRetriesWhenCommandFailsDespiteParsedOutput(t *testing.T) {
	runner := &fakeGrokRunner{output: []byte(strings.Join([]string{`{"type":"text","data":"partial"}`, `{"type":"end","stopReason":"EndTurn","sessionId":"grok-sess-1"}`}, "\n")), err: errors.New("grok exited 1")}
	executor := NewGrokExecutorForTest(grokExecutorOptions{Executable: "grok", Cwd: "/repo", RunCommand: runner.Run})

	result, err := executor.AttemptWorkspaceDelivery(context.Background(), WorkspaceDeliveryAttempt{Delivery: globaldb.PendingDelivery{DeliveryID: "pd-grok", WorkspaceID: "ws-1", SubscriptionID: "sub-1", TargetType: "harness_session", TargetID: "grok-sess-1", EventIDs: []string{"we-1"}, Status: "attempted", Attempts: 1}})
	if err == nil || result.Status != WorkspaceDeliveryAttemptRetry || !strings.Contains(result.LastError, "grok exited 1") {
		t.Fatalf("delivery result = %#v error = %v, want retry preserving command failure", result, err)
	}
}

func TestGrokWorkspaceDeliveryTurnRedactsDurableIDs(t *testing.T) {
	turn := grokWorkspaceDeliveryTurn(WorkspaceDeliveryAttempt{Delivery: globaldb.PendingDelivery{DeliveryID: "pd-secret", WorkspaceID: "ws-1", SubscriptionID: "sub-1", EventIDs: []string{"we-secret"}}})
	if strings.Contains(turn, "pd-secret") || strings.Contains(turn, "we-secret") {
		t.Fatalf("grok delivery turn leaked durable ids: %s", turn)
	}
	if !strings.Contains(turn, `"event_count":1`) {
		t.Fatalf("grok delivery turn = %s, want redacted event_count", turn)
	}
}
