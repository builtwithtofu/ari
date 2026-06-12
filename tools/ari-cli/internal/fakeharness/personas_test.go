package fakeharness

import (
	"bytes"
	"strings"
	"testing"
)

func runFake(t *testing.T, stdin string, env []string, argv ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Runner{Stdin: strings.NewReader(stdin), Stdout: &stdout, Stderr: &stderr, Env: env}.Run(argv)
	return code, stdout.String(), stderr.String()
}

func TestStatefulSessionsTagTurnsAcrossResumes(t *testing.T) {
	t.Parallel()
	state := t.TempDir()
	cases := []struct {
		name     string
		harness  string
		first    []string
		resume   []string
		wantTurn string
	}{
		{name: "opencode session flag", harness: "opencode", first: []string{"run", "--format", "json", "--session", "sess-a", "hi"}, resume: []string{"run", "--format", "json", "--session", "sess-a", "again"}, wantTurn: "(turn 2)"},
		{name: "pi session flag", harness: "pi", first: []string{"-p", "--mode", "json", "--session", "sess-pi", "hi"}, resume: []string{"-p", "--mode", "json", "--session", "sess-pi", "again"}, wantTurn: "(turn 2)"},
		{name: "grok resume flag", harness: "grok", first: []string{"-p", "hi", "--output-format", "json"}, resume: []string{"-p", "again", "-r", "fake-grok-session", "--output-format", "json"}, wantTurn: "(turn 2)"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			env := []string{EnvHarness + "=" + tc.harness, EnvStateDir + "=" + state}
			code, first, stderr := runFake(t, "", env, append([]string{"fake-" + tc.harness}, tc.first...)...)
			if code != 0 || !strings.Contains(first, "(turn 1)") {
				t.Fatalf("first run code=%d stdout=%q stderr=%q, want turn 1", code, first, stderr)
			}
			code, second, stderr := runFake(t, "", env, append([]string{"fake-" + tc.harness}, tc.resume...)...)
			if code != 0 || !strings.Contains(second, tc.wantTurn) {
				t.Fatalf("resume run code=%d stdout=%q stderr=%q, want %s", code, second, stderr, tc.wantTurn)
			}
		})
	}
}

func TestStatelessRunsOmitTurnTags(t *testing.T) {
	t.Parallel()
	code, stdout, _ := runFake(t, "", []string{EnvHarness + "=claude"}, "fake-claude", "--bg", "do it")
	if code != 0 || strings.Contains(stdout, "(turn") {
		t.Fatalf("stateless run code=%d stdout=%q, want no turn tag", code, stdout)
	}
}

func TestClaudeRejectsResumeAsRestartOnly(t *testing.T) {
	t.Parallel()
	code, _, stderr := runFake(t, "", []string{EnvHarness + "=claude"}, "fake-claude", "--resume", "abc123")
	if code != 1 || !strings.Contains(stderr, "cannot be resumed") {
		t.Fatalf("code=%d stderr=%q, want restart-only rejection", code, stderr)
	}
}

func TestGrokResumeFlagRequiresExistingSession(t *testing.T) {
	t.Parallel()
	state := t.TempDir()
	env := []string{EnvHarness + "=grok", EnvStateDir + "=" + state}
	code, _, stderr := runFake(t, "", env, "fake-grok", "-p", "hi", "-r", "missing-session")
	if code != 1 || !strings.Contains(stderr, "no session found") {
		t.Fatalf("code=%d stderr=%q, want missing session error", code, stderr)
	}
	if code, _, _ := runFake(t, "", env, "fake-grok", "-p", "hi"); code != 0 {
		t.Fatalf("seed session failed")
	}
	code, stdout, _ := runFake(t, "", env, "fake-grok", "-p", "hi", "-r", "fake-grok-session", "--output-format", "json")
	if code != 0 || !strings.Contains(stdout, `"turn":2`) {
		t.Fatalf("code=%d stdout=%q, want resumed turn 2", code, stdout)
	}
}

