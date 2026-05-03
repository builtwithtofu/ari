package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestCodexExecutorMapsAppServerNotifications(t *testing.T) {
	transport := newFakeCodexTransport([]codexNotification{
		{Method: "thread/started", Params: mustRawJSON(t, `{"thread":{"id":"thr_123"}}`)},
		{Method: "turn/started", Params: mustRawJSON(t, `{"threadId":"thr_123","turn":{"id":"turn_456"}}`)},
		{Method: "item/agentMessage/delta", Params: mustRawJSON(t, `{"threadId":"thr_123","turnId":"turn_456","delta":"hello"}`)},
		{Method: "item/completed", Params: mustRawJSON(t, `{"threadId":"thr_123","turnId":"turn_456","item":{"id":"item_1","type":"agent_message","text":"hello world"}}`)},
		{Method: "thread/tokenUsage/updated", Params: mustRawJSON(t, `{"threadId":"thr_123","turnId":"turn_456","tokenUsage":{"last":{"inputTokens":9,"outputTokens":3},"total":{"inputTokens":9,"outputTokens":3}}}`)},
		{Method: "turn/completed", Params: mustRawJSON(t, `{"threadId":"thr_123","turn":{"id":"turn_456","status":"completed"}}`)},
	})
	executor := NewCodexExecutorForTest(codexExecutorOptions{Executable: "codex", Cwd: "/repo", StartTransport: fakeCodexStarter(transport)})
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}

	run, items, err := StartExecutorRun(context.Background(), executor, packet, AgentProfile{Name: "executor", Model: "gpt-5.1-codex", Prompt: "Do it", InvocationClass: HarnessInvocationAgent})
	if err != nil {
		t.Fatalf("StartExecutorRun returned error: %v", err)
	}
	if run.Executor != HarnessNameCodex || run.ProviderSessionID != "thr_123" || run.ProviderRunID != "turn_456" || run.AgentSessionID == run.ProviderSessionID || !isULID(run.AgentSessionID) {
		t.Fatalf("run = %#v, want Ari run id with Codex provider thread/session and turn/run", run)
	}
	if len(items) != 4 {
		t.Fatalf("items len = %d, want lifecycle/message/token/completed items: %#v", len(items), items)
	}
	if items[0].RunID != run.AgentSessionID || items[0].SourceID != run.AgentSessionID || items[0].Kind != "lifecycle" || items[0].Status != "running" {
		t.Fatalf("first item = %#v, want Ari-linked running lifecycle", items[0])
	}
	if items[1].Kind != "agent_text" || items[1].Text != "hello world" || items[1].ID != run.AgentSessionID+":item_1" || items[1].Metadata["provider_item_id"] != "item_1" || items[1].Metadata["provider_kind"] != "agent_message" {
		t.Fatalf("message item = %#v, want completed agent text", items[1])
	}
	if !transport.closed {
		t.Fatal("transport was not closed after completed Codex turn")
	}
	if got := strings.Join(transport.calledMethods, ","); got != "initialize,initialized,thread/start,turn/start" {
		t.Fatalf("called methods = %q, want initialize/initialized/thread/start/turn/start", got)
	}
	threadStart := transport.paramsByMethod["thread/start"]
	if threadStart["experimentalRawEvents"] != false || threadStart["persistExtendedHistory"] != true || threadStart["approvalPolicy"] != "never" {
		t.Fatalf("thread/start params = %#v, want explicit app-server defaults", threadStart)
	}
}

func TestCodexExecutorAdvertisesFinalResponseCapability(t *testing.T) {
	executor := NewCodexExecutorForTest(codexExecutorOptions{Executable: "codex", Cwd: "/repo", StartTransport: fakeCodexStarter(newFakeCodexTransport(nil))})
	if !harnessCapabilitiesContain(executor.Descriptor().Capabilities, HarnessCapabilityFinalResponse) {
		t.Fatalf("codex capabilities = %#v, want final_response", executor.Descriptor().Capabilities)
	}
}

