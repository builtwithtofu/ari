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

func TestAuthCommandRegistersDoctor(t *testing.T) {
	cmd := NewAuthCmd()
	for _, subcommand := range cmd.Commands() {
		if subcommand.Name() == "doctor" {
			return
		}
	}
	t.Fatalf("auth command did not register doctor subcommand")
}

func TestAuthDoctorCommandUsesDaemonDiagnosis(t *testing.T) {
	restore := replaceAuthCommandDeps(t)
	defer restore()
	var ensured bool
	authEnsureDaemonRunning = func(ctx context.Context, cfg *config.Config) error {
		_ = ctx
		if cfg.Daemon.SocketPath != "/tmp/ari-test.sock" {
			t.Fatalf("socket path = %q, want test socket", cfg.Daemon.SocketPath)
		}
		ensured = true
		return nil
	}
	var diagnoseReq daemon.HarnessAuthDiagnoseRequest
	authDiagnoseRPC = func(ctx context.Context, socketPath string, req daemon.HarnessAuthDiagnoseRequest) (daemon.HarnessAuthDiagnoseResponse, error) {
		_ = ctx
		if socketPath != "/tmp/ari-test.sock" {
			t.Fatalf("diagnose socket = %q, want test socket", socketPath)
		}
		diagnoseReq = req
		return daemon.HarnessAuthDiagnoseResponse{Harnesses: []daemon.HarnessAuthDiagnostic{
			{Harness: daemon.HarnessNameClaude, Installed: true, Status: daemon.HarnessAuthAuthenticated, DefaultSlot: daemon.HarnessAuthStatus{Harness: daemon.HarnessNameClaude, Name: "default", AuthSlotID: "claude-default", Status: daemon.HarnessAuthAuthenticated, AriSecretStorage: daemon.HarnessAriSecretStorageNone}, Auth: daemon.HarnessAuthDescriptor{NamedSlotStatus: daemon.HarnessAuthSupportPartial, NamedSlotExecution: daemon.HarnessAuthSupportUnsupported, SlotScope: "global", RiskLabels: []string{"provider_owned", "client_side_login"}, Caveats: []string{"macos_keychain_limits_named_slot_isolation"}}, ProviderMethods: daemon.HarnessAuthProviderMethodDiagnostic{Status: "skipped"}},
			{Harness: daemon.HarnessNameCodex, Installed: true, Status: daemon.HarnessAuthRequired, DefaultSlot: daemon.HarnessAuthStatus{Harness: daemon.HarnessNameCodex, Name: "default", AuthSlotID: "codex-default", Status: daemon.HarnessAuthRequired, AriSecretStorage: daemon.HarnessAriSecretStorageNone}, NamedSlots: []daemon.AuthSlotResponse{{AuthSlotID: "codex-work", Harness: daemon.HarnessNameCodex, Label: "work", ProviderLabel: "chatgpt", CredentialOwner: string(daemon.HarnessCredentialOwnerProvider), Status: string(daemon.HarnessAuthRequired)}}, Auth: daemon.HarnessAuthDescriptor{NamedSlotStatus: daemon.HarnessAuthSupportUnsupported, NamedSlotExecution: daemon.HarnessAuthSupportUnsupported, SlotScope: "global", RiskLabels: []string{"named_slot_projection_required", "provider_owned"}, Caveats: []string{"named_slot_execution_blocked_until_codex_home_projection"}}, ProviderMethods: daemon.HarnessAuthProviderMethodDiagnostic{Status: "skipped"}, NextStep: "Run `ari auth login --harness codex` and complete the provider's device-code login."},
			{Harness: daemon.HarnessNameOpenCode, Installed: false, Status: daemon.HarnessAuthNotInstalled, DefaultSlot: daemon.HarnessAuthStatus{Harness: daemon.HarnessNameOpenCode, Name: "default", AuthSlotID: "opencode-default", Status: daemon.HarnessAuthNotInstalled, AriSecretStorage: daemon.HarnessAriSecretStorageNone}, Auth: daemon.HarnessAuthDescriptor{NamedSlotStatus: daemon.HarnessAuthSupportPartial, NamedSlotExecution: daemon.HarnessAuthSupportUnsupported, SlotScope: "global", RiskLabels: []string{"ari_secrets_required_for_isolated_named_execution", "provider_owned"}, Caveats: []string{"provider_methods_discovery_is_optional"}}, ProviderMethods: daemon.HarnessAuthProviderMethodDiagnostic{Status: "skipped"}, NextStep: "Install OpenCode, then run `ari auth login --harness opencode`."},
		}}, nil
	}
	cmd := newAuthDoctorCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--workspace-id", "ws_123"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("auth doctor returned error: %v", err)
	}
	if !ensured {
		t.Fatalf("auth doctor did not ensure daemon running")
	}
	if diagnoseReq.WorkspaceID != "ws_123" {
		t.Fatalf("diagnose request = %#v, want workspace diagnosis", diagnoseReq)
	}
	if diagnoseReq.DiscoverProviderMethods {
		t.Fatalf("diagnose request = %#v, did not expect provider method discovery by default", diagnoseReq)
	}
	got := out.String()
	for _, want := range []string{
		"claude\n  installed:       installed\n  status:          authenticated",
		"codex\n  installed:       installed\n  status:          auth_required",
		"  named slots:     work(chatgpt):auth_required",
		"  next step:       Run `ari auth login --harness codex` and complete the provider's device-code login.",
		"opencode\n  installed:       not_installed\n  status:          not_installed",
		"  next step:       Install OpenCode, then run `ari auth login --harness opencode`.",
	} {
		if !bytes.Contains([]byte(got), []byte(want)) {
			t.Fatalf("output = %q, want line containing %q", got, want)
		}
	}
	for _, hidden := range []string{"named execution:", "risks:", "caveats:", "method discovery:", "connected providers:", "provider methods:", "remediation:", "next step:       none"} {
		if bytes.Contains([]byte(got), []byte(hidden)) {
			t.Fatalf("output = %q, did not expect detailed field %q", got, hidden)
		}
	}
	assertAuthDoctorOutputHasNoSecretFields(t, got)
}

