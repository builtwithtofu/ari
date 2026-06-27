package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/config"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
	"github.com/sourcegraph/jsonrpc2"
)

func TestRootRegistersCommandCommand(t *testing.T) {
	tests := []struct {
		name string
		path []string
		want string
	}{
		{name: "command root registered", path: []string{"command"}, want: "command"},
		{name: "exec root registered", path: []string{"exec"}, want: "exec"},
		{name: "profile root registered", path: []string{"profile"}, want: "profile"},
		{name: "session root registered", path: []string{"session"}, want: "session"},
		{name: "daemon root still registered", path: []string{"daemon"}, want: "daemon"},
		{name: "workspace root still registered", path: []string{"workspace"}, want: "workspace"},
	}

	root := NewRootCmd()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd, _, err := root.Find(tc.path)
			if err != nil {
				t.Fatalf("find %v: %v", tc.path, err)
			}
			if cmd == nil {
				t.Fatalf("expected command %v to be registered", tc.path)
			}
			if cmd.Name() != tc.want {
				t.Fatalf("command name = %q, want %q", cmd.Name(), tc.want)
			}
		})
	}
}

func TestSessionSubcommandsUseCommittedOrchestrationSurface(t *testing.T) {
	root := NewRootCmd()
	for _, path := range [][]string{{"session", "start"}, {"session", "list"}, {"session", "show"}, {"session", "message", "send"}, {"session", "call"}, {"session", "fanout"}} {
		cmd, _, err := root.Find(path)
		if err != nil {
			t.Fatalf("find %v: %v", path, err)
		}
		if cmd == nil {
			t.Fatalf("expected command %v to be registered", path)
		}
	}
	if legacy, _, err := root.Find([]string{"agents"}); err == nil && legacy != nil && legacy.Name() == "agents" {
		t.Fatalf("legacy agents command is still registered as a public orchestration surface")
	}
}

func TestContextExcerptSubcommandsUseCommittedOrchestrationSurface(t *testing.T) {
	root := NewRootCmd()
	for _, path := range [][]string{{"context", "excerpt", "tail"}, {"context", "excerpt", "range"}, {"context", "excerpt", "messages"}, {"context", "excerpt", "show"}} {
		cmd, _, err := root.Find(path)
		if err != nil {
			t.Fatalf("find %v: %v", path, err)
		}
		if cmd == nil {
			t.Fatalf("expected command %v to be registered", path)
		}
	}
}

func TestProfileSubcommandsExist(t *testing.T) {
	root := NewRootCmd()
	for _, path := range [][]string{{"profile", "create"}, {"profile", "list"}, {"profile", "show"}, {"profile", "defaults"}} {
		cmd, _, err := root.Find(path)
		if err != nil {
			t.Fatalf("find %v: %v", path, err)
		}
		if cmd == nil {
			t.Fatalf("expected command %v to be registered", path)
		}
	}
}

func TestTopLevelCommandSubcommandsExist(t *testing.T) {
	root := NewRootCmd()

	create, _, err := root.Find([]string{"command", "create"})
	if err != nil {
		t.Fatalf("find command create: %v", err)
	}
	list, _, err := root.Find([]string{"command", "list"})
	if err != nil {
		t.Fatalf("find command list: %v", err)
	}
	show, _, err := root.Find([]string{"command", "show"})
	if err != nil {
		t.Fatalf("find command show: %v", err)
	}
	run, _, err := root.Find([]string{"command", "run"})
	if err != nil {
		t.Fatalf("find command run: %v", err)
	}
	remove, _, err := root.Find([]string{"command", "remove"})
	if err != nil {
		t.Fatalf("find command remove: %v", err)
	}
	if create == nil || list == nil || show == nil || run == nil || remove == nil {
		t.Fatal("expected command definition subcommands to be registered")
	}
}

func TestTopLevelExecSubcommandsExist(t *testing.T) {
	root := NewRootCmd()

	run, _, err := root.Find([]string{"exec", "run"})
	if err != nil {
		t.Fatalf("find exec run: %v", err)
	}
	list, _, err := root.Find([]string{"exec", "list"})
	if err != nil {
		t.Fatalf("find exec list: %v", err)
	}
	show, _, err := root.Find([]string{"exec", "show"})
	if err != nil {
		t.Fatalf("find exec show: %v", err)
	}
	output, _, err := root.Find([]string{"exec", "output"})
	if err != nil {
		t.Fatalf("find exec output: %v", err)
	}
	stop, _, err := root.Find([]string{"exec", "stop"})
	if err != nil {
		t.Fatalf("find exec stop: %v", err)
	}
	if run == nil || list == nil || show == nil || output == nil || stop == nil {
		t.Fatal("expected exec subcommands to be registered")
	}
}

func TestExecOperationUsesFreshContextAfterWorkflowSetup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolveWorkflowContext := commandResolveWorkflowContext
	originalEnsure := commandEnsureDaemonRunning
	originalList := commandListRPC
	var setupCtx context.Context
	commandEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	commandResolveWorkflowContext = func(ctx context.Context, _ string, workspaceOverride string) (WorkflowContext, error) {
		setupCtx = ctx
		return WorkflowContext{WorkspaceID: "ws-1", Workspace: &daemon.WorkspaceGetResponse{WorkspaceID: "ws-1", OriginRoot: t.TempDir()}, Source: WorkflowContextSourceExplicit}, nil
	}
	commandListRPC = func(ctx context.Context, _ string, workspaceID string) (daemon.CommandListResponse, error) {
		if ctx == nil {
			t.Fatal("command list RPC received nil context")
		}
		if setupCtx == nil {
			t.Fatal("setup context was not captured")
		}
		if ctx == setupCtx {
			t.Fatal("command list reused setup context; want a fresh operation timeout")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("command list context has no operation deadline")
		}
		if workspaceID != "ws-1" {
			t.Fatalf("workspaceID = %q, want ws-1", workspaceID)
		}
		return daemon.CommandListResponse{}, nil
	}
	t.Cleanup(func() {
		commandResolveWorkflowContext = originalResolveWorkflowContext
		commandEnsureDaemonRunning = originalEnsure
		commandListRPC = originalList
	})

	if _, err := executeRootCommand("exec", "list", "--workspace", "alpha"); err != nil {
		t.Fatalf("exec list returned error: %v", err)
	}
}