func TestCodexStdioTransportReadsLargeNotificationLines(t *testing.T) {
	largeText := strings.Repeat("x", 128*1024)
	params, err := json.Marshal(map[string]any{"item": map[string]string{"id": "large", "type": "agent_message", "text": largeText}})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	message := codexRPCMessage{Method: "item/completed", Params: params}
	encoded, err := json.Marshal(message)
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}
	transport := newCodexStdioTransport(nil, nopWriteCloser{Buffer: bytes.NewBuffer(nil)}, bytes.NewReader(append(encoded, '\n')), strings.NewReader(""), 1)
	select {
	case got := <-transport.Notifications():
		if got.Method != "item/completed" || len(got.Params) == 0 {
			t.Fatalf("notification = %#v, want large item/completed", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for large notification")
	}
}

func TestCodexExecutorReportsMissingExecutableBeforeStart(t *testing.T) {
	executor := NewCodexExecutorForTest(codexExecutorOptions{Executable: "missing-codex", Cwd: "/repo", StartTransport: func(ctx context.Context, opts codexExecutorOptions) (codexTransport, error) {
		return nil, &HarnessUnavailableError{Harness: HarnessNameCodex, Reason: "missing_executable", Executable: opts.Executable, Probe: opts.Executable + " --version", RequiredCapability: HarnessCapabilityAgentSessionFromContext, StartInvoked: false}
	}})
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}

	_, _, err := StartExecutorRun(context.Background(), executor, packet)
	unavailable := &HarnessUnavailableError{}
	if !errors.As(err, &unavailable) {
		t.Fatalf("error = %T %[1]v, want HarnessUnavailableError", err)
	}
	if unavailable.StartInvoked || unavailable.Executable != "missing-codex" || unavailable.RequiredCapability != HarnessCapabilityAgentSessionFromContext {
		t.Fatalf("unavailable = %#v, want pre-start missing executable", unavailable)
	}
}

func TestCodexAuthStatusNormalizesProviderOwnedReadiness(t *testing.T) {
	exitCode := 0
	executor := NewCodexExecutorForTest(codexExecutorOptions{Executable: "codex", Cwd: "/repo", RunAuthCommand: func(ctx context.Context, opts codexExecutorOptions, args []string) (commandRunResult, error) {
		_ = ctx
		_ = opts
		if strings.Join(args, " ") != "login status" {
			t.Fatalf("args = %q, want login status", strings.Join(args, " "))
		}
		return commandRunResult{ExitCode: &exitCode}, nil
	}})

	status, err := executor.AuthStatus(context.Background(), HarnessAuthSlot{AuthSlotID: "codex-default", Harness: HarnessNameCodex})
	if err != nil {
		t.Fatalf("AuthStatus returned error: %v", err)
	}
	if status.Status != HarnessAuthAuthenticated || status.AuthSlotID != "codex-default" || status.AriSecretStorage != HarnessAriSecretStorageNone {
		t.Fatalf("status = %#v, want authenticated provider-owned slot", status)
	}
}

func TestCodexAuthStartRelaysDeviceCodeWithoutSecrets(t *testing.T) {
	exitCode := 0
	executor := NewCodexExecutorForTest(codexExecutorOptions{Executable: "codex", Cwd: "/repo", RunAuthCommand: func(ctx context.Context, opts codexExecutorOptions, args []string) (commandRunResult, error) {
		_ = ctx
		_ = opts
		if strings.Join(args, " ") != "login --device-auth" {
			t.Fatalf("args = %q, want login --device-auth", strings.Join(args, " "))
		}
		return commandRunResult{Output: []byte("Open https://codex.example/device and enter code ABCD-EFGH\nlogin id auth_123\n"), ExitCode: &exitCode}, nil
	}})

	status, err := executor.AuthStart(context.Background(), HarnessAuthSlot{AuthSlotID: "codex-default", Harness: HarnessNameCodex}, "device_code")
	if err != nil {
		t.Fatalf("AuthStart returned error: %v", err)
	}
	if status.Status != HarnessAuthInProgress || status.Remediation == nil || status.Remediation.VerificationURL != "https://codex.example/device" || status.Remediation.UserCode != "ABCD-EFGH" || status.Remediation.SecretOwnedBy != HarnessNameCodex || status.AriSecretStorage != HarnessAriSecretStorageNone {
		t.Fatalf("status = %#v, want non-secret device-code remediation", status)
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal status: %v", err)
	}
	if strings.Contains(string(encoded), "access_token") || strings.Contains(string(encoded), "refresh_token") || strings.Contains(string(encoded), "api_key") {
		t.Fatalf("auth start leaked token-like field: %s", encoded)
	}
}

