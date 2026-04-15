package cmd

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	closeCmd, _, err := workspace.Find([]string{"close"})
	if err != nil {
		t.Fatalf("find workspace close: %v", err)
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
	attach, _, err := workspace.Find([]string{"attach"})
	if err != nil {
		t.Fatalf("find workspace attach: %v", err)
	}
	workspaceCommand, _, err := workspace.Find([]string{"command"})
	if err != nil {
		t.Fatalf("find workspace command: %v", err)
	}
	workspaceCommandCreate, _, err := workspace.Find([]string{"command", "create"})
	if err != nil {
		t.Fatalf("find workspace command create: %v", err)
	}
	workspaceCommandList, _, err := workspace.Find([]string{"command", "list"})
	if err != nil {
		t.Fatalf("find workspace command list: %v", err)
	}
	workspaceCommandShow, _, err := workspace.Find([]string{"command", "show"})
	if err != nil {
		t.Fatalf("find workspace command show: %v", err)
	}
	workspaceCommandRemove, _, err := workspace.Find([]string{"command", "remove"})
	if err != nil {
		t.Fatalf("find workspace command remove: %v", err)
	}

	if create == nil || list == nil || show == nil || closeCmd == nil || suspend == nil || resume == nil || set == nil || current == nil || clear == nil || switchCmd == nil || folderAdd == nil || folderRemove == nil || attach == nil || workspaceCommand == nil || workspaceCommandCreate == nil || workspaceCommandList == nil || workspaceCommandShow == nil || workspaceCommandRemove == nil {
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

	createOut, err := executeRootCommand("workspace", "command", "create", "test", "go", "test", "./...")
	if err != nil {
		t.Fatalf("workspace command create returned error: %v", err)
	}
	if !strings.Contains(createOut, "Workspace command created: cmd-1 (test)") {
		t.Fatalf("workspace command create output = %q, want creation line", createOut)
	}

	listOut, err := executeRootCommand("workspace", "command", "list")
	if err != nil {
		t.Fatalf("workspace command list returned error: %v", err)
	}
	if !strings.Contains(listOut, "cmd-1") || !strings.Contains(listOut, "test") {
		t.Fatalf("workspace command list output = %q, want command row", listOut)
	}
	if !strings.Contains(listOut, "go test ./...") {
		t.Fatalf("workspace command list output = %q, want full command line", listOut)
	}

	showOut, err := executeRootCommand("workspace", "command", "show", "test")
	if err != nil {
		t.Fatalf("workspace command show returned error: %v", err)
	}
	if !strings.Contains(showOut, "Command: go") {
		t.Fatalf("workspace command show output = %q, want command details", showOut)
	}

	removeOut, err := executeRootCommand("workspace", "command", "remove", "test")
	if err != nil {
		t.Fatalf("workspace command remove returned error: %v", err)
	}
	if !strings.Contains(removeOut, "Workspace command remove: removed") {
		t.Fatalf("workspace command remove output = %q, want remove status", removeOut)
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

	_, err := executeRootCommand("workspace", "command", "list", "extra")
	if err == nil {
		t.Fatal("workspace command list returned nil error for unexpected args")
	}
	if called {
		t.Fatal("workspace command list RPC called unexpectedly")
	}
}

func TestWorkspaceAttachWithoutAgentUsesRunningDefaultHarnessAgent(t *testing.T) {
	root := t.TempDir()
	workspaceRoot := filepath.Join(root, "work", "clay")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
	if err := os.Chdir(workspaceRoot); err != nil {
		t.Fatalf("os.Chdir returned error: %v", err)
	}
	t.Setenv("ARI_DEFAULT_HARNESS", "opencode")

	originalEnsure := workspaceEnsureDaemonRunning
	originalList := workspaceListRPC
	originalGet := workspaceGetRPC
	originalAgentList := agentListRPC
	originalAgentGet := agentGetRPC
	originalSpawn := agentSpawnRPC
	originalPrepare := agentAttachPrepareTerminalFn
	originalSize := agentAttachTerminalSize
	originalAttachRPC := agentAttachRPC
	originalRunSession := agentAttachRunSession

	workspaceEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{Workspaces: []daemon.WorkspaceSummary{{WorkspaceID: "ws-1", Name: "clay"}}}, nil
	}
	workspaceGetRPC = func(_ context.Context, _ string, workspaceID string) (daemon.WorkspaceGetResponse, error) {
		switch workspaceID {
		case "ws-1":
			return daemon.WorkspaceGetResponse{WorkspaceID: "ws-1", Name: "clay", OriginRoot: workspaceRoot}, nil
		default:
			return daemon.WorkspaceGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "workspace not found"}
		}
	}
	agentListRPC = func(context.Context, string, string) (daemon.AgentListResponse, error) {
		return daemon.AgentListResponse{Agents: []daemon.AgentSummary{{AgentID: "agent-1", Status: "running"}}}, nil
	}
	agentGetRPC = func(context.Context, string, string, string) (daemon.AgentGetResponse, error) {
		return daemon.AgentGetResponse{AgentID: "agent-1", WorkspaceID: "ws-1", Status: "running", Harness: "opencode"}, nil
	}
	agentSpawnRPC = func(context.Context, string, daemon.AgentSpawnRequest) (daemon.AgentSpawnResponse, error) {
		return daemon.AgentSpawnResponse{}, errors.New("agent spawn should not be called when running default exists")
	}
	agentAttachPrepareTerminalFn = func(*cobra.Command, context.Context) (func(), error) {
		return func() {}, nil
	}
	agentAttachTerminalSize = func(*cobra.Command) (uint16, uint16) { return 80, 24 }
	gotReq := daemon.AgentAttachRequest{}
	agentAttachRPC = func(_ context.Context, _ string, req daemon.AgentAttachRequest) (daemon.AgentAttachResponse, error) {
		gotReq = req
		return daemon.AgentAttachResponse{Token: "tok-1", Status: "pending"}, nil
	}
	agentAttachRunSession = func(context.Context, io.Reader, io.Writer, string, string, uint16, uint16, <-chan os.Signal, func() (uint16, uint16)) (attachSessionOutcome, error) {
		return attachSessionOutcome{Detached: true}, nil
	}
	t.Cleanup(func() {
		workspaceEnsureDaemonRunning = originalEnsure
		workspaceListRPC = originalList
		workspaceGetRPC = originalGet
		agentListRPC = originalAgentList
		agentGetRPC = originalAgentGet
		agentSpawnRPC = originalSpawn
		agentAttachPrepareTerminalFn = originalPrepare
		agentAttachTerminalSize = originalSize
		agentAttachRPC = originalAttachRPC
		agentAttachRunSession = originalRunSession
	})

	out, err := executeRootCommand("workspace", "attach")
	if err != nil {
		t.Fatalf("execute workspace attach: %v", err)
	}
	if gotReq.AgentID != "agent-1" {
		t.Fatalf("workspace attach agent_id = %q, want %q", gotReq.AgentID, "agent-1")
	}
	if !strings.Contains(out, "Attaching to agent \"agent-1\" in workspace ws-1") {
		t.Fatalf("workspace attach output = %q, want attach status line", out)
	}
}