func TestAuthDoctorRequestsDaemonProviderMethodDiscovery(t *testing.T) {
	restore := replaceAuthCommandDeps(t)
	defer restore()
	authEnsureDaemonRunning = func(ctx context.Context, cfg *config.Config) error {
		_ = ctx
		_ = cfg
		return nil
	}
	authDiagnoseRPC = func(ctx context.Context, socketPath string, req daemon.HarnessAuthDiagnoseRequest) (daemon.HarnessAuthDiagnoseResponse, error) {
		_ = ctx
		_ = socketPath
		if !req.DiscoverProviderMethods {
			t.Fatalf("diagnose request = %#v, want provider method discovery", req)
		}
		return daemon.HarnessAuthDiagnoseResponse{Harnesses: []daemon.HarnessAuthDiagnostic{{Harness: daemon.HarnessNameOpenCode, Installed: true, Status: daemon.HarnessAuthAuthenticated, DefaultSlot: daemon.HarnessAuthStatus{Harness: daemon.HarnessNameOpenCode, Name: "default", AuthSlotID: "opencode-default", Status: daemon.HarnessAuthAuthenticated, AriSecretStorage: daemon.HarnessAriSecretStorageNone}, Auth: daemon.HarnessAuthDescriptor{NamedSlotStatus: daemon.HarnessAuthSupportPartial, NamedSlotExecution: daemon.HarnessAuthSupportUnsupported, SlotScope: "global", RiskLabels: []string{"provider_owned"}, Caveats: []string{"provider_methods_discovery_is_optional"}}, ProviderMethods: daemon.HarnessAuthProviderMethodDiagnostic{Status: "ok", Connected: []string{"openai"}, Providers: map[string][]daemon.HarnessAuthMethodInfo{"anthropic": {{Type: "api", Label: "Anthropic API key"}}, "openai": {{Type: "oauth", Label: "ChatGPT browser"}, {Type: "api", Label: "OpenAI API key"}}}}}}}, nil
	}
	cmd := newAuthDoctorCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--discover-methods", "--detailed"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("auth doctor returned error: %v", err)
	}
	got := out.String()
	for _, want := range []string{"method discovery: ok", "connected providers: openai", "provider methods: anthropic:api,openai:api+oauth", "named execution: unsupported"} {
		if !bytes.Contains([]byte(got), []byte(want)) {
			t.Fatalf("output = %q, want %q", got, want)
		}
	}
	assertAuthDoctorOutputHasNoSecretFields(t, got)
}

