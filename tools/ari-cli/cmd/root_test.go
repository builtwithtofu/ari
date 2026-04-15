package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/config"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/spf13/cobra"
)

func TestRootRunUsesNonInteractivePath(t *testing.T) {
	originalIsInteractive := rootIsInteractiveTerminal
	originalInteractiveRun := rootRunInteractive
	originalNonInteractiveRun := rootRunNonInteractive
	t.Cleanup(func() {
		rootIsInteractiveTerminal = originalIsInteractive
		rootRunInteractive = originalInteractiveRun
		rootRunNonInteractive = originalNonInteractiveRun
	})

	interactiveCalled := false
	nonInteractiveCalled := false

	rootIsInteractiveTerminal = func(cmd *cobra.Command) bool {
		_ = cmd
		return false
	}
	rootRunInteractive = func(cmd *cobra.Command, args []string) error {
		_ = cmd
		_ = args
		interactiveCalled = true
		return nil
	}
	rootRunNonInteractive = func(cmd *cobra.Command, args []string) error {
		_ = cmd
		_ = args
		nonInteractiveCalled = true
		return nil
	}

	if _, err := executeRootCommandRaw(); err != nil {
		t.Fatalf("executeRootCommandRaw returned error: %v", err)
	}

	if interactiveCalled {
		t.Fatal("interactive path called, want false")
	}
	if !nonInteractiveCalled {
		t.Fatal("non-interactive path called = false, want true")
	}
}

func TestRootRunUsesInteractivePath(t *testing.T) {
	originalIsInteractive := rootIsInteractiveTerminal
	originalInteractiveRun := rootRunInteractive
	originalNonInteractiveRun := rootRunNonInteractive
	originalAttach := rootRunWorkspaceAttach
	t.Cleanup(func() {
		rootIsInteractiveTerminal = originalIsInteractive
		rootRunInteractive = originalInteractiveRun
		rootRunNonInteractive = originalNonInteractiveRun
		rootRunWorkspaceAttach = originalAttach
	})

	interactiveCalled := false
	nonInteractiveCalled := false

	rootIsInteractiveTerminal = func(cmd *cobra.Command) bool {
		_ = cmd
		return true
	}
	rootRunWorkspaceAttach = func(cmd *cobra.Command, args []string) error {
		_ = cmd
		_ = args
		interactiveCalled = true
		return nil
	}
	rootRunNonInteractive = func(cmd *cobra.Command, args []string) error {
		_ = cmd
		_ = args
		nonInteractiveCalled = true
		return nil
	}

	if _, err := executeRootCommandRaw(); err != nil {
		t.Fatalf("executeRootCommandRaw returned error: %v", err)
	}

	if !interactiveCalled {
		t.Fatal("interactive path called = false, want true")
	}
	if nonInteractiveCalled {
		t.Fatal("non-interactive path called, want false")
	}
}

func TestRootRunInteractiveDelegatesToWorkspaceAttachPath(t *testing.T) {
	originalInteractive := rootRunInteractive
	originalAttach := rootRunWorkspaceAttach
	t.Cleanup(func() {
		rootRunInteractive = originalInteractive
		rootRunWorkspaceAttach = originalAttach
	})

	called := false
	rootRunWorkspaceAttach = func(cmd *cobra.Command, args []string) error {
		_ = cmd
		if len(args) != 0 {
			t.Fatalf("args length = %d, want 0", len(args))
		}
		called = true
		return nil
	}

	if err := rootRunInteractive(&cobra.Command{}, nil); err != nil {
		t.Fatalf("rootRunInteractive returned error: %v", err)
	}
	if !called {
		t.Fatal("rootRunWorkspaceAttach called = false, want true")
	}
}

func TestRootRunInteractiveFallsBackWithoutErrorWhenAttachNotImplemented(t *testing.T) {
	originalAttach := rootRunWorkspaceAttach
	t.Cleanup(func() {
		rootRunWorkspaceAttach = originalAttach
	})

	rootRunWorkspaceAttach = func(cmd *cobra.Command, args []string) error {
		_ = cmd
		_ = args
		return nil
	}

	if err := rootRunInteractive(&cobra.Command{}, nil); err != nil {
		t.Fatalf("rootRunInteractive returned error: %v", err)
	}
}