func TestResolveWorkspaceAttachTargetWithoutWorkspaceFlagUsesCWDResolution(t *testing.T) {
	originalResolve := workspaceAttachResolveWorkspaceFromCWD
	workspaceAttachResolveWorkspaceFromCWD = func(context.Context, string, string) (daemon.WorkspaceGetResponse, error) {
		return daemon.WorkspaceGetResponse{WorkspaceID: "ws-1", Name: "alpha", OriginRoot: "/tmp/work/alpha"}, nil
	}
	t.Cleanup(func() {
		workspaceAttachResolveWorkspaceFromCWD = originalResolve
	})

	cmd := &cobra.Command{}
	target, err := resolveWorkspaceAttachTarget(context.Background(), &config.Config{Daemon: config.DaemonConfig{SocketPath: "/tmp/daemon.sock"}}, cmd)
	if err != nil {
		t.Fatalf("resolveWorkspaceAttachTarget returned error: %v", err)
	}
	if target.WorkspaceID != "ws-1" {
		t.Fatalf("target workspace_id = %q, want %q", target.WorkspaceID, "ws-1")
	}
}

func TestWorkspaceAttachWithoutAgentSpawnsDefaultHarnessAgentWhenMissing(t *testing.T) {
	root := t.TempDir()
	workspaceRoot := filepath.Join(root, "work", "clay")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
	if err := os.Chdir(workspaceRoot); err != nil {
		t.Fatalf("os.Chdir returned error: %v", err)
	}
	t.Setenv("ARI_DEFAULT_HARNESS", "opencode")

	originalEnsure := workspaceEnsureDaemonRunning
	originalList := workspaceListRPC
	originalGet := workspaceGetRPC
	originalAgentList := agentListRPC
	originalSpawn := agentSpawnRPC
	originalPrepare := agentAttachPrepareTerminalFn
	originalSize := agentAttachTerminalSize
	originalAttachRPC := agentAttachRPC
	originalRunSession := agentAttachRunSession

	workspaceEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{Workspaces: []daemon.WorkspaceSummary{{WorkspaceID: "ws-1", Name: "clay"}}}, nil
	}
	workspaceGetRPC = func(_ context.Context, _ string, workspaceID string) (daemon.WorkspaceGetResponse, error) {
		switch workspaceID {
		case "ws-1":
			return daemon.WorkspaceGetResponse{WorkspaceID: "ws-1", Name: "clay", OriginRoot: workspaceRoot}, nil
		default:
			return daemon.WorkspaceGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "workspace not found"}
		}
	}
	agentListRPC = func(context.Context, string, string) (daemon.AgentListResponse, error) {
		return daemon.AgentListResponse{Agents: []daemon.AgentSummary{}}, nil
	}
	spawnReq := daemon.AgentSpawnRequest{}
	agentSpawnRPC = func(_ context.Context, _ string, req daemon.AgentSpawnRequest) (daemon.AgentSpawnResponse, error) {
		spawnReq = req
		return daemon.AgentSpawnResponse{AgentID: "agent-spawned", Status: "running"}, nil
	}
	agentAttachPrepareTerminalFn = func(*cobra.Command, context.Context) (func(), error) {
		return func() {}, nil
	}
	agentAttachTerminalSize = func(*cobra.Command) (uint16, uint16) { return 80, 24 }
	gotReq := daemon.AgentAttachRequest{}
	agentAttachRPC = func(_ context.Context, _ string, req daemon.AgentAttachRequest) (daemon.AgentAttachResponse, error) {
		gotReq = req
		return daemon.AgentAttachResponse{Token: "tok-1", Status: "pending"}, nil
	}
	agentAttachRunSession = func(context.Context, io.Reader, io.Writer, string, string, uint16, uint16, <-chan os.Signal, func() (uint16, uint16)) (attachSessionOutcome, error) {
		return attachSessionOutcome{Detached: true}, nil
	}
	t.Cleanup(func() {
		workspaceEnsureDaemonRunning = originalEnsure
		workspaceListRPC = originalList
		workspaceGetRPC = originalGet
		agentListRPC = originalAgentList
		agentSpawnRPC = originalSpawn
		agentAttachPrepareTerminalFn = originalPrepare
		agentAttachTerminalSize = originalSize
		agentAttachRPC = originalAttachRPC
		agentAttachRunSession = originalRunSession
	})

	out, err := executeRootCommand("workspace", "attach")
	if err != nil {
		t.Fatalf("execute workspace attach: %v", err)
	}
	if spawnReq.WorkspaceID != "ws-1" {
		t.Fatalf("agent spawn workspace_id = %q, want %q", spawnReq.WorkspaceID, "ws-1")
	}
	if spawnReq.Harness != "opencode" {
		t.Fatalf("agent spawn harness = %q, want %q", spawnReq.Harness, "opencode")
	}
	if gotReq.AgentID != "agent-spawned" {
		t.Fatalf("workspace attach agent_id = %q, want %q", gotReq.AgentID, "agent-spawned")
	}
	if !strings.Contains(out, "Attaching to agent \"agent-spawned\" in workspace ws-1") {
		t.Fatalf("workspace attach output = %q, want attach status line", out)
	}
}

