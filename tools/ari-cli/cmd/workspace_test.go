package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/config"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
	"github.com/sourcegraph/jsonrpc2"
	"github.com/spf13/cobra"
)

func TestRootRegistersWorkspaceCommand(t *testing.T) {
	root := NewRootCmd()
	workspaceCmd, _, err := root.Find([]string{"workspace"})
	if err != nil {
		t.Fatalf("find workspace command: %v", err)
	}

	if workspaceCmd == nil {
		t.Fatalf("expected workspace command to be registered")
	}

	if workspaceCmd.Name() != "workspace" {
		t.Fatalf("unexpected command name: %q", workspaceCmd.Name())
	}
}

func TestWorkspaceSubcommandsExist(t *testing.T) {
	workspace := NewWorkspaceCmd()

	create, _, err := workspace.Find([]string{"create"})
	if err != nil {
		t.Fatalf("find workspace create: %v", err)
	}
	list, _, err := workspace.Find([]string{"list"})
	if err != nil {
		t.Fatalf("find workspace list: %v", err)
	}
	show, _, err := workspace.Find([]string{"show"})
	if err != nil {
		t.Fatalf("find workspace show: %v", err)
	}
	suspend, _, err := workspace.Find([]string{"suspend"})
	if err != nil {
		t.Fatalf("find workspace suspend: %v", err)
	}
	resume, _, err := workspace.Find([]string{"resume"})
	if err != nil {
		t.Fatalf("find workspace resume: %v", err)
	}
	set, _, err := workspace.Find([]string{"set"})
	if err != nil {
		t.Fatalf("find workspace set: %v", err)
	}
	current, _, err := workspace.Find([]string{"current"})
	if err != nil {
		t.Fatalf("find workspace current: %v", err)
	}
	clear, _, err := workspace.Find([]string{"clear"})
	if err != nil {
		t.Fatalf("find workspace clear: %v", err)
	}
	switchCmd, _, err := workspace.Find([]string{"switch"})
	if err != nil {
		t.Fatalf("find workspace switch: %v", err)
	}
	folderAdd, _, err := workspace.Find([]string{"folder", "add"})
	if err != nil {
		t.Fatalf("find workspace folder add: %v", err)
	}
	folderRemove, _, err := workspace.Find([]string{"folder", "remove"})
	if err != nil {
		t.Fatalf("find workspace folder remove: %v", err)
	}
	if create == nil || list == nil || show == nil || suspend == nil || resume == nil || set == nil || current == nil || clear == nil || switchCmd == nil || folderAdd == nil || folderRemove == nil {
		t.Fatal("expected workspace subcommands to be registered")
	}
}

func TestWorkspaceCommandCreateListShowRemoveLifecycle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalReadActive := workspaceCommandReadActiveSession
	originalCreateRPC := workspaceCommandCreateRPC
	originalListRPC := workspaceCommandListRPC
	originalGetRPC := workspaceCommandGetRPC
	originalRemoveRPC := workspaceCommandRemoveRPC
	originalEnsureScope := workspaceCommandEnsureScope
	originalWorkspaceGet := workspaceGetRPC
	originalWorkspaceList := workspaceListRPC

	workspaceCommandReadActiveSession = func() (string, error) { return "ws-1", nil }
	workspaceCommandEnsureScope = func(*daemon.WorkspaceGetResponse, string) error { return nil }
	workspaceGetRPC = func(context.Context, string, string) (daemon.WorkspaceGetResponse, error) {
		return daemon.WorkspaceGetResponse{WorkspaceID: "ws-1", Name: "alpha", OriginRoot: t.TempDir()}, nil
	}
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{Workspaces: []daemon.WorkspaceSummary{{WorkspaceID: "ws-1", Name: "alpha", Status: "active"}}}, nil
	}
	created := daemon.WorkspaceCommandCreateResponse{}
	workspaceCommandCreateRPC = func(_ context.Context, _ string, req daemon.WorkspaceCommandCreateRequest) (daemon.WorkspaceCommandCreateResponse, error) {
		created = daemon.WorkspaceCommandCreateResponse{
			CommandID: "cmd-1",
			Name:      req.Name,
			Command:   req.Command,
			Args:      req.Args,
			CreatedAt: "now",
			UpdatedAt: "now",
		}
		return created, nil
	}
	workspaceCommandListRPC = func(context.Context, string, daemon.WorkspaceCommandListRequest) (daemon.WorkspaceCommandListResponse, error) {
		return daemon.WorkspaceCommandListResponse{Commands: []daemon.WorkspaceCommandSummary{{CommandID: "cmd-1", Name: "test", Command: "go", Args: []string{"test", "./..."}}}}, nil
	}
	workspaceCommandGetRPC = func(context.Context, string, daemon.WorkspaceCommandGetRequest) (daemon.WorkspaceCommandGetResponse, error) {
		return daemon.WorkspaceCommandGetResponse{CommandID: "cmd-1", Name: "test", Command: "go", Args: []string{"test", "./..."}}, nil
	}
	workspaceCommandRemoveRPC = func(context.Context, string, daemon.WorkspaceCommandRemoveRequest) (daemon.WorkspaceCommandRemoveResponse, error) {
		return daemon.WorkspaceCommandRemoveResponse{Status: "removed"}, nil
	}
	t.Cleanup(func() {
		workspaceCommandReadActiveSession = originalReadActive
		workspaceCommandCreateRPC = originalCreateRPC
		workspaceCommandListRPC = originalListRPC
		workspaceCommandGetRPC = originalGetRPC
		workspaceCommandRemoveRPC = originalRemoveRPC
		workspaceCommandEnsureScope = originalEnsureScope
		workspaceGetRPC = originalWorkspaceGet
		workspaceListRPC = originalWorkspaceList
	})

	createOut, err := executeRootCommand("command", "create", "test", "go", "test", "./...", "--count=1")
	if err != nil {
		t.Fatalf("workspace command create returned error: %v", err)
	}
	if !strings.Contains(createOut, "Command created: cmd-1 (test)") {
		t.Fatalf("workspace command create output = %q, want creation line", createOut)
	}
	if !strings.Contains(createOut, "go test ./... --count=1") {
		t.Fatalf("workspace command create output = %q, want command flag preserved as arg", createOut)
	}

	listOut, err := executeRootCommand("command", "list")
	if err != nil {
		t.Fatalf("workspace command list returned error: %v", err)
	}
	if !strings.Contains(listOut, "cmd-1") || !strings.Contains(listOut, "test") {
		t.Fatalf("workspace command list output = %q, want command row", listOut)
	}
	if !strings.Contains(listOut, "go test ./...") {
		t.Fatalf("workspace command list output = %q, want full command line", listOut)
	}

	showOut, err := executeRootCommand("command", "show", "test")
	if err != nil {
		t.Fatalf("workspace command show returned error: %v", err)
	}
	if !strings.Contains(showOut, "Command: go") {
		t.Fatalf("workspace command show output = %q, want command details", showOut)
	}

	removeOut, err := executeRootCommand("command", "remove", "test")
	if err != nil {
		t.Fatalf("workspace command remove returned error: %v", err)
	}
	if !strings.Contains(removeOut, "Command remove: removed") {
		t.Fatalf("workspace command remove output = %q, want remove status", removeOut)
	}
}