func TestAuthDoctorRendererOmitsCredentialSourceAndRawMetadataFields(t *testing.T) {
	var out bytes.Buffer
	err := writeAuthDoctorResponse(&out, []daemon.HarnessAuthDiagnostic{
		{Harness: daemon.HarnessNameOpenCode, Installed: true, Status: daemon.HarnessAuthAuthenticated, DefaultSlot: daemon.HarnessAuthStatus{Harness: daemon.HarnessNameOpenCode, AuthSlotID: "opencode-default", Name: "default", Status: daemon.HarnessAuthAuthenticated, AriSecretStorage: daemon.HarnessAriSecretStorageNone}, NamedSlots: []daemon.AuthSlotResponse{{AuthSlotID: "opencode-work", Harness: daemon.HarnessNameOpenCode, Label: "work\trow", ProviderLabel: "openai\nprovider", CredentialOwner: string(daemon.HarnessCredentialOwnerProvider), Status: string(daemon.HarnessAuthAuthenticated)}}, Auth: daemon.HarnessAuthDescriptor{StatusCheck: daemon.HarnessAuthSupportSupported, Login: daemon.HarnessAuthSupportPartial, LoginMethods: []string{"opencode_interactive"}, Logout: daemon.HarnessAuthSupportSupported, NamedSlotStatus: daemon.HarnessAuthSupportPartial, NamedSlotExecution: daemon.HarnessAuthSupportUnsupported, SlotScope: "global", CredentialOwner: daemon.HarnessCredentialOwnerProvider, RiskLabels: []string{"provider_owned", "ari_secrets_required_for_isolated_named_execution"}, Caveats: []string{"provider hints are labels, not credentials"}}, ProviderMethods: daemon.HarnessAuthProviderMethodDiagnostic{Status: "ok", Connected: []string{"openai\nprovider"}, Providers: map[string][]daemon.HarnessAuthMethodInfo{"openai": {{Type: "oauth", Label: "Browser"}}}}},
	}, true)
	if err != nil {
		t.Fatalf("writeAuthDoctorResponse returned error: %v", err)
	}
	got := out.String()
	for _, want := range []string{"connected providers: openai_provider", "provider methods: openai:oauth", "named slots:     work_row(openai_provider):authenticated", "named execution: unsupported", "caveats:         provider hints are labels, not credentials"} {
		if !bytes.Contains([]byte(got), []byte(want)) {
			t.Fatalf("output = %q, want %q", got, want)
		}
	}
	assertAuthDoctorOutputHasNoSecretFields(t, got)
}

func assertAuthDoctorOutputHasNoSecretFields(t *testing.T, output string) {
	t.Helper()
	for _, secretField := range []string{"access_token", "refresh_token", "api_key", "credential_source_ref", "source_ref", "metadata_json", "raw_metadata"} {
		if bytes.Contains([]byte(output), []byte(secretField)) {
			t.Fatalf("output = %q, must not include secret field %q", output, secretField)
		}
	}
}