func TestCodexAuthStartRejectsUnsupportedNamedSlotBeforeProviderCommand(t *testing.T) {
	called := false
	executor := NewCodexExecutorForTest(codexExecutorOptions{Executable: "codex", Cwd: "/repo", RunAuthCommand: func(ctx context.Context, opts codexExecutorOptions, args []string) (commandRunResult, error) {
		_ = ctx
		_ = opts
		_ = args
		called = true
		return commandRunResult{}, nil
	}})

	_, err := executor.AuthStart(context.Background(), HarnessAuthSlot{AuthSlotID: "codex-work", Harness: HarnessNameCodex}, "device_code")
	unavailable := &HarnessUnavailableError{}
	if !errors.As(err, &unavailable) {
		t.Fatalf("AuthStart error = %T %[1]v, want HarnessUnavailableError", err)
	}
	if unavailable.Reason != "auth_slot_selection_unsupported" || unavailable.StartInvoked || called {
		t.Fatalf("unavailable = %#v called = %v, want unsupported before provider command", unavailable, called)
	}
}

func TestCodexAuthLogoutRejectsUnsupportedNamedSlotBeforeProviderCommand(t *testing.T) {
	called := false
	executor := NewCodexExecutorForTest(codexExecutorOptions{Executable: "codex", Cwd: "/repo", RunAuthCommand: func(ctx context.Context, opts codexExecutorOptions, args []string) (commandRunResult, error) {
		_ = ctx
		_ = opts
		_ = args
		called = true
		return commandRunResult{}, nil
	}})

	_, err := executor.AuthLogout(context.Background(), HarnessAuthSlot{AuthSlotID: "codex-work", Harness: HarnessNameCodex})
	unavailable := &HarnessUnavailableError{}
	if !errors.As(err, &unavailable) {
		t.Fatalf("AuthLogout error = %T %[1]v, want HarnessUnavailableError", err)
	}
	if unavailable.Reason != "auth_slot_selection_unsupported" || unavailable.StartInvoked || called {
		t.Fatalf("unavailable = %#v called = %v, want unsupported before provider command", unavailable, called)
	}
}

func TestCodexAuthLogoutInvokesProviderLogoutWhenAuthenticated(t *testing.T) {
	exitCode := 0
	var calls []string
	executor := NewCodexExecutorForTest(codexExecutorOptions{Executable: "codex", Cwd: "/repo", RunAuthCommand: func(ctx context.Context, opts codexExecutorOptions, args []string) (commandRunResult, error) {
		_ = ctx
		_ = opts
		calls = append(calls, strings.Join(args, " "))
		return commandRunResult{ExitCode: &exitCode}, nil
	}})

	status, err := executor.AuthLogout(context.Background(), HarnessAuthSlot{AuthSlotID: "codex-default", Harness: HarnessNameCodex})
	if err != nil {
		t.Fatalf("AuthLogout returned error: %v", err)
	}
	if got := strings.Join(calls, ","); got != "login status,logout" {
		t.Fatalf("calls = %q, want login status then logout", got)
	}
	if status.Status != HarnessAuthRequired || status.Remediation == nil || status.Remediation.Method != "device_code" || status.AriSecretStorage != HarnessAriSecretStorageNone {
		t.Fatalf("status = %#v, want provider-owned auth_required remediation", status)
	}
}

