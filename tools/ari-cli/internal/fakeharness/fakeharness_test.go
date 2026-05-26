package fakeharness

import (
	"bytes"
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