func TestGrokContinueUsesMostRecentSession(t *testing.T) {
	t.Parallel()
	state := t.TempDir()
	env := []string{EnvHarness + "=grok", EnvStateDir + "=" + state}
	if code, _, _ := runFake(t, "", env, "fake-grok", "-p", "hi"); code != 0 {
		t.Fatal("seed failed")
	}
	code, stdout, _ := runFake(t, "", env, "fake-grok", "-p", "hi", "-c", "--output-format", "json")
	if code != 0 || !strings.Contains(stdout, `"sessionId":"fake-grok-session"`) || !strings.Contains(stdout, `"turn":2`) {
		t.Fatalf("code=%d stdout=%q, want continue of most recent session at turn 2", code, stdout)
	}
}

func TestPiPrintModeEmitsJSONEventStream(t *testing.T) {
	t.Parallel()
	code, stdout, _ := runFake(t, "", []string{EnvHarness + "=pi"}, "fake-pi", "-p", "--mode", "json", "build it")
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	for _, want := range []string{`"type":"session_start"`, `"type":"agent_start"`, `"type":"message_update"`, `"usage":{"input":1,"output":1}`, `"type":"agent_end"`} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout %q missing %q", stdout, want)
		}
	}
}

func TestGrokStreamingJSONEmitsEventStream(t *testing.T) {
	t.Parallel()
	code, stdout, _ := runFake(t, "", []string{EnvHarness + "=grok"}, "fake-grok", "-p", "hi", "--output-format", "streaming-json")
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	for _, want := range []string{`"type":"text"`, `"type":"thought"`, `"type":"end"`, `"sessionId":"fake-grok-session"`} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout %q missing %q", stdout, want)
		}
	}
}

func TestCodexAppServerEngineAnswersInteractively(t *testing.T) {
	t.Parallel()
	stdin := strings.Join([]string{
		`{"id":1,"method":"initialize","params":{"clientInfo":{"name":"ari"}}}`,
		`{"method":"initialized"}`,
		`{"id":2,"method":"thread/start","params":{}}`,
		`{"id":3,"method":"turn/start","params":{"threadId":"fake-codex-thread"}}`,
	}, "\n") + "\n"
	code, stdout, stderr := runFake(t, stdin, []string{EnvHarness + "=codex"}, "fake-codex", "app-server", "--listen", "stdio://")
	if code != 0 {
		t.Fatalf("exit code = %d stderr=%q", code, stderr)
	}
	for _, want := range []string{`"thread":{"id":"fake-codex-thread"}`, `"method":"item/completed"`, `"method":"thread/tokenUsage/updated"`, `"method":"turn/completed"`} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout %q missing %q", stdout, want)
		}
	}
}

func TestPiRPCEngineAnswersPromptAndState(t *testing.T) {
	t.Parallel()
	state := t.TempDir()
	stdin := strings.Join([]string{
		`{"id":"req-1","type":"prompt","message":"build it"}`,
		`{"type":"get_state"}`,
		`{"type":"switch_session","sessionPath":"/sessions/other-session.jsonl"}`,
		`{"id":"req-2","type":"prompt","message":"continue"}`,
	}, "\n") + "\n"
	code, stdout, stderr := runFake(t, stdin, []string{EnvHarness + "=pi", EnvStateDir + "=" + state}, "fake-pi", "--mode", "rpc")
	if code != 0 {
		t.Fatalf("exit code = %d stderr=%q", code, stderr)
	}
	for _, want := range []string{`"command":"prompt","success":true,"id":"req-1"`, `"type":"agent_end"`, `"command":"get_state"`, `"sessionId":"fake-pi-session"`, `"command":"switch_session"`, `"id":"req-2"`} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout %q missing %q", stdout, want)
		}
	}
}

func TestGrokACPEngineAnswersPrompt(t *testing.T) {
	t.Parallel()
	stdin := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
		`{"jsonrpc":"2.0","id":2,"method":"session/new"}`,
		`{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"sessionId":"fake-grok-session","prompt":"hi"}}`,
	}, "\n") + "\n"
	code, stdout, stderr := runFake(t, stdin, []string{EnvHarness + "=grok"}, "fake-grok", "agent", "stdio")
	if code != 0 {
		t.Fatalf("exit code = %d stderr=%q", code, stderr)
	}
	for _, want := range []string{`"sessionId":"fake-grok-session"`, `"method":"session/update"`, `"stopReason":"end_turn"`} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout %q missing %q", stdout, want)
		}
	}
}

