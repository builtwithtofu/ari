package cmd

import (
	"bytes"
	"context"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/config"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/spf13/cobra"
)

func TestAuthDisplayNameUsesPublicNameNotInternalSlotID(t *testing.T) {
	name := authDisplayName(daemon.HarnessAuthStatus{Harness: daemon.HarnessNameCodex, Name: "work", AuthSlotID: "codex-work"})
	if name != "work" {
		t.Fatalf("authDisplayName = %q, want public account name", name)
	}
}

func TestAuthDisplayNameUsesDefaultForDefaultAccount(t *testing.T) {
	name := authDisplayName(daemon.HarnessAuthStatus{Harness: daemon.HarnessNameCodex, AuthSlotID: "codex-default"})
	if name != "default" {
		t.Fatalf("authDisplayName = %q, want default", name)
	}
}

func TestAuthSlotIDForNameUsesDefaultAccount(t *testing.T) {
	if got := authSlotIDForName(daemon.HarnessNameCodex, ""); got != "codex-default" {
		t.Fatalf("authSlotIDForName = %q, want codex-default", got)
	}
}

func TestAuthSlotIDForNameKeepsAriNameSeparateFromOpenCodeProvider(t *testing.T) {
	if got := authSlotIDForName(daemon.HarnessNameOpenCode, "work"); got != "opencode-work" {
		t.Fatalf("authSlotIDForName = %q, want Ari account slot id", got)
	}
	args := authProviderLoginArgs(daemon.HarnessNameOpenCode, "opencode_interactive", "")
	want := []string{"auth", "login"}
	if len(args) != len(want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args = %#v, want %#v", args, want)
		}
	}
}

func TestPromptAuthHarnessShowsProviderPicker(t *testing.T) {
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetIn(bytes.NewBufferString("2\n"))

	selected, err := promptAuthHarness(cmd, []string{"codex", "claude"})
	if err != nil {
		t.Fatalf("promptAuthHarness returned error: %v", err)
	}
	if selected != "claude" {
		t.Fatalf("selected = %q, want claude", selected)
	}
	if !bytes.Contains(out.Bytes(), []byte("Choose provider:")) {
		t.Fatalf("output = %q, want provider picker", out.String())
	}
}

func TestPromptAuthLoginMethodShowsProviderOwnedOptions(t *testing.T) {
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetIn(bytes.NewBufferString("2\n"))

	selected, err := promptAuthLoginMethod(cmd, authLoginMethods(daemon.HarnessNameCodex))
	if err != nil {
		t.Fatalf("promptAuthLoginMethod returned error: %v", err)
	}
	if selected != "device_code" {
		t.Fatalf("selected = %q, want device_code", selected)
	}
	if got := out.String(); !bytes.Contains([]byte(got), []byte("ChatGPT account / browser login")) || !bytes.Contains([]byte(got), []byte("Device code")) {
		t.Fatalf("output = %q, want Codex login options", got)
	}
}

func TestWriteAuthLoginResponsePrintsProviderOwnedFlow(t *testing.T) {
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := writeAuthLoginResponse(cmd, daemon.HarnessAuthStatus{Harness: daemon.HarnessNameCodex, Name: "default", Status: daemon.HarnessAuthInProgress, Remediation: &daemon.HarnessAuthRemediation{VerificationURL: "https://codex.example/device", UserCode: "ABCD-EFGH"}})
	if err != nil {
		t.Fatalf("writeAuthLoginResponse returned error: %v", err)
	}
	if got := out.String(); !bytes.Contains([]byte(got), []byte("Open: https://codex.example/device")) || !bytes.Contains([]byte(got), []byte("Code: ABCD-EFGH")) {
		t.Fatalf("output = %q, want device auth remediation", got)
	}
}

func TestWriteAuthLogoutResponseIsIdempotentForLoggedOutAccount(t *testing.T) {
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := writeAuthLogoutResponse(cmd, daemon.HarnessAuthStatus{Harness: daemon.HarnessNameCodex, AuthSlotID: "codex-default", Status: daemon.HarnessAuthRequired})
	if err != nil {
		t.Fatalf("writeAuthLogoutResponse returned error: %v", err)
	}
	if got := out.String(); !bytes.Contains([]byte(got), []byte("codex auth auth_required for default")) || !bytes.Contains([]byte(got), []byte("Already logged out")) {
		t.Fatalf("output = %q, want idempotent logout message", got)
	}
}

func TestAuthLogoutCommandUsesDefaultAccountWhenNameOmitted(t *testing.T) {
	restore := replaceAuthCommandDeps(t)
	defer restore()
	var captured daemon.HarnessAuthLogoutRequest
	authLogoutRPC = func(ctx context.Context, socketPath string, req daemon.HarnessAuthLogoutRequest) (daemon.HarnessAuthLogoutResponse, error) {
		_ = ctx
		if socketPath != "/tmp/ari-test.sock" {
			t.Fatalf("socketPath = %q, want test socket", socketPath)
		}
		captured = req
		return daemon.HarnessAuthLogoutResponse{Status: daemon.HarnessAuthStatus{Harness: daemon.HarnessNameCodex, AuthSlotID: req.AuthSlotID, Status: daemon.HarnessAuthRequired, AriSecretStorage: daemon.HarnessAriSecretStorageNone}}, nil
	}
	cmd := newAuthLogoutCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--harness", daemon.HarnessNameCodex})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("auth logout returned error: %v", err)
	}
	if captured.AuthSlotID != "codex-default" {
		t.Fatalf("logout request = %#v, want default account slot", captured)
	}
	if got := out.String(); !bytes.Contains([]byte(got), []byte("Already logged out")) {
		t.Fatalf("output = %q, want idempotent logout message", got)
	}
}

