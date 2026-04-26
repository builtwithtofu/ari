package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/config"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/spf13/cobra"
)

func replaceRootDeps(t *testing.T, deps rootRunDeps) {
	t.Helper()
	original := rootDeps
	rootDeps = deps
	t.Cleanup(func() {
		rootDeps = original
	})
}

func TestRootRunUsesNonInteractivePath(t *testing.T) {
	originalInteractiveRun := rootRunInteractive
	originalNonInteractiveRun := rootRunNonInteractive
	t.Cleanup(func() {
		rootRunInteractive = originalInteractiveRun
		rootRunNonInteractive = originalNonInteractiveRun
	})

	interactiveCalled := false
	nonInteractiveCalled := false

	replaceRootDeps(t, rootRunDeps{isInteractiveTerminal: func(cmd *cobra.Command) bool {
		_ = cmd
		return false
	}})
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
	originalInteractiveRun := rootRunInteractive
	originalNonInteractiveRun := rootRunNonInteractive
	t.Cleanup(func() {
		rootRunInteractive = originalInteractiveRun
		rootRunNonInteractive = originalNonInteractiveRun
	})

	interactiveCalled := false
	nonInteractiveCalled := false

	replaceRootDeps(t, rootRunDeps{isInteractiveTerminal: func(cmd *cobra.Command) bool {
		_ = cmd
		return true
	}, runWorkspaceAttach: func(cmd *cobra.Command, args []string) error {
		_ = cmd
		_ = args
		interactiveCalled = true
		return nil
	}})
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
	t.Cleanup(func() {
		rootRunInteractive = originalInteractive
	})

	called := false
	replaceRootDeps(t, rootRunDeps{runWorkspaceAttach: func(cmd *cobra.Command, args []string) error {
		_ = cmd
		if len(args) != 0 {
			t.Fatalf("args length = %d, want 0", len(args))
		}
		called = true
		return nil
	}})

	if err := rootRunInteractive(&cobra.Command{}, nil); err != nil {
		t.Fatalf("rootRunInteractive returned error: %v", err)
	}
	if !called {
		t.Fatal("rootRunWorkspaceAttach called = false, want true")
	}
}

func TestRootRunInteractiveFallsBackWithoutErrorWhenAttachNotImplemented(t *testing.T) {
	replaceRootDeps(t, rootRunDeps{runWorkspaceAttach: func(cmd *cobra.Command, args []string) error {
		_ = cmd
		_ = args
		return nil
	}})

	if err := rootRunInteractive(&cobra.Command{}, nil); err != nil {
		t.Fatalf("rootRunInteractive returned error: %v", err)
	}
}

func TestRootRunNonInteractiveRendersWorkspaceDashboard(t *testing.T) {
	originalRunInteractive := rootRunInteractive
	t.Cleanup(func() {
		rootRunInteractive = originalRunInteractive
	})

	deps := rootDeps
	deps.isInteractiveTerminal = func(cmd *cobra.Command) bool {
		_ = cmd
		return false
	}
	rootRunInteractive = func(cmd *cobra.Command, args []string) error {
		_ = cmd
		_ = args
		t.Fatal("interactive handler called unexpectedly")
		return nil
	}
	deps.configuredDaemonConfig = func() (*config.Config, error) {
		return &config.Config{Daemon: config.DaemonConfig{SocketPath: "/tmp/daemon.sock"}}, nil
	}
	deps.ensureDaemonRunning = func(ctx context.Context, cfg *config.Config) error {
		_ = ctx
		_ = cfg
		return nil
	}
	deps.resolveWorkspaceFromCWD = func(ctx context.Context, socketPath, cwd string) (daemon.WorkspaceGetResponse, error) {
		_ = ctx
		_ = socketPath
		_ = cwd
		return daemon.WorkspaceGetResponse{WorkspaceID: "ws-1", Name: "clay", Status: "active", OriginRoot: "/tmp/work/clay"}, nil
	}
	deps.agentListRPC = func(ctx context.Context, socketPath, sessionID string) (daemon.AgentListResponse, error) {
		_ = ctx
		_ = socketPath
		_ = sessionID
		return daemon.AgentListResponse{Agents: []daemon.AgentSummary{{AgentID: "a1"}, {AgentID: "a2"}}}, nil
	}
	deps.workspaceActivityRPC = func(ctx context.Context, socketPath, workspaceID string) (daemon.WorkspaceActivityResponse, error) {
		_ = ctx
		_ = socketPath
		if workspaceID != "ws-1" {
			t.Fatalf("workspace activity id = %q, want ws-1", workspaceID)
		}
		return daemon.WorkspaceActivityResponse{
			WorkspaceID:   "ws-1",
			WorkspaceName: "clay",
			VCS:           daemon.DiffSummary{Backend: "jj", ChangedFiles: 3},
			Attention:     daemon.AttentionSummary{Level: "action-required", Items: []daemon.AttentionItem{{Kind: "proof_failed", SourceID: "proof_cmd-1", Message: "just verify"}}},
			Processes:     []daemon.ProcessActivity{{ID: "cmd-1", Kind: "command", Status: "running", Label: "just verify"}},
			Agents:        []daemon.AgentActivity{{ID: "a1", Status: "running", Executor: "codex"}, {ID: "a2", Status: "exited", Executor: "opencode"}},
			Proofs:        []daemon.ProofResultSummary{{ID: "proof_cmd-1", Status: "failed", Command: "just verify"}},
		}, nil
	}
	replaceRootDeps(t, deps)

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
	if !strings.Contains(out, "VCS: jj (3 changed files)") {
		t.Fatalf("output = %q, want vcs projection line", out)
	}
	if !strings.Contains(out, "Processes: 1") {
		t.Fatalf("output = %q, want process count", out)
	}
	if !strings.Contains(out, "Latest proof: failed just verify") {
		t.Fatalf("output = %q, want latest proof line", out)
	}
	if !strings.Contains(out, "Attention: action-required (1 items)") {
		t.Fatalf("output = %q, want attention line", out)
	}
}

func TestRootRunNonInteractivePrintsNoWorkspaceMatchHint(t *testing.T) {
	deps := rootDeps
	deps.isInteractiveTerminal = func(cmd *cobra.Command) bool {
		_ = cmd
		return false
	}
	deps.configuredDaemonConfig = func() (*config.Config, error) {
		return &config.Config{Daemon: config.DaemonConfig{SocketPath: "/tmp/daemon.sock"}}, nil
	}
	deps.ensureDaemonRunning = func(ctx context.Context, cfg *config.Config) error {
		_ = ctx
		_ = cfg
		return nil
	}
	deps.resolveWorkspaceFromCWD = func(ctx context.Context, socketPath, cwd string) (daemon.WorkspaceGetResponse, error) {
		_ = ctx
		_ = socketPath
		_ = cwd
		return daemon.WorkspaceGetResponse{}, workspaceCWDResolutionError{reason: workspaceCWDReasonNoMatch}
	}
	replaceRootDeps(t, deps)

	out, err := executeRootCommandRaw()
	if err == nil {
		t.Fatal("executeRootCommandRaw returned nil error")
	}
	if err.Error() != "No workspace matches current directory" {
		t.Fatalf("executeRootCommandRaw error = %q, want %q", err.Error(), "No workspace matches current directory")
	}
	if !strings.Contains(out, "No workspace matches current directory") {
		t.Fatalf("output = %q, want no-match hint", out)
	}
}