func TestWorkspaceCommandRunDoesNotConstructPartialExecWorkflow(t *testing.T) {
	data, err := os.ReadFile("workspace_command.go")
	if err != nil {
		t.Fatalf("ReadFile workspace_command.go returned error: %v", err)
	}
	if strings.Contains(string(data), "&execWorkflow{") {
		t.Fatal("workspace command run constructs execWorkflow directly; use newExecWorkflow so ctx and cancel are initialized")
	}
}

func TestWorkspaceTargetingHelpRegistersWorkspaceFlagOnly(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "exec run", args: []string{"exec", "run"}},
		{name: "workspace command create", args: []string{"command", "create"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd, _, err := NewRootCmd().Find(tc.args)
			if err != nil {
				t.Fatalf("find %v returned error: %v", tc.args, err)
			}
			workspaceFlag := cmd.Flags().Lookup("workspace")
			if workspaceFlag == nil {
				t.Fatalf("%v has no workspace flag", tc.args)
			}
			if workspaceFlag.Usage != "Target workspace id or name (defaults to active workspace)" {
				t.Fatalf("workspace flag usage = %q, want target workspace wording", workspaceFlag.Usage)
			}
			if executionRootFlag := cmd.Flags().Lookup("execution-root"); executionRootFlag != nil {
				t.Fatalf("%v unexpectedly registers execution-root flag", tc.args)
			}
		})
	}
}

func TestCommandListRejectsActiveWorkspaceOutsideWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd returned error: %v", err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("os.Chdir returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})

	originalResolver := workflowContextResolver
	originalResolveTarget := commandResolveWorkspaceTarget
	originalEnsure := commandEnsureDaemonRunning
	originalSessionGet := workspaceGetRPC
	originalList := commandListRPC

	workflowContextResolver = &WorkflowContextResolver{store: activeWorkspaceStoreFunc{read: func() (string, error) {
		return "sess-1", nil
	}}, resolveTarget: func(context.Context, string, string) (resolvedWorkspaceTarget, error) {
		workspace := daemon.WorkspaceGetResponse{WorkspaceID: "sess-1", OriginRoot: t.TempDir()}
		return resolvedWorkspaceTarget{WorkspaceID: workspace.WorkspaceID, Workspace: &workspace}, nil
	}}
	commandResolveWorkspaceTarget = workflowContextResolver.resolveTarget
	commandEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	workspaceGetRPC = func(context.Context, string, string) (daemon.WorkspaceGetResponse, error) {
		return daemon.WorkspaceGetResponse{WorkspaceID: "sess-1", OriginRoot: t.TempDir()}, nil
	}
	commandListRPC = func(context.Context, string, string) (daemon.CommandListResponse, error) {
		return daemon.CommandListResponse{}, errors.New("command list should not be called")
	}
	t.Cleanup(func() {
		workflowContextResolver = originalResolver
		commandResolveWorkspaceTarget = originalResolveTarget
		commandEnsureDaemonRunning = originalEnsure
		workspaceGetRPC = originalSessionGet
		commandListRPC = originalList
	})

	_, err = executeRootCommandRaw("exec", "list")
	if err == nil {
		t.Fatal("command list returned nil error for cross-workspace active workspace")
	}
	if err.Error() != "Active workspace belongs to a different workspace; use --workspace <id-or-name> to target a workspace explicitly" {
		t.Fatalf("command list error = %q, want %q", err.Error(), "Active workspace belongs to a different workspace; use --workspace <id-or-name> to target a workspace explicitly")
	}
}

func TestCommandListWorkspaceOverrideBypassesWorkspaceSafety(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolver := workflowContextResolver
	originalResolveTarget := commandResolveWorkspaceTarget
	originalEnsure := commandEnsureDaemonRunning
	originalSessionGet := workspaceGetRPC
	originalList := commandListRPC

	workflowContextResolver = &WorkflowContextResolver{store: activeWorkspaceStoreFunc{read: func() (string, error) {
		return "", errors.New("active workspace should not be read when --workspace is provided")
	}}, resolveTarget: func(context.Context, string, string) (resolvedWorkspaceTarget, error) {
		workspace := daemon.WorkspaceGetResponse{WorkspaceID: "sess-1", OriginRoot: t.TempDir()}
		return resolvedWorkspaceTarget{WorkspaceID: workspace.WorkspaceID, Workspace: &workspace}, nil
	}}
	commandResolveWorkspaceTarget = workflowContextResolver.resolveTarget
	commandEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	workspaceGetRPC = func(context.Context, string, string) (daemon.WorkspaceGetResponse, error) {
		return daemon.WorkspaceGetResponse{WorkspaceID: "sess-1", OriginRoot: t.TempDir()}, nil
	}
	called := false
	commandListRPC = func(context.Context, string, string) (daemon.CommandListResponse, error) {
		called = true
		return daemon.CommandListResponse{}, nil
	}
	t.Cleanup(func() {
		workflowContextResolver = originalResolver
		commandResolveWorkspaceTarget = originalResolveTarget
		commandEnsureDaemonRunning = originalEnsure
		workspaceGetRPC = originalSessionGet
		commandListRPC = originalList
	})

	_, err := executeRootCommand("exec", "list", "--workspace", "alpha")
	if err != nil {
		t.Fatalf("command list with --workspace returned error: %v", err)
	}
	if !called {
		t.Fatal("command list RPC not called with --workspace override")
	}
}