func TestWorkspaceCommandRunExecutesDefinitionAndPrintsOutput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalReadActive := workspaceCommandReadActiveSession
	originalEnsureScope := workspaceCommandEnsureScope
	originalGetDef := workspaceCommandGetRPC
	originalResolveTarget := commandResolveSessionTarget
	originalEnsure := commandEnsureDaemonRunning
	originalRun := commandRunRPC
	originalGet := commandGetRPC
	originalOutput := commandOutputRPC

	workspaceCommandReadActiveSession = func() (string, error) { return "ws-1", nil }
	workspaceCommandEnsureScope = func(*daemon.WorkspaceGetResponse, string) error { return nil }
	workspaceCommandGetRPC = func(context.Context, string, daemon.WorkspaceCommandGetRequest) (daemon.WorkspaceCommandGetResponse, error) {
		return daemon.WorkspaceCommandGetResponse{CommandID: "cmd-def-1", Name: "test", Command: "go", Args: []string{"test", "./..."}}, nil
	}
	commandResolveSessionTarget = func(context.Context, string, string) (resolvedSessionTarget, error) {
		return resolvedSessionTarget{WorkspaceID: "ws-1", Session: &daemon.WorkspaceGetResponse{WorkspaceID: "ws-1", OriginRoot: t.TempDir()}}, nil
	}
	commandEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	gotRun := daemon.CommandRunRequest{}
	commandRunRPC = func(_ context.Context, _ string, req daemon.CommandRunRequest) (daemon.CommandRunResponse, error) {
		gotRun = req
		return daemon.CommandRunResponse{CommandID: "run-1", Status: "running"}, nil
	}
	commandGetRPC = func(context.Context, string, string, string) (daemon.CommandGetResponse, error) {
		return daemon.CommandGetResponse{CommandID: "run-1", Status: "exited"}, nil
	}
	commandOutputRPC = func(context.Context, string, string, string) (daemon.CommandOutputResponse, error) {
		return daemon.CommandOutputResponse{Output: "ok\n"}, nil
	}
	t.Cleanup(func() {
		workspaceCommandReadActiveSession = originalReadActive
		workspaceCommandEnsureScope = originalEnsureScope
		workspaceCommandGetRPC = originalGetDef
		commandResolveSessionTarget = originalResolveTarget
		commandEnsureDaemonRunning = originalEnsure
		commandRunRPC = originalRun
		commandGetRPC = originalGet
		commandOutputRPC = originalOutput
	})

	out, err := executeRootCommand("command", "run", "test")
	if err != nil {
		t.Fatalf("command run returned error: %v", err)
	}
	if gotRun.Command != "go" {
		t.Fatalf("command run command = %q, want %q", gotRun.Command, "go")
	}
	if len(gotRun.Args) != 2 || gotRun.Args[0] != "test" || gotRun.Args[1] != "./..." {
		t.Fatalf("command run args = %#v, want [test ./...]", gotRun.Args)
	}
	if !strings.Contains(out, "ok\n") {
		t.Fatalf("command run output = %q, want command output", out)
	}
}

func TestWorkspaceCommandListRejectsUnexpectedArgs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalReadActive := workspaceCommandReadActiveSession
	originalListRPC := workspaceCommandListRPC
	workspaceCommandReadActiveSession = func() (string, error) { return "ws-1", nil }
	called := false
	workspaceCommandListRPC = func(context.Context, string, daemon.WorkspaceCommandListRequest) (daemon.WorkspaceCommandListResponse, error) {
		called = true
		return daemon.WorkspaceCommandListResponse{}, nil
	}
	t.Cleanup(func() {
		workspaceCommandReadActiveSession = originalReadActive
		workspaceCommandListRPC = originalListRPC
	})

	_, err := executeRootCommand("command", "list", "extra")
	if err == nil {
		t.Fatal("workspace command list returned nil error for unexpected args")
	}
	if called {
		t.Fatal("workspace command list RPC called unexpectedly")
	}
}

func TestWorkspaceSwitchRequiresInteractiveTerminal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalTTY := workspaceSwitchIsInteractiveTerminal
	workspaceSwitchIsInteractiveTerminal = func(*cobra.Command) bool { return false }
	t.Cleanup(func() {
		workspaceSwitchIsInteractiveTerminal = originalTTY
	})

	_, err := executeRootCommand("workspace", "switch")
	if err == nil {
		t.Fatal("workspace switch returned nil error for non-interactive terminal")
	}
	if err.Error() != "workspace switch requires an interactive terminal; use workspace set <id-or-name>" {
		t.Fatalf("workspace switch error = %q, want %q", err.Error(), "workspace switch requires an interactive terminal; use workspace set <id-or-name>")
	}
}

