package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/spf13/cobra"
)

var (
	authEnsureDaemonRunning = ensureDaemonRunning
	authStatusRPC           = func(ctx context.Context, socketPath string, req daemon.HarnessAuthStatusRequest) (daemon.HarnessAuthStatusResponse, error) {
		return callDaemonRPC[daemon.HarnessAuthStatusResponse](ctx, socketPath, "auth.status", req)
	}
	authStartRPC = func(ctx context.Context, socketPath string, req daemon.HarnessAuthStartRequest) (daemon.HarnessAuthStartResponse, error) {
		return callDaemonRPC[daemon.HarnessAuthStartResponse](ctx, socketPath, "auth.start", req)
	}
	authLogoutRPC = func(ctx context.Context, socketPath string, req daemon.HarnessAuthLogoutRequest) (daemon.HarnessAuthLogoutResponse, error) {
		return callDaemonRPC[daemon.HarnessAuthLogoutResponse](ctx, socketPath, "auth.logout", req)
	}
	authDiagnoseRPC = func(ctx context.Context, socketPath string, req daemon.HarnessAuthDiagnoseRequest) (daemon.HarnessAuthDiagnoseResponse, error) {
		return callDaemonRPC[daemon.HarnessAuthDiagnoseResponse](ctx, socketPath, "auth.diagnose", req)
	}
	authProviderMethodsRPC = func(ctx context.Context, socketPath string, req daemon.HarnessAuthProviderMethodsRequest) (daemon.HarnessAuthProviderMethodsResponse, error) {
		return callDaemonRPC[daemon.HarnessAuthProviderMethodsResponse](ctx, socketPath, "auth.provider_methods", req)
	}
	authSlotSaveRPC = func(ctx context.Context, socketPath string, req daemon.AuthSlotSaveRequest) (daemon.AuthSlotResponse, error) {
		return callDaemonRPC[daemon.AuthSlotResponse](ctx, socketPath, "auth.slot.save", req)
	}
	authSlotListRPC = func(ctx context.Context, socketPath string, req daemon.AuthSlotListRequest) (daemon.AuthSlotListResponse, error) {
		return callDaemonRPC[daemon.AuthSlotListResponse](ctx, socketPath, "auth.slot.list", req)
	}
	authSlotRemoveRPC = func(ctx context.Context, socketPath string, req daemon.AuthSlotRemoveRequest) (daemon.AuthSlotRemoveResponse, error) {
		return callDaemonRPC[daemon.AuthSlotRemoveResponse](ctx, socketPath, "auth.slot.remove", req)
	}
	authRunProviderLogin = runProviderLoginCommand
)

func NewAuthCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "Inspect provider-owned harness auth"}
	cmd.AddCommand(newAuthStatusCmd(), newAuthLoginCmd(), newAuthLogoutCmd(), newAuthDoctorCmd(), newAuthListCmd(), newAuthRemoveCmd())
	return cmd
}

func newAuthListCmd() *cobra.Command {
	var harness string
	cmd := &cobra.Command{Use: "list", Short: "List Ari auth slots", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		_ = args
		cfg, err := configuredDaemonConfig()
		if err != nil {
			return err
		}
		if err := authEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		resp, err := authSlotListRPC(ctx, cfg.Daemon.SocketPath, daemon.AuthSlotListRequest{Harness: harness})
		if err != nil {
			return err
		}
		return writeAuthSlotList(cmd.OutOrStdout(), resp.Slots)
	}}
	cmd.Flags().StringVar(&harness, "harness", "", "Filter auth slots by harness")
	return cmd
}

