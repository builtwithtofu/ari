package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"
)

func main() {
	if os.Getenv("ARI_AUTH_LIVE_SMOKE") != "1" {
		fmt.Println("auth-live-smoke: skipped; set ARI_AUTH_LIVE_SMOKE=1 to run opt-in real harness OAuth initiation smoke")
		return
	}
	tmp, err := os.MkdirTemp("", "ari-auth-live-smoke-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	for _, r := range []result{smokeClaude(tmp), smokeCodex(tmp), smokeOpenCode(tmp), smokePi(tmp), smokeGrok(tmp)} {
		fmt.Printf("%s: %s pathway=%s %s\n", r.harness, r.status, r.pathway, r.detail)
	}
}

type result struct{ harness, status, pathway, detail string }

type unsupportedError struct{ message string }

func (e unsupportedError) Error() string { return e.message }

func smokeClaude(tmp string) result {
	path, ok := executable("claude", "ARI_CLAUDE_EXECUTABLE")
	if !ok {
		return result{"claude", "skipped", "claude_auth_login_console", "executable not found"}
	}
	if runtime.GOOS == "darwin" {
		return result{"claude", "unsafe", "claude_auth_login_console", "macOS Keychain isolation cannot be proven by this smoke"}
	}
	extra := map[string]string{"CLAUDE_CONFIG_DIR": filepath.Join(tmp, "claude")}
	statusOut, statusErr := runBounded(tmp, extra, path, "auth", "status", "--json")
	if statusErr == nil && claudeStatusAuthenticated(statusOut) {
		return result{"claude", "unsafe", "claude_auth_login_console", "temp CLAUDE_CONFIG_DIR saw existing authentication before login"}
	}
	out, err := runBounded(tmp, extra, path, "auth", "login", "--console")
	return classify("claude", "claude_auth_login_console", out, err)
}

func smokeCodex(tmp string) result {
	path, ok := executable("codex", "ARI_CODEX_EXECUTABLE")
	if !ok {
		return result{"codex", "skipped", "codex_device_code", "executable not found"}
	}
	out, err := runBounded(tmp, map[string]string{"CODEX_HOME": filepath.Join(tmp, "codex")}, path, "login", "--device-auth")
	return classify("codex", "codex_device_code", out, err)
}

func smokeOpenCode(tmp string) result {
	path, ok := executable("opencode", "ARI_OPENCODE_EXECUTABLE")
	if !ok {
		return result{"opencode", "skipped", "opencode_provider_auth", "executable not found"}
	}
	out, err := runOpenCodeProviderAuth(tmp, path)
	return classify("opencode", "opencode_provider_auth", out, err)
}

func smokePi(tmp string) result {
	if _, ok := executable("pi", "ARI_PI_EXECUTABLE"); !ok {
		return result{"pi", "skipped", "pi_provider_env_key", "executable not found"}
	}
	// pi has no provider login command; auth is provider env keys. There is
	// no OAuth initiation to smoke, so report the pathway as unsupported.
	_ = tmp
	return result{"pi", "unsupported", "pi_provider_env_key", "pi auth is provider env keys; no login flow to initiate"}
}

func smokeGrok(tmp string) result {
	path, ok := executable("grok", "ARI_GROK_EXECUTABLE")
	if !ok {
		return result{"grok", "skipped", "grok_device_code", "executable not found"}
	}
	out, err := runBounded(tmp, map[string]string{"GROK_HOME": filepath.Join(tmp, "grok")}, path, "login", "--device-auth")
	return classify("grok", "grok_device_code", out, err)
}

func executable(name, env string) (string, bool) {
	if v := strings.TrimSpace(os.Getenv(env)); v != "" {
		return v, true
	}
	p, err := exec.LookPath(name)
	return p, err == nil
}

func runBounded(tmp string, extra map[string]string, exe string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Env = smokeEnv(tmp, extra)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		return "", err
	}
	err := waitCommand(ctx, cmd)
	return redact(out.String()), err
}