func TestCommandListAllowsOriginRootWhenBroaderThanFolder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	workspaceRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspaceRoot, "repo-a"), 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(workspaceRoot, "repo-b"), 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd returned error: %v", err)
	}
	if err := os.Chdir(filepath.Join(workspaceRoot, "repo-b")); err != nil {
		t.Fatalf("os.Chdir returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})

	originalResolver := workflowContextResolver
	originalResolveTarget := commandResolveWorkspaceTarget
	originalEnsure := commandEnsureDaemonRunning
	originalSessionGet := workspaceGetRPC
	originalList := commandListRPC

	workflowContextResolver = &WorkflowContextResolver{store: activeWorkspaceStoreFunc{read: func() (string, error) {
		return "sess-1", nil
	}}, resolveTarget: func(context.Context, string, string) (resolvedWorkspaceTarget, error) {
		workspace := daemon.WorkspaceGetResponse{WorkspaceID: "sess-1", OriginRoot: workspaceRoot, Folders: []daemon.WorkspaceFolderInfo{{Path: filepath.Join(workspaceRoot, "repo-a")}}}
		return resolvedWorkspaceTarget{WorkspaceID: workspace.WorkspaceID, Workspace: &workspace}, nil
	}}
	commandResolveWorkspaceTarget = workflowContextResolver.resolveTarget
	commandEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	workspaceGetRPC = func(context.Context, string, string) (daemon.WorkspaceGetResponse, error) {
		return daemon.WorkspaceGetResponse{
			WorkspaceID: "sess-1",
			OriginRoot:  workspaceRoot,
			Folders:     []daemon.WorkspaceFolderInfo{{Path: filepath.Join(workspaceRoot, "repo-a")}},
		}, nil
	}
	called := false
	commandListRPC = func(context.Context, string, string) (daemon.CommandListResponse, error) {
		called = true
		return daemon.CommandListResponse{}, nil
	}
	t.Cleanup(func() {
		workflowContextResolver = originalResolver
		commandResolveWorkspaceTarget = originalResolveTarget
		commandEnsureDaemonRunning = originalEnsure
		workspaceGetRPC = originalSessionGet
		commandListRPC = originalList
	})

	_, err = executeRootCommand("exec", "list")
	if err != nil {
		t.Fatalf("command list returned error: %v", err)
	}
	if !called {
		t.Fatal("command list RPC not called when cwd is within origin root")
	}
}

func TestCommandListEnvActiveWorkspaceBypassesWorkspaceSafety(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ARI_ACTIVE_WORKSPACE", "sess-env")

	originalResolver := workflowContextResolver
	originalResolveTarget := commandResolveWorkspaceTarget
	originalEnsure := commandEnsureDaemonRunning
	originalSessionGet := workspaceGetRPC
	originalList := commandListRPC

	workflowContextResolver = &WorkflowContextResolver{store: activeWorkspaceStoreFunc{read: func() (string, error) {
		return "sess-env", nil
	}}, resolveTarget: func(context.Context, string, string) (resolvedWorkspaceTarget, error) {
		workspace := daemon.WorkspaceGetResponse{WorkspaceID: "sess-env", OriginRoot: t.TempDir()}
		return resolvedWorkspaceTarget{WorkspaceID: workspace.WorkspaceID, Workspace: &workspace}, nil
	}}
	commandResolveWorkspaceTarget = workflowContextResolver.resolveTarget
	commandEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	workspaceGetRPC = func(context.Context, string, string) (daemon.WorkspaceGetResponse, error) {
		return daemon.WorkspaceGetResponse{WorkspaceID: "sess-env", OriginRoot: t.TempDir()}, nil
	}
	called := false
	commandListRPC = func(context.Context, string, string) (daemon.CommandListResponse, error) {
		called = true
		return daemon.CommandListResponse{}, nil
	}
	t.Cleanup(func() {
		workflowContextResolver = originalResolver
		commandResolveWorkspaceTarget = originalResolveTarget
		commandEnsureDaemonRunning = originalEnsure
		workspaceGetRPC = originalSessionGet
		commandListRPC = originalList
	})

	_, err := executeRootCommandRaw("exec", "list")
	if err != nil {
		t.Fatalf("command list with env override returned error: %v", err)
	}
	if !called {
		t.Fatal("command list RPC not called with env active-workspace override")
	}
}

func TestCommandSubcommandsRejectActiveWorkspaceOutsideWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd returned error: %v", err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("os.Chdir returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})

	originalResolver := workflowContextResolver
	originalResolveTarget := commandResolveWorkspaceTarget
	originalEnsure := commandEnsureDaemonRunning
	originalSessionGet := workspaceGetRPC
	originalRun := commandRunRPC
	originalList := commandListRPC
	originalShow := commandGetRPC
	originalOutput := commandOutputRPC
	originalStop := commandStopRPC

	workflowContextResolver = &WorkflowContextResolver{store: activeWorkspaceStoreFunc{read: func() (string, error) {
		return "sess-1", nil
	}}, resolveTarget: func(context.Context, string, string) (resolvedWorkspaceTarget, error) {
		workspace := daemon.WorkspaceGetResponse{WorkspaceID: "sess-1", OriginRoot: t.TempDir()}
		return resolvedWorkspaceTarget{WorkspaceID: workspace.WorkspaceID, Workspace: &workspace}, nil
	}}
	commandResolveWorkspaceTarget = workflowContextResolver.resolveTarget
	commandEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	workspaceGetRPC = func(context.Context, string, string) (daemon.WorkspaceGetResponse, error) {
		return daemon.WorkspaceGetResponse{WorkspaceID: "sess-1", OriginRoot: t.TempDir()}, nil
	}
	commandRunRPC = func(context.Context, string, daemon.CommandRunRequest) (daemon.CommandRunResponse, error) {
		return daemon.CommandRunResponse{}, errors.New("command run should not be called")
	}
	commandListRPC = func(context.Context, string, string) (daemon.CommandListResponse, error) {
		return daemon.CommandListResponse{}, errors.New("command list should not be called")
	}
	commandGetRPC = func(context.Context, string, string, string) (daemon.CommandGetResponse, error) {
		return daemon.CommandGetResponse{}, errors.New("command show should not be called")
	}
	commandOutputRPC = func(context.Context, string, string, string) (daemon.CommandOutputResponse, error) {
		return daemon.CommandOutputResponse{}, errors.New("command output should not be called")
	}
	commandStopRPC = func(context.Context, string, string, string) (daemon.CommandStopResponse, error) {
		return daemon.CommandStopResponse{}, errors.New("command stop should not be called")
	}
	t.Cleanup(func() {
		workflowContextResolver = originalResolver
		commandResolveWorkspaceTarget = originalResolveTarget
		commandEnsureDaemonRunning = originalEnsure
		workspaceGetRPC = originalSessionGet
		commandRunRPC = originalRun
		commandListRPC = originalList
		commandGetRPC = originalShow
		commandOutputRPC = originalOutput
		commandStopRPC = originalStop
	})

	tests := []struct {
		name string
		args []string
	}{
		{name: "run", args: []string{"exec", "run", "--", "echo", "hi"}},
		{name: "list", args: []string{"exec", "list"}},
		{name: "show", args: []string{"exec", "show", "cmd-1"}},
		{name: "output", args: []string{"exec", "output", "cmd-1"}},
		{name: "stop", args: []string{"exec", "stop", "cmd-1"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := executeRootCommandRaw(tc.args...)
			if err == nil {
				t.Fatalf("%s returned nil error", tc.name)
			}
			if err.Error() != "Active workspace belongs to a different workspace; use --workspace <id-or-name> to target a workspace explicitly" {
				t.Fatalf("%s error = %q, want workspace mismatch error", tc.name, err.Error())
			}
		})
	}
}