func TestWorkspaceSwitchSelectsOnlyAvailableWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalTTY := workspaceSwitchIsInteractiveTerminal
	originalList := workspaceListRPC
	workspaceSwitchIsInteractiveTerminal = func(*cobra.Command) bool { return true }
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{Workspaces: []daemon.WorkspaceSummary{{WorkspaceID: "sess-11111111", Name: "alpha", Status: "active"}}}, nil
	}
	t.Cleanup(func() {
		workspaceSwitchIsInteractiveTerminal = originalTTY
		workspaceListRPC = originalList
	})

	out, err := executeRootCommand("workspace", "switch")
	if err != nil {
		t.Fatalf("execute workspace switch: %v", err)
	}
	if !strings.Contains(out, "Active workspace set: sess-11111111") {
		t.Fatalf("session switch output = %q, want active workspace confirmation", out)
	}

	active, err := config.ReadPersistedActiveWorkspace()
	if err != nil {
		t.Fatalf("ReadPersistedActiveWorkspace returned error: %v", err)
	}
	if active != "sess-11111111" {
		t.Fatalf("persisted active session = %q, want %q", active, "sess-11111111")
	}
}

func TestWorkspaceSwitchSelectsSingleIdleWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalTTY := workspaceSwitchIsInteractiveTerminal
	originalList := workspaceListRPC
	workspaceSwitchIsInteractiveTerminal = func(*cobra.Command) bool { return true }
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{Workspaces: []daemon.WorkspaceSummary{{WorkspaceID: "sess-idle", Name: "old", Status: "idle"}}}, nil
	}
	t.Cleanup(func() {
		workspaceSwitchIsInteractiveTerminal = originalTTY
		workspaceListRPC = originalList
	})

	out, err := executeRootCommand("workspace", "switch")
	if err != nil {
		t.Fatalf("workspace switch returned error: %v", err)
	}
	if !strings.Contains(out, "Active workspace set: sess-idle") {
		t.Fatalf("workspace switch output = %q, want idle workspace selected", out)
	}
}

func TestWorkspaceSwitchInteractiveSelectionForMultipleWorkspaces(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalTTY := workspaceSwitchIsInteractiveTerminal
	originalList := workspaceListRPC
	workspaceSwitchIsInteractiveTerminal = func(*cobra.Command) bool { return true }
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{Workspaces: []daemon.WorkspaceSummary{
			{WorkspaceID: "sess-11111111", Name: "alpha", Status: "active"},
			{WorkspaceID: "sess-22222222", Name: "beta", Status: "suspended"},
		}}, nil
	}
	t.Cleanup(func() {
		workspaceSwitchIsInteractiveTerminal = originalTTY
		workspaceListRPC = originalList
	})

	out, err := executeRootCommandWithInput("2\n", "workspace", "switch")
	if err != nil {
		t.Fatalf("execute workspace switch: %v", err)
	}
	if !strings.Contains(out, "Select workspace") {
		t.Fatalf("workspace switch output = %q, want selection prompt", out)
	}
	if !strings.Contains(out, "sess-11111111") || !strings.Contains(out, "sess-22222222") {
		t.Fatalf("session switch output = %q, want full session ids", out)
	}

	active, err := config.ReadPersistedActiveWorkspace()
	if err != nil {
		t.Fatalf("ReadPersistedActiveWorkspace returned error: %v", err)
	}
	if active != "sess-22222222" {
		t.Fatalf("persisted active session = %q, want %q", active, "sess-22222222")
	}
}

func TestWorkspaceSwitchWithEnvOverrideMentionsPersistedValue(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ARI_ACTIVE_WORKSPACE", "sess-env")

	originalTTY := workspaceSwitchIsInteractiveTerminal
	originalList := workspaceListRPC
	workspaceSwitchIsInteractiveTerminal = func(*cobra.Command) bool { return true }
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{Workspaces: []daemon.WorkspaceSummary{{WorkspaceID: "sess-11111111", Name: "alpha", Status: "active"}}}, nil
	}
	t.Cleanup(func() {
		workspaceSwitchIsInteractiveTerminal = originalTTY
		workspaceListRPC = originalList
	})

	out, err := executeRootCommand("workspace", "switch")
	if err != nil {
		t.Fatalf("execute workspace switch: %v", err)
	}
	if !strings.Contains(out, "Persisted active workspace set: sess-11111111; ARI_ACTIVE_WORKSPACE still overrides it in this shell") {
		t.Fatalf("workspace switch output = %q, want env override warning", out)
	}
}

func TestWorkspaceSetCurrentAndClear(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalGet := workspaceGetRPC
	originalList := workspaceListRPC
	workspaceGetRPC = func(context.Context, string, string) (daemon.WorkspaceGetResponse, error) {
		return daemon.WorkspaceGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "session not found"}
	}
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{Workspaces: []daemon.WorkspaceSummary{{WorkspaceID: "sess-12345678", Name: "alpha", Status: "active"}}}, nil
	}
	t.Cleanup(func() {
		workspaceGetRPC = originalGet
		workspaceListRPC = originalList
	})

	setOut, err := executeRootCommand("workspace", "set", "alpha")
	if err != nil {
		t.Fatalf("execute workspace set: %v", err)
	}
	if !strings.Contains(setOut, "sess-12345678") {
		t.Fatalf("workspace set output = %q, want canonical workspace id", setOut)
	}

	currentOut, err := executeRootCommand("workspace", "current")
	if err != nil {
		t.Fatalf("execute workspace current: %v", err)
	}
	if !strings.Contains(currentOut, "sess-12345678") {
		t.Fatalf("workspace current output = %q, want stored workspace id", currentOut)
	}

	clearOut, err := executeRootCommand("workspace", "clear")
	if err != nil {
		t.Fatalf("execute workspace clear: %v", err)
	}
	if !strings.Contains(clearOut, "Cleared active workspace") {
		t.Fatalf("workspace clear output = %q, want clear confirmation", clearOut)
	}

	_, err = executeRootCommand("workspace", "current")
	if err == nil {
		t.Fatal("workspace current after clear returned nil error")
	}
	if err.Error() != "No active workspace is set" {
		t.Fatalf("workspace current after clear error = %q, want %q", err.Error(), "No active workspace is set")
	}
}