func newAuthRemoveCmd() *cobra.Command {
	var harness string
	var name string
	var yes bool
	cmd := &cobra.Command{Use: "remove", Short: "Remove Ari auth slot metadata", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		_ = args
		if strings.TrimSpace(harness) == "" || strings.TrimSpace(name) == "" {
			return fmt.Errorf("--harness and --name are required for non-interactive auth remove")
		}
		if !yes {
			return fmt.Errorf("refusing to remove auth slot without --yes")
		}
		cfg, err := configuredDaemonConfig()
		if err != nil {
			return err
		}
		if err := authEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		authSlotID := authSlotIDForName(harness, name)
		resp, err := authSlotRemoveRPC(ctx, cfg.Daemon.SocketPath, daemon.AuthSlotRemoveRequest{AuthSlotID: authSlotID})
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "removed auth slot %s\n", resp.AuthSlotID)
		return err
	}}
	cmd.Flags().StringVar(&harness, "harness", "", "Provider harness for the auth slot")
	cmd.Flags().StringVar(&name, "name", "", "Auth account name")
	cmd.Flags().BoolVar(&yes, "yes", false, "Confirm auth slot metadata removal")
	return cmd
}

func newAuthDoctorCmd() *cobra.Command {
	var workspaceID string
	var discoverMethods bool
	var detailed bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose provider-owned harness auth readiness",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = args
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if err := authEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()
			resp, err := authDiagnoseRPC(ctx, cfg.Daemon.SocketPath, daemon.HarnessAuthDiagnoseRequest{WorkspaceID: workspaceID, DiscoverProviderMethods: discoverMethods})
			if err != nil {
				return err
			}
			return writeAuthDoctorResponse(cmd.OutOrStdout(), resp.Harnesses, detailed)
		},
	}
	cmd.Flags().StringVar(&workspaceID, "workspace-id", "", "Workspace id for provider-home context")
	cmd.Flags().BoolVar(&discoverMethods, "discover-methods", false, "Discover provider auth methods when a harness supports daemon-side discovery")
	cmd.Flags().BoolVar(&detailed, "detailed", false, "Show capability details, caveats, risks, and discovered provider methods")
	return cmd
}

func newAuthLogoutCmd() *cobra.Command {
	var harness string
	var name string
	var workspaceID string
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Log out a provider-owned auth account when supported",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = args
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if err := authEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}
			selectedHarness := strings.TrimSpace(harness)
			if selectedHarness == "" {
				ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
				status, err := authStatusRPC(ctx, cfg.Daemon.SocketPath, daemon.HarnessAuthStatusRequest{WorkspaceID: workspaceID})
				cancel()
				if err != nil {
					return err
				}
				selectedHarness, err = promptAuthHarness(cmd, authHarnessOptions(status.Statuses))
				if err != nil {
					return err
				}
			}
			accountName := strings.TrimSpace(name)
			if accountName == "" {
				accountName = "default"
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()
			resp, err := authLogoutRPC(ctx, cfg.Daemon.SocketPath, daemon.HarnessAuthLogoutRequest{AuthSlotID: authSlotIDForName(selectedHarness, accountName), WorkspaceID: workspaceID})
			if err != nil {
				return err
			}
			return writeAuthLogoutResponse(cmd, resp.Status)
		},
	}
	cmd.Flags().StringVar(&harness, "harness", "", "Provider harness to log out")
	cmd.Flags().StringVar(&name, "name", "", "Auth account name")
	cmd.Flags().StringVar(&workspaceID, "workspace-id", "", "Workspace id for provider-home context")
	return cmd
}