func TestCommandListUsesSingleSessionGetForActiveWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ARI_ACTIVE_WORKSPACE", "")

	workspaceRoot := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd returned error: %v", err)
	}
	if err := os.Chdir(workspaceRoot); err != nil {
		t.Fatalf("os.Chdir returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})

	originalResolver := workflowContextResolver
	originalResolveTarget := commandResolveWorkspaceTarget
	originalEnsure := commandEnsureDaemonRunning
	originalSessionGet := workspaceGetRPC
	originalList := commandListRPC
	sessionGetCalls := 0

	workflowContextResolver = &WorkflowContextResolver{store: activeWorkspaceStoreFunc{read: func() (string, error) {
		return "sess-1", nil
	}}, resolveTarget: func(context.Context, string, string) (resolvedWorkspaceTarget, error) {
		sessionGetCalls++
		workspace := daemon.WorkspaceGetResponse{WorkspaceID: "sess-1", OriginRoot: workspaceRoot}
		return resolvedWorkspaceTarget{WorkspaceID: workspace.WorkspaceID, Workspace: &workspace}, nil
	}}
	commandResolveWorkspaceTarget = workflowContextResolver.resolveTarget
	commandEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	workspaceGetRPC = func(context.Context, string, string) (daemon.WorkspaceGetResponse, error) {
		sessionGetCalls++
		return daemon.WorkspaceGetResponse{WorkspaceID: "sess-1", OriginRoot: workspaceRoot}, nil
	}
	commandListRPC = func(context.Context, string, string) (daemon.CommandListResponse, error) {
		return daemon.CommandListResponse{}, nil
	}
	t.Cleanup(func() {
		workflowContextResolver = originalResolver
		commandResolveWorkspaceTarget = originalResolveTarget
		commandEnsureDaemonRunning = originalEnsure
		workspaceGetRPC = originalSessionGet
		commandListRPC = originalList
	})

	if _, err := executeRootCommandRaw("exec", "list"); err != nil {
		t.Fatalf("command list returned error: %v", err)
	}
	if sessionGetCalls != 1 {
		t.Fatalf("workspaceGetRPC calls = %d, want 1", sessionGetCalls)
	}
}

func TestExecSubcommandsExist(t *testing.T) {
	execCmd := NewExecCmd()

	tests := []struct {
		name string
		path []string
		want string
	}{
		{name: "run", path: []string{"run"}, want: "run"},
		{name: "list", path: []string{"list"}, want: "list"},
		{name: "show", path: []string{"show"}, want: "show"},
		{name: "output", path: []string{"output"}, want: "output"},
		{name: "stop", path: []string{"stop"}, want: "stop"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd, _, err := execCmd.Find(tc.path)
			if err != nil {
				t.Fatalf("find command %s: %v", tc.want, err)
			}
			if cmd == nil {
				t.Fatalf("expected subcommand %q to be registered", tc.want)
			}
			if cmd.Name() != tc.want {
				t.Fatalf("subcommand name = %q, want %q", cmd.Name(), tc.want)
			}
		})
	}
}