func TestWorkspaceUseSetsDaemonActiveContext(t *testing.T) {
	originalGet := workspaceGetRPC
	originalList := workspaceListRPC
	originalContextSet := workspaceContextSetRPC
	t.Cleanup(func() {
		workspaceGetRPC = originalGet
		workspaceListRPC = originalList
		workspaceContextSetRPC = originalContextSet
	})
	workspaceGetRPC = func(_ context.Context, _ string, workspaceID string) (daemon.WorkspaceGetResponse, error) {
		if workspaceID == "ws-123" {
			return daemon.WorkspaceGetResponse{WorkspaceID: "ws-123", Name: "alpha", Status: "active"}, nil
		}
		return daemon.WorkspaceGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "workspace not found"}
	}
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{Workspaces: []daemon.WorkspaceSummary{{WorkspaceID: "ws-123", Name: "alpha", Status: "active"}}}, nil
	}
	workspaceContextSetRPC = func(ctx context.Context, socketPath string, req daemon.ContextSetRequest) (daemon.ContextSetResponse, error) {
		_ = ctx
		_ = socketPath
		if req.WorkspaceID != "ws-123" {
			t.Fatalf("context.set workspace = %q, want ws-123", req.WorkspaceID)
		}
		return daemon.ContextSetResponse{Current: daemon.ActiveWorkspaceContext{WorkspaceID: "ws-123", Version: "v1"}}, nil
	}

	out, err := executeRootCommand("workspace", "use", "alpha")
	if err != nil {
		t.Fatalf("execute workspace use: %v", err)
	}
	if !strings.Contains(out, "Active workspace set: ws-123") {
		t.Fatalf("workspace use output = %q, want daemon context confirmation", out)
	}
}

func TestWorkspaceHelpHidesLegacyActiveContextCommands(t *testing.T) {
	out, err := executeRootCommandRaw("workspace", "--help")
	if err != nil {
		t.Fatalf("execute workspace help returned error: %v", err)
	}
	for _, hidden := range []string{"set", "current", "clear", "switch"} {
		if strings.Contains(out, "\n  "+hidden+" ") {
			t.Fatalf("workspace help = %q, want legacy active-context command %q hidden", out, hidden)
		}
	}
	if !strings.Contains(out, "\n  use ") {
		t.Fatalf("workspace help = %q, want daemon context use command visible", out)
	}
}

func TestWorkspaceClearWithEnvOverrideClearsPersistedValue(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ARI_ACTIVE_WORKSPACE", "sess-env")
	if err := config.WriteActiveWorkspace("sess-stored"); err != nil {
		t.Fatalf("WriteActiveWorkspace returned error: %v", err)
	}

	out, err := executeRootCommand("workspace", "clear")
	if err != nil {
		t.Fatalf("execute workspace clear: %v", err)
	}
	if !strings.Contains(out, "Cleared persisted active workspace") {
		t.Fatalf("workspace clear output = %q, want persisted-clear message", out)
	}

	persisted, err := config.ReadPersistedActiveWorkspace()
	if err != nil {
		t.Fatalf("ReadPersistedActiveWorkspace returned error: %v", err)
	}
	if persisted != "" {
		t.Fatalf("persisted active session after clear = %q, want empty", persisted)
	}
}

func TestWorkspaceSetWithEnvOverrideMentionsOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ARI_ACTIVE_WORKSPACE", "sess-env")

	originalGet := workspaceGetRPC
	originalList := workspaceListRPC
	workspaceGetRPC = func(context.Context, string, string) (daemon.WorkspaceGetResponse, error) {
		return daemon.WorkspaceGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "session not found"}
	}
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{Workspaces: []daemon.WorkspaceSummary{{WorkspaceID: "sess-12345678", Name: "alpha", Status: "active"}}}, nil
	}
	t.Cleanup(func() {
		workspaceGetRPC = originalGet
		workspaceListRPC = originalList
	})

	out, err := executeRootCommand("workspace", "set", "alpha")
	if err != nil {
		t.Fatalf("execute workspace set: %v", err)
	}
	if !strings.Contains(out, "ARI_ACTIVE_WORKSPACE still overrides it in this shell") {
		t.Fatalf("workspace set output = %q, want env override warning", out)
	}
}

func TestWorkspaceCreateUsesCWDDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})

	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	originalCreate := workspaceCreateRPC
	var gotReq daemon.WorkspaceCreateRequest
	workspaceCreateRPC = func(_ context.Context, _ string, req daemon.WorkspaceCreateRequest) (daemon.WorkspaceCreateResponse, error) {
		gotReq = req
		return daemon.WorkspaceCreateResponse{WorkspaceID: "sess-1", Name: req.Name, Status: "active", Folder: req.Folder, VCSType: "git", IsPrimary: true, OriginRoot: req.OriginRoot}, nil
	}
	t.Cleanup(func() {
		workspaceCreateRPC = originalCreate
	})

	if err := os.MkdirAll(filepath.Join(cwd, ".git"), 0o755); err != nil {
		t.Fatalf("create .git dir: %v", err)
	}

	out, err := executeRootCommand("workspace", "create", "alpha")
	if err != nil {
		t.Fatalf("execute workspace create: %v", err)
	}

	if gotReq.Folder != cwd {
		t.Fatalf("create folder = %q, want %q", gotReq.Folder, cwd)
	}
	if gotReq.OriginRoot != cwd {
		t.Fatalf("create origin root = %q, want %q", gotReq.OriginRoot, cwd)
	}
	if gotReq.CleanupPolicy != "manual" {
		t.Fatalf("create cleanup policy = %q, want %q", gotReq.CleanupPolicy, "manual")
	}
	if gotReq.VCSPreference != "auto" {
		t.Fatalf("create vcs preference = %q, want %q", gotReq.VCSPreference, "auto")
	}
	if !strings.Contains(out, "Workspace created: alpha") {
		t.Fatalf("workspace create output = %q, want created confirmation", out)
	}
}

