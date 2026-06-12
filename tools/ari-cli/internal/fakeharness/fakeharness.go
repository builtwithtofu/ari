package fakeharness

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	EnvHarness  = "ARI_FAKE_HARNESS"
	EnvMode     = "ARI_FAKE_HARNESS_MODE"
	EnvRecord   = "ARI_FAKE_HARNESS_RECORD"
	EnvSentinel = "ARI_FAKE_HARNESS_SENTINEL"
	// EnvStateDir enables stateful fake sessions: each run appends a turn to
	// `<state>/<harness>/sessions/<id>.jsonl` so resume flags can be proven
	// to reattach instead of silently starting over.
	EnvStateDir = "ARI_FAKE_HARNESS_STATE_DIR"
)

type Invocation struct {
	Harness    string            `json:"harness"`
	Mode       string            `json:"mode"`
	Args       []string          `json:"args"`
	Env        map[string]string `json:"env,omitempty"`
	Projection map[string]string `json:"projection,omitempty"`
	Stdin      string            `json:"stdin,omitempty"`
}

type Runner struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Env    []string
}

// runModes is the parsed ARI_FAKE_HARNESS_MODE value. The mode env accepts a
// comma-separated list so behavior modes can be combined with output
// modifiers, e.g. `authenticated,stream-incremental`.
type runModes struct {
	behavior          string
	streamIncremental bool
	exitCode          *int
}

func parseModes(raw string) runModes {
	modes := runModes{behavior: "authenticated"}
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		switch {
		case entry == "":
		case entry == "stream-incremental":
			modes.streamIncremental = true
		case strings.HasPrefix(entry, "exit-code:"):
			if code, err := strconv.Atoi(strings.TrimPrefix(entry, "exit-code:")); err == nil {
				modes.exitCode = &code
			}
		default:
			modes.behavior = entry
		}
	}
	return modes
}

func (r Runner) Run(argv []string) int {
	if r.Stdin == nil {
		r.Stdin = os.Stdin
	}
	if r.Stdout == nil {
		r.Stdout = os.Stdout
	}
	if r.Stderr == nil {
		r.Stderr = os.Stderr
	}
	if r.Env == nil {
		r.Env = os.Environ()
	}
	env := envMap(r.Env)
	harness := strings.TrimSpace(env[EnvHarness])
	if harness == "" {
		harness = harnessFromArgv(argv)
	}
	modes := parseModes(env[EnvMode])
	args := []string(nil)
	if len(argv) > 1 {
		args = append(args, argv[1:]...)
	}
	sentinel := env[EnvSentinel]
	if sentinel != "" && containsArg(args, sentinel) {
		_, _ = fmt.Fprintln(r.Stderr, "fake harness sentinel leak trap: sentinel observed in process input")
		return 86
	}
	interactive := isInteractiveInvocation(harness, args, modes.behavior)
	stdin := ""
	if !interactive && shouldReadStdin(modes.behavior, args) {
		stdin = readAvailable(r.Stdin)
	}
	if sentinel != "" && strings.Contains(stdin, sentinel) {
		_, _ = fmt.Fprintln(r.Stderr, "fake harness sentinel leak trap: sentinel observed in process input")
		return 86
	}
	inv := Invocation{Harness: harness, Mode: strings.TrimSpace(env[EnvMode]), Args: args, Env: safeEnv(env), Projection: projectionSummary(env), Stdin: stdinSummary(stdin)}
	if inv.Mode == "" {
		inv.Mode = "authenticated"
	}
	if err := recordInvocation(env[EnvRecord], inv); err != nil {
		_, _ = fmt.Fprintf(r.Stderr, "fake harness record: %v\n", err)
		return 1
	}
	out := newLineSink(r.Stdout, modes.streamIncremental)
	run := personaRun{
		harness:  harness,
		args:     args,
		stdin:    stdin,
		stdinRaw: r.Stdin,
		out:      out,
		stderr:   r.Stderr,
		sentinel: sentinel,
		state:    newSessionStore(env[EnvStateDir]),
		env:      env,
	}
	code := dispatch(run, modes)
	if modes.exitCode != nil {
		return *modes.exitCode
	}
	return code
}

func dispatch(run personaRun, modes runModes) int {
	switch modes.behavior {
	case "auth-required":
		return authRequired(run.out, run.harness)
	case "delivery-claude-pty":
		return deliveryClaudePTY(run.out, run.stdin)
	case "delivery-codex-app-server":
		return deliveryCodexAppServer(run.out, run.stdin)
	case "malformed", "unknown-output":
		run.out.line("{not-json")
		return 0
	case "logout-success":
		run.out.line("logged out")
		return 0
	case "oauth-start":
		return oauthStart(run.out, run.harness)
	case "hang":
		return hangUntilSignal()
	case "exit-rate-limit":
		run.out.line(`{"type":"error","error":{"type":"rate_limit_error","message":"fake rate limit"},"retryable":true}`)
		return 1
	case "partial-failure":
		return partialFailure(run)
	case "auth-expired-midrun":
		return authExpiredMidrun(run)
	default:
		return authenticated(run)
	}
}