func TestCommandRunUsesSeparatorArguments(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolveTarget := commandResolveWorkspaceTarget
	originalResolver := workflowContextResolver
	originalEnsure := commandEnsureDaemonRunning
	originalEnsureScope := commandEnsureWorkspaceScope
	originalRun := commandRunRPC
	originalGet := commandGetRPC
	originalOutput := commandOutputRPC

	commandResolveWorkspaceTarget = func(context.Context, string, string) (resolvedWorkspaceTarget, error) {
		return resolvedWorkspaceTarget{WorkspaceID: "sess-1", Workspace: &daemon.WorkspaceGetResponse{WorkspaceID: "sess-1", OriginRoot: t.TempDir()}}, nil
	}
	workflowContextResolver = &WorkflowContextResolver{store: activeWorkspaceStoreFunc{read: func() (string, error) {
		return "sess-1", nil
	}}, resolveTarget: resolveWorkspaceTarget}
	commandEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	commandEnsureWorkspaceScope = func(context.Context, *daemon.WorkspaceGetResponse, string) error { return nil }
	var gotReq daemon.CommandRunRequest
	commandRunRPC = func(_ context.Context, _ string, req daemon.CommandRunRequest) (daemon.CommandRunResponse, error) {
		gotReq = req
		return daemon.CommandRunResponse{CommandID: "cmd-1", Status: "running"}, nil
	}
	commandGetRPC = func(context.Context, string, string, string) (daemon.CommandGetResponse, error) {
		return daemon.CommandGetResponse{CommandID: "cmd-1", Status: "exited"}, nil
	}
	commandOutputRPC = func(context.Context, string, string, string) (daemon.CommandOutputResponse, error) {
		return daemon.CommandOutputResponse{Output: "ok\n"}, nil
	}
	t.Cleanup(func() {
		commandResolveWorkspaceTarget = originalResolveTarget
		workflowContextResolver = originalResolver
		commandEnsureDaemonRunning = originalEnsure
		commandEnsureWorkspaceScope = originalEnsureScope
		commandRunRPC = originalRun
		commandGetRPC = originalGet
		commandOutputRPC = originalOutput
	})

	out, err := executeRootCommand("exec", "run", "--", "go", "test", "./...")
	if err != nil {
		t.Fatalf("execute command run: %v", err)
	}

	if gotReq.Command != "go" {
		t.Fatalf("command run request command = %q, want %q", gotReq.Command, "go")
	}
	if len(gotReq.Args) != 2 || gotReq.Args[0] != "test" || gotReq.Args[1] != "./..." {
		t.Fatalf("command run args = %#v, want [test ./...]", gotReq.Args)
	}
	if !strings.Contains(out, "Command started: cmd-1") {
		t.Fatalf("command run output = %q, want command started line", out)
	}
	if !strings.Contains(out, "ok\n") {
		t.Fatalf("command run output = %q, want command output", out)
	}
}

func TestCommandRunStopsPollingAtConfiguredWallclockCap(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolveTarget := commandResolveWorkspaceTarget
	originalResolver := workflowContextResolver
	originalEnsure := commandEnsureDaemonRunning
	originalEnsureScope := commandEnsureWorkspaceScope
	originalRun := commandRunRPC
	originalGet := commandGetRPC
	originalMaxDuration := oneOffCommandMaxDuration

	commandResolveWorkspaceTarget = func(context.Context, string, string) (resolvedWorkspaceTarget, error) {
		return resolvedWorkspaceTarget{WorkspaceID: "sess-1", Workspace: &daemon.WorkspaceGetResponse{WorkspaceID: "sess-1", OriginRoot: t.TempDir()}}, nil
	}
	workflowContextResolver = &WorkflowContextResolver{store: activeWorkspaceStoreFunc{read: func() (string, error) { return "sess-1", nil }}, resolveTarget: resolveWorkspaceTarget}
	commandEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	commandEnsureWorkspaceScope = func(context.Context, *daemon.WorkspaceGetResponse, string) error { return nil }
	commandRunRPC = func(context.Context, string, daemon.CommandRunRequest) (daemon.CommandRunResponse, error) {
		return daemon.CommandRunResponse{CommandID: "cmd-1", Status: "running"}, nil
	}
	commandGetRPC = func(context.Context, string, string, string) (daemon.CommandGetResponse, error) {
		return daemon.CommandGetResponse{CommandID: "cmd-1", Status: "running"}, nil
	}
	oneOffCommandMaxDuration = 20 * time.Millisecond
	t.Cleanup(func() {
		commandResolveWorkspaceTarget = originalResolveTarget
		workflowContextResolver = originalResolver
		commandEnsureDaemonRunning = originalEnsure
		commandEnsureWorkspaceScope = originalEnsureScope
		commandRunRPC = originalRun
		commandGetRPC = originalGet
		oneOffCommandMaxDuration = originalMaxDuration
	})

	_, err := executeRootCommandRaw("exec", "run", "--", "go", "test")
	if err == nil {
		t.Fatal("execute command run returned nil error for command that exceeded wallclock cap")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("execute command run error = %v, want context deadline exceeded", err)
	}
}

func TestCommandRunPrintsNoOutputMessageWhenOutputIsEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolveTarget := commandResolveWorkspaceTarget
	originalResolver := workflowContextResolver
	originalEnsure := commandEnsureDaemonRunning
	originalEnsureScope := commandEnsureWorkspaceScope
	originalRun := commandRunRPC
	originalGet := commandGetRPC
	originalOutput := commandOutputRPC

	commandResolveWorkspaceTarget = func(context.Context, string, string) (resolvedWorkspaceTarget, error) {
		return resolvedWorkspaceTarget{WorkspaceID: "sess-1", Workspace: &daemon.WorkspaceGetResponse{WorkspaceID: "sess-1", OriginRoot: t.TempDir()}}, nil
	}
	workflowContextResolver = &WorkflowContextResolver{store: activeWorkspaceStoreFunc{read: func() (string, error) { return "sess-1", nil }}, resolveTarget: resolveWorkspaceTarget}
	commandEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	commandEnsureWorkspaceScope = func(context.Context, *daemon.WorkspaceGetResponse, string) error { return nil }
	commandRunRPC = func(context.Context, string, daemon.CommandRunRequest) (daemon.CommandRunResponse, error) {
		return daemon.CommandRunResponse{CommandID: "cmd-1", Status: "running"}, nil
	}
	commandGetRPC = func(context.Context, string, string, string) (daemon.CommandGetResponse, error) {
		return daemon.CommandGetResponse{CommandID: "cmd-1", Status: "exited"}, nil
	}
	commandOutputRPC = func(context.Context, string, string, string) (daemon.CommandOutputResponse, error) {
		return daemon.CommandOutputResponse{Output: ""}, nil
	}
	t.Cleanup(func() {
		commandResolveWorkspaceTarget = originalResolveTarget
		workflowContextResolver = originalResolver
		commandEnsureDaemonRunning = originalEnsure
		commandEnsureWorkspaceScope = originalEnsureScope
		commandRunRPC = originalRun
		commandGetRPC = originalGet
		commandOutputRPC = originalOutput
	})

	out, err := executeRootCommand("exec", "run", "--", "echo", "hi")
	if err != nil {
		t.Fatalf("exec run returned error: %v", err)
	}
	if !strings.Contains(out, "Command produced no output.") {
		t.Fatalf("exec run output = %q, want no-output note", out)
	}
}