func TestWorkspaceCreateUsesDetectedVCSRootForDefaultFolder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	repoRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatalf("create .git dir: %v", err)
	}
	subdir := filepath.Join(repoRoot, "nested", "work")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("create nested dir: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
	if err := os.Chdir(subdir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	originalCreate := workspaceCreateRPC
	var gotReq daemon.WorkspaceCreateRequest
	workspaceCreateRPC = func(_ context.Context, _ string, req daemon.WorkspaceCreateRequest) (daemon.WorkspaceCreateResponse, error) {
		gotReq = req
		return daemon.WorkspaceCreateResponse{WorkspaceID: "sess-1", Name: req.Name, Status: "active", Folder: req.Folder, VCSType: "git", IsPrimary: true, OriginRoot: req.OriginRoot}, nil
	}
	t.Cleanup(func() {
		workspaceCreateRPC = originalCreate
	})

	_, err = executeRootCommand("workspace", "create", "alpha")
	if err != nil {
		t.Fatalf("execute workspace create: %v", err)
	}

	if gotReq.Folder != repoRoot {
		t.Fatalf("create folder = %q, want repo root %q", gotReq.Folder, repoRoot)
	}
	if gotReq.OriginRoot != subdir {
		t.Fatalf("create origin root = %q, want %q", gotReq.OriginRoot, subdir)
	}
	if gotReq.VCSPreference != "auto" {
		t.Fatalf("create vcs preference = %q, want %q", gotReq.VCSPreference, "auto")
	}
}

func TestWorkspaceFolderCommandsNormalizeRelativePaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	originalGet := workspaceGetRPC
	originalList := workspaceListRPC
	originalAdd := workspaceAddFolderRPC
	originalRemove := workspaceRemoveFolderRPC

	workspaceGetRPC = func(context.Context, string, string) (daemon.WorkspaceGetResponse, error) {
		return daemon.WorkspaceGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "session not found"}
	}
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{Workspaces: []daemon.WorkspaceSummary{{WorkspaceID: "sess-1", Name: "alpha"}}}, nil
	}
	var addPath string
	workspaceAddFolderRPC = func(_ context.Context, _ string, req daemon.WorkspaceAddFolderRequest) (daemon.WorkspaceAddFolderResponse, error) {
		addPath = req.FolderPath
		return daemon.WorkspaceAddFolderResponse{FolderPath: req.FolderPath, VCSType: "git"}, nil
	}
	var removePath string
	workspaceRemoveFolderRPC = func(_ context.Context, _ string, req daemon.WorkspaceRemoveFolderRequest) error {
		removePath = req.FolderPath
		return nil
	}
	t.Cleanup(func() {
		workspaceGetRPC = originalGet
		workspaceListRPC = originalList
		workspaceAddFolderRPC = originalAdd
		workspaceRemoveFolderRPC = originalRemove
	})

	_, err = executeRootCommand("workspace", "folder", "add", "alpha", "relative/repo")
	if err != nil {
		t.Fatalf("execute folder add: %v", err)
	}
	if addPath != filepath.Join(cwd, "relative", "repo") {
		t.Fatalf("folder add path = %q, want %q", addPath, filepath.Join(cwd, "relative", "repo"))
	}

	_, err = executeRootCommand("workspace", "folder", "remove", "alpha", "relative/repo")
	if err != nil {
		t.Fatalf("execute folder remove: %v", err)
	}
	if removePath != filepath.Join(cwd, "relative", "repo") {
		t.Fatalf("folder remove path = %q, want %q", removePath, filepath.Join(cwd, "relative", "repo"))
	}
}

func TestWorkspaceListPrintsEntries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalList := workspaceListRPC
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{Workspaces: []daemon.WorkspaceSummary{
			{WorkspaceID: "11111111-1111-1111-1111-111111111111", Name: "alpha", Status: "active", FolderCount: 2, CreatedAt: "now"},
			{WorkspaceID: "22222222-2222-2222-2222-222222222222", Name: "beta", Status: "suspended", FolderCount: 1, CreatedAt: "later"},
		}}, nil
	}
	t.Cleanup(func() {
		workspaceListRPC = originalList
	})

	out, err := executeRootCommand("workspace", "list")
	if err != nil {
		t.Fatalf("execute workspace list: %v", err)
	}

	if !strings.Contains(out, "alpha") {
		t.Fatalf("workspace list output = %q, want alpha", out)
	}
	if !strings.Contains(out, "beta") {
		t.Fatalf("workspace list output = %q, want beta", out)
	}
}

func TestResolveSessionIdentifierByNameAndPrefix(t *testing.T) {
	originalGet := workspaceGetRPC
	originalList := workspaceListRPC

	workspaceGetRPC = func(_ context.Context, _ string, workspaceID string) (daemon.WorkspaceGetResponse, error) {
		switch workspaceID {
		case "aaaaaaaa-1111-1111-1111-111111111111":
			return daemon.WorkspaceGetResponse{WorkspaceID: workspaceID, Name: "alpha"}, nil
		case "bbbbbbbb-2222-2222-2222-222222222222":
			return daemon.WorkspaceGetResponse{WorkspaceID: workspaceID, Name: "beta"}, nil
		default:
			return daemon.WorkspaceGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "session not found"}
		}
	}
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{Workspaces: []daemon.WorkspaceSummary{
			{WorkspaceID: "aaaaaaaa-1111-1111-1111-111111111111", Name: "alpha"},
			{WorkspaceID: "bbbbbbbb-2222-2222-2222-222222222222", Name: "beta"},
		}}, nil
	}
	t.Cleanup(func() {
		workspaceGetRPC = originalGet
		workspaceListRPC = originalList
	})

	byName, err := resolveSessionIdentifier(context.Background(), "/tmp/daemon.sock", "alpha")
	if err != nil {
		t.Fatalf("resolve by name returned error: %v", err)
	}
	if byName != "aaaaaaaa-1111-1111-1111-111111111111" {
		t.Fatalf("resolve by name = %q, want %q", byName, "aaaaaaaa-1111-1111-1111-111111111111")
	}

	byPrefix, err := resolveSessionIdentifier(context.Background(), "/tmp/daemon.sock", "bbbb")
	if err != nil {
		t.Fatalf("resolve by prefix returned error: %v", err)
	}
	if byPrefix != "bbbbbbbb-2222-2222-2222-222222222222" {
		t.Fatalf("resolve by prefix = %q, want %q", byPrefix, "bbbbbbbb-2222-2222-2222-222222222222")
	}
}