func newAuthLoginCmd() *cobra.Command {
	var harness string
	var name string
	var provider string
	var workspaceID string
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Start a provider-owned auth login flow",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = args
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if err := authEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}
			selectedHarness := strings.TrimSpace(harness)
			if selectedHarness == "" {
				statusCtx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
				status, err := authStatusRPC(statusCtx, cfg.Daemon.SocketPath, daemon.HarnessAuthStatusRequest{WorkspaceID: workspaceID})
				cancel()
				if err != nil {
					return err
				}
				selectedHarness, err = promptAuthHarness(cmd, authHarnessOptions(status.Statuses))
				if err != nil {
					return err
				}
			}
			accountName := strings.TrimSpace(name)
			if accountName == "" {
				accountName = "default"
			}
			providerID := strings.TrimSpace(provider)
			methodOptions := authLoginMethods(selectedHarness)
			if selectedHarness == daemon.HarnessNameOpenCode {
				methodsCtx, methodsCancel := context.WithTimeout(cmd.Context(), 10*time.Second)
				methodsResp, err := authProviderMethodsRPC(methodsCtx, cfg.Daemon.SocketPath, daemon.HarnessAuthProviderMethodsRequest{Harness: selectedHarness, WorkspaceID: workspaceID})
				methodsCancel()
				if err != nil {
					return err
				}
				methodsByProvider := methodsResp.Providers
				if providerID == "" {
					providerID, err = promptAuthString(cmd, "Choose OpenCode provider:", "Provider: ", sortedOpenCodeProviders(methodsByProvider))
					if err != nil {
						return err
					}
				}
				providerMethods := methodsByProvider[providerID]
				if len(providerMethods) == 0 {
					return fmt.Errorf("OpenCode provider %q has no auth methods", providerID)
				}
				methodOptions = openCodeLoginMethods(providerMethods)
			}
			saveCtx, saveCancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			if _, err := authSlotSaveRPC(saveCtx, cfg.Daemon.SocketPath, daemon.AuthSlotSaveRequest{AuthSlotID: authSlotIDForName(selectedHarness, accountName), Harness: selectedHarness, Label: accountName, ProviderLabel: providerID}); err != nil {
				saveCancel()
				return err
			}
			saveCancel()
			method, err := promptAuthLoginMethod(cmd, methodOptions)
			if err != nil {
				return err
			}
			authSlotID := authSlotIDForName(selectedHarness, accountName)
			if authMethodRunsClientSide(selectedHarness, method) {
				if err := authRunProviderLogin(cmd.Context(), selectedHarness, method, providerID, authSlotID); err != nil {
					return err
				}
				refreshCtx, refreshCancel := context.WithTimeout(cmd.Context(), 10*time.Second)
				defer refreshCancel()
				status, err := authStatusRPC(refreshCtx, cfg.Daemon.SocketPath, daemon.HarnessAuthStatusRequest{WorkspaceID: workspaceID, Slots: []daemon.HarnessAuthSlot{{AuthSlotID: authSlotID}}})
				if err != nil {
					return err
				}
				if len(status.Statuses) > 0 {
					return writeAuthLoginResponse(cmd, status.Statuses[0])
				}
				return nil
			}
			if authMethodIsProviderGuidance(selectedHarness, method) {
				return writeAuthLoginResponse(cmd, daemon.NewHarnessAuthRequired(selectedHarness, authSlotIDForName(selectedHarness, accountName), daemon.HarnessAuthRemediation{Kind: daemon.HarnessAuthRemediationProviderAuthFlow, Method: "api_key_provider_setup", SecretOwnedBy: selectedHarness}))
			}
			resp, err := authStartRPC(cmd.Context(), cfg.Daemon.SocketPath, daemon.HarnessAuthStartRequest{AuthSlotID: authSlotIDForName(selectedHarness, accountName), WorkspaceID: workspaceID, Method: method})
			if err != nil {
				return err
			}
			return writeAuthLoginResponse(cmd, resp.Status)
		},
	}
	cmd.Flags().StringVar(&harness, "harness", "", "Provider harness to log in")
	cmd.Flags().StringVar(&name, "name", "", "Auth account name")
	cmd.Flags().StringVar(&provider, "provider", "", "Provider-specific auth target, such as an OpenCode provider id")
	cmd.Flags().StringVar(&workspaceID, "workspace-id", "", "Workspace id for provider-home context")
	return cmd
}

