package fakeharness

import (
	"fmt"
	"io"
	"strings"
)

// personaRun bundles everything a persona needs to answer one invocation.
type personaRun struct {
	harness  string
	args     []string
	stdin    string
	stdinRaw io.Reader
	out      *lineSink
	stderr   io.Writer
	sentinel string
	state    sessionStore
	env      map[string]string
}

// turnSuffix tags persona output with the session turn when state is enabled
// so tests can prove a resume reattached instead of starting over.
func (run personaRun) turnSuffix(turn int) string {
	if !run.state.enabled() {
		return ""
	}
	return fmt.Sprintf(" (turn %d)", turn)
}

func authenticated(run personaRun) int {
	args := run.args
	if len(args) >= 2 && args[0] == "login" && args[1] == "--device-auth" {
		return oauthStart(run.out, run.harness)
	}
	if len(args) >= 2 && args[0] == "auth" && args[1] == "logout" {
		run.out.line("logged out")
		return 0
	}
	switch run.harness {
	case "claude":
		return claudePersona(run)
	case "codex":
		return codexPersona(run)
	case "opencode":
		return opencodePersona(run)
	case "pi":
		return piPersona(run)
	case "grok":
		return grokPersona(run)
	default:
		run.out.line(`{"ok":true}`)
		return 0
	}
}

func claudePersona(run personaRun) int {
	if len(run.args) >= 2 && run.args[0] == "auth" && run.args[1] == "status" {
		run.out.line(`{"authenticated":true}`)
		return 0
	}
	if containsExactArg(run.args, "--resume") {
		_, _ = fmt.Fprintln(run.stderr, "claude background sessions cannot be resumed; start a new session")
		return 1
	}
	sessionID := "fake-claude-session"
	turn := run.state.appendTurn(run.harness, sessionID, stdinSummary(run.stdin))
	run.out.linef(`{"result":"fake claude response%s","session_id":"%s","usage":{"input_tokens":1,"output_tokens":1}}`, run.turnSuffix(turn), sessionID)
	return 0
}

func codexPersona(run personaRun) int {
	if len(run.args) >= 2 && run.args[0] == "login" && run.args[1] == "status" {
		run.out.line("Logged in")
		return 0
	}
	if containsExactArg(run.args, "app-server") {
		return codexAppServerEngine(run)
	}
	run.out.line(`{"jsonrpc":"2.0","id":1,"result":{}}`)
	return 0
}

func opencodePersona(run personaRun) int {
	if len(run.args) >= 2 && run.args[0] == "auth" && run.args[1] == "list" {
		run.out.line(`[{"provider":"anthropic","authenticated":true}]`)
		return 0
	}
	if len(run.args) >= 1 && run.args[0] == "serve" {
		return serveOpenCode(run.out)
	}
	sessionID, ok := flagValue(run.args, "--session", "-s")
	if !ok || strings.TrimSpace(sessionID) == "" {
		sessionID = "fake-opencode-session"
	}
	turn := run.state.appendTurn(run.harness, sessionID, stdinSummary(run.stdin))
	run.out.linef(`{"type":"session.status","properties":{"sessionID":"%s"}}`, sessionID)
	run.out.linef(`{"type":"message.part.updated","properties":{"part":{"sessionID":"%s","type":"text","text":"fake opencode response%s"}}}`, sessionID, run.turnSuffix(turn))
	run.out.linef(`{"type":"message.updated","properties":{"info":{"sessionID":"%s","tokens":{"input":1,"output":1}}}}`, sessionID)
	return 0
}

// piPersona answers pi print-mode invocations: `pi -p ...`, `--mode json`,
// session flags (--session, -c, --no-session, --session-dir). RPC mode is
// routed to piRPCEngine before stdin is pre-read.
func piPersona(run personaRun) int {
	if hasFlagValue(run.args, "--mode", "rpc") {
		return piRPCEngine(run)
	}
	sessionID, ephemeral := piSessionFromArgs(run)
	turn := 1
	if !ephemeral {
		turn = run.state.appendTurn(run.harness, sessionID, stdinSummary(run.stdin))
	}
	text := "fake pi response" + run.turnSuffix(turn)
	if !hasFlagValue(run.args, "--mode", "json") {
		run.out.line(text)
		return 0
	}
	sessionPath := run.state.sessionPath(run.harness, sessionID)
	run.out.linef(`{"type":"session_start","sessionPath":%q,"sessionId":%q}`, sessionPath, sessionID)
	run.out.line(`{"type":"agent_start"}`)
	run.out.linef(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":%q}}`, text)
	run.out.linef(`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":%q}],"usage":{"input":1,"output":1},"stopReason":"stop"}}`, text)
	run.out.linef(`{"type":"agent_end","messages":[{"role":"assistant","content":[{"type":"text","text":%q}]}]}`, text)
	return 0
}

func piSessionFromArgs(run personaRun) (sessionID string, ephemeral bool) {
	if containsExactArg(run.args, "--no-session") {
		return "fake-pi-ephemeral", true
	}
	if value, ok := flagValue(run.args, "--session-id"); ok && strings.TrimSpace(value) != "" {
		// --session-id uses the exact project session id, creating it if missing.
		return strings.TrimSpace(value), false
	}
	if value, ok := flagValue(run.args, "--session"); ok && strings.TrimSpace(value) != "" {
		return piSessionIDFromRef(value), false
	}
	if containsExactArg(run.args, "-c") || containsExactArg(run.args, "--continue") {
		if latest := run.state.latestSession(run.harness); latest != "" {
			return latest, false
		}
	}
	return "fake-pi-session", false
}

// piSessionIDFromRef accepts either a session id or a session file path,
// matching pi's `--session <path|id>` flag.
func piSessionIDFromRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if base := strings.TrimSuffix(ref[strings.LastIndex(ref, "/")+1:], ".jsonl"); base != "" {
		return base
	}
	return ref
}