func TestResolveSessionIdentifierRejectsAmbiguousPrefix(t *testing.T) {
	originalGet := workspaceGetRPC
	originalList := workspaceListRPC

	workspaceGetRPC = func(context.Context, string, string) (daemon.WorkspaceGetResponse, error) {
		return daemon.WorkspaceGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "session not found"}
	}
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{Workspaces: []daemon.WorkspaceSummary{
			{WorkspaceID: "abc11111-1111-1111-1111-111111111111", Name: "alpha"},
			{WorkspaceID: "abc22222-2222-2222-2222-222222222222", Name: "beta"},
		}}, nil
	}
	t.Cleanup(func() {
		workspaceGetRPC = originalGet
		workspaceListRPC = originalList
	})

	_, err := resolveSessionIdentifier(context.Background(), "/tmp/daemon.sock", "abc")
	if err == nil {
		t.Fatal("resolve ambiguous prefix returned nil error")
	}
	if err.Error() != "Workspace ID prefix is ambiguous" {
		t.Fatalf("resolve ambiguous prefix error = %q, want %q", err.Error(), "Workspace ID prefix is ambiguous")
	}
}

func TestResolveSessionIdentifierResolvesDuplicateNamesByCWD(t *testing.T) {
	root := t.TempDir()
	left := filepath.Join(root, "src", "clay")
	right := filepath.Join(root, "work", "clay")
	if err := os.MkdirAll(left, 0o755); err != nil {
		t.Fatalf("os.MkdirAll left returned error: %v", err)
	}
	if err := os.MkdirAll(right, 0o755); err != nil {
		t.Fatalf("os.MkdirAll right returned error: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
	if err := os.Chdir(right); err != nil {
		t.Fatalf("os.Chdir returned error: %v", err)
	}

	originalGet := workspaceGetRPC
	originalList := workspaceListRPC

	workspaceGetRPC = func(_ context.Context, _ string, workspaceID string) (daemon.WorkspaceGetResponse, error) {
		switch workspaceID {
		case "ws-left":
			return daemon.WorkspaceGetResponse{WorkspaceID: workspaceID, Name: "clay", OriginRoot: left}, nil
		case "ws-right":
			return daemon.WorkspaceGetResponse{WorkspaceID: workspaceID, Name: "clay", OriginRoot: right}, nil
		default:
			return daemon.WorkspaceGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "session not found"}
		}
	}
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{Workspaces: []daemon.WorkspaceSummary{
			{WorkspaceID: "ws-left", Name: "clay"},
			{WorkspaceID: "ws-right", Name: "clay"},
		}}, nil
	}
	t.Cleanup(func() {
		workspaceGetRPC = originalGet
		workspaceListRPC = originalList
	})

	workspaceID, err := resolveSessionIdentifier(context.Background(), "/tmp/daemon.sock", "clay")
	if err != nil {
		t.Fatalf("resolveSessionIdentifier returned error: %v", err)
	}
	if workspaceID != "ws-right" {
		t.Fatalf("workspaceID = %q, want %q", workspaceID, "ws-right")
	}
}

func TestResolveSessionIdentifierPrefersCWDWhenWorkspaceGetByNameWouldSucceed(t *testing.T) {
	root := t.TempDir()
	left := filepath.Join(root, "src", "clay")
	right := filepath.Join(root, "work", "clay")
	if err := os.MkdirAll(left, 0o755); err != nil {
		t.Fatalf("os.MkdirAll left returned error: %v", err)
	}
	if err := os.MkdirAll(right, 0o755); err != nil {
		t.Fatalf("os.MkdirAll right returned error: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
	if err := os.Chdir(right); err != nil {
		t.Fatalf("os.Chdir returned error: %v", err)
	}

	originalGet := workspaceGetRPC
	originalList := workspaceListRPC

	workspaceGetRPC = func(_ context.Context, _ string, workspaceID string) (daemon.WorkspaceGetResponse, error) {
		switch workspaceID {
		case "clay":
			return daemon.WorkspaceGetResponse{WorkspaceID: "ws-left", Name: "clay", OriginRoot: left}, nil
		case "ws-left":
			return daemon.WorkspaceGetResponse{WorkspaceID: "ws-left", Name: "clay", OriginRoot: left}, nil
		case "ws-right":
			return daemon.WorkspaceGetResponse{WorkspaceID: "ws-right", Name: "clay", OriginRoot: right}, nil
		default:
			return daemon.WorkspaceGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "session not found"}
		}
	}
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{Workspaces: []daemon.WorkspaceSummary{
			{WorkspaceID: "ws-left", Name: "clay"},
			{WorkspaceID: "ws-right", Name: "clay"},
		}}, nil
	}
	t.Cleanup(func() {
		workspaceGetRPC = originalGet
		workspaceListRPC = originalList
	})

	workspaceID, err := resolveSessionIdentifier(context.Background(), "/tmp/daemon.sock", "clay")
	if err != nil {
		t.Fatalf("resolveSessionIdentifier returned error: %v", err)
	}
	if workspaceID != "ws-right" {
		t.Fatalf("workspaceID = %q, want %q", workspaceID, "ws-right")
	}
}