func TestWorkspaceAttachWithoutAgentSkipsStaleAgentEntry(t *testing.T) {
	root := t.TempDir()
	workspaceRoot := filepath.Join(root, "work", "clay")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
	if err := os.Chdir(workspaceRoot); err != nil {
		t.Fatalf("os.Chdir returned error: %v", err)
	}
	t.Setenv("ARI_DEFAULT_HARNESS", "opencode")

	originalEnsure := workspaceEnsureDaemonRunning
	originalList := workspaceListRPC
	originalGet := workspaceGetRPC
	originalAgentList := agentListRPC
	originalAgentGet := agentGetRPC
	originalPrepare := agentAttachPrepareTerminalFn
	originalSize := agentAttachTerminalSize
	originalAttachRPC := agentAttachRPC
	originalRunSession := agentAttachRunSession

	workspaceEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{Workspaces: []daemon.WorkspaceSummary{{WorkspaceID: "ws-1", Name: "clay"}}}, nil
	}
	workspaceGetRPC = func(_ context.Context, _ string, workspaceID string) (daemon.WorkspaceGetResponse, error) {
		if workspaceID != "ws-1" {
			return daemon.WorkspaceGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "workspace not found"}
		}
		return daemon.WorkspaceGetResponse{WorkspaceID: "ws-1", Name: "clay", OriginRoot: workspaceRoot}, nil
	}
	agentListRPC = func(context.Context, string, string) (daemon.AgentListResponse, error) {
		return daemon.AgentListResponse{Agents: []daemon.AgentSummary{{AgentID: "agent-stale", Status: "running"}, {AgentID: "agent-live", Status: "running"}}}, nil
	}
	agentGetRPC = func(_ context.Context, _ string, _ string, agentID string) (daemon.AgentGetResponse, error) {
		if agentID == "agent-stale" {
			return daemon.AgentGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.AgentNotFound), Message: "agent not found"}
		}
		return daemon.AgentGetResponse{AgentID: "agent-live", WorkspaceID: "ws-1", Status: "running", Harness: "opencode"}, nil
	}
	agentAttachPrepareTerminalFn = func(*cobra.Command, context.Context) (func(), error) {
		return func() {}, nil
	}
	agentAttachTerminalSize = func(*cobra.Command) (uint16, uint16) { return 80, 24 }
	gotReq := daemon.AgentAttachRequest{}
	agentAttachRPC = func(_ context.Context, _ string, req daemon.AgentAttachRequest) (daemon.AgentAttachResponse, error) {
		gotReq = req
		return daemon.AgentAttachResponse{Token: "tok-1", Status: "pending"}, nil
	}
	agentAttachRunSession = func(context.Context, io.Reader, io.Writer, string, string, uint16, uint16, <-chan os.Signal, func() (uint16, uint16)) (attachSessionOutcome, error) {
		return attachSessionOutcome{Detached: true}, nil
	}
	t.Cleanup(func() {
		workspaceEnsureDaemonRunning = originalEnsure
		workspaceListRPC = originalList
		workspaceGetRPC = originalGet
		agentListRPC = originalAgentList
		agentGetRPC = originalAgentGet
		agentAttachPrepareTerminalFn = originalPrepare
		agentAttachTerminalSize = originalSize
		agentAttachRPC = originalAttachRPC
		agentAttachRunSession = originalRunSession
	})

	_, err = executeRootCommand("workspace", "attach")
	if err != nil {
		t.Fatalf("execute workspace attach: %v", err)
	}
	if gotReq.AgentID != "agent-live" {
		t.Fatalf("workspace attach agent_id = %q, want %q", gotReq.AgentID, "agent-live")
	}
}

