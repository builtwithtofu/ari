package fakeharness

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

const (
	EnvHarness  = "ARI_FAKE_HARNESS"
	EnvMode     = "ARI_FAKE_HARNESS_MODE"
	EnvRecord   = "ARI_FAKE_HARNESS_RECORD"
	EnvSentinel = "ARI_FAKE_HARNESS_SENTINEL"
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
	mode := strings.TrimSpace(env[EnvMode])
	if mode == "" {
		mode = "authenticated"
	}
	args := []string(nil)
	if len(argv) > 1 {
		args = append(args, argv[1:]...)
	}
	stdin := ""
	if shouldReadStdin(args) {
		stdin = readAvailable(r.Stdin)
	}
	if sentinel := env[EnvSentinel]; sentinel != "" && (strings.Contains(stdin, sentinel) || containsArg(args, sentinel)) {
		_, _ = fmt.Fprintln(r.Stderr, "fake harness sentinel leak trap: sentinel observed in process input")
		return 86
	}
	inv := Invocation{Harness: harness, Mode: mode, Args: args, Env: safeEnv(env), Projection: projectionSummary(env), Stdin: stdinSummary(stdin)}
	if err := recordInvocation(env[EnvRecord], inv); err != nil {
		_, _ = fmt.Fprintf(r.Stderr, "fake harness record: %v\n", err)
		return 1
	}
	switch mode {
	case "auth-required":
		return authRequired(r.Stdout, harness)
	case "malformed", "unknown-output":
		_, _ = fmt.Fprintln(r.Stdout, "{not-json")
		return 0
	case "logout-success":
		_, _ = fmt.Fprintln(r.Stdout, "logged out")
		return 0
	case "oauth-start":
		return oauthStart(r.Stdout, harness)
	default:
		return authenticated(r.Stdout, harness, args)
	}
}

func authenticated(w io.Writer, harness string, args []string) int {
	if len(args) >= 2 && args[0] == "login" && args[1] == "--device-auth" {
		return oauthStart(w, harness)
	}
	if len(args) >= 2 && args[0] == "auth" && args[1] == "logout" {
		writeLine(w, "logged out")
		return 0
	}
	switch harness {
	case "claude":
		if len(args) >= 2 && args[0] == "auth" && args[1] == "status" {
			writeLine(w, `{"authenticated":true}`)
			return 0
		}
		writeLine(w, `{"result":"fake claude response","session_id":"fake-claude-session","usage":{"input_tokens":1,"output_tokens":1}}`)
	case "codex":
		if len(args) >= 2 && args[0] == "login" && args[1] == "status" {
			writeLine(w, "Logged in")
			return 0
		}
		writeLine(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
	case "opencode":
		if len(args) >= 2 && args[0] == "auth" && args[1] == "list" {
			writeLine(w, `[{"provider":"anthropic","authenticated":true}]`)
			return 0
		}
		if len(args) >= 1 && args[0] == "serve" {
			return serveOpenCode(w)
		}
		writeLine(w, `{"type":"session.status","properties":{"sessionID":"fake-opencode-session"}}`)
		writeLine(w, `{"type":"message.part.updated","properties":{"part":{"sessionID":"fake-opencode-session","type":"text","text":"fake opencode response"}}}`)
		writeLine(w, `{"type":"message.updated","properties":{"info":{"sessionID":"fake-opencode-session","tokens":{"input":1,"output":1}}}}`)
	default:
		writeLine(w, `{"ok":true}`)
	}
	return 0
}

func writeLine(w io.Writer, line string) {
	_, _ = fmt.Fprintln(w, line)
}

func authRequired(w io.Writer, harness string) int {
	if harness == "claude" {
		writeLine(w, `{"authenticated":false}`)
	} else {
		writeLine(w, "not authenticated")
	}
	return 1
}

func oauthStart(w io.Writer, harness string) int {
	switch harness {
	case "codex":
		writeLine(w, "Open https://example.invalid/device and enter code FAKE-CODE")
	default:
		_, _ = fmt.Fprintf(w, "%s auth URL: https://example.invalid/oauth/start\n", harness)
	}
	return 0
}

func serveOpenCode(w io.Writer) int {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_, _ = fmt.Fprintf(w, "listen failed: %v\n", err)
		return 1
	}
	defer func() { _ = listener.Close() }()
	mux := http.NewServeMux()
	mux.HandleFunc("/provider", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"connected":["anthropic"]}`) })
	mux.HandleFunc("/provider/auth", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"anthropic":[{"type":"oauth","label":"browser"}]}`)
	})
	server := &http.Server{Handler: mux}
	_, _ = fmt.Fprintf(w, "http://%s\n", listener.Addr().String())
	if flusher, ok := w.(interface{ Flush() }); ok {
		flusher.Flush()
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		_, _ = fmt.Fprintf(w, "serve failed: %v\n", err)
		return 1
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

func shouldReadStdin(args []string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-p" && args[i+1] == "-" {
			return true
		}
	}
	return false
}

func stdinSummary(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(sum[:8])
}

func safeEnv(env map[string]string) map[string]string {
	keys := []string{"HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "CLAUDE_CONFIG_DIR", "CODEX_HOME", EnvHarness, EnvMode}
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
	for _, k := range []string{"CLAUDE_CONFIG_DIR", "CODEX_HOME", "OPENCODE_AUTH_CONTENT"} {
		if v, ok := env[k]; ok && strings.TrimSpace(v) != "" {
			if k == "OPENCODE_AUTH_CONTENT" {
				sum := sha256.Sum256([]byte(v))
				out[k] = "present sha256:" + hex.EncodeToString(sum[:8])
			} else {
				out[k] = v
			}
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
