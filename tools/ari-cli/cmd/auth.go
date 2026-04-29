package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/client"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/spf13/cobra"
)

var (
	authEnsureDaemonRunning = ensureDaemonRunning
	authStatusRPC           = func(ctx context.Context, socketPath string, req daemon.HarnessAuthStatusRequest) (daemon.HarnessAuthStatusResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.HarnessAuthStatusResponse
		if err := rpcClient.Call(ctx, "auth.status", req, &response); err != nil {
			return daemon.HarnessAuthStatusResponse{}, err
		}
		return response, nil
	}
	authStartRPC = func(ctx context.Context, socketPath string, req daemon.HarnessAuthStartRequest) (daemon.HarnessAuthStartResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.HarnessAuthStartResponse
		if err := rpcClient.Call(ctx, "auth.start", req, &response); err != nil {
			return daemon.HarnessAuthStartResponse{}, err
		}
		return response, nil
	}
	authLogoutRPC = func(ctx context.Context, socketPath string, req daemon.HarnessAuthLogoutRequest) (daemon.HarnessAuthLogoutResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.HarnessAuthLogoutResponse
		if err := rpcClient.Call(ctx, "auth.logout", req, &response); err != nil {
			return daemon.HarnessAuthLogoutResponse{}, err
		}
		return response, nil
	}
	authSlotSaveRPC = func(ctx context.Context, socketPath string, req daemon.AuthSlotSaveRequest) (daemon.AuthSlotResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.AuthSlotResponse
		if err := rpcClient.Call(ctx, "auth.slot.save", req, &response); err != nil {
			return daemon.AuthSlotResponse{}, err
		}
		return response, nil
	}
	authRunProviderLogin = runProviderLoginCommand
	authOpenCodeMethods  = fetchOpenCodeAuthMethods
)

func NewAuthCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "Inspect provider-owned harness auth"}
	cmd.AddCommand(newAuthStatusCmd(), newAuthLoginCmd(), newAuthLogoutCmd())
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
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()
			selectedHarness := strings.TrimSpace(harness)
			if selectedHarness == "" {
				status, err := authStatusRPC(ctx, cfg.Daemon.SocketPath, daemon.HarnessAuthStatusRequest{WorkspaceID: workspaceID})
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
			statusCtx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()
			selectedHarness := strings.TrimSpace(harness)
			if selectedHarness == "" {
				status, err := authStatusRPC(statusCtx, cfg.Daemon.SocketPath, daemon.HarnessAuthStatusRequest{WorkspaceID: workspaceID})
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
				methodsByProvider, err := authOpenCodeMethods(cmd.Context())
				if err != nil {
					return err
				}
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
			if _, err := authSlotSaveRPC(statusCtx, cfg.Daemon.SocketPath, daemon.AuthSlotSaveRequest{AuthSlotID: authSlotIDForName(selectedHarness, accountName), Harness: selectedHarness, Label: accountName, ProviderLabel: providerID}); err != nil {
				return err
			}
			method, err := promptAuthLoginMethod(cmd, methodOptions)
			if err != nil {
				return err
			}
			if authMethodRunsClientSide(selectedHarness, method) {
				if err := authRunProviderLogin(cmd.Context(), selectedHarness, method, providerID); err != nil {
					return err
				}
				refreshCtx, refreshCancel := context.WithTimeout(cmd.Context(), 10*time.Second)
				defer refreshCancel()
				status, err := authStatusRPC(refreshCtx, cfg.Daemon.SocketPath, daemon.HarnessAuthStatusRequest{WorkspaceID: workspaceID, Slots: []daemon.HarnessAuthSlot{{AuthSlotID: authSlotIDForName(selectedHarness, accountName)}}})
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

func runProviderLoginCommand(ctx context.Context, harness, method, provider string) error {
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
	return command.Run()
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
		return method == "opencode_interactive"
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

type openCodeAuthMethod struct {
	Type  string `json:"type"`
	Label string `json:"label"`
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

func openCodeLoginMethods(methods []openCodeAuthMethod) []authLoginMethod {
	options := make([]authLoginMethod, 0, len(methods))
	for _, method := range methods {
		label := strings.TrimSpace(method.Label)
		if label == "" {
			continue
		}
		options = append(options, authLoginMethod{Method: label, Label: label})
	}
	return options
}

func sortedOpenCodeProviders(methods map[string][]openCodeAuthMethod) []string {
	providers := make([]string, 0, len(methods))
	for provider := range methods {
		providers = append(providers, provider)
	}
	slices.Sort(providers)
	return providers
}

func fetchOpenCodeAuthMethods(ctx context.Context) (map[string][]openCodeAuthMethod, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, daemon.HarnessExecutable("opencode", daemon.EnvOpenCodeExecutable), "serve", "--port", "0", "--hostname", "127.0.0.1")
	pipe, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	command.Stderr = command.Stdout
	if err := command.Start(); err != nil {
		return nil, err
	}
	defer func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	}()
	serverURL, err := readOpenCodeServerURL(ctx, pipe)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, serverURL+"/provider/auth", nil)
	if err != nil {
		return nil, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = response.Body.Close()
	}()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("opencode provider auth returned HTTP %d", response.StatusCode)
	}
	var methods map[string][]openCodeAuthMethod
	if err := json.NewDecoder(response.Body).Decode(&methods); err != nil {
		return nil, err
	}
	return methods, nil
}

func readOpenCodeServerURL(ctx context.Context, reader io.Reader) (string, error) {
	lines := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	urlPattern := regexp.MustCompile(`http://127\.0\.0\.1:[0-9]+`)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				return "", fmt.Errorf("opencode server exited before printing URL")
			}
			if match := urlPattern.FindString(line); match != "" {
				return match, nil
			}
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
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
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\tname=%s\tsecrets=provider-owned\n", status.Harness, status.Status, authDisplayName(status)); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&workspaceID, "workspace-id", "", "Workspace id for provider-home context")
	return cmd
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
