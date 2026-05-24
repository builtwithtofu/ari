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
	folderAdd, _, err := workspace.Find([]string{"folder", "add"})
	if err != nil {
		t.Fatalf("find workspace folder add: %v", err)
	}
	folderRemove, _, err := workspace.Find([]string{"folder", "remove"})
	if err != nil {
		t.Fatalf("find workspace folder remove: %v", err)
	}
	if create == nil || list == nil || show == nil || suspend == nil || resume == nil || folderAdd == nil || folderRemove == nil {
		t.Fatal("expected workspace subcommands to be registered")
	}
}

func TestWorkspaceCommandCreateListShowRemoveLifecycle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolver := workflowContextResolver
	originalCreateRPC := workspaceCommandCreateRPC
	originalListRPC := workspaceCommandListRPC
	originalGetRPC := workspaceCommandGetRPC
	originalRemoveRPC := workspaceCommandRemoveRPC
	originalEnsureScope := workspaceCommandEnsureScope
	originalWorkspaceGet := workspaceGetRPC
	originalWorkspaceList := workspaceListRPC

	workflowContextResolver = &WorkflowContextResolver{store: activeWorkspaceStoreFunc{read: func() (string, error) { return "ws-1", nil }}, resolveTarget: func(context.Context, string, string) (resolvedWorkspaceTarget, error) {
		workspace := daemon.WorkspaceGetResponse{WorkspaceID: "ws-1", Name: "alpha", OriginRoot: t.TempDir()}
		return resolvedWorkspaceTarget{WorkspaceID: workspace.WorkspaceID, Workspace: &workspace}, nil
	}}
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
		workflowContextResolver = originalResolver
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

	originalResolver := workflowContextResolver
	originalEnsureScope := workspaceCommandEnsureScope
	originalGetDef := workspaceCommandGetRPC
	originalResolveTarget := commandResolveWorkspaceTarget
	originalEnsure := commandEnsureDaemonRunning
	originalRun := commandRunRPC
	originalGet := commandGetRPC
	originalOutput := commandOutputRPC

	workflowContextResolver = &WorkflowContextResolver{store: activeWorkspaceStoreFunc{read: func() (string, error) { return "ws-1", nil }}, resolveTarget: func(context.Context, string, string) (resolvedWorkspaceTarget, error) {
		workspace := daemon.WorkspaceGetResponse{WorkspaceID: "ws-1", OriginRoot: t.TempDir()}
		return resolvedWorkspaceTarget{WorkspaceID: workspace.WorkspaceID, Workspace: &workspace}, nil
	}}
	workspaceCommandEnsureScope = func(*daemon.WorkspaceGetResponse, string) error { return nil }
	workspaceCommandGetRPC = func(context.Context, string, daemon.WorkspaceCommandGetRequest) (daemon.WorkspaceCommandGetResponse, error) {
		return daemon.WorkspaceCommandGetResponse{CommandID: "cmd-def-1", Name: "test", Command: "go", Args: []string{"test", "./..."}}, nil
	}
	commandResolveWorkspaceTarget = func(context.Context, string, string) (resolvedWorkspaceTarget, error) {
		return resolvedWorkspaceTarget{WorkspaceID: "ws-1", Workspace: &daemon.WorkspaceGetResponse{WorkspaceID: "ws-1", OriginRoot: t.TempDir()}}, nil
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
		workflowContextResolver = originalResolver
		workspaceCommandEnsureScope = originalEnsureScope
		workspaceCommandGetRPC = originalGetDef
		commandResolveWorkspaceTarget = originalResolveTarget
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

	originalResolver := workflowContextResolver
	originalListRPC := workspaceCommandListRPC
	workflowContextResolver = &WorkflowContextResolver{store: activeWorkspaceStoreFunc{read: func() (string, error) { return "ws-1", nil }}, resolveTarget: resolveWorkspaceTarget}
	called := false
	workspaceCommandListRPC = func(context.Context, string, daemon.WorkspaceCommandListRequest) (daemon.WorkspaceCommandListResponse, error) {
		called = true
		return daemon.WorkspaceCommandListResponse{}, nil
	}
	t.Cleanup(func() {
		workflowContextResolver = originalResolver
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

func TestWorkspaceUseSetsDaemonActiveContext(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	originalGet := workspaceGetRPC
	originalList := workspaceListRPC
	originalResolve := workspaceResolveRPC
	originalContextSet := workspaceContextSetRPC
	t.Cleanup(func() {
		workspaceGetRPC = originalGet
		workspaceListRPC = originalList
		workspaceResolveRPC = originalResolve
		workspaceContextSetRPC = originalContextSet
	})
	workspaceResolveRPC = func(context.Context, string, daemon.WorkspaceResolveRequest) (daemon.WorkspaceResolveResponse, error) {
		return daemon.WorkspaceResolveResponse{Workspace: daemon.WorkspaceGetResponse{WorkspaceID: "ws-123", Name: "alpha", Status: "active"}}, nil
	}
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

func TestWorkspaceCreateDefaultsToEmptyWorkspace(t *testing.T) {
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
		return daemon.WorkspaceCreateResponse{WorkspaceID: "sess-1", Name: req.Name, Status: "active", OriginRoot: req.OriginRoot}, nil
	}
	t.Cleanup(func() {
		workspaceCreateRPC = originalCreate
	})

	out, err := executeRootCommand("workspace", "create", "alpha")
	if err != nil {
		t.Fatalf("execute workspace create: %v", err)
	}

	if gotReq.OriginRoot != "" {
		t.Fatalf("create origin root = %q, want empty", gotReq.OriginRoot)
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

func TestWorkspaceCreateUsesExplicitFolder(t *testing.T) {
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
	originalAdd := workspaceAddFolderRPC
	var gotReq daemon.WorkspaceCreateRequest
	var gotAdd daemon.WorkspaceAddFolderRequest
	workspaceCreateRPC = func(_ context.Context, _ string, req daemon.WorkspaceCreateRequest) (daemon.WorkspaceCreateResponse, error) {
		gotReq = req
		return daemon.WorkspaceCreateResponse{WorkspaceID: "sess-1", Name: req.Name, Status: "active", OriginRoot: req.OriginRoot}, nil
	}
	workspaceAddFolderRPC = func(_ context.Context, _ string, req daemon.WorkspaceAddFolderRequest) (daemon.WorkspaceAddFolderResponse, error) {
		gotAdd = req
		return daemon.WorkspaceAddFolderResponse{FolderPath: req.FolderPath, VCSType: "git"}, nil
	}
	t.Cleanup(func() {
		workspaceCreateRPC = originalCreate
		workspaceAddFolderRPC = originalAdd
	})

	_, err = executeRootCommand("workspace", "create", "alpha", "--folder", repoRoot)
	if err != nil {
		t.Fatalf("execute workspace create: %v", err)
	}

	if gotReq.OriginRoot != repoRoot {
		t.Fatalf("create origin root = %q, want repo root %q", gotReq.OriginRoot, repoRoot)
	}
	if gotAdd.WorkspaceID != "sess-1" || gotAdd.FolderPath != repoRoot {
		t.Fatalf("add folder request = %#v, want workspace sess-1 repo root %q", gotAdd, repoRoot)
	}
	if gotReq.VCSPreference != "auto" {
		t.Fatalf("create vcs preference = %q, want %q", gotReq.VCSPreference, "auto")
	}
}

func TestWorkspaceCreateWithFolderReportsPartialAttachFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWD) })
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	originalCreate := workspaceCreateRPC
	originalAdd := workspaceAddFolderRPC
	workspaceCreateRPC = func(_ context.Context, _ string, req daemon.WorkspaceCreateRequest) (daemon.WorkspaceCreateResponse, error) {
		return daemon.WorkspaceCreateResponse{WorkspaceID: "sess-1", Name: req.Name, Status: "active"}, nil
	}
	workspaceAddFolderRPC = func(context.Context, string, daemon.WorkspaceAddFolderRequest) (daemon.WorkspaceAddFolderResponse, error) {
		return daemon.WorkspaceAddFolderResponse{}, &jsonrpc2.Error{Code: int64(rpc.InvalidParams), Message: "folder is not a VCS root"}
	}
	t.Cleanup(func() {
		workspaceCreateRPC = originalCreate
		workspaceAddFolderRPC = originalAdd
	})

	_, err = executeRootCommand("workspace", "create", "alpha", "--folder", ".")
	if err == nil {
		t.Fatal("workspace create returned nil error for folder attach failure")
	}
	if !strings.Contains(err.Error(), "Workspace created: alpha (sess-1), but adding folder failed") || !strings.Contains(err.Error(), "folder is not a VCS root") {
		t.Fatalf("workspace create error = %q, want partial attach failure", err.Error())
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
	originalResolve := workspaceResolveRPC
	originalAdd := workspaceAddFolderRPC
	originalRemove := workspaceRemoveFolderRPC

	workspaceResolveRPC = func(context.Context, string, daemon.WorkspaceResolveRequest) (daemon.WorkspaceResolveResponse, error) {
		return daemon.WorkspaceResolveResponse{Workspace: daemon.WorkspaceGetResponse{WorkspaceID: "sess-1", Name: "alpha"}}, nil
	}

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
		workspaceResolveRPC = originalResolve
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

func TestResolveWorkspaceTargetUsesDaemonResolver(t *testing.T) {
	originalResolve := workspaceResolveRPC
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd returned error: %v", err)
	}
	var gotReq daemon.WorkspaceResolveRequest
	workspaceResolveRPC = func(_ context.Context, socketPath string, req daemon.WorkspaceResolveRequest) (daemon.WorkspaceResolveResponse, error) {
		if socketPath != "/tmp/daemon.sock" {
			t.Fatalf("socket path = %q, want /tmp/daemon.sock", socketPath)
		}
		gotReq = req
		return daemon.WorkspaceResolveResponse{Workspace: daemon.WorkspaceGetResponse{WorkspaceID: "ws-1", Name: "alpha"}}, nil
	}
	t.Cleanup(func() { workspaceResolveRPC = originalResolve })

	target, err := resolveWorkspaceTarget(context.Background(), "/tmp/daemon.sock", "alpha")
	if err != nil {
		t.Fatalf("resolveWorkspaceTarget returned error: %v", err)
	}
	if target.WorkspaceID != "ws-1" || target.Workspace == nil || target.Workspace.Name != "alpha" {
		t.Fatalf("target = %#v, want resolved workspace", target)
	}
	if gotReq.Identifier != "alpha" || gotReq.CWD != originalWD {
		t.Fatalf("resolver request = %#v, want identifier alpha and cwd %q", gotReq, originalWD)
	}
}

func TestResolveWorkspaceTargetMapsDaemonResolverErrors(t *testing.T) {
	originalResolve := workspaceResolveRPC
	workspaceResolveRPC = func(context.Context, string, daemon.WorkspaceResolveRequest) (daemon.WorkspaceResolveResponse, error) {
		return daemon.WorkspaceResolveResponse{}, &jsonrpc2.Error{Code: int64(rpc.InvalidParams), Message: "Workspace ID prefix is ambiguous"}
	}
	t.Cleanup(func() { workspaceResolveRPC = originalResolve })

	_, err := resolveWorkspaceIdentifier(context.Background(), "/tmp/daemon.sock", "abc")
	if err == nil {
		t.Fatal("resolveWorkspaceIdentifier returned nil error")
	}
	if err.Error() != "Workspace ID prefix is ambiguous" {
		t.Fatalf("resolveWorkspaceIdentifier error = %q, want %q", err.Error(), "Workspace ID prefix is ambiguous")
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
		return daemon.WorkspaceCreateResponse{WorkspaceID: "sess-1", Name: req.Name, Status: "active", OriginRoot: req.OriginRoot}, nil
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

func TestWorkspaceSetupExistingCallsDaemonFlowAndPrintsActiveWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWD) })
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	project := filepath.Join(cwd, "project")
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatalf("create project git dir: %v", err)
	}

	originalSetup := workspaceSetupExistingRPC
	var gotReq daemon.WorkspaceSetupExistingRequest
	workspaceSetupExistingRPC = func(_ context.Context, _ string, req daemon.WorkspaceSetupExistingRequest) (daemon.WorkspaceSetupExistingResponse, error) {
		gotReq = req
		return daemon.WorkspaceSetupExistingResponse{WorkspaceID: "ws-project", Name: req.Name, Folder: req.Folder, VCSType: "git", ActiveWorkspace: "ws-project", RollbackPointID: "op-checkpoint"}, nil
	}
	t.Cleanup(func() { workspaceSetupExistingRPC = originalSetup })

	out, err := executeRootCommand("workspace", "setup", "project", "./project", "--vcs-preference", "git")
	if err != nil {
		t.Fatalf("workspace setup returned error: %v", err)
	}
	if gotReq.Name != "project" || gotReq.Folder != project || gotReq.VCSPreference != "git" {
		t.Fatalf("setup request = %#v, want project/%s/git", gotReq, project)
	}
	if !strings.Contains(out, "Project workspace ready: project (ws-project)") || !strings.Contains(out, "Active workspace: ws-project") || !strings.Contains(out, "Inspect: ari workspace show") {
		t.Fatalf("workspace setup output = %q", out)
	}
}