func TestWorkspaceAttachWithoutAgentPrefersSoleRunningAgentOverSpawn(t *testing.T) {
	root := t.TempDir()
	workspaceRoot := filepath.Join(root, "work", "clay")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
	if err := os.Chdir(workspaceRoot); err != nil {
		t.Fatalf("os.Chdir returned error: %v", err)
	}
	t.Setenv("ARI_DEFAULT_HARNESS", "opencode")

	originalEnsure := workspaceEnsureDaemonRunning
	originalList := workspaceListRPC
	originalGet := workspaceGetRPC
	originalAgentList := agentListRPC
	originalAgentGet := agentGetRPC
	originalSpawn := agentSpawnRPC
	originalPrepare := agentAttachPrepareTerminalFn
	originalSize := agentAttachTerminalSize
	originalAttachRPC := agentAttachRPC
	originalRunSession := agentAttachRunSession

	workspaceEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{Workspaces: []daemon.WorkspaceSummary{{WorkspaceID: "ws-1", Name: "clay"}}}, nil
	}
	workspaceGetRPC = func(_ context.Context, _ string, workspaceID string) (daemon.WorkspaceGetResponse, error) {
		if workspaceID != "ws-1" {
			return daemon.WorkspaceGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "workspace not found"}
		}
		return daemon.WorkspaceGetResponse{WorkspaceID: "ws-1", Name: "clay", OriginRoot: workspaceRoot}, nil
	}
	agentListRPC = func(context.Context, string, string) (daemon.AgentListResponse, error) {
		return daemon.AgentListResponse{Agents: []daemon.AgentSummary{{AgentID: "agent-sole", Status: "running"}}}, nil
	}
	agentGetRPC = func(context.Context, string, string, string) (daemon.AgentGetResponse, error) {
		return daemon.AgentGetResponse{AgentID: "agent-sole", WorkspaceID: "ws-1", Status: "running", Harness: "claude"}, nil
	}
	agentSpawnRPC = func(context.Context, string, daemon.AgentSpawnRequest) (daemon.AgentSpawnResponse, error) {
		return daemon.AgentSpawnResponse{}, errors.New("agent spawn should not be called when exactly one running agent exists")
	}
	agentAttachPrepareTerminalFn = func(*cobra.Command, context.Context) (func(), error) {
		return func() {}, nil
	}
	agentAttachTerminalSize = func(*cobra.Command) (uint16, uint16) { return 80, 24 }
	gotReq := daemon.AgentAttachRequest{}
	agentAttachRPC = func(_ context.Context, _ string, req daemon.AgentAttachRequest) (daemon.AgentAttachResponse, error) {
		gotReq = req
		return daemon.AgentAttachResponse{Token: "tok-1", Status: "pending"}, nil
	}
	agentAttachRunSession = func(context.Context, io.Reader, io.Writer, string, string, uint16, uint16, <-chan os.Signal, func() (uint16, uint16)) (attachSessionOutcome, error) {
		return attachSessionOutcome{Detached: true}, nil
	}
	t.Cleanup(func() {
		workspaceEnsureDaemonRunning = originalEnsure
		workspaceListRPC = originalList
		workspaceGetRPC = originalGet
		agentListRPC = originalAgentList
		agentGetRPC = originalAgentGet
		agentSpawnRPC = originalSpawn
		agentAttachPrepareTerminalFn = originalPrepare
		agentAttachTerminalSize = originalSize
		agentAttachRPC = originalAttachRPC
		agentAttachRunSession = originalRunSession
	})

	_, err = executeRootCommand("workspace", "attach")
	if err != nil {
		t.Fatalf("execute workspace attach: %v", err)
	}
	if gotReq.AgentID != "agent-sole" {
		t.Fatalf("workspace attach agent_id = %q, want %q", gotReq.AgentID, "agent-sole")
	}
}

func TestWorkspaceAttachWithoutAgentFailsWhenAgentLookupFails(t *testing.T) {
	root := t.TempDir()
	workspaceRoot := filepath.Join(root, "work", "clay")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
	if err := os.Chdir(workspaceRoot); err != nil {
		t.Fatalf("os.Chdir returned error: %v", err)
	}
	t.Setenv("ARI_DEFAULT_HARNESS", "opencode")

	originalEnsure := workspaceEnsureDaemonRunning
	originalList := workspaceListRPC
	originalGet := workspaceGetRPC
	originalAgentList := agentListRPC
	originalAgentGet := agentGetRPC
	originalSpawn := agentSpawnRPC

	workspaceEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{Workspaces: []daemon.WorkspaceSummary{{WorkspaceID: "ws-1", Name: "clay"}}}, nil
	}
	workspaceGetRPC = func(_ context.Context, _ string, workspaceID string) (daemon.WorkspaceGetResponse, error) {
		if workspaceID != "ws-1" {
			return daemon.WorkspaceGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "workspace not found"}
		}
		return daemon.WorkspaceGetResponse{WorkspaceID: "ws-1", Name: "clay", OriginRoot: workspaceRoot}, nil
	}
	agentListRPC = func(context.Context, string, string) (daemon.AgentListResponse, error) {
		return daemon.AgentListResponse{Agents: []daemon.AgentSummary{{AgentID: "agent-1", Status: "running"}}}, nil
	}
	agentGetRPC = func(context.Context, string, string, string) (daemon.AgentGetResponse, error) {
		return daemon.AgentGetResponse{}, errors.New("lookup failed")
	}
	agentSpawnRPC = func(context.Context, string, daemon.AgentSpawnRequest) (daemon.AgentSpawnResponse, error) {
		return daemon.AgentSpawnResponse{}, errors.New("agent spawn should not be called when lookup fails")
	}
	t.Cleanup(func() {
		workspaceEnsureDaemonRunning = originalEnsure
		workspaceListRPC = originalList
		workspaceGetRPC = originalGet
		agentListRPC = originalAgentList
		agentGetRPC = originalAgentGet
		agentSpawnRPC = originalSpawn
	})

	_, err = executeRootCommand("workspace", "attach")
	if err == nil {
		t.Fatal("execute workspace attach returned nil error")
	}
	if err.Error() != "lookup failed" {
		t.Fatalf("workspace attach error = %q, want %q", err.Error(), "lookup failed")
	}
}