// hangUntilSignal blocks until SIGTERM/SIGINT so timeout and cancellation
// paths can be exercised against a real process.
func hangUntilSignal() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	return 143
}

// lineSink writes whole output lines, optionally one OS write at a time with
// short pauses so consumers are exercised against partial-stream reads.
type lineSink struct {
	w           io.Writer
	incremental bool
}

func newLineSink(w io.Writer, incremental bool) *lineSink {
	return &lineSink{w: w, incremental: incremental}
}

func (s *lineSink) line(line string) {
	_, _ = fmt.Fprintln(s.w, line)
	if s.incremental {
		if flusher, ok := s.w.(interface{ Flush() }); ok {
			flusher.Flush()
		}
		if syncer, ok := s.w.(interface{ Sync() error }); ok {
			_ = syncer.Sync()
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func (s *lineSink) linef(format string, args ...any) {
	s.line(fmt.Sprintf(format, args...))
}

func authRequired(w *lineSink, harness string) int {
	switch harness {
	case "claude":
		w.line(`{"authenticated":false}`)
	case "pi":
		w.line("No API key configured for provider anthropic")
	default:
		w.line("not authenticated")
	}
	return 1
}

func oauthStart(w *lineSink, harness string) int {
	switch harness {
	case "codex", "grok":
		w.line("Open https://example.invalid/device and enter code FAKE-CODE")
	default:
		w.linef("%s auth URL: https://example.invalid/oauth/start", harness)
	}
	return 0
}

func envMap(env []string) map[string]string {
	m := map[string]string{}
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			m[k] = v
		}
	}
	return m
}

func harnessFromArgv(argv []string) string {
	if len(argv) == 0 {
		return "fake"
	}
	b := filepath.Base(argv[0])
	return strings.TrimPrefix(b, "fake-")
}

func readAvailable(r io.Reader) string {
	var b strings.Builder
	_, _ = io.Copy(&b, r)
	return b.String()
}

func containsArg(args []string, s string) bool {
	for _, a := range args {
		if strings.Contains(a, s) {
			return true
		}
	}
	return false
}

// isInteractiveInvocation reports whether this invocation speaks an
// interactive line protocol on stdin/stdout (RPC engines). Interactive
// invocations must not pre-read stdin to EOF: residents block until close.
func isInteractiveInvocation(harness string, args []string, behavior string) bool {
	if behavior != "authenticated" {
		return false
	}
	switch harness {
	case "codex":
		return containsExactArg(args, "app-server")
	case "pi":
		return hasFlagValue(args, "--mode", "rpc")
	case "grok":
		return len(args) >= 2 && args[0] == "agent" && args[1] == "stdio"
	default:
		return false
	}
}

func containsExactArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func hasFlagValue(args []string, flag, value string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

func flagValue(args []string, names ...string) (string, bool) {
	for i := 0; i < len(args); i++ {
		for _, name := range names {
			if args[i] == name && i+1 < len(args) {
				return args[i+1], true
			}
		}
	}
	return "", false
}

func shouldReadStdin(behavior string, args []string) bool {
	if behavior == "delivery-claude-pty" || behavior == "delivery-codex-app-server" {
		return true
	}
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-p" && args[i+1] == "-" {
			return true
		}
	}
	return false
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(sum[:8])
}

func stdinSummary(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return shortHash(s)
}

func safeEnv(env map[string]string) map[string]string {
	keys := []string{"HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "CLAUDE_CONFIG_DIR", "CODEX_HOME", "GROK_HOME", EnvHarness, EnvMode, EnvStateDir}
	out := map[string]string{}
	for _, k := range keys {
		if v := strings.TrimSpace(env[k]); v != "" {
			out[k] = v
		}
	}
	return out
}

func projectionSummary(env map[string]string) map[string]string {
	out := map[string]string{}
	for _, k := range []string{"CLAUDE_CONFIG_DIR", "CODEX_HOME", "GROK_HOME"} {
		if v, ok := env[k]; ok && strings.TrimSpace(v) != "" {
			out[k] = v
		}
	}
	for _, k := range []string{"OPENCODE_AUTH_CONTENT", "ANTHROPIC_API_KEY", "XAI_API_KEY"} {
		if v, ok := env[k]; ok && strings.TrimSpace(v) != "" {
			sum := sha256.Sum256([]byte(v))
			out[k] = "present sha256:" + hex.EncodeToString(sum[:8])
		}
	}
	return out
}

func recordInvocation(path string, inv Invocation) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	return enc.Encode(inv)
}

func DecodeInvocations(r io.Reader) ([]Invocation, error) {
	var out []Invocation
	s := bufio.NewScanner(r)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		var inv Invocation
		if err := json.Unmarshal([]byte(line), &inv); err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, s.Err()
}

func SortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