func runProviderLoginCommand(ctx context.Context, harness, method, provider, authSlotID string) error {
	var command *exec.Cmd
	switch strings.TrimSpace(harness) {
	case daemon.HarnessNameCodex:
		command = exec.CommandContext(ctx, daemon.HarnessExecutable("codex", daemon.EnvCodexExecutable), "login")
	case daemon.HarnessNameClaude:
		command = exec.CommandContext(ctx, daemon.HarnessExecutable("claude", daemon.EnvClaudeExecutable), authProviderLoginArgs(harness, method, provider)...)
	case daemon.HarnessNameOpenCode:
		command = exec.CommandContext(ctx, daemon.HarnessExecutable("opencode", daemon.EnvOpenCodeExecutable), authProviderLoginArgs(harness, method, provider)...)
	default:
		return fmt.Errorf("provider login is not available for %s", strings.TrimSpace(harness))
	}
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if env, err := providerLoginEnv(harness, authSlotID); err != nil {
		return err
	} else if len(env) > 0 {
		command.Env = env
	}
	return command.Run()
}

func providerLoginEnv(harness, authSlotID string) ([]string, error) {
	harness = strings.TrimSpace(harness)
	authSlotID = strings.TrimSpace(authSlotID)
	if authSlotID == "" || authSlotID == harness+"-default" {
		return nil, nil
	}
	configRoot, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("resolve auth slot config root: %w", err)
	}
	safeSlotID := safeAuthSlotPathComponent(authSlotID)
	if safeSlotID == "" {
		return nil, fmt.Errorf("auth slot id %q cannot be used as a config path", authSlotID)
	}
	var key string
	switch harness {
	case daemon.HarnessNameCodex:
		key = "CODEX_HOME"
	case daemon.HarnessNameClaude:
		key = "CLAUDE_CONFIG_DIR"
	default:
		return nil, nil
	}
	home := filepath.Join(configRoot, "ari", "auth-slots", harness, safeSlotID)
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, fmt.Errorf("create auth slot config dir: %w", err)
	}
	return append(os.Environ(), key+"="+home), nil
}

func safeAuthSlotPathComponent(value string) string {
	value = strings.TrimSpace(value)
	var out strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			out.WriteRune(r)
		default:
			out.WriteByte('-')
		}
	}
	return strings.Trim(strings.ToLower(out.String()), "-._")
}

func authProviderLoginArgs(harness, method, provider string) []string {
	switch strings.TrimSpace(harness) {
	case daemon.HarnessNameClaude:
		args := []string{"auth", "login"}
		if method == "console" {
			args = append(args, "--console")
		}
		return args
	case daemon.HarnessNameOpenCode:
		args := []string{"auth", "login"}
		if provider != "" && provider != "default" {
			args = append(args, "--provider", provider)
		}
		if method != "" && method != "opencode_interactive" {
			args = append(args, "--method", method)
		}
		return args
	default:
		return []string{"login"}
	}
}

// authSlotIDForName maps Ari's public account name to the internal auth slot id.
// Do not use this name as a provider-native selector. For example, `--name work`
// may represent a Codex work account or an OpenCode OpenRouter account; OpenCode's
// provider id is selected separately via `--provider` or OpenCode's own login flow.
// Keeping these concepts separate prevents Ari account names from leaking provider
// implementation details into the public CLI model.
func authSlotIDForName(harness, name string) string {
	harness = strings.TrimSpace(harness)
	name = strings.TrimSpace(name)
	if name == "" || name == "default" {
		return harness + "-default"
	}
	return harness + "-" + name
}

func authMethodRunsClientSide(harness, method string) bool {
	switch strings.TrimSpace(harness) {
	case daemon.HarnessNameCodex:
		return method == "browser"
	case daemon.HarnessNameClaude:
		return method == "browser" || method == "console"
	case daemon.HarnessNameOpenCode:
		return strings.TrimSpace(method) != ""
	default:
		return method == "browser"
	}
}

func authMethodIsProviderGuidance(harness, method string) bool {
	harness = strings.TrimSpace(harness)
	return method == "api_key" && (harness == daemon.HarnessNameCodex || harness == daemon.HarnessNameClaude)
}