func TestWorkspaceAttachWithoutAgentRejectsMultipleDefaultHarnessMatches(t *testing.T) {
	root := t.TempDir()
	workspaceRoot := filepath.Join(root, "work", "clay")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
	if err := os.Chdir(workspaceRoot); err != nil {
		t.Fatalf("os.Chdir returned error: %v", err)
	}
	t.Setenv("ARI_DEFAULT_HARNESS", "opencode")

	originalEnsure := workspaceEnsureDaemonRunning
	originalList := workspaceListRPC
	originalGet := workspaceGetRPC
	originalAgentList := agentListRPC
	originalAgentGet := agentGetRPC
	originalSpawn := agentSpawnRPC

	workspaceEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{Workspaces: []daemon.WorkspaceSummary{{WorkspaceID: "ws-1", Name: "clay"}}}, nil
	}
	workspaceGetRPC = func(_ context.Context, _ string, workspaceID string) (daemon.WorkspaceGetResponse, error) {
		if workspaceID != "ws-1" {
			return daemon.WorkspaceGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "workspace not found"}
		}
		return daemon.WorkspaceGetResponse{WorkspaceID: "ws-1", Name: "clay", OriginRoot: workspaceRoot}, nil
	}
	agentListRPC = func(context.Context, string, string) (daemon.AgentListResponse, error) {
		return daemon.AgentListResponse{Agents: []daemon.AgentSummary{{AgentID: "agent-a", Status: "running"}, {AgentID: "agent-b", Status: "running"}}}, nil
	}
	agentGetRPC = func(_ context.Context, _ string, _ string, agentID string) (daemon.AgentGetResponse, error) {
		return daemon.AgentGetResponse{AgentID: agentID, WorkspaceID: "ws-1", Status: "running", Harness: "opencode"}, nil
	}
	agentSpawnRPC = func(context.Context, string, daemon.AgentSpawnRequest) (daemon.AgentSpawnResponse, error) {
		return daemon.AgentSpawnResponse{}, errors.New("agent spawn should not be called when default harness match is ambiguous")
	}
	t.Cleanup(func() {
		workspaceEnsureDaemonRunning = originalEnsure
		workspaceListRPC = originalList
		workspaceGetRPC = originalGet
		agentListRPC = originalAgentList
		agentGetRPC = originalAgentGet
		agentSpawnRPC = originalSpawn
	})

	_, err = executeRootCommand("workspace", "attach")
	if err == nil {
		t.Fatal("execute workspace attach returned nil error")
	}
	if err.Error() != "Multiple running agents match default_harness; run `ari workspace attach <agent-id>`" {
		t.Fatalf("workspace attach error = %q, want %q", err.Error(), "Multiple running agents match default_harness; run `ari workspace attach <agent-id>`")
	}
}