func TestResolveSessionIdentifierRejectsDuplicateNamesWithoutCWDMatch(t *testing.T) {
	root := t.TempDir()
	left := filepath.Join(root, "src", "clay")
	right := filepath.Join(root, "work", "clay")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(left, 0o755); err != nil {
		t.Fatalf("os.MkdirAll left returned error: %v", err)
	}
	if err := os.MkdirAll(right, 0o755); err != nil {
		t.Fatalf("os.MkdirAll right returned error: %v", err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("os.MkdirAll outside returned error: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
	if err := os.Chdir(outside); err != nil {
		t.Fatalf("os.Chdir returned error: %v", err)
	}

	originalGet := workspaceGetRPC
	originalList := workspaceListRPC

	workspaceGetRPC = func(_ context.Context, _ string, workspaceID string) (daemon.WorkspaceGetResponse, error) {
		switch workspaceID {
		case "ws-left":
			return daemon.WorkspaceGetResponse{WorkspaceID: workspaceID, Name: "clay", OriginRoot: left}, nil
		case "ws-right":
			return daemon.WorkspaceGetResponse{WorkspaceID: workspaceID, Name: "clay", OriginRoot: right}, nil
		default:
			return daemon.WorkspaceGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "session not found"}
		}
	}
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{Workspaces: []daemon.WorkspaceSummary{
			{WorkspaceID: "ws-left", Name: "clay"},
			{WorkspaceID: "ws-right", Name: "clay"},
		}}, nil
	}
	t.Cleanup(func() {
		workspaceGetRPC = originalGet
		workspaceListRPC = originalList
	})

	_, err = resolveSessionIdentifier(context.Background(), "/tmp/daemon.sock", "clay")
	if err == nil {
		t.Fatal("resolveSessionIdentifier returned nil error")
	}
	if err.Error() != "Workspace name is ambiguous; run `ari workspace set <id-or-name>` to choose one" {
		t.Fatalf("resolveSessionIdentifier error = %q, want %q", err.Error(), "Workspace name is ambiguous; run `ari workspace set <id-or-name>` to choose one")
	}
}

func TestResolveSessionIdentifierUsesDirectNameLookupWhenWorkspaceListFails(t *testing.T) {
	originalGet := workspaceGetRPC
	originalList := workspaceListRPC

	workspaceGetRPC = func(_ context.Context, _ string, workspaceID string) (daemon.WorkspaceGetResponse, error) {
		switch workspaceID {
		case "clay":
			return daemon.WorkspaceGetResponse{WorkspaceID: "ws-stale", Name: "clay"}, nil
		default:
			return daemon.WorkspaceGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "session not found"}
		}
	}
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{}, &jsonrpc2.Error{Code: int64(rpc.InvalidParams), Message: "workspace list failed"}
	}
	t.Cleanup(func() {
		workspaceGetRPC = originalGet
		workspaceListRPC = originalList
	})

	workspaceID, err := resolveSessionIdentifier(context.Background(), "/tmp/daemon.sock", "clay")
	if err != nil {
		t.Fatalf("resolveSessionIdentifier returned error: %v", err)
	}
	if workspaceID != "ws-stale" {
		t.Fatalf("workspaceID = %q, want %q", workspaceID, "ws-stale")
	}
}

func TestResolveSessionIdentifierIgnoresStaleDuplicateWhenCWDMatchesLiveWorkspace(t *testing.T) {
	root := t.TempDir()
	right := filepath.Join(root, "work", "clay")
	if err := os.MkdirAll(right, 0o755); err != nil {
		t.Fatalf("os.MkdirAll right returned error: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
	if err := os.Chdir(right); err != nil {
		t.Fatalf("os.Chdir returned error: %v", err)
	}

	originalGet := workspaceGetRPC
	originalList := workspaceListRPC

	workspaceGetRPC = func(_ context.Context, _ string, workspaceID string) (daemon.WorkspaceGetResponse, error) {
		switch workspaceID {
		case "ws-left":
			return daemon.WorkspaceGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "session not found"}
		case "ws-right":
			return daemon.WorkspaceGetResponse{WorkspaceID: workspaceID, Name: "clay", OriginRoot: right}, nil
		default:
			return daemon.WorkspaceGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "session not found"}
		}
	}
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{Workspaces: []daemon.WorkspaceSummary{
			{WorkspaceID: "ws-left", Name: "clay"},
			{WorkspaceID: "ws-right", Name: "clay"},
		}}, nil
	}
	t.Cleanup(func() {
		workspaceGetRPC = originalGet
		workspaceListRPC = originalList
	})

	workspaceID, err := resolveSessionIdentifier(context.Background(), "/tmp/daemon.sock", "clay")
	if err != nil {
		t.Fatalf("resolveSessionIdentifier returned error: %v", err)
	}
	if workspaceID != "ws-right" {
		t.Fatalf("workspaceID = %q, want %q", workspaceID, "ws-right")
	}
}

func TestResolveSessionIdentifierReturnsLiveDuplicateWithoutCWDMatch(t *testing.T) {
	root := t.TempDir()
	right := filepath.Join(root, "work", "clay")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(right, 0o755); err != nil {
		t.Fatalf("os.MkdirAll right returned error: %v", err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("os.MkdirAll outside returned error: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
	if err := os.Chdir(outside); err != nil {
		t.Fatalf("os.Chdir returned error: %v", err)
	}

	originalGet := workspaceGetRPC
	originalList := workspaceListRPC

	workspaceGetRPC = func(_ context.Context, _ string, workspaceID string) (daemon.WorkspaceGetResponse, error) {
		switch workspaceID {
		case "ws-left":
			return daemon.WorkspaceGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "session not found"}
		case "ws-right":
			return daemon.WorkspaceGetResponse{WorkspaceID: workspaceID, Name: "clay", OriginRoot: right}, nil
		default:
			return daemon.WorkspaceGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "session not found"}
		}
	}
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{Workspaces: []daemon.WorkspaceSummary{
			{WorkspaceID: "ws-left", Name: "clay"},
			{WorkspaceID: "ws-right", Name: "clay"},
		}}, nil
	}
	t.Cleanup(func() {
		workspaceGetRPC = originalGet
		workspaceListRPC = originalList
	})

	workspaceID, err := resolveSessionIdentifier(context.Background(), "/tmp/daemon.sock", "clay")
	if err != nil {
		t.Fatalf("resolveSessionIdentifier returned error: %v", err)
	}
	if workspaceID != "ws-right" {
		t.Fatalf("workspaceID = %q, want %q", workspaceID, "ws-right")
	}
}