type authLoginMethod struct {
	Method string
	Label  string
}

func authLoginMethods(harness string) []authLoginMethod {
	switch strings.TrimSpace(harness) {
	case daemon.HarnessNameCodex:
		return []authLoginMethod{{Method: "browser", Label: "ChatGPT account / browser login"}, {Method: "device_code", Label: "Device code"}, {Method: "api_key", Label: "API key setup guidance"}}
	case daemon.HarnessNameClaude:
		return []authLoginMethod{{Method: "browser", Label: "Claude account"}, {Method: "console", Label: "Claude Console account"}, {Method: "api_key", Label: "API key/helper setup guidance"}}
	case daemon.HarnessNameOpenCode:
		return []authLoginMethod{{Method: "opencode_interactive", Label: "OpenCode provider login"}}
	default:
		return []authLoginMethod{{Method: "browser", Label: "Provider login"}}
	}
}

func openCodeLoginMethods(methods []daemon.HarnessAuthMethodInfo) []authLoginMethod {
	options := make([]authLoginMethod, 0, len(methods))
	for _, method := range methods {
		methodType := strings.TrimSpace(method.Type)
		label := strings.TrimSpace(method.Label)
		if methodType == "" || label == "" {
			continue
		}
		options = append(options, authLoginMethod{Method: methodType, Label: label})
	}
	return options
}

func sortedOpenCodeProviders(methods map[string][]daemon.HarnessAuthMethodInfo) []string {
	providers := make([]string, 0, len(methods))
	for provider := range methods {
		providers = append(providers, provider)
	}
	slices.Sort(providers)
	return providers
}

func promptAuthLoginMethod(cmd *cobra.Command, options []authLoginMethod) (string, error) {
	if len(options) == 0 {
		return "", fmt.Errorf("no auth login methods available")
	}
	if len(options) == 1 {
		return options[0].Method, nil
	}
	if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Choose login method:"); err != nil {
		return "", err
	}
	for index, option := range options {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  %d. %s\n", index+1, option.Label); err != nil {
			return "", err
		}
	}
	if _, err := fmt.Fprint(cmd.OutOrStdout(), "Method: "); err != nil {
		return "", err
	}
	scanner := bufio.NewScanner(cmd.InOrStdin())
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("read login method choice: %w", err)
		}
		return "", fmt.Errorf("read login method choice: no input")
	}
	choice, err := strconv.Atoi(strings.TrimSpace(scanner.Text()))
	if err != nil || choice < 1 || choice > len(options) {
		return "", fmt.Errorf("login method choice must be a number between 1 and %d", len(options))
	}
	return options[choice-1].Method, nil
}

func promptAuthString(cmd *cobra.Command, title, prompt string, options []string) (string, error) {
	if len(options) == 0 {
		return "", fmt.Errorf("no options available")
	}
	if len(options) == 1 {
		return options[0], nil
	}
	if _, err := fmt.Fprintln(cmd.OutOrStdout(), title); err != nil {
		return "", err
	}
	for index, option := range options {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  %d. %s\n", index+1, option); err != nil {
			return "", err
		}
	}
	if _, err := fmt.Fprint(cmd.OutOrStdout(), prompt); err != nil {
		return "", err
	}
	scanner := bufio.NewScanner(cmd.InOrStdin())
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("read selection: no input")
	}
	choice, err := strconv.Atoi(strings.TrimSpace(scanner.Text()))
	if err != nil || choice < 1 || choice > len(options) {
		return "", fmt.Errorf("selection must be a number between 1 and %d", len(options))
	}
	return options[choice-1], nil
}

