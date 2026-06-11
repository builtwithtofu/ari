package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const sentinel = "ARI_SENTINEL_SECRET_do_not_record"

type proofEnv struct {
	tmp    string
	ari    string
	record string
	folder string
	env    []string
}

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "auth-proof-fake: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	proof, cleanup, err := setupProofEnv()
	if err != nil {
		return err
	}
	defer cleanup()

	daemonCtx, cancelDaemon := context.WithCancel(context.Background())
	defer cancelDaemon()
	daemonCmd := exec.CommandContext(daemonCtx, proof.ari, "daemon", "start", "--background-child")
	daemonCmd.Env = proof.env
	daemonCmd.Stdout = os.Stdout
	daemonCmd.Stderr = os.Stderr
	if err := daemonCmd.Start(); err != nil {
		return fmt.Errorf("start ari daemon: %w", err)
	}
	defer func() {
		_ = runAri(proof, "daemon", "stop")
		if err := waitProcess(daemonCmd, 5*time.Second); err != nil {
			cancelDaemon()
			_ = daemonCmd.Wait()
		}
	}()
	if err := waitForDaemon(proof); err != nil {
		return err
	}
	workspaceID, err := createProofWorkspace(proof)
	if err != nil {
		return err
	}
	if out, err := runAriWithInput(proof, "", "api", "workspace.add_folder", "--params", fmt.Sprintf(`{"workspace_id":%q,"folder_path":%q}`, workspaceID, proof.folder)); err != nil {
		return fmt.Errorf("attach proof workspace folder: %w\n%s", err, out)
	}
	if err := runAri(proof, "profile", "create", "proof-opencode", "--workspace-id", workspaceID, "--harness", "opencode", "--prompt", "proof", "--invocation-class", "sticky"); err != nil {
		return fmt.Errorf("create opencode proof profile: %w", err)
	}
	if err := runAri(proof, "profile", "create", "proof-pi", "--workspace-id", workspaceID, "--harness", "pi", "--prompt", "proof", "--invocation-class", "sticky"); err != nil {
		return fmt.Errorf("create pi proof profile: %w", err)
	}

	checks := []struct {
		name  string
		stdin string
		args  []string
		want  []string
	}{
		{name: "doctor", args: []string{"auth", "doctor", "--discover-methods", "--detailed"}, want: []string{"claude", "codex", "opencode", "pi"}},
		{name: "status", args: []string{"auth", "status"}, want: []string{"claude", "codex", "opencode", "pi", "authenticated"}},
		{name: "codex-device-login", stdin: "2\n", args: []string{"auth", "login", "--harness", "codex", "--name", "proof"}, want: []string{"auth_in_progress", "FAKE-CODE"}},
		{name: "claude-console-login", stdin: "2\n", args: []string{"auth", "login", "--harness", "claude", "--name", "proof"}, want: []string{"authenticated"}},
		{name: "claude-logout", args: []string{"auth", "logout", "--harness", "claude", "--name", "proof"}, want: []string{"auth_required"}},
		{name: "opencode-auth-methods", args: []string{"api", "auth.provider_methods", "--params", `{"harness":"opencode"}`}, want: []string{"anthropic", "browser"}},
		{name: "opencode-session", args: []string{"session", "start", "proof-opencode", "--workspace", workspaceID, "--session", "proof-opencode", "--message", "hello"}, want: []string{"Session started: proof-opencode"}},
		{name: "pi-session", args: []string{"session", "start", "proof-pi", "--workspace", workspaceID, "--session", "proof-pi", "--message", "hello"}, want: []string{"Session started: proof-pi"}},
	}
	for _, check := range checks {
		out, err := runAriWithInput(proof, check.stdin, check.args...)
		if err != nil {
			return fmt.Errorf("ari %s failed: %w\n%s", check.name, err, out)
		}
		for _, want := range check.want {
			if !strings.Contains(strings.ToLower(out), strings.ToLower(want)) {
				return fmt.Errorf("ari %s output missing %q:\n%s", check.name, want, out)
			}
		}
		fmt.Printf("ok ari-boundary %s\n", check.name)
	}

	if err := runFakeSelfChecks(proof); err != nil {
		return err
	}
	if err := assertRecordSafe(proof.record); err != nil {
		return err
	}
	fmt.Printf("recorded safe fake harness invocations at %s (temporary, removed on exit)\n", proof.record)
	fmt.Println("auth-proof-fake passed")
	return nil
}

