package fakeharness

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestRunnerDispatchesHarnessModesAndRecordsSafely(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	record := dir + "/invocations.jsonl"
	sentinel := "ARI_SENTINEL_SECRET_do_not_record"
	cases := []struct {
		name, harness, mode string
		args                []string
		want                string
		code                int
	}{
		{"claude status", "claude", "authenticated", []string{"auth", "status", "--json"}, `"authenticated":true`, 0},
		{"codex oauth", "codex", "oauth-start", []string{"login", "--device-auth"}, "FAKE-CODE", 0},
		{"opencode malformed", "opencode", "malformed", []string{"run", "--format", "json", "hi"}, "{not-json", 0},
		{"logout", "claude", "logout-success", []string{"auth", "logout"}, "logged out", 0},
		{"auth required", "codex", "auth-required", []string{"login", "status"}, "not authenticated", 1},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Runner{Stdin: strings.NewReader("prompt without secret"), Stdout: &stdout, Stderr: &stderr, Env: []string{EnvHarness + "=" + tc.harness, EnvMode + "=" + tc.mode, EnvRecord + "=" + record, EnvSentinel + "=" + sentinel, "OPENCODE_AUTH_CONTENT=" + sentinel, "CODEX_HOME=" + dir + "/codex"}}.Run(append([]string{"fake-harness"}, tc.args...))
			if code != tc.code {
				t.Fatalf("exit code = %d, want %d, stderr %s", code, tc.code, stderr.String())
			}
			if !strings.Contains(stdout.String(), tc.want) {
				t.Fatalf("stdout %q does not contain %q", stdout.String(), tc.want)
			}
		})
	}
	data, err := os.ReadFile(record)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if strings.Contains(string(data), sentinel) {
		t.Fatalf("record leaked sentinel: %s", data)
	}
	inv, err := DecodeInvocations(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(inv) != len(cases) {
		t.Fatalf("recorded %d invocations, want %d", len(inv), len(cases))
	}
	if got := inv[0].Projection["OPENCODE_AUTH_CONTENT"]; !strings.HasPrefix(got, "present sha256:") {
		t.Fatalf("projection summary = %q", got)
	}
}

func TestRunnerSentinelLeakTrapFailsBeforeRecordingRawSecret(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	record := dir + "/invocations.jsonl"
	sentinel := "ARI_SENTINEL_SECRET_do_not_record"
	var stdout, stderr bytes.Buffer
	code := Runner{Stdin: strings.NewReader("leak " + sentinel), Stdout: &stdout, Stderr: &stderr, Env: []string{EnvHarness + "=claude", EnvMode + "=authenticated", EnvRecord + "=" + record, EnvSentinel + "=" + sentinel}}.Run([]string{"fake-claude", "--bare", "-p", "-", "--output-format", "json"})
	if code != 86 {
		t.Fatalf("exit code = %d, want 86", code)
	}
	if !strings.Contains(stderr.String(), "sentinel leak trap") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	data, err := os.ReadFile(record)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if strings.Contains(string(data), sentinel) {
		t.Fatalf("record leaked sentinel: %s", data)
	}
}

func TestSafeEnvDoesNotRecordSensitiveAmbientAuth(t *testing.T) {
	t.Parallel()
	env := safeEnv(map[string]string{
		"HOME":                  "/tmp/home",
		"OPENCODE_AUTH_CONTENT": "secret",
		"ANTHROPIC_API_KEY":     "secret",
		"CODEX_HOME":            "/tmp/codex",
	})
	if env["HOME"] == "" || env["CODEX_HOME"] == "" {
		t.Fatalf("safe env missing expected non-secret keys: %#v", env)
	}
	if env["OPENCODE_AUTH_CONTENT"] != "" || env["ANTHROPIC_API_KEY"] != "" {
		t.Fatalf("safe env leaked sensitive keys: %#v", env)
	}
}

func TestRunnerProvidesDeliveryProofSurfaces(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	record := dir + "/delivery-invocations.jsonl"
	cases := []struct {
		name, harness, mode string
		args                []string
		stdin               string
		want                []string
	}{
		{name: "claude managed pty", harness: "claude", mode: "delivery-claude-pty", args: []string{"managed-pty"}, stdin: "visible task for claude\n", want: []string{`"channel":"managed_pty"`, `"status":"completed"`}},
		{name: "codex app server", harness: "codex", mode: "delivery-codex-app-server", args: []string{"app-server"}, stdin: `{"jsonrpc":"2.0","id":1,"method":"turn/start","params":{"prompt":"visible task for codex"}}`, want: []string{`"method":"turn/completed"`, `"turn_id":"fake-codex-turn"`}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Runner{Stdin: strings.NewReader(tc.stdin), Stdout: &stdout, Stderr: &stderr, Env: []string{EnvHarness + "=" + tc.harness, EnvMode + "=" + tc.mode, EnvRecord + "=" + record}}.Run(append([]string{"fake-" + tc.harness}, tc.args...))
			if code != 0 {
				t.Fatalf("exit code = %d, stderr %s", code, stderr.String())
			}
			for _, want := range tc.want {
				if !strings.Contains(stdout.String(), want) {
					t.Fatalf("stdout %q does not contain %q", stdout.String(), want)
				}
			}
		})
	}
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	invocations, err := DecodeInvocations(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(invocations) != len(cases) {
		t.Fatalf("recorded %d invocations, want %d", len(invocations), len(cases))
	}
	for _, inv := range invocations {
		if inv.Stdin == "" || strings.Contains(inv.Stdin, "visible task") {
			t.Fatalf("invocation stdin summary = %q, want hashed non-raw prompt", inv.Stdin)
		}
	}
}

func TestOpenCodeDeliveryHandlerAcceptsPromptAndExposesCompletionEvent(t *testing.T) {
	t.Parallel()
	var recorded OpenCodePromptDelivery
	server := httptest.NewServer(OpenCodeDeliveryHandler(func(delivery OpenCodePromptDelivery) {
		recorded = delivery
	}))
	t.Cleanup(server.Close)

	response, err := http.Post(server.URL+"/api/session/sess_123/prompt", "application/json", strings.NewReader(`{"text":"visible task for opencode","delivery":"queue","idempotency_key":"pd-1"}`))
	if err != nil {
		t.Fatalf("post prompt returned error: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll prompt response returned error: %v", err)
	}
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), `"status":"queued"`) || recorded.SessionID != "sess_123" || recorded.IdempotencyKey != "pd-1" || recorded.Delivery != "queue" {
		t.Fatalf("prompt response status=%d body=%s recorded=%#v", response.StatusCode, body, recorded)
	}

	events, err := http.Get(server.URL + "/api/session/sess_123/events")
	if err != nil {
		t.Fatalf("get events returned error: %v", err)
	}
	defer func() { _ = events.Body.Close() }()
	eventsBody, err := io.ReadAll(events.Body)
	if err != nil {
		t.Fatalf("ReadAll events returned error: %v", err)
	}
	if events.StatusCode != http.StatusOK || !strings.Contains(string(eventsBody), `"type":"session.idle"`) || !strings.Contains(string(eventsBody), `"prompt_id":"fake-opencode-prompt"`) {
		t.Fatalf("events response status=%d body=%s", events.StatusCode, eventsBody)
	}
}