func TestInteractiveEngineSentinelTrap(t *testing.T) {
	t.Parallel()
	sentinel := "ARI_SENTINEL_SECRET_do_not_record"
	stdin := `{"id":"req-1","type":"prompt","message":"leak ` + sentinel + `"}` + "\n"
	code, _, stderr := runFake(t, stdin, []string{EnvHarness + "=pi", EnvSentinel + "=" + sentinel}, "fake-pi", "--mode", "rpc")
	if code != 86 || !strings.Contains(stderr, "sentinel leak trap") {
		t.Fatalf("code=%d stderr=%q, want sentinel trap", code, stderr)
	}
}

func TestFailureModes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		harness string
		mode    string
		args    []string
		want    string
		code    int
	}{
		{"rate limit", "claude", "exit-rate-limit", []string{"--bg", "hi"}, "rate_limit_error", 1},
		{"partial failure opencode", "opencode", "partial-failure", []string{"run", "--format", "json", "hi"}, `"type":"error"`, 1},
		{"partial failure pi", "pi", "partial-failure", []string{"-p", "hi"}, "fake pi stream failure", 1},
		{"auth expired midrun", "claude", "auth-expired-midrun", []string{"--bg", "hi"}, "authentication_error", 1},
		{"explicit exit code", "claude", "authenticated,exit-code:7", []string{"--bg", "hi"}, "fake claude response", 7},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, _ := runFake(t, "", []string{EnvHarness + "=" + tc.harness, EnvMode + "=" + tc.mode}, append([]string{"fake-" + tc.harness}, tc.args...)...)
			if code != tc.code || !strings.Contains(stdout, tc.want) {
				t.Fatalf("code=%d stdout=%q, want code=%d containing %q", code, stdout, tc.code, tc.want)
			}
		})
	}
}

func TestModeListCombinesBehaviorAndStreaming(t *testing.T) {
	t.Parallel()
	code, stdout, _ := runFake(t, "", []string{EnvHarness + "=grok", EnvMode + "=authenticated,stream-incremental"}, "fake-grok", "-p", "hi", "--output-format", "streaming-json")
	if code != 0 || !strings.Contains(stdout, `"type":"end"`) {
		t.Fatalf("code=%d stdout=%q, want streamed result", code, stdout)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %d (%q), want 3 NDJSON events", len(lines), stdout)
	}
}

func TestProjectionSummaryHashesProviderKeys(t *testing.T) {
	t.Parallel()
	summary := projectionSummary(map[string]string{"ANTHROPIC_API_KEY": "secret", "XAI_API_KEY": "secret", "GROK_HOME": "/tmp/grok"})
	if !strings.HasPrefix(summary["ANTHROPIC_API_KEY"], "present sha256:") || !strings.HasPrefix(summary["XAI_API_KEY"], "present sha256:") {
		t.Fatalf("summary = %#v, want hashed provider keys", summary)
	}
	if summary["GROK_HOME"] != "/tmp/grok" {
		t.Fatalf("summary = %#v, want grok home recorded verbatim", summary)
	}
	if strings.Contains(summary["ANTHROPIC_API_KEY"], "secret") {
		t.Fatalf("summary leaked secret: %#v", summary)
	}
}

func TestInteractiveEngineReportsScannerErrors(t *testing.T) {
	t.Parallel()
	longLine := strings.Repeat("x", 4*1024*1024+1) + "\n"
	code, _, stderr := runFake(t, longLine, []string{EnvHarness + "=pi"}, "fake-pi", "--mode", "rpc")
	if code == 0 || !strings.Contains(stderr, "stdin read error") {
		t.Fatalf("code=%d stderr=%q, want scanner error failure", code, stderr)
	}
}