func TestCodexAuthLogoutIsIdempotentWhenAlreadyLoggedOut(t *testing.T) {
	statusExitCode := 1
	var calls []string
	executor := NewCodexExecutorForTest(codexExecutorOptions{Executable: "codex", Cwd: "/repo", RunAuthCommand: func(ctx context.Context, opts codexExecutorOptions, args []string) (commandRunResult, error) {
		_ = ctx
		_ = opts
		calls = append(calls, strings.Join(args, " "))
		return commandRunResult{ExitCode: &statusExitCode}, nil
	}})

	status, err := executor.AuthLogout(context.Background(), HarnessAuthSlot{AuthSlotID: "codex-default", Harness: HarnessNameCodex})
	if err != nil {
		t.Fatalf("AuthLogout returned error: %v", err)
	}
	if got := strings.Join(calls, ","); got != "login status" {
		t.Fatalf("calls = %q, want status only for idempotent logout", got)
	}
	if status.Status != HarnessAuthRequired || status.AriSecretStorage != HarnessAriSecretStorageNone {
		t.Fatalf("status = %#v, want auth_required without provider logout command", status)
	}
}

func TestCodexAuthLogoutReportsProviderFailureAfterLogoutCommand(t *testing.T) {
	statusExitCode := 0
	logoutExitCode := 42
	var calls []string
	executor := NewCodexExecutorForTest(codexExecutorOptions{Executable: "codex", Cwd: "/repo", RunAuthCommand: func(ctx context.Context, opts codexExecutorOptions, args []string) (commandRunResult, error) {
		_ = ctx
		_ = opts
		call := strings.Join(args, " ")
		calls = append(calls, call)
		if call == "logout" {
			return commandRunResult{ExitCode: &logoutExitCode}, nil
		}
		return commandRunResult{ExitCode: &statusExitCode}, nil
	}})

	_, err := executor.AuthLogout(context.Background(), HarnessAuthSlot{AuthSlotID: "codex-default", Harness: HarnessNameCodex})
	unavailable := &HarnessUnavailableError{}
	if !errors.As(err, &unavailable) {
		t.Fatalf("AuthLogout error = %T %[1]v, want HarnessUnavailableError", err)
	}
	if got := strings.Join(calls, ","); got != "login status,logout" {
		t.Fatalf("calls = %q, want status then failed logout", got)
	}
	if unavailable.Reason != "auth_logout_failed" || !unavailable.StartInvoked {
		t.Fatalf("unavailable = %#v, want failed provider logout after invocation", unavailable)
	}
}

func TestCodexAuthStartRejectsUnsupportedMethodBeforeProviderCommand(t *testing.T) {
	called := false
	executor := NewCodexExecutorForTest(codexExecutorOptions{Executable: "codex", Cwd: "/repo", RunAuthCommand: func(ctx context.Context, opts codexExecutorOptions, args []string) (commandRunResult, error) {
		_ = ctx
		_ = opts
		_ = args
		called = true
		return commandRunResult{}, nil
	}})

	_, err := executor.AuthStart(context.Background(), HarnessAuthSlot{AuthSlotID: "codex-default", Harness: HarnessNameCodex}, "sso")
	unavailable := &HarnessUnavailableError{}
	if !errors.As(err, &unavailable) {
		t.Fatalf("AuthStart error = %T %[1]v, want HarnessUnavailableError", err)
	}
	if unavailable.Reason != "auth_method_unsupported" || unavailable.StartInvoked || called {
		t.Fatalf("unavailable = %#v called = %v, want unsupported method before provider command", unavailable, called)
	}
}

func TestCodexAuthStartReturnsBrowserLoginClientHandoff(t *testing.T) {
	called := false
	executor := NewCodexExecutorForTest(codexExecutorOptions{Executable: "codex", Cwd: "/repo", RunAuthCommand: func(ctx context.Context, opts codexExecutorOptions, args []string) (commandRunResult, error) {
		_ = ctx
		_ = opts
		_ = args
		called = true
		return commandRunResult{}, nil
	}})

	status, err := executor.AuthStart(context.Background(), HarnessAuthSlot{AuthSlotID: "codex-default", Harness: HarnessNameCodex}, "browser")
	if err != nil {
		t.Fatalf("AuthStart returned error: %v", err)
	}
	if status.Status != HarnessAuthRequired || status.Remediation == nil || status.Remediation.Method != "client_provider_login" || called {
		t.Fatalf("status = %#v called = %v, want client-side provider login handoff", status, called)
	}
}