func TestAuthDoctorRendersDaemonDiagnostics(t *testing.T) {
	var out bytes.Buffer
	if err := writeAuthDoctorResponse(&out, []daemon.HarnessAuthDiagnostic{{Harness: daemon.HarnessNameClaude, Installed: true, Status: daemon.HarnessAuthUnknown, DefaultSlot: daemon.HarnessAuthStatus{Harness: daemon.HarnessNameClaude, AuthSlotID: "claude-default", Status: daemon.HarnessAuthUnknown}, Auth: daemon.HarnessAuthDescriptor{NamedSlotExecution: daemon.HarnessAuthSupportUnsupported, RiskLabels: []string{"provider_owned"}}, NextStep: "Run `ari auth login --harness claude` or check the provider's native auth setup."}}, true); err != nil {
		t.Fatalf("writeAuthDoctorResponse returned error: %v", err)
	}
	if got := out.String(); !bytes.Contains([]byte(got), []byte("claude\n  installed:       installed\n  status:          unknown")) || !bytes.Contains([]byte(got), []byte("named execution: unsupported")) || !bytes.Contains([]byte(got), []byte("risks:           provider_owned")) {
		t.Fatalf("output = %q, want rendered daemon diagnostic", got)
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

func TestAuthLogoutCommandScopesTimeoutToFinalRPC(t *testing.T) {
	restore := replaceAuthCommandDeps(t)
	defer restore()
	var statusCtx context.Context
	var logoutCtx context.Context
	authStatusRPC = func(ctx context.Context, socketPath string, req daemon.HarnessAuthStatusRequest) (daemon.HarnessAuthStatusResponse, error) {
		_ = socketPath
		_ = req
		statusCtx = ctx
		return daemon.HarnessAuthStatusResponse{Statuses: []daemon.HarnessAuthStatus{{Harness: daemon.HarnessNameCodex, AuthSlotID: "codex-default", Status: daemon.HarnessAuthAuthenticated}}}, nil
	}
	authLogoutRPC = func(ctx context.Context, socketPath string, req daemon.HarnessAuthLogoutRequest) (daemon.HarnessAuthLogoutResponse, error) {
		_ = socketPath
		_ = req
		logoutCtx = ctx
		return daemon.HarnessAuthLogoutResponse{Status: daemon.HarnessAuthStatus{Harness: daemon.HarnessNameCodex, AuthSlotID: req.AuthSlotID, Status: daemon.HarnessAuthRequired, AriSecretStorage: daemon.HarnessAriSecretStorageNone}}, nil
	}
	cmd := newAuthLogoutCmd()
	cmd.SetIn(bytes.NewBufferString("1\n"))
	cmd.SetOut(&bytes.Buffer{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("auth logout returned error: %v", err)
	}
	if statusCtx == nil || logoutCtx == nil || statusCtx == logoutCtx {
		t.Fatalf("statusCtx=%p logoutCtx=%p, want separate RPC timeout contexts", statusCtx, logoutCtx)
	}
}

func TestAuthLoginCommandGetsOpenCodeProviderMethodsFromDaemon(t *testing.T) {
	restore := replaceAuthCommandDeps(t)
	defer restore()
	authProviderMethodsRPC = func(ctx context.Context, socketPath string, req daemon.HarnessAuthProviderMethodsRequest) (daemon.HarnessAuthProviderMethodsResponse, error) {
		_ = ctx
		_ = socketPath
		if req.Harness != daemon.HarnessNameOpenCode {
			t.Fatalf("provider methods req = %#v, want OpenCode", req)
		}
		return daemon.HarnessAuthProviderMethodsResponse{Status: "ok", Providers: map[string][]daemon.HarnessAuthMethodInfo{"openai": {{Type: "oauth", Label: "ChatGPT Pro/Plus (browser)"}}}}, nil
	}
	var saved daemon.AuthSlotSaveRequest
	authSlotSaveRPC = func(ctx context.Context, socketPath string, req daemon.AuthSlotSaveRequest) (daemon.AuthSlotResponse, error) {
		_ = ctx
		_ = socketPath
		saved = req
		return daemon.AuthSlotResponse{AuthSlotID: req.AuthSlotID, Harness: req.Harness, Label: req.Label, ProviderLabel: req.ProviderLabel}, nil
	}
	var ranProvider bool
	var capturedMethod string
	authRunProviderLogin = func(ctx context.Context, harness, method, provider string) error {
		_ = ctx
		ranProvider = true
		capturedMethod = method
		if harness != daemon.HarnessNameOpenCode || provider != "openai" {
			t.Fatalf("provider login harness=%q provider=%q, want opencode/openai", harness, provider)
		}
		return nil
	}
	authStartRPC = func(ctx context.Context, socketPath string, req daemon.HarnessAuthStartRequest) (daemon.HarnessAuthStartResponse, error) {
		_ = ctx
		_ = socketPath
		_ = req
		t.Fatal("authStartRPC was called for OpenCode provider method; want client-side provider CLI")
		return daemon.HarnessAuthStartResponse{}, nil
	}
	cmd := newAuthLoginCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--harness", daemon.HarnessNameOpenCode, "--provider", "openai"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("auth login returned error: %v", err)
	}
	if !ranProvider || capturedMethod != "oauth" {
		t.Fatalf("ranProvider=%v method=%q, want OpenCode oauth provider login", ranProvider, capturedMethod)
	}
	if saved.AuthSlotID != "opencode-default" || saved.ProviderLabel != "openai" {
		t.Fatalf("saved slot = %#v, want OpenCode provider binding", saved)
	}
}

func TestAuthLoginCommandScopesTimeoutToPostPromptSlotSave(t *testing.T) {
	restore := replaceAuthCommandDeps(t)
	defer restore()
	var statusCtx context.Context
	var saveCtx context.Context
	authStatusRPC = func(ctx context.Context, socketPath string, req daemon.HarnessAuthStatusRequest) (daemon.HarnessAuthStatusResponse, error) {
		_ = socketPath
		_ = req
		statusCtx = ctx
		return daemon.HarnessAuthStatusResponse{Statuses: []daemon.HarnessAuthStatus{{Harness: daemon.HarnessNameCodex, AuthSlotID: "codex-default", Status: daemon.HarnessAuthAuthenticated}}}, nil
	}
	authSlotSaveRPC = func(ctx context.Context, socketPath string, req daemon.AuthSlotSaveRequest) (daemon.AuthSlotResponse, error) {
		_ = socketPath
		_ = req
		saveCtx = ctx
		return daemon.AuthSlotResponse{AuthSlotID: req.AuthSlotID, Harness: req.Harness, Label: req.Label}, nil
	}
	cmd := newAuthLoginCmd()
	cmd.SetIn(bytes.NewBufferString("1\n1\n"))
	cmd.SetOut(&bytes.Buffer{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("auth login returned error: %v", err)
	}
	if statusCtx == nil || saveCtx == nil || statusCtx == saveCtx {
		t.Fatalf("statusCtx=%p saveCtx=%p, want separate RPC timeout contexts", statusCtx, saveCtx)
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
	originalDiagnose := authDiagnoseRPC
	originalProviderMethods := authProviderMethodsRPC
	originalSlotSave := authSlotSaveRPC
	originalProviderLogin := authRunProviderLogin
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
	authDiagnoseRPC = func(ctx context.Context, socketPath string, req daemon.HarnessAuthDiagnoseRequest) (daemon.HarnessAuthDiagnoseResponse, error) {
		_ = ctx
		_ = socketPath
		_ = req
		return daemon.HarnessAuthDiagnoseResponse{}, nil
	}
	authProviderMethodsRPC = func(ctx context.Context, socketPath string, req daemon.HarnessAuthProviderMethodsRequest) (daemon.HarnessAuthProviderMethodsResponse, error) {
		_ = ctx
		_ = socketPath
		_ = req
		return daemon.HarnessAuthProviderMethodsResponse{}, nil
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
	return func() {
		authEnsureDaemonRunning = originalEnsure
		authStatusRPC = originalStatus
		authStartRPC = originalStart
		authLogoutRPC = originalLogout
		authDiagnoseRPC = originalDiagnose
		authProviderMethodsRPC = originalProviderMethods
		authSlotSaveRPC = originalSlotSave
		authRunProviderLogin = originalProviderLogin
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
	options := openCodeLoginMethods([]daemon.HarnessAuthMethodInfo{{Type: "oauth", Label: "ChatGPT Pro/Plus (browser)"}, {Type: "api", Label: "Manually enter API Key"}})
	if len(options) != 2 || options[0].Method != "oauth" || options[0].Label != "ChatGPT Pro/Plus (browser)" || options[1].Method != "api" || options[1].Label != "Manually enter API Key" {
		t.Fatalf("options = %#v, want provider method types with display labels", options)
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