func TestWorkspaceAttachUsesCWDWorkspaceAndRunsAttachFlow(t *testing.T) {
	root := t.TempDir()
	workspaceRoot := filepath.Join(root, "work", "clay")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
	if err := os.Chdir(workspaceRoot); err != nil {
		t.Fatalf("os.Chdir returned error: %v", err)
	}

	originalEnsure := workspaceEnsureDaemonRunning
	originalCfg := rootConfiguredDaemonConfig
	originalList := workspaceListRPC
	originalGet := workspaceGetRPC
	originalPrepare := agentAttachPrepareTerminalFn
	originalSize := agentAttachTerminalSize
	originalAttachRPC := agentAttachRPC
	originalRunSession := agentAttachRunSession

	workspaceEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	rootConfiguredDaemonConfig = func() (*config.Config, error) {
		return &config.Config{Daemon: config.DaemonConfig{SocketPath: "/tmp/daemon.sock"}}, nil
	}
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{Workspaces: []daemon.WorkspaceSummary{{WorkspaceID: "ws-1", Name: "clay"}}}, nil
	}
	workspaceGetRPC = func(_ context.Context, _ string, workspaceID string) (daemon.WorkspaceGetResponse, error) {
		switch workspaceID {
		case "ws-1":
			return daemon.WorkspaceGetResponse{WorkspaceID: "ws-1", Name: "clay", OriginRoot: workspaceRoot}, nil
		default:
			return daemon.WorkspaceGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "workspace not found"}
		}
	}
	agentAttachPrepareTerminalFn = func(*cobra.Command, context.Context) (func(), error) {
		return func() {}, nil
	}
	agentAttachTerminalSize = func(*cobra.Command) (uint16, uint16) { return 80, 24 }
	gotReq := daemon.AgentAttachRequest{}
	agentAttachRPC = func(_ context.Context, _ string, req daemon.AgentAttachRequest) (daemon.AgentAttachResponse, error) {
		gotReq = req
		return daemon.AgentAttachResponse{Token: "tok-1", Status: "pending"}, nil
	}
	agentAttachRunSession = func(context.Context, io.Reader, io.Writer, string, string, uint16, uint16, <-chan os.Signal, func() (uint16, uint16)) (attachSessionOutcome, error) {
		return attachSessionOutcome{Detached: true}, nil
	}
	t.Cleanup(func() {
		workspaceEnsureDaemonRunning = originalEnsure
		rootConfiguredDaemonConfig = originalCfg
		workspaceListRPC = originalList
		workspaceGetRPC = originalGet
		agentAttachPrepareTerminalFn = originalPrepare
		agentAttachTerminalSize = originalSize
		agentAttachRPC = originalAttachRPC
		agentAttachRunSession = originalRunSession
	})

	out, err := executeRootCommand("workspace", "attach", "agent-1")
	if err != nil {
		t.Fatalf("execute workspace attach: %v", err)
	}
	if gotReq.WorkspaceID != "ws-1" {
		t.Fatalf("workspace attach workspace_id = %q, want %q", gotReq.WorkspaceID, "ws-1")
	}
	if gotReq.AgentID != "agent-1" {
		t.Fatalf("workspace attach agent_id = %q, want %q", gotReq.AgentID, "agent-1")
	}
	if !strings.Contains(out, "Attaching to agent \"agent-1\" in workspace ws-1") {
		t.Fatalf("workspace attach output = %q, want attach status line", out)
	}
	if !strings.Contains(out, "Detached from agent \"agent-1\".") {
		t.Fatalf("workspace attach output = %q, want detach line", out)
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

func TestWorkspaceSwitchSkipsClosedWorkspaces(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalTTY := workspaceSwitchIsInteractiveTerminal
	originalList := workspaceListRPC
	workspaceSwitchIsInteractiveTerminal = func(*cobra.Command) bool { return true }
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{Workspaces: []daemon.WorkspaceSummary{{WorkspaceID: "sess-closed", Name: "old", Status: "closed"}}}, nil
	}
	t.Cleanup(func() {
		workspaceSwitchIsInteractiveTerminal = originalTTY
		workspaceListRPC = originalList
	})

	_, err := executeRootCommand("workspace", "switch")
	if err == nil {
		t.Fatal("workspace switch returned nil error with only closed workspaces")
	}
	if err.Error() != "No open workspaces available; create one with `ari workspace create <name>`" {
		t.Fatalf("workspace switch error = %q, want %q", err.Error(), "No open workspaces available; create one with `ari workspace create <name>`")
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

func TestWorkspaceCloseClearsMatchingActiveWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalGet := workspaceGetRPC
	originalClose := workspaceCloseRPC
	workspaceGetRPC = func(context.Context, string, string) (daemon.WorkspaceGetResponse, error) {
		return daemon.WorkspaceGetResponse{WorkspaceID: "sess-1", Name: "alpha"}, nil
	}
	workspaceCloseRPC = func(context.Context, string, string) (daemon.WorkspaceCloseResponse, error) {
		return daemon.WorkspaceCloseResponse{Status: "closed"}, nil
	}
	t.Cleanup(func() {
		workspaceGetRPC = originalGet
		workspaceCloseRPC = originalClose
	})

	if err := config.WriteActiveWorkspace("sess-1"); err != nil {
		t.Fatalf("WriteActiveWorkspace returned error: %v", err)
	}

	_, err := executeRootCommand("workspace", "close", "alpha")
	if err != nil {
		t.Fatalf("execute workspace close: %v", err)
	}

	active, err := config.ReadActiveWorkspace()
	if err != nil {
		t.Fatalf("ReadActiveWorkspace returned error: %v", err)
	}
	if active != "" {
		t.Fatalf("active session after close = %q, want empty", active)
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

func TestWorkspaceSetResumesLatestResumableAgentHistory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalGet := workspaceGetRPC
	originalList := workspaceListRPC
	originalAgentList := agentListRPC
	originalAgentGet := agentGetRPC
	originalAgentSpawn := agentSpawnRPC

	workspaceGetRPC = func(context.Context, string, string) (daemon.WorkspaceGetResponse, error) {
		return daemon.WorkspaceGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "session not found"}
	}
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{Workspaces: []daemon.WorkspaceSummary{{WorkspaceID: "sess-12345678", Name: "alpha", Status: "active"}}}, nil
	}
	agentListRPC = func(context.Context, string, string) (daemon.AgentListResponse, error) {
		return daemon.AgentListResponse{Agents: []daemon.AgentSummary{{AgentID: "agt-2"}, {AgentID: "agt-1"}}}, nil
	}
	agentGetRPC = func(_ context.Context, _ string, _ string, agentID string) (daemon.AgentGetResponse, error) {
		if agentID == "agt-2" {
			return daemon.AgentGetResponse{AgentID: "agt-2", Harness: "opencode", HarnessResumableID: ""}, nil
		}
		return daemon.AgentGetResponse{AgentID: "agt-1", Harness: "opencode", HarnessResumableID: "resume-42"}, nil
	}
	var gotSpawn daemon.AgentSpawnRequest
	agentSpawnRPC = func(_ context.Context, _ string, req daemon.AgentSpawnRequest) (daemon.AgentSpawnResponse, error) {
		gotSpawn = req
		return daemon.AgentSpawnResponse{AgentID: "agt-new", Status: "running"}, nil
	}
	t.Cleanup(func() {
		workspaceGetRPC = originalGet
		workspaceListRPC = originalList
		agentListRPC = originalAgentList
		agentGetRPC = originalAgentGet
		agentSpawnRPC = originalAgentSpawn
	})

	out, err := executeRootCommand("workspace", "set", "alpha")
	if err != nil {
		t.Fatalf("execute workspace set: %v", err)
	}
	if gotSpawn.WorkspaceID != "sess-12345678" {
		t.Fatalf("spawn workspace id = %q, want %q", gotSpawn.WorkspaceID, "sess-12345678")
	}
	if gotSpawn.Harness != "opencode" {
		t.Fatalf("spawn harness = %q, want %q", gotSpawn.Harness, "opencode")
	}
	if len(gotSpawn.Args) != 2 || gotSpawn.Args[0] != "--session" || gotSpawn.Args[1] != "resume-42" {
		t.Fatalf("spawn args = %#v, want [--session resume-42]", gotSpawn.Args)
	}
	if !strings.Contains(out, "Resumed agent conversation") {
		t.Fatalf("workspace set output = %q, want resume confirmation", out)
	}
}

func TestWorkspaceSetWithoutResumableHistoryDoesNotSpawn(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalGet := workspaceGetRPC
	originalList := workspaceListRPC
	originalAgentList := agentListRPC
	originalAgentGet := agentGetRPC
	originalAgentSpawn := agentSpawnRPC

	workspaceGetRPC = func(context.Context, string, string) (daemon.WorkspaceGetResponse, error) {
		return daemon.WorkspaceGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "session not found"}
	}
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{Workspaces: []daemon.WorkspaceSummary{{WorkspaceID: "sess-12345678", Name: "alpha", Status: "active"}}}, nil
	}
	agentListRPC = func(context.Context, string, string) (daemon.AgentListResponse, error) {
		return daemon.AgentListResponse{Agents: []daemon.AgentSummary{{AgentID: "agt-1"}}}, nil
	}
	agentGetRPC = func(context.Context, string, string, string) (daemon.AgentGetResponse, error) {
		return daemon.AgentGetResponse{AgentID: "agt-1", Harness: "codex", HarnessResumableID: ""}, nil
	}
	spawnCalled := false
	agentSpawnRPC = func(_ context.Context, _ string, _ daemon.AgentSpawnRequest) (daemon.AgentSpawnResponse, error) {
		spawnCalled = true
		return daemon.AgentSpawnResponse{AgentID: "agt-new", Status: "running"}, nil
	}
	t.Cleanup(func() {
		workspaceGetRPC = originalGet
		workspaceListRPC = originalList
		agentListRPC = originalAgentList
		agentGetRPC = originalAgentGet
		agentSpawnRPC = originalAgentSpawn
	})

	out, err := executeRootCommand("workspace", "set", "alpha")
	if err != nil {
		t.Fatalf("execute workspace set: %v", err)
	}
	if spawnCalled {
		t.Fatal("agent spawn called unexpectedly")
	}
	if !strings.Contains(out, "No resumable agent history found") {
		t.Fatalf("workspace set output = %q, want no-history message", out)
	}
}