func TestCodexAuthStartReturnsProviderOwnedAPIKeyGuidance(t *testing.T) {
	called := false
	executor := NewCodexExecutorForTest(codexExecutorOptions{Executable: "codex", Cwd: "/repo", RunAuthCommand: func(ctx context.Context, opts codexExecutorOptions, args []string) (commandRunResult, error) {
		_ = ctx
		_ = opts
		_ = args
		called = true
		return commandRunResult{}, nil
	}})

	status, err := executor.AuthStart(context.Background(), HarnessAuthSlot{AuthSlotID: "codex-default", Harness: HarnessNameCodex}, "api_key")
	if err != nil {
		t.Fatalf("AuthStart returned error: %v", err)
	}
	if status.Status != HarnessAuthRequired || status.Remediation == nil || status.Remediation.Method != "api_key_provider_setup" || called {
		t.Fatalf("status = %#v called = %v, want provider-owned API key guidance without command", status, called)
	}
}

func TestCodexExecutorFailsWhenNotificationStreamEndsBeforeTurnCompletes(t *testing.T) {
	transport := newFakeCodexTransport([]codexNotification{
		{Method: "thread/started", Params: mustRawJSON(t, `{"thread":{"id":"thr_123"}}`)},
		{Method: "item/completed", Params: mustRawJSON(t, `{"threadId":"thr_123","turnId":"turn_456","item":{"id":"item_1","type":"agent_message","text":"partial"}}`)},
	})
	executor := NewCodexExecutorForTest(codexExecutorOptions{Executable: "codex", Cwd: "/repo", StartTransport: fakeCodexStarter(transport)})
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}

	_, _, err := StartExecutorRun(context.Background(), executor, packet)
	if err == nil || !strings.Contains(err.Error(), "ended before turn completed") {
		t.Fatalf("StartExecutorRun error = %v, want failed incomplete Codex stream", err)
	}
}

func TestCodexStdioTransportReturnsWhenServerExitsBeforeResponse(t *testing.T) {
	stdoutReader, stdoutWriter := io.Pipe()
	_ = stdoutWriter.Close()
	transport := newCodexStdioTransport(nil, nopWriteCloser{Buffer: bytes.NewBuffer(nil)}, stdoutReader, strings.NewReader(""), 4)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)

	var result codexInitializeResult
	err := transport.Call(ctx, "initialize", nil, &result)
	if err == nil || !strings.Contains(err.Error(), "closed before initialize response") {
		t.Fatalf("Call error = %v, want prompt closed-before-response error", err)
	}
}

func TestCodexStdioTransportNotificationOverflowDoesNotBlockResponse(t *testing.T) {
	responseID := int64(1)
	messages := []codexRPCMessage{
		{Method: "thread/started", Params: mustRawJSON(t, `{"thread":{"id":"thr_123"}}`)},
		{Method: "item/completed", Params: mustRawJSON(t, `{"item":{"id":"item_1","type":"agent_message","text":"first"}}`)},
		{Method: "item/completed", Params: mustRawJSON(t, `{"item":{"id":"item_2","type":"agent_message","text":"second"}}`)},
		{ID: &responseID, Result: mustRawJSON(t, `{"userAgent":"codex/0.0"}`)},
	}
	var encoded []byte
	for _, message := range messages {
		line, err := json.Marshal(message)
		if err != nil {
			t.Fatalf("marshal message: %v", err)
		}
		encoded = append(encoded, append(line, '\n')...)
	}
	transport := newCodexStdioTransport(nil, nopWriteCloser{Buffer: bytes.NewBuffer(nil)}, bytes.NewReader(encoded), strings.NewReader(""), 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)

	var result codexInitializeResult
	if err := transport.Call(ctx, "initialize", nil, &result); err != nil {
		t.Fatalf("Call returned error after notification overflow: %v", err)
	}
}