func TestAuthLogoutCommandUsesNamedAccount(t *testing.T) {
	restore := replaceAuthCommandDeps(t)
	defer restore()
	var captured daemon.HarnessAuthLogoutRequest
	authLogoutRPC = func(ctx context.Context, socketPath string, req daemon.HarnessAuthLogoutRequest) (daemon.HarnessAuthLogoutResponse, error) {
		_ = ctx
		_ = socketPath
		captured = req
		return daemon.HarnessAuthLogoutResponse{Status: daemon.HarnessAuthStatus{Harness: daemon.HarnessNameCodex, AuthSlotID: req.AuthSlotID, Status: daemon.HarnessAuthRequired, AriSecretStorage: daemon.HarnessAriSecretStorageNone}}, nil
	}
	cmd := newAuthLogoutCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--harness", daemon.HarnessNameCodex, "--name", "work"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("auth logout returned error: %v", err)
	}
	if captured.AuthSlotID != "codex-work" {
		t.Fatalf("logout request = %#v, want named account slot", captured)
	}
}

func TestHarnessExecutableUsesOpenCodeOverride(t *testing.T) {
	t.Setenv(daemon.EnvOpenCodeExecutable, "/tmp/custom-opencode")
	if got := daemon.HarnessExecutable("opencode", daemon.EnvOpenCodeExecutable); got != "/tmp/custom-opencode" {
		t.Fatalf("HarnessExecutable = %q, want override", got)
	}
}

func replaceAuthCommandDeps(t *testing.T) func() {
	t.Helper()
	t.Setenv("ARI_DAEMON_SOCKET_PATH", "/tmp/ari-test.sock")
	originalEnsure := authEnsureDaemonRunning
	originalStatus := authStatusRPC
	originalStart := authStartRPC
	originalLogout := authLogoutRPC
	originalSlotSave := authSlotSaveRPC
	originalProviderLogin := authRunProviderLogin
	originalOpenCodeMethods := authOpenCodeMethods
	authEnsureDaemonRunning = func(ctx context.Context, cfg *config.Config) error {
		_ = ctx
		_ = cfg
		return nil
	}
	authStatusRPC = func(ctx context.Context, socketPath string, req daemon.HarnessAuthStatusRequest) (daemon.HarnessAuthStatusResponse, error) {
		_ = ctx
		_ = socketPath
		_ = req
		return daemon.HarnessAuthStatusResponse{}, nil
	}
	authStartRPC = func(ctx context.Context, socketPath string, req daemon.HarnessAuthStartRequest) (daemon.HarnessAuthStartResponse, error) {
		_ = ctx
		_ = socketPath
		_ = req
		return daemon.HarnessAuthStartResponse{}, nil
	}
	authLogoutRPC = func(ctx context.Context, socketPath string, req daemon.HarnessAuthLogoutRequest) (daemon.HarnessAuthLogoutResponse, error) {
		_ = ctx
		_ = socketPath
		_ = req
		return daemon.HarnessAuthLogoutResponse{}, nil
	}
	authSlotSaveRPC = func(ctx context.Context, socketPath string, req daemon.AuthSlotSaveRequest) (daemon.AuthSlotResponse, error) {
		_ = ctx
		_ = socketPath
		_ = req
		return daemon.AuthSlotResponse{}, nil
	}
	authRunProviderLogin = func(ctx context.Context, harness, method, provider string) error {
		_ = ctx
		_ = harness
		_ = method
		_ = provider
		return nil
	}
	authOpenCodeMethods = func(ctx context.Context) (map[string][]openCodeAuthMethod, error) {
		_ = ctx
		return nil, nil
	}
	return func() {
		authEnsureDaemonRunning = originalEnsure
		authStatusRPC = originalStatus
		authStartRPC = originalStart
		authLogoutRPC = originalLogout
		authSlotSaveRPC = originalSlotSave
		authRunProviderLogin = originalProviderLogin
		authOpenCodeMethods = originalOpenCodeMethods
	}
}

func TestAuthProviderLoginArgsUseOpenCodeInteractiveLoginByDefault(t *testing.T) {
	args := authProviderLoginArgs(daemon.HarnessNameOpenCode, "ChatGPT Pro/Plus (headless)", "openai")
	want := []string{"auth", "login", "--provider", "openai", "--method", "ChatGPT Pro/Plus (headless)"}
	if len(args) != len(want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args = %#v, want %#v", args, want)
		}
	}
}

func TestAuthProviderLoginArgsCanPassExplicitOpenCodeProviderOnly(t *testing.T) {
	args := authProviderLoginArgs(daemon.HarnessNameOpenCode, "opencode_interactive", "openrouter")
	want := []string{"auth", "login", "--provider", "openrouter"}
	if len(args) != len(want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args = %#v, want %#v", args, want)
		}
	}
}

func TestOpenCodeLoginMethodsUseRealMethodLabels(t *testing.T) {
	options := openCodeLoginMethods([]openCodeAuthMethod{{Type: "oauth", Label: "ChatGPT Pro/Plus (browser)"}, {Type: "api", Label: "Manually enter API Key"}})
	if len(options) != 2 || options[0].Method != "ChatGPT Pro/Plus (browser)" || options[1].Method != "Manually enter API Key" {
		t.Fatalf("options = %#v, want labels as method selectors", options)
	}
}

func TestAuthProviderLoginArgsUseClaudeConsoleFlag(t *testing.T) {
	args := authProviderLoginArgs(daemon.HarnessNameClaude, "console", "")
	want := []string{"auth", "login", "--console"}
	if len(args) != len(want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args = %#v, want %#v", args, want)
		}
	}
}