func TestResolveSessionIdentifierFallsBackToDirectNameLookupAfterListMismatch(t *testing.T) {
	originalGet := workspaceGetRPC
	originalList := workspaceListRPC

	workspaceGetRPC = func(_ context.Context, _ string, workspaceID string) (daemon.WorkspaceGetResponse, error) {
		switch workspaceID {
		case "clay":
			return daemon.WorkspaceGetResponse{WorkspaceID: "ws-stale", Name: "clay"}, nil
		case "ws-other":
			return daemon.WorkspaceGetResponse{WorkspaceID: "ws-other", Name: "other"}, nil
		default:
			return daemon.WorkspaceGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "session not found"}
		}
	}
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{Workspaces: []daemon.WorkspaceSummary{{WorkspaceID: "ws-other", Name: "other"}}}, nil
	}
	t.Cleanup(func() {
		workspaceGetRPC = originalGet
		workspaceListRPC = originalList
	})

	workspaceID, err := resolveSessionIdentifier(context.Background(), "/tmp/daemon.sock", "clay")
	if err != nil {
		t.Fatalf("resolveSessionIdentifier returned error: %v", err)
	}
	if workspaceID != "ws-stale" {
		t.Fatalf("workspaceID = %q, want %q", workspaceID, "ws-stale")
	}
}

func TestResolveSessionIdentifierReturnsUniqueNameMatchFromWorkspaceList(t *testing.T) {
	originalGet := workspaceGetRPC
	originalList := workspaceListRPC

	workspaceGetRPC = func(_ context.Context, _ string, workspaceID string) (daemon.WorkspaceGetResponse, error) {
		switch workspaceID {
		case "alpha", "ws-stale":
			return daemon.WorkspaceGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "session not found"}
		default:
			return daemon.WorkspaceGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "session not found"}
		}
	}
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{Workspaces: []daemon.WorkspaceSummary{{WorkspaceID: "ws-stale", Name: "alpha"}}}, nil
	}
	t.Cleanup(func() {
		workspaceGetRPC = originalGet
		workspaceListRPC = originalList
	})

	workspaceID, err := resolveSessionIdentifier(context.Background(), "/tmp/daemon.sock", "alpha")
	if err != nil {
		t.Fatalf("resolveSessionIdentifier returned error: %v", err)
	}
	if workspaceID != "ws-stale" {
		t.Fatalf("workspaceID = %q, want %q", workspaceID, "ws-stale")
	}
}

func TestResolveSessionTargetUniqueNameKeepsSessionWhenDirectLookupSucceeds(t *testing.T) {
	originalGet := workspaceGetRPC
	originalList := workspaceListRPC

	workspaceGetRPC = func(_ context.Context, _ string, workspaceID string) (daemon.WorkspaceGetResponse, error) {
		switch workspaceID {
		case "alpha":
			return daemon.WorkspaceGetResponse{WorkspaceID: "ws-1", Name: "alpha", OriginRoot: t.TempDir()}, nil
		default:
			return daemon.WorkspaceGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "session not found"}
		}
	}
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{Workspaces: []daemon.WorkspaceSummary{{WorkspaceID: "ws-1", Name: "alpha"}}}, nil
	}
	t.Cleanup(func() {
		workspaceGetRPC = originalGet
		workspaceListRPC = originalList
	})

	target, err := resolveSessionTarget(context.Background(), "/tmp/daemon.sock", "alpha")
	if err != nil {
		t.Fatalf("resolveSessionTarget returned error: %v", err)
	}
	if target.WorkspaceID != "ws-1" {
		t.Fatalf("workspaceID = %q, want %q", target.WorkspaceID, "ws-1")
	}
	if target.Session == nil {
		t.Fatal("target session = nil, want non-nil")
	}
}

func TestResolveSessionIdentifierReturnsNotFoundWhenAllDuplicateMatchesStale(t *testing.T) {
	originalGet := workspaceGetRPC
	originalList := workspaceListRPC

	workspaceGetRPC = func(_ context.Context, _ string, _ string) (daemon.WorkspaceGetResponse, error) {
		return daemon.WorkspaceGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "session not found"}
	}
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{Workspaces: []daemon.WorkspaceSummary{
			{WorkspaceID: "ws-left", Name: "clay"},
			{WorkspaceID: "ws-right", Name: "clay"},
		}}, nil
	}
	t.Cleanup(func() {
		workspaceGetRPC = originalGet
		workspaceListRPC = originalList
	})

	_, err := resolveSessionIdentifier(context.Background(), "/tmp/daemon.sock", "clay")
	if err == nil {
		t.Fatal("resolveSessionIdentifier returned nil error")
	}
	if err.Error() != "Workspace not found" {
		t.Fatalf("resolveSessionIdentifier error = %q, want %q", err.Error(), "Workspace not found")
	}
}

func TestWorkspaceCreateAllowsVCSPreferenceOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(cwd, ".git"), 0o755); err != nil {
		t.Fatalf("create .git dir: %v", err)
	}

	originalCreate := workspaceCreateRPC
	var gotReq daemon.WorkspaceCreateRequest
	workspaceCreateRPC = func(_ context.Context, _ string, req daemon.WorkspaceCreateRequest) (daemon.WorkspaceCreateResponse, error) {
		gotReq = req
		return daemon.WorkspaceCreateResponse{WorkspaceID: "sess-1", Name: req.Name, Status: "active", Folder: req.Folder, VCSType: "git", IsPrimary: true, OriginRoot: req.OriginRoot}, nil
	}
	t.Cleanup(func() {
		workspaceCreateRPC = originalCreate
	})

	_, err = executeRootCommand("workspace", "create", "alpha", "--vcs-preference", "jj")
	if err != nil {
		t.Fatalf("execute workspace create: %v", err)
	}

	if gotReq.VCSPreference != "jj" {
		t.Fatalf("create vcs preference = %q, want %q", gotReq.VCSPreference, "jj")
	}
}