func TestRootRunNonInteractiveRendersWorkspaceDashboard(t *testing.T) {
	originalIsInteractive := rootIsInteractiveTerminal
	originalRunInteractive := rootRunInteractive
	originalConfigured := rootConfiguredDaemonConfig
	originalEnsure := rootEnsureDaemonRunning
	originalResolve := rootResolveWorkspaceFromCWD
	originalAgentList := rootAgentListRPC
	t.Cleanup(func() {
		rootIsInteractiveTerminal = originalIsInteractive
		rootRunInteractive = originalRunInteractive
		rootConfiguredDaemonConfig = originalConfigured
		rootEnsureDaemonRunning = originalEnsure
		rootResolveWorkspaceFromCWD = originalResolve
		rootAgentListRPC = originalAgentList
	})

	rootIsInteractiveTerminal = func(cmd *cobra.Command) bool {
		_ = cmd
		return false
	}
	rootRunInteractive = func(cmd *cobra.Command, args []string) error {
		_ = cmd
		_ = args
		t.Fatal("interactive handler called unexpectedly")
		return nil
	}
	rootConfiguredDaemonConfig = func() (*config.Config, error) {
		return &config.Config{Daemon: config.DaemonConfig{SocketPath: "/tmp/daemon.sock"}}, nil
	}
	rootEnsureDaemonRunning = func(ctx context.Context, cfg *config.Config) error {
		_ = ctx
		_ = cfg
		return nil
	}
	rootResolveWorkspaceFromCWD = func(ctx context.Context, socketPath, cwd string) (daemon.WorkspaceGetResponse, error) {
		_ = ctx
		_ = socketPath
		_ = cwd
		return daemon.WorkspaceGetResponse{WorkspaceID: "ws-1", Name: "clay", Status: "active", OriginRoot: "/tmp/work/clay"}, nil
	}
	rootAgentListRPC = func(ctx context.Context, socketPath, sessionID string) (daemon.AgentListResponse, error) {
		_ = ctx
		_ = socketPath
		_ = sessionID
		return daemon.AgentListResponse{Agents: []daemon.AgentSummary{{AgentID: "a1"}, {AgentID: "a2"}}}, nil
	}

	out, err := executeRootCommandRaw()
	if err != nil {
		t.Fatalf("executeRootCommandRaw returned error: %v", err)
	}
	if !strings.Contains(out, "Workspace: clay") {
		t.Fatalf("output = %q, want workspace line", out)
	}
	if !strings.Contains(out, "ID: ws-1") {
		t.Fatalf("output = %q, want id line", out)
	}
	if !strings.Contains(out, "Status: active") {
		t.Fatalf("output = %q, want status line", out)
	}
	if !strings.Contains(out, "Agents: 2") {
		t.Fatalf("output = %q, want agents count", out)
	}
}

func TestRootRunNonInteractivePrintsNoWorkspaceMatchHint(t *testing.T) {
	originalIsInteractive := rootIsInteractiveTerminal
	originalConfigured := rootConfiguredDaemonConfig
	originalEnsure := rootEnsureDaemonRunning
	originalResolve := rootResolveWorkspaceFromCWD
	t.Cleanup(func() {
		rootIsInteractiveTerminal = originalIsInteractive
		rootConfiguredDaemonConfig = originalConfigured
		rootEnsureDaemonRunning = originalEnsure
		rootResolveWorkspaceFromCWD = originalResolve
	})

	rootIsInteractiveTerminal = func(cmd *cobra.Command) bool {
		_ = cmd
		return false
	}
	rootConfiguredDaemonConfig = func() (*config.Config, error) {
		return &config.Config{Daemon: config.DaemonConfig{SocketPath: "/tmp/daemon.sock"}}, nil
	}
	rootEnsureDaemonRunning = func(ctx context.Context, cfg *config.Config) error {
		_ = ctx
		_ = cfg
		return nil
	}
	rootResolveWorkspaceFromCWD = func(ctx context.Context, socketPath, cwd string) (daemon.WorkspaceGetResponse, error) {
		_ = ctx
		_ = socketPath
		_ = cwd
		return daemon.WorkspaceGetResponse{}, workspaceCWDResolutionError{reason: workspaceCWDReasonNoMatch}
	}

	out, err := executeRootCommandRaw()
	if err != nil {
		t.Fatalf("executeRootCommandRaw returned error: %v", err)
	}
	if !strings.Contains(out, "No workspace matches current directory") {
		t.Fatalf("output = %q, want no-match hint", out)
	}
}