func smokeEnv(tmp string, extra map[string]string) []string {
	env := minimalEnv()
	env = append(env, "HOME="+filepath.Join(tmp, "home"), "XDG_CONFIG_HOME="+filepath.Join(tmp, "xdg-config"), "XDG_DATA_HOME="+filepath.Join(tmp, "xdg-data"))
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
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

func runOpenCodeProviderAuth(tmp, exe string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	password := "ari-live-smoke-local"
	cmd := exec.CommandContext(ctx, exe, "serve", "--port", "0", "--hostname", "127.0.0.1")
	cmd.Env = smokeEnv(tmp, map[string]string{"OPENCODE_SERVER_PASSWORD": password})
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return "", err
	}
	defer func() {
		stopProcessGroup(cmd)
		_ = cmd.Wait()
	}()
	serverURL, transcript, err := readServerURL(ctx, stdout)
	if err != nil {
		return transcript, err
	}
	client := http.Client{Timeout: 3 * time.Second}
	methods, err := getOpenCodeEndpoint(ctx, &client, serverURL+"/provider/auth", password)
	if err != nil {
		return transcript, err
	}
	if !strings.Contains(strings.ToLower(methods), "oauth") && !strings.Contains(strings.ToLower(methods), "browser") {
		return transcript + "\nprovider auth methods returned no bounded OAuth/browser method", unsupportedError{message: "opencode provider auth initiation unsupported"}
	}
	return "provider auth methods observed; bounded authorize endpoint not implemented", unsupportedError{message: "opencode bounded authorize endpoint is not implemented in Ari"}
}

func claudeStatusAuthenticated(out string) bool {
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	return strings.Contains(lower, "authenticated") && !strings.Contains(lower, "not authenticated") && !strings.Contains(lower, "false")
}

func readServerURL(ctx context.Context, r io.Reader) (string, string, error) {
	lines := make(chan string, 1)
	go func() {
		s := bufio.NewScanner(r)
		for s.Scan() {
			lines <- s.Text()
		}
		close(lines)
	}()
	var transcript []string
	for {
		select {
		case <-ctx.Done():
			return "", strings.Join(transcript, "\n"), ctx.Err()
		case line, ok := <-lines:
			if !ok {
				return "", strings.Join(transcript, "\n"), fmt.Errorf("opencode serve exited before URL")
			}
			line = redact(line)
			transcript = append(transcript, line)
			if i := strings.Index(line, "http://"); i >= 0 {
				return strings.TrimSpace(line[i:]), strings.Join(transcript, "\n"), nil
			}
		}
	}
}

func getOpenCodeEndpoint(ctx context.Context, client *http.Client, url, password string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("opencode:"+password)))
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return redact(string(body)), fmt.Errorf("%s returned HTTP %d", url, resp.StatusCode)
	}
	return redact(string(body)), nil
}

func stopProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		return
	}
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	time.Sleep(500 * time.Millisecond)
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
}

func waitCommand(ctx context.Context, cmd *exec.Cmd) error {
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		stopProcessGroup(cmd)
		<-done
		return ctx.Err()
	}
}

func classify(harness, pathway, out string, err error) result {
	text := strings.TrimSpace(out)
	var unsupported unsupportedError
	if errors.As(err, &unsupported) {
		return result{harness, "unsupported", pathway, firstLine(text)}
	}
	if text == "" && err != nil {
		return result{harness, "failed", pathway, err.Error()}
	}
	if err != nil {
		return result{harness, "failed", pathway, firstLine(text)}
	}
	lower := strings.ToLower(text)
	if hasAuthInitiationSignal(lower) {
		return result{harness, "exercised", pathway, text}
	}
	return result{harness, "unsupported", pathway, firstLine(text)}
}

func hasAuthInitiationSignal(lower string) bool {
	return strings.Contains(lower, "http://") || strings.Contains(lower, "https://") || strings.Contains(lower, "localhost") || regexp.MustCompile(`(?i)(user\s*)?code[:\s]+[a-z0-9-]{4,}`).MatchString(lower)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func redact(s string) string {
	if token := strings.TrimSpace(os.Getenv("ARI_AUTH_LIVE_SMOKE_TOKEN")); token != "" {
		s = strings.ReplaceAll(s, token, "[redacted]")
	}
	s = regexp.MustCompile(`(?i)(access_token|refresh_token|id_token|token|code|state)=([^\s&]+)`).ReplaceAllString(s, "$1=[redacted]")
	s = regexp.MustCompile(`(?i)(bearer\s+)[a-z0-9._~+/=-]+`).ReplaceAllString(s, "$1[redacted]")
	s = regexp.MustCompile(`(?i)((?:user\s*)?code[:\s]+)[a-z0-9-]{4,}`).ReplaceAllString(s, "$1[redacted]")
	return s
}