func TestCodexStdioTransportPreservesTerminalNotificationOnOverflow(t *testing.T) {
	responseID := int64(1)
	messages := []codexRPCMessage{
		{Method: "item/completed", Params: mustRawJSON(t, `{"item":{"id":"item_1","type":"agent_message","text":"first"}}`)},
		{Method: "turn/completed", Params: mustRawJSON(t, `{"turn":{"id":"turn_456","status":"completed"}}`)},
		{ID: &responseID, Result: mustRawJSON(t, `{"userAgent":"codex/0.0"}`)},
	}
	var encoded []byte
	for _, message := range messages {
		line, err := json.Marshal(message)
		if err != nil {
			t.Fatalf("marshal message: %v", err)
		}
		encoded = append(encoded, append(line, '\n')...)
	}
	transport := newCodexStdioTransport(nil, nopWriteCloser{Buffer: bytes.NewBuffer(nil)}, bytes.NewReader(encoded), strings.NewReader(""), 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)

	var result codexInitializeResult
	if err := transport.Call(ctx, "initialize", nil, &result); err != nil {
		t.Fatalf("Call returned error after terminal notification overflow: %v", err)
	}
	select {
	case terminal := <-transport.Notifications():
		if terminal.Method != "turn/completed" {
			t.Fatalf("terminal notification = %#v, want turn/completed", terminal)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for preserved terminal notification")
	}
}

func mustRawJSON(t *testing.T, value string) json.RawMessage {
	t.Helper()
	if !json.Valid([]byte(value)) {
		t.Fatalf("invalid fixture JSON: %s", value)
	}
	return json.RawMessage(value)
}

type nopWriteCloser struct {
	*bytes.Buffer
}

func (w nopWriteCloser) Close() error { return nil }

type fakeCodexTransport struct {
	notifications  chan codexNotification
	calledMethods  []string
	paramsByMethod map[string]map[string]any
	closed         bool
}

func newFakeCodexTransport(notifications []codexNotification) *fakeCodexTransport {
	ch := make(chan codexNotification, len(notifications))
	for _, notification := range notifications {
		ch <- notification
	}
	close(ch)
	return &fakeCodexTransport{notifications: ch, paramsByMethod: map[string]map[string]any{}}
}

func fakeCodexStarter(transport *fakeCodexTransport) codexTransportStarter {
	return func(ctx context.Context, opts codexExecutorOptions) (codexTransport, error) {
		_ = ctx
		_ = opts
		return transport, nil
	}
}

func (t *fakeCodexTransport) Call(ctx context.Context, method string, params any, result any) error {
	_ = ctx
	t.calledMethods = append(t.calledMethods, method)
	if params != nil {
		encodedParams, err := json.Marshal(params)
		if err != nil {
			return err
		}
		var decoded map[string]any
		if err := json.Unmarshal(encodedParams, &decoded); err != nil {
			return err
		}
		t.paramsByMethod[method] = decoded
	}
	encoded := []byte(`{}`)
	switch method {
	case "initialize":
		encoded = []byte(`{"userAgent":"codex/0.0","codexHome":"/tmp/codex","platformFamily":"linux","platformOs":"linux"}`)
	case "thread/start":
		encoded = []byte(`{"thread":{"id":"thr_123"}}`)
	case "turn/start":
		encoded = []byte(`{"turn":{"id":"turn_456"}}`)
	}
	return json.Unmarshal(encoded, result)
}

func (t *fakeCodexTransport) Notify(ctx context.Context, method string, params any) error {
	_ = ctx
	_ = params
	t.calledMethods = append(t.calledMethods, method)
	return nil
}

func (t *fakeCodexTransport) Notifications() <-chan codexNotification {
	return t.notifications
}

func (t *fakeCodexTransport) PID() int { return 0 }

func (t *fakeCodexTransport) ProcessSample(context.Context) *ProcessMetricsSample { return nil }

func (t *fakeCodexTransport) Close() error {
	t.closed = true
	return nil
}