func TestCommandRunReturnsErrorOnNonZeroExit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolveTarget := commandResolveWorkspaceTarget
	originalResolver := workflowContextResolver
	originalEnsure := commandEnsureDaemonRunning
	originalEnsureScope := commandEnsureWorkspaceScope
	originalRun := commandRunRPC
	originalGet := commandGetRPC
	originalOutput := commandOutputRPC

	commandResolveWorkspaceTarget = func(context.Context, string, string) (resolvedWorkspaceTarget, error) {
		return resolvedWorkspaceTarget{WorkspaceID: "sess-1", Workspace: &daemon.WorkspaceGetResponse{WorkspaceID: "sess-1", OriginRoot: t.TempDir()}}, nil
	}
	workflowContextResolver = &WorkflowContextResolver{store: activeWorkspaceStoreFunc{read: func() (string, error) { return "sess-1", nil }}, resolveTarget: resolveWorkspaceTarget}
	commandEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	commandEnsureWorkspaceScope = func(context.Context, *daemon.WorkspaceGetResponse, string) error { return nil }
	commandRunRPC = func(context.Context, string, daemon.CommandRunRequest) (daemon.CommandRunResponse, error) {
		return daemon.CommandRunResponse{CommandID: "cmd-1", Status: "running"}, nil
	}
	commandGetRPC = func(context.Context, string, string, string) (daemon.CommandGetResponse, error) {
		exitCode := 7
		return daemon.CommandGetResponse{CommandID: "cmd-1", Status: "exited", ExitCode: &exitCode}, nil
	}
	commandOutputRPC = func(context.Context, string, string, string) (daemon.CommandOutputResponse, error) {
		return daemon.CommandOutputResponse{Output: "failed\n"}, nil
	}
	t.Cleanup(func() {
		commandResolveWorkspaceTarget = originalResolveTarget
		workflowContextResolver = originalResolver
		commandEnsureDaemonRunning = originalEnsure
		commandEnsureWorkspaceScope = originalEnsureScope
		commandRunRPC = originalRun
		commandGetRPC = originalGet
		commandOutputRPC = originalOutput
	})

	_, err := executeRootCommand("exec", "run", "--", "echo", "hi")
	if err == nil {
		t.Fatal("exec run returned nil error")
	}
	if err.Error() != "Command exited with code 7" {
		t.Fatalf("exec run error = %q, want %q", err.Error(), "Command exited with code 7")
	}
}

func TestCommandListShowOutputStop(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolver := workflowContextResolver
	originalResolveTarget := commandResolveWorkspaceTarget
	originalEnsureScope := commandEnsureWorkspaceScope
	originalList := commandListRPC
	originalShow := commandGetRPC
	originalOutput := commandOutputRPC
	originalStop := commandStopRPC
	workflowContextResolver = &WorkflowContextResolver{store: activeWorkspaceStoreFunc{read: func() (string, error) {
		return "sess-1", nil
	}}, resolveTarget: func(context.Context, string, string) (resolvedWorkspaceTarget, error) {
		workspace := daemon.WorkspaceGetResponse{WorkspaceID: "sess-1", OriginRoot: t.TempDir()}
		return resolvedWorkspaceTarget{WorkspaceID: workspace.WorkspaceID, Workspace: &workspace}, nil
	}}
	commandResolveWorkspaceTarget = workflowContextResolver.resolveTarget
	commandEnsureWorkspaceScope = func(context.Context, *daemon.WorkspaceGetResponse, string) error { return nil }
	commandListRPC = func(context.Context, string, string) (daemon.CommandListResponse, error) {
		return daemon.CommandListResponse{Commands: []daemon.CommandSummary{{CommandID: "cmd-1", Command: "go test", Status: "running", StartedAt: "now"}}}, nil
	}
	commandGetRPC = func(context.Context, string, string, string) (daemon.CommandGetResponse, error) {
		exitCode := 0
		return daemon.CommandGetResponse{CommandID: "cmd-1", WorkspaceID: "sess-1", Command: "go test", Args: `["./..."]`, Status: "exited", ExitCode: &exitCode, StartedAt: "now", FinishedAt: "later"}, nil
	}
	commandOutputRPC = func(context.Context, string, string, string) (daemon.CommandOutputResponse, error) {
		return daemon.CommandOutputResponse{Output: "ok\n"}, nil
	}
	commandStopRPC = func(context.Context, string, string, string) (daemon.CommandStopResponse, error) {
		return daemon.CommandStopResponse{Status: "stopping"}, nil
	}
	t.Cleanup(func() {
		workflowContextResolver = originalResolver
		commandResolveWorkspaceTarget = originalResolveTarget
		commandEnsureWorkspaceScope = originalEnsureScope
		commandListRPC = originalList
		commandGetRPC = originalShow
		commandOutputRPC = originalOutput
		commandStopRPC = originalStop
	})

	listOut, err := executeRootCommand("exec", "list")
	if err != nil {
		t.Fatalf("execute command list: %v", err)
	}
	if !strings.Contains(listOut, "cmd-1") {
		t.Fatalf("command list output = %q, want command id", listOut)
	}

	showOut, err := executeRootCommand("exec", "show", "cmd-1")
	if err != nil {
		t.Fatalf("execute command show: %v", err)
	}
	if !strings.Contains(showOut, "State: exited") {
		t.Fatalf("command show output = %q, want state", showOut)
	}

	outputOut, err := executeRootCommand("exec", "output", "cmd-1")
	if err != nil {
		t.Fatalf("execute command output: %v", err)
	}
	if !strings.Contains(outputOut, "ok") {
		t.Fatalf("command output output = %q, want output content", outputOut)
	}

	stopOut, err := executeRootCommand("exec", "stop", "cmd-1")
	if err != nil {
		t.Fatalf("execute command stop: %v", err)
	}
	if !strings.Contains(stopOut, "stopping") {
		t.Fatalf("command stop output = %q, want stopping status", stopOut)
	}
}