func newAuthStatusCmd() *cobra.Command {
	var workspaceID string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Summarize harness auth readiness without reading secrets",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = args
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if err := authEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()
			resp, err := authStatusRPC(ctx, cfg.Daemon.SocketPath, daemon.HarnessAuthStatusRequest{WorkspaceID: workspaceID})
			if err != nil {
				return err
			}
			for _, status := range resp.Statuses {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\tname=%s\tsecrets=%s\n", authDoctorSafeField(status.Harness), authDoctorSafeField(string(status.Status)), authDoctorSafeField(authDisplayName(status)), authDoctorSafeField(authStatusSecretOwner(status))); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&workspaceID, "workspace-id", "", "Workspace id for provider-home context")
	return cmd
}

func authStatusSecretOwner(status daemon.HarnessAuthStatus) string {
	if status.AriSecretStorage != "" && status.AriSecretStorage != daemon.HarnessAriSecretStorageNone {
		return string(status.AriSecretStorage)
	}
	return "provider-owned"
}

func writeAuthSlotList(w io.Writer, slots []daemon.AuthSlotResponse) error {
	for _, slot := range slots {
		label := authDoctorSafeField(slot.Label)
		provider := authDoctorSafeField(slot.ProviderLabel)
		if provider == "" {
			provider = "-"
		}
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\tprovider=%s\tstatus=%s\n", authDoctorSafeField(slot.Harness), label, authDoctorSafeField(slot.AuthSlotID), provider, authDoctorSafeField(slot.Status)); err != nil {
			return err
		}
	}
	return nil
}

func authDisplayName(status daemon.HarnessAuthStatus) string {
	if status.Name != "" {
		return status.Name
	}
	if status.AuthSlotID == status.Harness+"-default" {
		return "default"
	}
	return "unknown"
}

func authHarnessOptions(statuses []daemon.HarnessAuthStatus) []string {
	seen := map[string]bool{}
	options := []string{}
	for _, status := range statuses {
		harness := strings.TrimSpace(status.Harness)
		if harness == "" || seen[harness] {
			continue
		}
		seen[harness] = true
		options = append(options, harness)
	}
	return options
}

func writeAuthDoctorResponse(w io.Writer, diagnostics []daemon.HarnessAuthDiagnostic, detailed bool) error {
	for _, diagnostic := range diagnostics {
		installed := "installed"
		if !diagnostic.Installed {
			installed = "not_installed"
		}
		if _, err := fmt.Fprintf(w, "%s\n", authDoctorSafeField(diagnostic.Harness)); err != nil {
			return err
		}
		fields := [][2]string{
			{"installed", installed},
			{"status", string(diagnostic.Status)},
			{"default slot", authDoctorSafeField(authDisplayName(diagnostic.DefaultSlot))},
			{"named slots", authDoctorNamedSlots(diagnostic.NamedSlots)},
		}
		if detailed {
			fields = append(
				fields,
				[2]string{"named execution", string(diagnostic.Auth.NamedSlotExecution)},
				[2]string{"named status", string(diagnostic.Auth.NamedSlotStatus)},
				[2]string{"slot scope", authDoctorSafeField(diagnostic.Auth.SlotScope)},
				[2]string{"risks", authDoctorLabels(diagnostic.Auth.RiskLabels)},
				[2]string{"caveats", authDoctorLabels(diagnostic.Auth.Caveats)},
			)
			if diagnostic.Harness == daemon.HarnessNameOpenCode {
				fields = append(
					fields,
					[2]string{"method discovery", authDoctorSafeField(authDoctorMethodDiscoveryStatus(diagnostic.ProviderMethods.Status))},
					[2]string{"connected providers", authDoctorLabels(diagnostic.ProviderMethods.Connected)},
					[2]string{"provider methods", authDoctorOpenCodeProviderMethods(diagnostic.ProviderMethods.Providers)},
				)
			}
		}
		if nextStep := authDoctorSafeField(diagnostic.NextStep); nextStep != "none" {
			fields = append(fields, [2]string{"next step", nextStep})
		}
		for _, field := range fields {
			if _, err := fmt.Fprintf(w, "  %-16s %s\n", field[0]+":", field[1]); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	return nil
}

func authDoctorMethodDiscoveryStatus(status string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		return "skipped"
	}
	return status
}

func authDoctorOpenCodeProviderMethods(methods map[string][]daemon.HarnessAuthMethodInfo) string {
	if len(methods) == 0 {
		return "none"
	}
	providers := make([]string, 0, len(methods))
	for provider := range methods {
		providers = append(providers, provider)
	}
	slices.Sort(providers)
	parts := make([]string, 0, len(providers))
	for _, provider := range providers {
		methodLabels := []string{}
		for _, method := range methods[provider] {
			methodType := strings.TrimSpace(method.Type)
			if methodType != "" {
				methodLabels = append(methodLabels, authDoctorSafeField(methodType))
			}
		}
		if len(methodLabels) == 0 {
			continue
		}
		slices.Sort(methodLabels)
		parts = append(parts, authDoctorSafeField(provider)+":"+strings.Join(methodLabels, "+"))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ",")
}

func authDoctorNamedSlots(slots []daemon.AuthSlotResponse) string {
	named := []string{}
	for _, slot := range slots {
		label := strings.TrimSpace(slot.Label)
		if label == "" || label == "default" || slot.AuthSlotID == slot.Harness+"-default" {
			continue
		}
		provider := strings.TrimSpace(slot.ProviderLabel)
		if provider != "" {
			label += "(" + authDoctorSafeField(provider) + ")"
		}
		named = append(named, authDoctorSafeField(label)+":"+authDoctorSafeField(slot.Status))
	}
	if len(named) == 0 {
		return "none"
	}
	slices.Sort(named)
	return strings.Join(named, ",")
}

func authDoctorLabels(labels []string) string {
	cleaned := []string{}
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		cleaned = append(cleaned, authDoctorSafeField(label))
	}
	if len(cleaned) == 0 {
		return "none"
	}
	slices.Sort(cleaned)
	return strings.Join(cleaned, ",")
}

