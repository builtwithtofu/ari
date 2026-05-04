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

	if !interactiveCalled {
		t.Fatal("interactive path called = false, want true")
	}
	if nonInteractiveCalled {
		t.Fatal("non-interactive path called, want false")
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
		t.Fatal("dashboard path must not resolve workspace from cwd")
		return daemon.WorkspaceGetResponse{}, nil
	}
	deps.dashboardRPC = func(ctx context.Context, socketPath, cwd string) (daemon.DashboardGetResponse, error) {
		_ = ctx
		_ = socketPath
		_ = cwd
		activity := daemon.WorkspaceStatusResponse{
			WorkspaceID:   "ws-1",
			WorkspaceName: "clay",
			VCS:           daemon.DiffSummary{Backend: "jj", ChangedFiles: 3},
			Attention:     daemon.AttentionSummary{Level: "action-required", Items: []daemon.AttentionItem{{Kind: "proof_failed", SourceID: "proof_cmd-1", Message: "just verify"}}},
			Processes:     []daemon.ProcessActivity{{ID: "cmd-1", Kind: "command", Status: "running", Label: "just verify"}},
			Sessions:      []daemon.SessionActivity{{ID: "a1", Status: "running", Executor: "codex"}, {ID: "a2", Status: "exited", Executor: "opencode"}},
			Proofs:        []daemon.ProofResultSummary{{ID: "proof_cmd-1", Status: "failed", Command: "just verify"}},
		}
		return daemon.DashboardGetResponse{ActiveContext: daemon.ActiveWorkspaceContext{WorkspaceID: "ws-1"}, EffectiveWorkspaceID: "ws-1", Status: activity}, nil
	}
	replaceRootDeps(t, deps)

	out, err := executeRootCommandRaw()
	if err != nil {
		t.Fatalf("executeRootCommandRaw returned error: %v", err)
	}
	if !strings.Contains(out, "Active workspace: clay") {
		t.Fatalf("output = %q, want workspace line", out)
	}
	if !strings.Contains(out, "ID: ws-1") {
		t.Fatalf("output = %q, want id line", out)
	}
	if !strings.Contains(out, "Sessions: 2") {
		t.Fatalf("output = %q, want session count", out)
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

func TestStatusUsesDaemonDashboardActiveContext(t *testing.T) {
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
		t.Fatal("status must not resolve active workspace from cwd")
		return daemon.WorkspaceGetResponse{}, nil
	}
	deps.dashboardRPC = func(ctx context.Context, socketPath, cwd string) (daemon.DashboardGetResponse, error) {
		_ = ctx
		if socketPath != "/tmp/daemon.sock" {
			t.Fatalf("socket path = %q, want configured socket", socketPath)
		}
		if cwd == "" {
			t.Fatal("cwd not passed to dashboard.get")
		}
		return daemon.DashboardGetResponse{
			ActiveContext:        daemon.ActiveWorkspaceContext{WorkspaceID: "ws-active", Version: "v1"},
			EffectiveWorkspaceID: "ws-active",
			Status: daemon.WorkspaceStatusResponse{
				WorkspaceID:   "ws-active",
				WorkspaceName: "active workspace",
				VCS:           daemon.DiffSummary{Backend: "jj", ChangedFiles: 2},
				Attention:     daemon.AttentionSummary{Level: "running", Items: []daemon.AttentionItem{{Kind: "session_running", SourceID: "ag-1", Message: "codex"}}},
				Sessions:      []daemon.SessionActivity{{ID: "ag-1", Status: "running", Executor: "codex"}},
			},
			ResumeActions:  []daemon.ResumeAction{{ID: "resume:session:ag-1", Kind: "resume_session", WorkspaceID: "ws-active", SourceID: "ag-1", Label: "codex"}},
			CWDMemberships: []daemon.WorkspaceMembership{{WorkspaceID: "ws-cwd", Name: "cwd workspace", FolderPath: "/tmp/cwd", Active: false}},
		}, nil
	}
	replaceRootDeps(t, deps)

	out, err := executeRootCommandRaw("status")
	if err != nil {
		t.Fatalf("execute status returned error: %v", err)
	}
	for _, want := range []string{"Active workspace: active workspace", "ID: ws-active", "Attention: running (1 items)", "Resume session: codex", "CWD workspace: cwd workspace"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status output = %q, want %q", out, want)
		}
	}
}

func TestRootHelpHidesLowLevelMirrorCommands(t *testing.T) {
	out, err := executeRootCommandRaw("--help")
	if err != nil {
		t.Fatalf("execute root help returned error: %v", err)
	}
	for _, hidden := range []string{"agent", "agents", "exec", "command", "final-response", "telemetry"} {
		if strings.Contains(out, "\n  "+hidden+" ") {
			t.Fatalf("root help = %q, want low-level mirror command %q hidden", out, hidden)
		}
	}
	for _, visible := range []string{"api", "status", "workspace", "auth", "profile", "session", "context", "timeline"} {
		if !strings.Contains(out, visible) {
			t.Fatalf("root help = %q, want workflow command %q visible", out, visible)
		}
	}
}