func TestCommandShowNotFoundMapsError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolver := workflowContextResolver
	originalResolveTarget := commandResolveWorkspaceTarget
	originalEnsureScope := commandEnsureWorkspaceScope
	originalShow := commandGetRPC
	workflowContextResolver = &WorkflowContextResolver{store: activeWorkspaceStoreFunc{read: func() (string, error) {
		return "sess-1", nil
	}}, resolveTarget: func(context.Context, string, string) (resolvedWorkspaceTarget, error) {
		workspace := daemon.WorkspaceGetResponse{WorkspaceID: "sess-1", OriginRoot: t.TempDir()}
		return resolvedWorkspaceTarget{WorkspaceID: workspace.WorkspaceID, Workspace: &workspace}, nil
	}}
	commandResolveWorkspaceTarget = workflowContextResolver.resolveTarget
	commandEnsureWorkspaceScope = func(context.Context, *daemon.WorkspaceGetResponse, string) error { return nil }
	commandGetRPC = func(context.Context, string, string, string) (daemon.CommandGetResponse, error) {
		return daemon.CommandGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.CommandNotFound), Message: "command not found"}
	}
	t.Cleanup(func() {
		workflowContextResolver = originalResolver
		commandResolveWorkspaceTarget = originalResolveTarget
		commandEnsureWorkspaceScope = originalEnsureScope
		commandGetRPC = originalShow
	})

	_, err := executeRootCommand("exec", "show", "missing")
	if err == nil {
		t.Fatal("command show returned nil error for missing command")
	}
	if err.Error() != "Command not found" {
		t.Fatalf("command show error = %q, want %q", err.Error(), "Command not found")
	}
}

func TestCommandShowSessionNotFoundMapsError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolver := workflowContextResolver
	originalResolveTarget := commandResolveWorkspaceTarget
	originalEnsureScope := commandEnsureWorkspaceScope
	originalShow := commandGetRPC
	workflowContextResolver = &WorkflowContextResolver{store: activeWorkspaceStoreFunc{read: func() (string, error) {
		return "sess-missing", nil
	}}, resolveTarget: func(context.Context, string, string) (resolvedWorkspaceTarget, error) {
		workspace := daemon.WorkspaceGetResponse{WorkspaceID: "sess-missing", OriginRoot: t.TempDir()}
		return resolvedWorkspaceTarget{WorkspaceID: workspace.WorkspaceID, Workspace: &workspace}, nil
	}}
	commandResolveWorkspaceTarget = workflowContextResolver.resolveTarget
	commandEnsureWorkspaceScope = func(context.Context, *daemon.WorkspaceGetResponse, string) error { return nil }
	commandGetRPC = func(context.Context, string, string, string) (daemon.CommandGetResponse, error) {
		return daemon.CommandGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "session not found"}
	}
	t.Cleanup(func() {
		workflowContextResolver = originalResolver
		commandResolveWorkspaceTarget = originalResolveTarget
		commandEnsureWorkspaceScope = originalEnsureScope
		commandGetRPC = originalShow
	})

	_, err := executeRootCommand("exec", "show", "cmd-1")
	if err == nil {
		t.Fatal("command show returned nil error for missing session")
	}
	if err.Error() != "Workspace not found" {
		t.Fatalf("command show error = %q, want %q", err.Error(), "Workspace not found")
	}
}

func TestCommandListUsesSessionFlagOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolveTarget := commandResolveWorkspaceTarget
	originalResolver := workflowContextResolver
	originalList := commandListRPC

	var gotLookup string
	commandResolveWorkspaceTarget = func(_ context.Context, _ string, idOrName string) (resolvedWorkspaceTarget, error) {
		gotLookup = idOrName
		return resolvedWorkspaceTarget{WorkspaceID: "sess-override", Workspace: &daemon.WorkspaceGetResponse{WorkspaceID: "sess-override", OriginRoot: t.TempDir()}}, nil
	}
	workflowContextResolver = &WorkflowContextResolver{store: activeWorkspaceStoreFunc{read: func() (string, error) {
		return "sess-active", nil
	}}, resolveTarget: resolveWorkspaceTarget}
	commandListRPC = func(context.Context, string, string) (daemon.CommandListResponse, error) {
		return daemon.CommandListResponse{}, nil
	}
	t.Cleanup(func() {
		commandResolveWorkspaceTarget = originalResolveTarget
		workflowContextResolver = originalResolver
		commandListRPC = originalList
	})

	_, err := executeRootCommand("exec", "list", "--workspace", "alpha")
	if err != nil {
		t.Fatalf("execute command list with --workspace: %v", err)
	}
	if gotLookup != "alpha" {
		t.Fatalf("session lookup argument = %q, want %q", gotLookup, "alpha")
	}
}