// grokPersona answers grok headless invocations, matching grok 0.2.45:
// `grok -p ...` with --output-format plain|json|streaming-json, -r/-c resume
// flags (-s is TUI-only and ignored headless). `grok login` starts OAuth or
// the device-code flow; `grok logout` clears credentials; `grok agent stdio`
// is routed to the ACP engine.
func grokPersona(run personaRun) int {
	if len(run.args) >= 1 && run.args[0] == "login" {
		if containsExactArg(run.args, "--device-auth") || containsExactArg(run.args, "--device-code") {
			run.out.line("Open https://example.invalid/device and enter code FAKE-CODE")
			return 0
		}
		return oauthStart(run.out, run.harness)
	}
	if len(run.args) >= 1 && run.args[0] == "logout" {
		run.out.line("logged out")
		return 0
	}
	if len(run.args) >= 2 && run.args[0] == "agent" && run.args[1] == "stdio" {
		return grokACPEngine(run)
	}
	sessionID, missing := grokSessionFromArgs(run)
	if missing {
		_, _ = fmt.Fprintf(run.stderr, "no session found: %s\n", sessionID)
		return 1
	}
	turn := run.state.appendTurn(run.harness, sessionID, stdinSummary(run.stdin))
	text := "fake grok response" + run.turnSuffix(turn)
	format, _ := flagValue(run.args, "--output-format")
	switch format {
	case "json":
		run.out.linef(`{"text":%q,"stopReason":"EndTurn","sessionId":%q,"requestId":"fake-grok-request","turn":%d}`, text, sessionID, turn)
	case "streaming-json":
		run.out.linef(`{"type":"text","data":%q}`, text)
		run.out.line(`{"type":"thought","data":"fake grok reasoning"}`)
		run.out.linef(`{"type":"end","stopReason":"EndTurn","sessionId":%q,"requestId":"fake-grok-request","turn":%d}`, sessionID, turn)
	default:
		run.out.line(text)
	}
	return 0
}

func grokSessionFromArgs(run personaRun) (sessionID string, missing bool) {
	// -s/--session-id is honored in the TUI only; headless runs ignore it.
	if value, ok := flagValue(run.args, "-r", "--resume"); ok && strings.TrimSpace(value) != "" {
		// -r resumes an existing session only.
		value = strings.TrimSpace(value)
		if run.state.enabled() && run.state.turnCount(run.harness, value) == 0 {
			return value, true
		}
		return value, false
	}
	if containsExactArg(run.args, "-c") || containsExactArg(run.args, "--continue") {
		if latest := run.state.latestSession(run.harness); latest != "" {
			return latest, false
		}
	}
	return "fake-grok-session", false
}

// partialFailure emits a valid persona-shaped prefix, then a mid-stream error
// and a nonzero exit so consumers prove partial-stream error handling.
func partialFailure(run personaRun) int {
	switch run.harness {
	case "claude":
		run.out.line(`{"result":"fake claude par`)
	case "codex":
		run.out.line(`{"jsonrpc":"2.0","id":1,"result":{"turn":{"id":"fake-codex-turn"}}}`)
		run.out.line(`{"method":"error","params":{"message":"fake codex stream failure"}}`)
	case "opencode":
		run.out.line(`{"type":"session.status","properties":{"sessionID":"fake-opencode-session"}}`)
		run.out.line(`{"type":"error","properties":{"message":"fake opencode stream failure"}}`)
	case "pi":
		run.out.line(`{"type":"agent_start"}`)
		run.out.line(`{"type":"error","message":"fake pi stream failure"}`)
	case "grok":
		run.out.line(`{"type":"text","data":"fake grok par"}`)
		run.out.line(`{"type":"error","message":"fake grok stream failure"}`)
	default:
		run.out.line(`{"type":"error","message":"fake stream failure"}`)
	}
	return 1
}

// authExpiredMidrun starts authenticated and fails with an auth error mid
// stream, proving consumers do not treat early output as final success.
func authExpiredMidrun(run personaRun) int {
	switch run.harness {
	case "claude":
		run.out.line(`{"result":"fake claude response","session_id":"fake-claude-session"}`)
		run.out.line(`{"type":"error","error":{"type":"authentication_error","message":"fake credentials expired"}}`)
	case "opencode":
		run.out.line(`{"type":"session.status","properties":{"sessionID":"fake-opencode-session"}}`)
		run.out.line(`{"type":"error","properties":{"message":"fake credentials expired","code":"auth_expired"}}`)
	default:
		run.out.line(`{"type":"error","error":{"type":"authentication_error","message":"fake credentials expired"}}`)
	}
	return 1
}