func TestRootHelpExposesPublicOrchestrationCommands(t *testing.T) {
	out, err := executeRootCommandRaw("--help")
	if err != nil {
		t.Fatalf("execute root help returned error: %v", err)
	}

	for _, visible := range []string{"profile", "session", "context", "status", "timeline", "workspace"} {
		if !strings.Contains(out, "\n  "+visible+" ") {
			t.Fatalf("root help = %q, want public orchestration command %q visible", out, visible)
		}
	}
	for _, hidden := range []string{"agents"} {
		if strings.Contains(out, "\n  "+hidden+" ") {
			t.Fatalf("root help = %q, want non-orchestration command %q hidden", out, hidden)
		}
	}
}

func TestRootRunNonInteractiveCountsStatusSessions(t *testing.T) {
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
		t.Fatal("dashboard path must not resolve workspace from cwd")
		return daemon.WorkspaceGetResponse{}, nil
	}
	deps.dashboardRPC = func(ctx context.Context, socketPath, cwd string) (daemon.DashboardGetResponse, error) {
		_ = ctx
		_ = socketPath
		_ = cwd
		return daemon.DashboardGetResponse{Status: daemon.WorkspaceStatusResponse{WorkspaceID: "ws-1", Sessions: []daemon.SessionActivity{{ID: "run-1"}, {ID: "run-2"}}}}, nil
	}
	replaceRootDeps(t, deps)

	out, err := executeRootCommandRaw()
	if err != nil {
		t.Fatalf("executeRootCommandRaw returned error: %v", err)
	}
	if !strings.Contains(out, "Sessions: 2") {
		t.Fatalf("output = %q, want status session count", out)
	}
}

func TestStatusRendersMessageWorkflowProjection(t *testing.T) {
	deps := rootDeps
	deps.isInteractiveTerminal = func(cmd *cobra.Command) bool { _ = cmd; return false }
	deps.configuredDaemonConfig = func() (*config.Config, error) {
		return &config.Config{Daemon: config.DaemonConfig{SocketPath: "/tmp/daemon.sock"}}, nil
	}
	deps.ensureDaemonRunning = func(ctx context.Context, cfg *config.Config) error { _ = ctx; _ = cfg; return nil }
	deps.dashboardRPC = func(ctx context.Context, socketPath, cwd string) (daemon.DashboardGetResponse, error) {
		_ = ctx
		_ = socketPath
		_ = cwd
		return daemon.DashboardGetResponse{Status: daemon.WorkspaceStatusResponse{WorkspaceID: "ws-1", WorkspaceName: "workspace", Attention: daemon.AttentionSummary{Level: "running", Items: []daemon.AttentionItem{{Kind: "session_waiting", SourceID: "run-1", Message: "executor"}, {Kind: "ephemeral_running", SourceID: "call-1-run", Message: "reviewer"}}}, Sessions: []daemon.SessionActivity{{ID: "run-1", Status: "waiting", Executor: "codex"}, {ID: "call-1-run", Status: "running", Executor: "opencode", Usage: "ephemeral", SourceSessionID: "run-1"}}, ContextExcerpts: []daemon.ContextExcerptActivity{{ContextExcerptID: "excerpt-1", SelectorType: "last_n", ItemCount: 5, TargetAgentID: "reviewer"}}, AgentMessages: []daemon.AgentMessageActivity{{AgentMessageID: "dm-1", Status: "delivered", SourceSessionID: "run-1", TargetAgentID: "reviewer", ContextExcerptCount: 1}}}}, nil
	}
	replaceRootDeps(t, deps)

	out, err := executeRootCommandRaw("status")
	if err != nil {
		t.Fatalf("execute status returned error: %v", err)
	}
	for _, want := range []string{"Waiting sessions: 1", "Running ephemeral calls: 1", "Session messages: 1", "Context excerpts: 1", "Context excerpt: excerpt-1 last_n 5 -> reviewer", "Session message: dm-1 delivered run-1 -> reviewer"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status output = %q, want %q", out, want)
		}
	}
}

func TestRootRunNonInteractivePropagatesDashboardActiveContextError(t *testing.T) {
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
		t.Fatal("dashboard path must not resolve workspace from cwd")
		return daemon.WorkspaceGetResponse{}, nil
	}
	deps.dashboardRPC = func(ctx context.Context, socketPath, cwd string) (daemon.DashboardGetResponse, error) {
		_ = ctx
		_ = socketPath
		_ = cwd
		return daemon.DashboardGetResponse{}, userFacingError{message: "active workspace context is not set"}
	}
	replaceRootDeps(t, deps)

	_, err := executeRootCommandRaw()
	if err == nil {
		t.Fatal("executeRootCommandRaw returned nil error")
	}
	if err.Error() != "active workspace context is not set" {
		t.Fatalf("executeRootCommandRaw error = %q, want active context error", err.Error())
	}
}