func TestWorkspaceSetSkipsRunningAgentsDuringAutoResume(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalGet := workspaceGetRPC
	originalList := workspaceListRPC
	originalAgentList := agentListRPC
	originalAgentGet := agentGetRPC
	originalAgentSpawn := agentSpawnRPC

	workspaceGetRPC = func(context.Context, string, string) (daemon.WorkspaceGetResponse, error) {
		return daemon.WorkspaceGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "session not found"}
	}
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{Workspaces: []daemon.WorkspaceSummary{{WorkspaceID: "sess-12345678", Name: "alpha", Status: "active"}}}, nil
	}
	agentListRPC = func(context.Context, string, string) (daemon.AgentListResponse, error) {
		return daemon.AgentListResponse{Agents: []daemon.AgentSummary{{AgentID: "agt-running"}, {AgentID: "agt-stopped"}}}, nil
	}
	agentGetRPC = func(_ context.Context, _ string, _ string, agentID string) (daemon.AgentGetResponse, error) {
		if agentID == "agt-running" {
			return daemon.AgentGetResponse{AgentID: agentID, Status: "running", Harness: "opencode", HarnessResumableID: "resume-running"}, nil
		}
		return daemon.AgentGetResponse{AgentID: agentID, Status: "stopped", Harness: "opencode", HarnessResumableID: "resume-stopped"}, nil
	}
	var gotSpawn daemon.AgentSpawnRequest
	agentSpawnRPC = func(_ context.Context, _ string, req daemon.AgentSpawnRequest) (daemon.AgentSpawnResponse, error) {
		gotSpawn = req
		return daemon.AgentSpawnResponse{AgentID: "agt-new", Status: "running"}, nil
	}
	t.Cleanup(func() {
		workspaceGetRPC = originalGet
		workspaceListRPC = originalList
		agentListRPC = originalAgentList
		agentGetRPC = originalAgentGet
		agentSpawnRPC = originalAgentSpawn
	})

	_, err := executeRootCommand("workspace", "set", "alpha")
	if err != nil {
		t.Fatalf("execute workspace set: %v", err)
	}
	if len(gotSpawn.Args) != 2 || gotSpawn.Args[1] != "resume-stopped" {
		t.Fatalf("spawn args = %#v, want resumable id from stopped agent", gotSpawn.Args)
	}
}

func TestWorkspaceSetUsesFreshContextForAgentDetailLookup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalGet := workspaceGetRPC
	originalList := workspaceListRPC
	originalAgentList := agentListRPC
	originalAgentGet := agentGetRPC
	originalAgentSpawn := agentSpawnRPC

	workspaceGetRPC = func(context.Context, string, string) (daemon.WorkspaceGetResponse, error) {
		return daemon.WorkspaceGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "session not found"}
	}
	workspaceListRPC = func(context.Context, string) (daemon.WorkspaceListResponse, error) {
		return daemon.WorkspaceListResponse{Workspaces: []daemon.WorkspaceSummary{{WorkspaceID: "sess-12345678", Name: "alpha", Status: "active"}}}, nil
	}
	agentListRPC = func(context.Context, string, string) (daemon.AgentListResponse, error) {
		time.Sleep(4 * time.Second)
		return daemon.AgentListResponse{Agents: []daemon.AgentSummary{{AgentID: "agt-1"}}}, nil
	}
	agentGetRPC = func(ctx context.Context, _ string, _ string, _ string) (daemon.AgentGetResponse, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			return daemon.AgentGetResponse{}, userFacingError{message: "deadline missing"}
		}
		if time.Until(deadline) < 4*time.Second {
			return daemon.AgentGetResponse{}, userFacingError{message: "deadline budget too small"}
		}
		return daemon.AgentGetResponse{AgentID: "agt-1", Status: "stopped", Harness: "opencode", HarnessResumableID: "resume-42"}, nil
	}
	spawnCalled := false
	agentSpawnRPC = func(_ context.Context, _ string, _ daemon.AgentSpawnRequest) (daemon.AgentSpawnResponse, error) {
		spawnCalled = true
		return daemon.AgentSpawnResponse{AgentID: "agt-new", Status: "running"}, nil
	}
	t.Cleanup(func() {
		workspaceGetRPC = originalGet
		workspaceListRPC = originalList
		agentListRPC = originalAgentList
		agentGetRPC = originalAgentGet
		agentSpawnRPC = originalAgentSpawn
	})

	_, err := executeRootCommand("workspace", "set", "alpha")
	if err != nil {
		t.Fatalf("execute workspace set: %v", err)
	}
	if !spawnCalled {
		t.Fatal("agent spawn was not called")
	}
}