func createProofWorkspace(proof proofEnv) (string, error) {
	out, err := runAriWithInput(proof, "", "api", "workspace.create", "--params", `{"name":"Proof","cleanup_policy":"manual"}`)
	if err != nil {
		return "", fmt.Errorf("create proof workspace: %w\n%s", err, out)
	}
	var resp struct {
		WorkspaceID string `json:"workspace_id"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		return "", fmt.Errorf("decode proof workspace response: %w: %s", err, out)
	}
	if strings.TrimSpace(resp.WorkspaceID) == "" {
		return "", fmt.Errorf("create proof workspace returned no workspace_id: %s", out)
	}
	return resp.WorkspaceID, nil
}

func setupProofEnv() (proofEnv, func(), error) {
	tmp, err := os.MkdirTemp("", "ari-auth-proof-fake-")
	if err != nil {
		return proofEnv{}, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	ari := filepath.Join(tmp, "ari")
	fake := filepath.Join(tmp, "fake-harness")
	folder, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		cleanup()
		return proofEnv{}, nil, err
	}
	if out, err := exec.Command("go", "build", "-o", ari, ".").CombinedOutput(); err != nil {
		cleanup()
		return proofEnv{}, nil, fmt.Errorf("build ari: %w: %s", err, out)
	}
	if out, err := exec.Command("go", "build", "-o", fake, "./cmd/fake-harness").CombinedOutput(); err != nil {
		cleanup()
		return proofEnv{}, nil, fmt.Errorf("build fake harness: %w: %s", err, out)
	}
	fakes := map[string]string{"claude": "fake-claude", "codex": "fake-codex", "opencode": "fake-opencode", "pi": "fake-pi"}
	for _, link := range fakes {
		if err := os.Symlink(fake, filepath.Join(tmp, link)); err != nil {
			cleanup()
			return proofEnv{}, nil, fmt.Errorf("link %s: %w", link, err)
		}
	}
	record := filepath.Join(tmp, "invocations.jsonl")
	env := append(
		minimalEnv(),
		"ARI_DAEMON_SOCKET_PATH="+filepath.Join(tmp, "daemon.sock"),
		"ARI_DAEMON_DB_PATH="+filepath.Join(tmp, "ari.db"),
		"ARI_DAEMON_PID_PATH="+filepath.Join(tmp, "daemon.pid"),
		"ARI_CLAUDE_EXECUTABLE="+filepath.Join(tmp, fakes["claude"]),
		"ARI_CODEX_EXECUTABLE="+filepath.Join(tmp, fakes["codex"]),
		"ARI_OPENCODE_EXECUTABLE="+filepath.Join(tmp, fakes["opencode"]),
		"ARI_PI_EXECUTABLE="+filepath.Join(tmp, fakes["pi"]),
		"ANTHROPIC_API_KEY=fake-proof-key",
		"ARI_FAKE_HARNESS_RECORD="+record,
		"ARI_FAKE_HARNESS_SENTINEL="+sentinel,
		"HOME="+filepath.Join(tmp, "home"),
		"XDG_CONFIG_HOME="+filepath.Join(tmp, "xdg-config"),
		"XDG_DATA_HOME="+filepath.Join(tmp, "xdg-data"),
	)
	return proofEnv{tmp: tmp, ari: ari, record: record, folder: folder, env: env}, cleanup, nil
}

func minimalEnv() []string {
	keys := []string{"PATH", "TERM", "NIX_SSL_CERT_FILE", "SSL_CERT_FILE", "USER", "LOGNAME"}
	env := make([]string, 0, len(keys))
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			env = append(env, key+"="+value)
		}
	}
	return env
}

func waitProcess(cmd *exec.Cmd, timeout time.Duration) error {
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		return fmt.Errorf("process did not exit within %s", timeout)
	}
}

func waitForDaemon(proof proofEnv) error {
	deadline := time.Now().Add(8 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		out, err := runAriWithInput(proof, "", "daemon", "status")
		last = out
		if err == nil && strings.Contains(out, "Daemon: running") {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("daemon did not become ready: %s", last)
}

func runAri(proof proofEnv, args ...string) error {
	_, err := runAriWithInput(proof, "", args...)
	return err
}

func runAriWithInput(proof proofEnv, stdin string, args ...string) (string, error) {
	cmd := exec.Command(proof.ari, args...)
	cmd.Env = proof.env
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

func runFakeSelfChecks(proof proofEnv) error {
	checks := []struct {
		name, harness, mode string
		args                []string
		want                string
	}{
		{"auth-required", "codex", "auth-required", []string{"login", "status"}, "not authenticated"},
		{"malformed", "opencode", "malformed", []string{"run", "--format", "json", "hi"}, "{not-json"},
	}
	for _, check := range checks {
		cmd := exec.Command(filepath.Join(proof.tmp, "fake-harness"), check.args...)
		cmd.Env = append(proof.env, "ARI_FAKE_HARNESS="+check.harness, "ARI_FAKE_HARNESS_MODE="+check.mode)
		out, err := cmd.CombinedOutput()
		if err != nil && check.mode != "auth-required" {
			return fmt.Errorf("fake %s failed: %w: %s", check.name, err, out)
		}
		if !strings.Contains(string(out), check.want) {
			return fmt.Errorf("fake %s output missing %q: %s", check.name, check.want, out)
		}
		fmt.Printf("ok fake-mode %s\n", check.name)
	}
	trap := exec.Command(filepath.Join(proof.tmp, "fake-claude"), "--bare", "-p", "-", "--output-format", "json")
	trap.Env = proof.env
	trap.Stdin = strings.NewReader("leak " + sentinel)
	if err := trap.Run(); err == nil {
		return fmt.Errorf("sentinel leak trap did not fail")
	}
	return nil
}

func assertRecordSafe(record string) error {
	data, err := os.ReadFile(record)
	if err != nil {
		return err
	}
	if strings.Contains(string(data), sentinel) {
		return fmt.Errorf("sentinel leaked into proof record")
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	found := map[string]bool{}
	for {
		var inv struct {
			Harness    string            `json:"harness"`
			Projection map[string]string `json:"projection"`
		}
		if err := dec.Decode(&inv); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("decode proof record: %w", err)
		}
		found[inv.Harness] = true
		if inv.Projection["CLAUDE_CONFIG_DIR"] != "" {
			found["CLAUDE_CONFIG_DIR"] = true
		}
		if inv.Projection["CODEX_HOME"] != "" {
			found["CODEX_HOME"] = true
		}
		if strings.HasPrefix(inv.Projection["OPENCODE_AUTH_CONTENT"], "present") {
			found["OPENCODE_AUTH_CONTENT"] = true
		}
	}
	for _, want := range []string{"claude", "codex", "opencode", "pi", "CODEX_HOME", "CLAUDE_CONFIG_DIR"} {
		if !found[want] {
			return fmt.Errorf("proof record missing %q", want)
		}
	}
	return nil
}