func TestCommandListRequiresActiveWorkspaceWhenSessionNotProvided(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolver := workflowContextResolver
	originalContextGet := workspaceContextGetRPC
	workflowContextResolver = &WorkflowContextResolver{store: activeWorkspaceStoreFunc{read: func() (string, error) {
		return "", nil
	}}, resolveTarget: resolveWorkspaceTarget}
	workspaceContextGetRPC = func(context.Context, string) (daemon.ContextGetResponse, error) {
		return daemon.ContextGetResponse{}, nil
	}
	t.Cleanup(func() {
		workflowContextResolver = originalResolver
		workspaceContextGetRPC = originalContextGet
	})

	_, err := executeRootCommand("exec", "list")
	if err == nil {
		t.Fatal("command list returned nil error without active workspace")
	}
	if err.Error() != "No active workspace is set" {
		t.Fatalf("command list error = %q, want %q", err.Error(), "No active workspace is set")
	}
}

func TestCommandListMissingActiveWorkspaceChecksDaemonContext(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolver := workflowContextResolver
	originalEnsure := commandEnsureDaemonRunning
	originalContextGet := workspaceContextGetRPC
	workflowContextResolver = &WorkflowContextResolver{store: activeWorkspaceStoreFunc{read: func() (string, error) {
		return "", nil
	}}, resolveTarget: resolveWorkspaceTarget}
	ensureCalled := false
	commandEnsureDaemonRunning = func(context.Context, *config.Config) error {
		ensureCalled = true
		return nil
	}
	workspaceContextGetRPC = func(context.Context, string) (daemon.ContextGetResponse, error) {
		return daemon.ContextGetResponse{}, nil
	}
	t.Cleanup(func() {
		workflowContextResolver = originalResolver
		commandEnsureDaemonRunning = originalEnsure
		workspaceContextGetRPC = originalContextGet
	})

	_, err := executeRootCommandRaw("exec", "list")
	if err == nil {
		t.Fatal("command list returned nil error without active workspace")
	}
	if err.Error() != "No active workspace is set" {
		t.Fatalf("command list error = %q, want %q", err.Error(), "No active workspace is set")
	}
	if !ensureCalled {
		t.Fatal("expected daemon ensure before reading daemon active context")
	}
}

func TestCommandSubcommandsUseSessionFlagOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolveTarget := commandResolveWorkspaceTarget
	originalResolver := workflowContextResolver
	originalEnsure := commandEnsureDaemonRunning
	originalRun := commandRunRPC
	originalList := commandListRPC
	originalShow := commandGetRPC
	originalOutput := commandOutputRPC
	originalStop := commandStopRPC

	workflowContextResolver = &WorkflowContextResolver{store: activeWorkspaceStoreFunc{read: func() (string, error) {
		return "", errors.New("active workspace should not be read when --workspace is provided")
	}}, resolveTarget: resolveWorkspaceTarget}
	commandEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	commandRunRPC = func(context.Context, string, daemon.CommandRunRequest) (daemon.CommandRunResponse, error) {
		return daemon.CommandRunResponse{CommandID: "cmd-1", Status: "running"}, nil
	}
	commandListRPC = func(context.Context, string, string) (daemon.CommandListResponse, error) {
		return daemon.CommandListResponse{}, nil
	}
	commandGetRPC = func(context.Context, string, string, string) (daemon.CommandGetResponse, error) {
		return daemon.CommandGetResponse{CommandID: "cmd-1", WorkspaceID: "sess-1", Command: "echo", Status: "exited", StartedAt: "now"}, nil
	}
	commandOutputRPC = func(context.Context, string, string, string) (daemon.CommandOutputResponse, error) {
		return daemon.CommandOutputResponse{Output: "ok\n"}, nil
	}
	commandStopRPC = func(context.Context, string, string, string) (daemon.CommandStopResponse, error) {
		return daemon.CommandStopResponse{Status: "stopping"}, nil
	}
	t.Cleanup(func() {
		commandResolveWorkspaceTarget = originalResolveTarget
		workflowContextResolver = originalResolver
		commandEnsureDaemonRunning = originalEnsure
		commandRunRPC = originalRun
		commandListRPC = originalList
		commandGetRPC = originalShow
		commandOutputRPC = originalOutput
		commandStopRPC = originalStop
	})

	tests := []struct {
		name string
		args []string
	}{
		{name: "run", args: []string{"exec", "run", "--workspace", "alpha", "--", "echo", "hi"}},
		{name: "list", args: []string{"exec", "list", "--workspace", "alpha"}},
		{name: "show", args: []string{"exec", "show", "cmd-1", "--workspace", "alpha"}},
		{name: "output", args: []string{"exec", "output", "cmd-1", "--workspace", "alpha"}},
		{name: "stop", args: []string{"exec", "stop", "cmd-1", "--workspace", "alpha"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotLookup := ""
			commandResolveWorkspaceTarget = func(_ context.Context, _ string, idOrName string) (resolvedWorkspaceTarget, error) {
				gotLookup = idOrName
				return resolvedWorkspaceTarget{WorkspaceID: "sess-override", Workspace: &daemon.WorkspaceGetResponse{WorkspaceID: "sess-override", OriginRoot: t.TempDir()}}, nil
			}

			_, err := executeRootCommand(tc.args...)
			if err != nil {
				t.Fatalf("execute command %s with --workspace: %v", tc.name, err)
			}
			if gotLookup != "alpha" {
				t.Fatalf("session lookup argument = %q, want %q", gotLookup, "alpha")
			}
		})
	}
}