func TestWorkspaceCloseDoesNotUseEnvOverrideToClearPersistedActive(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ARI_ACTIVE_WORKSPACE", "sess-env")

	originalGet := workspaceGetRPC
	originalClose := workspaceCloseRPC
	workspaceGetRPC = func(context.Context, string, string) (daemon.WorkspaceGetResponse, error) {
		return daemon.WorkspaceGetResponse{WorkspaceID: "sess-env", Name: "alpha"}, nil
	}
	workspaceCloseRPC = func(context.Context, string, string) (daemon.WorkspaceCloseResponse, error) {
		return daemon.WorkspaceCloseResponse{Status: "closed"}, nil
	}
	t.Cleanup(func() {
		workspaceGetRPC = originalGet
		workspaceCloseRPC = originalClose
	})

	if err := config.WriteActiveWorkspace("sess-stored"); err != nil {
		t.Fatalf("WriteActiveWorkspace returned error: %v", err)
	}

	_, err := executeRootCommand("workspace", "close", "alpha")
	if err != nil {
		t.Fatalf("execute workspace close: %v", err)
	}

	persisted, err := config.ReadPersistedActiveWorkspace()
	if err != nil {
		t.Fatalf("ReadPersistedActiveWorkspace returned error: %v", err)
	}
	if persisted != "sess-stored" {
		t.Fatalf("persisted active session after close = %q, want %q", persisted, "sess-stored")
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

func TestWorkspaceCreateAutoSpawnsDefaultHarnessAgent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ARI_DEFAULT_HARNESS", "codex")

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
	originalSpawn := agentSpawnRPC
	var gotSpawn daemon.AgentSpawnRequest
	workspaceCreateRPC = func(_ context.Context, _ string, req daemon.WorkspaceCreateRequest) (daemon.WorkspaceCreateResponse, error) {
		return daemon.WorkspaceCreateResponse{WorkspaceID: "sess-1", Name: req.Name, Status: "active", Folder: req.Folder, VCSType: "none", IsPrimary: true, OriginRoot: req.OriginRoot}, nil
	}
	agentSpawnRPC = func(_ context.Context, _ string, req daemon.AgentSpawnRequest) (daemon.AgentSpawnResponse, error) {
		gotSpawn = req
		return daemon.AgentSpawnResponse{AgentID: "agt-1", Status: "running"}, nil
	}
	t.Cleanup(func() {
		workspaceCreateRPC = originalCreate
		agentSpawnRPC = originalSpawn
	})

	out, err := executeRootCommand("workspace", "create", "alpha")
	if err != nil {
		t.Fatalf("execute workspace create: %v", err)
	}

	if gotSpawn.WorkspaceID != "sess-1" {
		t.Fatalf("spawn workspace id = %q, want %q", gotSpawn.WorkspaceID, "sess-1")
	}
	if gotSpawn.Harness != "codex" {
		t.Fatalf("spawn harness = %q, want %q", gotSpawn.Harness, "codex")
	}
	if !strings.Contains(out, "Workspace created: alpha") {
		t.Fatalf("workspace create output = %q, want workspace confirmation", out)
	}
	if !strings.Contains(out, "Agent started: agt-1") {
		t.Fatalf("workspace create output = %q, want agent confirmation", out)
	}
}

func TestWorkspaceCreateHarnessFlagOverridesConfiguredDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ARI_DEFAULT_HARNESS", "codex")

	originalCreate := workspaceCreateRPC
	originalSpawn := agentSpawnRPC
	var gotSpawn daemon.AgentSpawnRequest
	workspaceCreateRPC = func(_ context.Context, _ string, req daemon.WorkspaceCreateRequest) (daemon.WorkspaceCreateResponse, error) {
		return daemon.WorkspaceCreateResponse{WorkspaceID: "sess-1", Name: req.Name, Status: "active", Folder: req.Folder, VCSType: "none", IsPrimary: true, OriginRoot: req.OriginRoot}, nil
	}
	agentSpawnRPC = func(_ context.Context, _ string, req daemon.AgentSpawnRequest) (daemon.AgentSpawnResponse, error) {
		gotSpawn = req
		return daemon.AgentSpawnResponse{AgentID: "agt-1", Status: "running"}, nil
	}
	t.Cleanup(func() {
		workspaceCreateRPC = originalCreate
		agentSpawnRPC = originalSpawn
	})

	_, err := executeRootCommand("workspace", "create", "alpha", "--harness", "opencode")
	if err != nil {
		t.Fatalf("execute workspace create: %v", err)
	}

	if gotSpawn.Harness != "opencode" {
		t.Fatalf("spawn harness = %q, want %q", gotSpawn.Harness, "opencode")
	}
}

func TestWorkspaceCreateWarnsWhenAgentAutoSpawnFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ARI_DEFAULT_HARNESS", "codex")

	originalCreate := workspaceCreateRPC
	originalSpawn := agentSpawnRPC
	workspaceCreateRPC = func(_ context.Context, _ string, req daemon.WorkspaceCreateRequest) (daemon.WorkspaceCreateResponse, error) {
		return daemon.WorkspaceCreateResponse{WorkspaceID: "sess-1", Name: req.Name, Status: "active", Folder: req.Folder, VCSType: "none", IsPrimary: true, OriginRoot: req.OriginRoot}, nil
	}
	agentSpawnRPC = func(_ context.Context, _ string, _ daemon.AgentSpawnRequest) (daemon.AgentSpawnResponse, error) {
		return daemon.AgentSpawnResponse{}, userFacingError{message: "spawn failed"}
	}
	t.Cleanup(func() {
		workspaceCreateRPC = originalCreate
		agentSpawnRPC = originalSpawn
	})

	out, err := executeRootCommand("workspace", "create", "alpha")
	if err != nil {
		t.Fatalf("execute workspace create: %v", err)
	}
	if !strings.Contains(out, "Workspace created: alpha") {
		t.Fatalf("workspace create output = %q, want workspace confirmation", out)
	}
	if !strings.Contains(out, "Warning: workspace created but default agent did not start") {
		t.Fatalf("workspace create output = %q, want spawn warning", out)
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