func authDoctorSafeField(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return '_'
		}
		return r
	}, value)
	if value == "" {
		return "none"
	}
	return value
}

func promptAuthHarness(cmd *cobra.Command, options []string) (string, error) {
	if len(options) == 0 {
		return "", fmt.Errorf("no auth providers available")
	}
	if len(options) == 1 {
		return options[0], nil
	}
	if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Choose provider:"); err != nil {
		return "", err
	}
	for index, option := range options {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  %d. %s\n", index+1, option); err != nil {
			return "", err
		}
	}
	if _, err := fmt.Fprint(cmd.OutOrStdout(), "Provider: "); err != nil {
		return "", err
	}
	scanner := bufio.NewScanner(cmd.InOrStdin())
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("read provider choice: %w", err)
		}
		return "", fmt.Errorf("read provider choice: no input")
	}
	choice, err := strconv.Atoi(strings.TrimSpace(scanner.Text()))
	if err != nil || choice < 1 || choice > len(options) {
		return "", fmt.Errorf("provider choice must be a number between 1 and %d", len(options))
	}
	return options[choice-1], nil
}

func writeAuthLoginResponse(cmd *cobra.Command, status daemon.HarnessAuthStatus) error {
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s auth %s for %s\n", status.Harness, status.Status, authDisplayName(status)); err != nil {
		return err
	}
	if status.Remediation == nil {
		return nil
	}
	if status.Remediation.VerificationURL != "" {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Open: %s\n", status.Remediation.VerificationURL); err != nil {
			return err
		}
	}
	if status.Remediation.UserCode != "" {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Code: %s\n", status.Remediation.UserCode); err != nil {
			return err
		}
	}
	return nil
}

func writeAuthLogoutResponse(cmd *cobra.Command, status daemon.HarnessAuthStatus) error {
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s auth %s for %s\n", status.Harness, status.Status, authDisplayName(status)); err != nil {
		return err
	}
	if status.Status == daemon.HarnessAuthRequired {
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Already logged out or auth is not configured for this account."); err != nil {
			return err
		}
	}
	return nil
}
