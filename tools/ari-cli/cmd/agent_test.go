package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/config"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
	"github.com/sourcegraph/jsonrpc2"
	"github.com/spf13/cobra"
)

func TestRootRegistersAgentCommand(t *testing.T) {
	root := NewRootCmd()
	agentCmd, _, err := root.Find([]string{"agent"})
	if err != nil {
		t.Fatalf("find agent command: %v", err)
	}
	if agentCmd == nil {
		t.Fatal("expected agent command to be registered")
	}
	if agentCmd.Name() != "agent" {
		t.Fatalf("agent command name = %q, want %q", agentCmd.Name(), "agent")
	}
}

func TestAgentListRejectsActiveSessionOutsideWorkspace(t *testing.T) {
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

	originalResolve := commandResolveSessionIdentifier
	originalReadActive := agentReadActiveSession
	originalEnsure := agentEnsureDaemonRunning
	originalSessionGet := workspaceGetRPC
	originalList := agentListRPC

	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	agentReadActiveSession = func() (string, error) {
		return "sess-1", nil
	}
	agentEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	workspaceGetRPC = func(context.Context, string, string) (daemon.WorkspaceGetResponse, error) {
		return daemon.WorkspaceGetResponse{WorkspaceID: "sess-1", OriginRoot: t.TempDir()}, nil
	}
	agentListRPC = func(context.Context, string, string) (daemon.AgentListResponse, error) {
		return daemon.AgentListResponse{}, errors.New("agent list should not be called")
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		agentReadActiveSession = originalReadActive
		agentEnsureDaemonRunning = originalEnsure
		workspaceGetRPC = originalSessionGet
		agentListRPC = originalList
	})

	_, err = executeRootCommandRaw("agent", "list")
	if err == nil {
		t.Fatal("agent list returned nil error for cross-workspace active session")
	}
	if err.Error() != "Active workspace belongs to a different workspace; use --workspace <id-or-name> to override" {
		t.Fatalf("agent list error = %q, want %q", err.Error(), "Active workspace belongs to a different workspace; use --workspace <id-or-name> to override")
	}
}

func TestAgentListEnvActiveSessionBypassesWorkspaceSafety(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ARI_ACTIVE_WORKSPACE", "sess-env")

	originalResolve := commandResolveSessionIdentifier
	originalReadActive := agentReadActiveSession
	originalEnsure := agentEnsureDaemonRunning
	originalSessionGet := workspaceGetRPC
	originalList := agentListRPC

	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-env", nil
	}
	agentReadActiveSession = func() (string, error) {
		return "sess-env", nil
	}
	agentEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	workspaceGetRPC = func(context.Context, string, string) (daemon.WorkspaceGetResponse, error) {
		return daemon.WorkspaceGetResponse{WorkspaceID: "sess-env", OriginRoot: t.TempDir()}, nil
	}
	called := false
	agentListRPC = func(context.Context, string, string) (daemon.AgentListResponse, error) {
		called = true
		return daemon.AgentListResponse{}, nil
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		agentReadActiveSession = originalReadActive
		agentEnsureDaemonRunning = originalEnsure
		workspaceGetRPC = originalSessionGet
		agentListRPC = originalList
	})

	_, err := executeRootCommandRaw("agent", "list")
	if err != nil {
		t.Fatalf("agent list with env override returned error: %v", err)
	}
	if !called {
		t.Fatal("agent list RPC not called with env active-session override")
	}
}

func TestAgentSubcommandsRejectActiveSessionOutsideWorkspace(t *testing.T) {
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

	originalResolve := commandResolveSessionIdentifier
	originalReadActive := agentReadActiveSession
	originalEnsure := agentEnsureDaemonRunning
	originalSessionGet := workspaceGetRPC
	originalSpawn := agentSpawnRPC
	originalList := agentListRPC
	originalGet := agentGetRPC
	originalSend := agentSendRPC
	originalOutput := agentOutputRPC
	originalStop := agentStopRPC
	originalAttach := agentAttachRPC
	originalDetach := agentDetachRPC

	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	agentReadActiveSession = func() (string, error) {
		return "sess-1", nil
	}
	agentEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	workspaceGetRPC = func(context.Context, string, string) (daemon.WorkspaceGetResponse, error) {
		return daemon.WorkspaceGetResponse{WorkspaceID: "sess-1", OriginRoot: t.TempDir()}, nil
	}
	agentSpawnRPC = func(context.Context, string, daemon.AgentSpawnRequest) (daemon.AgentSpawnResponse, error) {
		return daemon.AgentSpawnResponse{}, errors.New("agent spawn should not be called")
	}
	agentListRPC = func(context.Context, string, string) (daemon.AgentListResponse, error) {
		return daemon.AgentListResponse{}, errors.New("agent list should not be called")
	}
	agentGetRPC = func(context.Context, string, string, string) (daemon.AgentGetResponse, error) {
		return daemon.AgentGetResponse{}, errors.New("agent show should not be called")
	}
	agentSendRPC = func(context.Context, string, daemon.AgentSendRequest) (daemon.AgentSendResponse, error) {
		return daemon.AgentSendResponse{}, errors.New("agent send should not be called")
	}
	agentOutputRPC = func(context.Context, string, string, string) (daemon.AgentOutputResponse, error) {
		return daemon.AgentOutputResponse{}, errors.New("agent output should not be called")
	}
	agentStopRPC = func(context.Context, string, string, string) (daemon.AgentStopResponse, error) {
		return daemon.AgentStopResponse{}, errors.New("agent stop should not be called")
	}
	agentAttachRPC = func(context.Context, string, daemon.AgentAttachRequest) (daemon.AgentAttachResponse, error) {
		return daemon.AgentAttachResponse{}, errors.New("agent attach should not be called")
	}
	agentDetachRPC = func(context.Context, string, daemon.AgentDetachRequest) (daemon.AgentDetachResponse, error) {
		return daemon.AgentDetachResponse{}, errors.New("agent detach should not be called")
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		agentReadActiveSession = originalReadActive
		agentEnsureDaemonRunning = originalEnsure
		workspaceGetRPC = originalSessionGet
		agentSpawnRPC = originalSpawn
		agentListRPC = originalList
		agentGetRPC = originalGet
		agentSendRPC = originalSend
		agentOutputRPC = originalOutput
		agentStopRPC = originalStop
		agentAttachRPC = originalAttach
		agentDetachRPC = originalDetach
	})

	tests := []struct {
		name string
		args []string
	}{
		{name: "spawn", args: []string{"agent", "spawn", "--", "claude-code"}},
		{name: "list", args: []string{"agent", "list"}},
		{name: "show", args: []string{"agent", "show", "claude"}},
		{name: "attach", args: []string{"agent", "attach", "claude"}},
		{name: "detach", args: []string{"agent", "detach", "claude"}},
		{name: "send", args: []string{"agent", "send", "claude", "--input", "hi"}},
		{name: "output", args: []string{"agent", "output", "claude"}},
		{name: "stop", args: []string{"agent", "stop", "claude"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := executeRootCommandRaw(tc.args...)
			if err == nil {
				t.Fatalf("%s returned nil error", tc.name)
			}
			if err.Error() != "Active workspace belongs to a different workspace; use --workspace <id-or-name> to override" {
				t.Fatalf("%s error = %q, want workspace mismatch error", tc.name, err.Error())
			}
		})
	}
}

func TestAgentListUsesSingleSessionGetForActiveWorkspace(t *testing.T) {
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

	originalReadActive := agentReadActiveSession
	originalEnsure := agentEnsureDaemonRunning
	originalSessionGet := workspaceGetRPC
	originalList := agentListRPC

	agentReadActiveSession = func() (string, error) {
		return "sess-1", nil
	}
	agentEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	sessionGetCalls := 0
	workspaceGetRPC = func(context.Context, string, string) (daemon.WorkspaceGetResponse, error) {
		sessionGetCalls++
		return daemon.WorkspaceGetResponse{WorkspaceID: "sess-1", OriginRoot: workspaceRoot}, nil
	}
	agentListRPC = func(context.Context, string, string) (daemon.AgentListResponse, error) {
		return daemon.AgentListResponse{}, nil
	}
	t.Cleanup(func() {
		agentReadActiveSession = originalReadActive
		agentEnsureDaemonRunning = originalEnsure
		workspaceGetRPC = originalSessionGet
		agentListRPC = originalList
	})

	if _, err := executeRootCommandRaw("agent", "list"); err != nil {
		t.Fatalf("agent list returned error: %v", err)
	}
	if sessionGetCalls != 1 {
		t.Fatalf("workspaceGetRPC calls = %d, want 1", sessionGetCalls)
	}
}

func TestAgentSubcommandsExist(t *testing.T) {
	agent := NewAgentCmd()

	tests := []struct {
		name string
		path []string
		want string
	}{
		{name: "spawn", path: []string{"spawn"}, want: "spawn"},
		{name: "list", path: []string{"list"}, want: "list"},
		{name: "show", path: []string{"show"}, want: "show"},
		{name: "attach", path: []string{"attach"}, want: "attach"},
		{name: "detach", path: []string{"detach"}, want: "detach"},
		{name: "send", path: []string{"send"}, want: "send"},
		{name: "output", path: []string{"output"}, want: "output"},
		{name: "stop", path: []string{"stop"}, want: "stop"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd, _, err := agent.Find(tc.path)
			if err != nil {
				t.Fatalf("find %v: %v", tc.path, err)
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

func TestAgentSpawnListShowOutputStop(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolve := commandResolveSessionIdentifier
	originalReadActive := agentReadActiveSession
	originalSpawn := agentSpawnRPC
	originalList := agentListRPC
	originalGet := agentGetRPC
	originalOutput := agentOutputRPC
	originalStop := agentStopRPC

	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	agentReadActiveSession = func() (string, error) {
		return "sess-1", nil
	}
	var gotSpawn daemon.AgentSpawnRequest
	agentSpawnRPC = func(_ context.Context, _ string, req daemon.AgentSpawnRequest) (daemon.AgentSpawnResponse, error) {
		gotSpawn = req
		return daemon.AgentSpawnResponse{AgentID: "agt-1", Status: "running"}, nil
	}
	agentListRPC = func(context.Context, string, string) (daemon.AgentListResponse, error) {
		return daemon.AgentListResponse{Agents: []daemon.AgentSummary{{AgentID: "agt-1", Name: "claude", Command: "claude-code", Status: "running", StartedAt: "now"}}}, nil
	}
	agentGetRPC = func(context.Context, string, string, string) (daemon.AgentGetResponse, error) {
		exitCode := 0
		return daemon.AgentGetResponse{AgentID: "agt-1", WorkspaceID: "sess-1", Name: "claude", Command: "claude-code", Args: `["--resume"]`, Status: "stopped", ExitCode: &exitCode, StartedAt: "now", StoppedAt: "later", Harness: "claude-code", HarnessResumableID: "resume-123", HarnessMetadata: []byte(`{"resume_source":"argv"}`)}, nil
	}
	agentOutputRPC = func(context.Context, string, string, string) (daemon.AgentOutputResponse, error) {
		return daemon.AgentOutputResponse{Output: "agent-output\n"}, nil
	}
	agentStopRPC = func(context.Context, string, string, string) (daemon.AgentStopResponse, error) {
		return daemon.AgentStopResponse{Status: "stopping"}, nil
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		agentReadActiveSession = originalReadActive
		agentSpawnRPC = originalSpawn
		agentListRPC = originalList
		agentGetRPC = originalGet
		agentOutputRPC = originalOutput
		agentStopRPC = originalStop
	})

	spawnOut, err := executeRootCommand("agent", "spawn", "--name", "claude", "--", "claude-code", "--resume")
	if err != nil {
		t.Fatalf("execute agent spawn: %v", err)
	}
	if gotSpawn.Name != "claude" {
		t.Fatalf("spawn name = %q, want %q", gotSpawn.Name, "claude")
	}
	if gotSpawn.Command != "claude-code" {
		t.Fatalf("spawn command = %q, want %q", gotSpawn.Command, "claude-code")
	}
	if len(gotSpawn.Args) != 1 || gotSpawn.Args[0] != "--resume" {
		t.Fatalf("spawn args = %v, want [--resume]", gotSpawn.Args)
	}
	if !strings.Contains(spawnOut, "Agent started: agt-1") {
		t.Fatalf("spawn output = %q, want start confirmation", spawnOut)
	}

	listOut, err := executeRootCommand("agent", "list")
	if err != nil {
		t.Fatalf("execute agent list: %v", err)
	}
	if !strings.Contains(listOut, "claude") {
		t.Fatalf("list output = %q, want agent name", listOut)
	}

	showOut, err := executeRootCommand("agent", "show", "claude")
	if err != nil {
		t.Fatalf("execute agent show: %v", err)
	}
	if !strings.Contains(showOut, "Status: stopped") {
		t.Fatalf("show output = %q, want status", showOut)
	}
	if !strings.Contains(showOut, "Harness: claude-code") {
		t.Fatalf("show output = %q, want harness", showOut)
	}
	if !strings.Contains(showOut, "Harness Resumable ID: resume-123") {
		t.Fatalf("show output = %q, want harness resumable id", showOut)
	}
	if !strings.Contains(showOut, `Harness Metadata: {"resume_source":"argv"}`) {
		t.Fatalf("show output = %q, want harness metadata", showOut)
	}

	outputOut, err := executeRootCommand("agent", "output", "claude")
	if err != nil {
		t.Fatalf("execute agent output: %v", err)
	}
	if !strings.Contains(outputOut, "agent-output") {
		t.Fatalf("output output = %q, want output payload", outputOut)
	}

	stopOut, err := executeRootCommand("agent", "stop", "claude")
	if err != nil {
		t.Fatalf("execute agent stop: %v", err)
	}
	if !strings.Contains(stopOut, "stopping") {
		t.Fatalf("stop output = %q, want stopping", stopOut)
	}
}

func TestAgentSendWithFlagInput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolve := commandResolveSessionIdentifier
	originalReadActive := agentReadActiveSession
	originalSend := agentSendRPC
	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	agentReadActiveSession = func() (string, error) {
		return "sess-1", nil
	}
	var got daemon.AgentSendRequest
	agentSendRPC = func(_ context.Context, _ string, req daemon.AgentSendRequest) (daemon.AgentSendResponse, error) {
		got = req
		return daemon.AgentSendResponse{Status: "sent"}, nil
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		agentReadActiveSession = originalReadActive
		agentSendRPC = originalSend
	})

	out, err := executeRootCommand("agent", "send", "claude", "--input", "fix bug")
	if err != nil {
		t.Fatalf("execute agent send with --input: %v", err)
	}
	if got.Input != "fix bug" {
		t.Fatalf("send input = %q, want %q", got.Input, "fix bug")
	}
	if !strings.Contains(out, "Input sent") {
		t.Fatalf("send output = %q, want confirmation", out)
	}
}

func TestAgentSendWithStdinPipe(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolve := commandResolveSessionIdentifier
	originalReadActive := agentReadActiveSession
	originalSend := agentSendRPC
	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	agentReadActiveSession = func() (string, error) {
		return "sess-1", nil
	}
	var got daemon.AgentSendRequest
	agentSendRPC = func(_ context.Context, _ string, req daemon.AgentSendRequest) (daemon.AgentSendResponse, error) {
		got = req
		return daemon.AgentSendResponse{Status: "sent"}, nil
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		agentReadActiveSession = originalReadActive
		agentSendRPC = originalSend
	})

	out, err := executeRootCommandWithInput("hello from stdin", "agent", "send", "claude")
	if err != nil {
		t.Fatalf("execute agent send with stdin: %v", err)
	}
	if got.Input != "hello from stdin" {
		t.Fatalf("send stdin input = %q, want %q", got.Input, "hello from stdin")
	}
	if !strings.Contains(out, "Input sent") {
		t.Fatalf("send output = %q, want confirmation", out)
	}
}

func TestAgentSendRejectsBothFlagAndStdin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err := executeRootCommandWithInput("from-stdin", "agent", "send", "claude", "--input", "from-flag")
	if err == nil {
		t.Fatal("agent send returned nil error when both --input and stdin provided")
	}
	if err.Error() != "Provide input via --input or stdin pipe, not both" {
		t.Fatalf("agent send error = %q, want %q", err.Error(), "Provide input via --input or stdin pipe, not both")
	}
}

func TestAgentSendRejectsMissingInput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err := executeRootCommand("agent", "send", "claude")
	if err == nil {
		t.Fatal("agent send returned nil error when input missing")
	}
	if err.Error() != "Provide input via --input or stdin pipe" {
		t.Fatalf("agent send error = %q, want %q", err.Error(), "Provide input via --input or stdin pipe")
	}
}

func TestAgentShowNotFoundMapsError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolve := commandResolveSessionIdentifier
	originalReadActive := agentReadActiveSession
	originalGet := agentGetRPC
	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	agentReadActiveSession = func() (string, error) {
		return "sess-1", nil
	}
	agentGetRPC = func(context.Context, string, string, string) (daemon.AgentGetResponse, error) {
		return daemon.AgentGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.AgentNotFound), Message: "agent not found"}
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		agentReadActiveSession = originalReadActive
		agentGetRPC = originalGet
	})

	_, err := executeRootCommand("agent", "show", "missing")
	if err == nil {
		t.Fatal("agent show returned nil error for missing agent")
	}
	if err.Error() != "Agent not found" {
		t.Fatalf("agent show error = %q, want %q", err.Error(), "Agent not found")
	}
}

func TestAgentSpawnAllowsHarnessWithoutExplicitCommand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolve := commandResolveSessionIdentifier
	originalReadActive := agentReadActiveSession
	originalSpawn := agentSpawnRPC
	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	agentReadActiveSession = func() (string, error) {
		return "sess-1", nil
	}
	var got daemon.AgentSpawnRequest
	agentSpawnRPC = func(_ context.Context, _ string, req daemon.AgentSpawnRequest) (daemon.AgentSpawnResponse, error) {
		got = req
		return daemon.AgentSpawnResponse{AgentID: "agt-1", Status: "running"}, nil
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		agentReadActiveSession = originalReadActive
		agentSpawnRPC = originalSpawn
	})

	out, err := executeRootCommand("agent", "spawn", "--harness", "opencode", "--", "--resume")
	if err != nil {
		t.Fatalf("execute harness-only spawn: %v", err)
	}
	if got.Harness != "opencode" {
		t.Fatalf("spawn harness = %q, want %q", got.Harness, "opencode")
	}
	if got.Command != "" {
		t.Fatalf("spawn command = %q, want empty for harness default", got.Command)
	}
	if len(got.Args) != 1 || got.Args[0] != "--resume" {
		t.Fatalf("spawn args = %v, want [--resume]", got.Args)
	}
	if !strings.Contains(out, "Agent started: agt-1") {
		t.Fatalf("spawn output = %q, want start confirmation", out)
	}
}

func TestAgentSpawnHarnessTreatsPositionalArgsAsHarnessArgs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolve := commandResolveSessionIdentifier
	originalReadActive := agentReadActiveSession
	originalSpawn := agentSpawnRPC
	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	agentReadActiveSession = func() (string, error) {
		return "sess-1", nil
	}
	var got daemon.AgentSpawnRequest
	agentSpawnRPC = func(_ context.Context, _ string, req daemon.AgentSpawnRequest) (daemon.AgentSpawnResponse, error) {
		got = req
		return daemon.AgentSpawnResponse{AgentID: "agt-1", Status: "running"}, nil
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		agentReadActiveSession = originalReadActive
		agentSpawnRPC = originalSpawn
	})

	_, err := executeRootCommand("agent", "spawn", "--harness", "opencode", "--", "review prompt")
	if err != nil {
		t.Fatalf("execute harness positional spawn: %v", err)
	}
	if got.Command != "" {
		t.Fatalf("spawn command = %q, want empty for harness-default invocation", got.Command)
	}
	if len(got.Args) != 1 || got.Args[0] != "review prompt" {
		t.Fatalf("spawn args = %v, want [review prompt]", got.Args)
	}
}

func TestMapAgentRPCErrorAgentAlreadyAttached(t *testing.T) {
	err := mapAgentRPCError(&jsonrpc2.Error{Code: int64(rpc.AgentAlreadyAttached), Message: "agent already has an active attach session"})
	if err == nil {
		t.Fatal("mapAgentRPCError returned nil error")
	}
	if err.Error() != "Agent already has an active attach session" {
		t.Fatalf("mapAgentRPCError message = %q, want %q", err.Error(), "Agent already has an active attach session")
	}
}

func TestAgentAttachAndDetachCommands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolve := commandResolveSessionIdentifier
	originalReadActive := agentReadActiveSession
	originalAttach := agentAttachRPC
	originalDetach := agentDetachRPC
	originalAttachTerminalSize := agentAttachTerminalSize
	originalAttachRunSession := agentAttachRunSession

	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	agentReadActiveSession = func() (string, error) {
		return "sess-1", nil
	}
	var gotAttach daemon.AgentAttachRequest
	agentAttachRPC = func(_ context.Context, _ string, req daemon.AgentAttachRequest) (daemon.AgentAttachResponse, error) {
		gotAttach = req
		return daemon.AgentAttachResponse{Token: "tok-1", Status: "pending"}, nil
	}
	var gotDetach daemon.AgentDetachRequest
	agentDetachRPC = func(_ context.Context, _ string, req daemon.AgentDetachRequest) (daemon.AgentDetachResponse, error) {
		gotDetach = req
		return daemon.AgentDetachResponse{Status: "detached"}, nil
	}
	agentAttachTerminalSize = func(_ *cobra.Command) (uint16, uint16) {
		return 132, 43
	}
	agentAttachRunSession = func(_ context.Context, input io.Reader, _ io.Writer, _ string, _ string, _ uint16, _ uint16, _ <-chan os.Signal, _ func() (uint16, uint16)) (attachSessionOutcome, error) {
		buf := make([]byte, 8)
		_, _ = input.Read(buf)
		return attachSessionOutcome{Detached: true}, nil
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		agentReadActiveSession = originalReadActive
		agentAttachRPC = originalAttach
		agentDetachRPC = originalDetach
		agentAttachTerminalSize = originalAttachTerminalSize
		agentAttachRunSession = originalAttachRunSession
	})

	attachOut, err := executeRootCommandWithInput(string([]byte{0x1c}), "agent", "attach", "claude")
	if err != nil {
		t.Fatalf("execute agent attach: %v", err)
	}
	if gotAttach.WorkspaceID != "sess-1" || gotAttach.AgentID != "claude" {
		t.Fatalf("agent attach request = %+v, want workspace_id sess-1 and agent_id claude", gotAttach)
	}
	if gotAttach.InitialCols != 132 || gotAttach.InitialRows != 43 {
		t.Fatalf("agent attach initial size = %dx%d, want 132x43", gotAttach.InitialCols, gotAttach.InitialRows)
	}
	if !strings.Contains(attachOut, "Detached from agent \"claude\".") {
		t.Fatalf("attach output = %q, want detach line", attachOut)
	}

	detachOut, err := executeRootCommand("agent", "detach", "claude")
	if err != nil {
		t.Fatalf("execute agent detach: %v", err)
	}
	if gotDetach.WorkspaceID != "sess-1" || gotDetach.AgentID != "claude" {
		t.Fatalf("agent detach request = %+v, want workspace_id sess-1 and agent_id claude", gotDetach)
	}
	if !strings.Contains(detachOut, "Agent detach: detached") {
		t.Fatalf("detach output = %q, want detached line", detachOut)
	}
}

func TestAgentListUsesSessionFlagOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolveTarget := commandResolveSessionTarget
	originalReadActive := agentReadActiveSession
	originalList := agentListRPC

	var gotLookup string
	commandResolveSessionTarget = func(_ context.Context, _ string, idOrName string) (resolvedSessionTarget, error) {
		gotLookup = idOrName
		return resolvedSessionTarget{WorkspaceID: "sess-override", Session: &daemon.WorkspaceGetResponse{WorkspaceID: "sess-override", OriginRoot: t.TempDir()}}, nil
	}
	agentReadActiveSession = func() (string, error) {
		return "sess-active", nil
	}
	agentListRPC = func(context.Context, string, string) (daemon.AgentListResponse, error) {
		return daemon.AgentListResponse{}, nil
	}
	t.Cleanup(func() {
		commandResolveSessionTarget = originalResolveTarget
		agentReadActiveSession = originalReadActive
		agentListRPC = originalList
	})

	_, err := executeRootCommand("agent", "list", "--workspace", "alpha")
	if err != nil {
		t.Fatalf("execute agent list with --workspace: %v", err)
	}
	if gotLookup != "alpha" {
		t.Fatalf("session lookup argument = %q, want %q", gotLookup, "alpha")
	}
}

func TestAgentListRequiresActiveWorkspaceWhenSessionNotProvided(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalReadActive := agentReadActiveSession
	agentReadActiveSession = func() (string, error) {
		return "", nil
	}
	t.Cleanup(func() {
		agentReadActiveSession = originalReadActive
	})

	_, err := executeRootCommand("agent", "list")
	if err == nil {
		t.Fatal("agent list returned nil error without active session")
	}
	if err.Error() != "No active workspace is set" {
		t.Fatalf("agent list error = %q, want %q", err.Error(), "No active workspace is set")
	}
}

func TestAgentListMissingActiveSessionDoesNotCallEnsure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalReadActive := agentReadActiveSession
	originalEnsure := agentEnsureDaemonRunning
	agentReadActiveSession = func() (string, error) {
		return "", nil
	}
	agentEnsureDaemonRunning = func(context.Context, *config.Config) error {
		return userFacingError{message: "ensure called unexpectedly"}
	}
	t.Cleanup(func() {
		agentReadActiveSession = originalReadActive
		agentEnsureDaemonRunning = originalEnsure
	})

	_, err := executeRootCommandRaw("agent", "list")
	if err == nil {
		t.Fatal("agent list returned nil error without active session")
	}
	if err.Error() != "No active workspace is set" {
		t.Fatalf("agent list error = %q, want %q", err.Error(), "No active workspace is set")
	}
}

func TestAgentSubcommandsUseSessionFlagOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolveTarget := commandResolveSessionTarget
	originalReadActive := agentReadActiveSession
	originalSpawn := agentSpawnRPC
	originalList := agentListRPC
	originalGet := agentGetRPC
	originalAttach := agentAttachRPC
	originalDetach := agentDetachRPC
	originalSend := agentSendRPC
	originalOutput := agentOutputRPC
	originalStop := agentStopRPC
	originalAttachTerminalSize := agentAttachTerminalSize
	originalAttachRunSession := agentAttachRunSession

	agentReadActiveSession = func() (string, error) {
		return "", errors.New("active workspace should not be read when --workspace is provided")
	}
	agentSpawnRPC = func(context.Context, string, daemon.AgentSpawnRequest) (daemon.AgentSpawnResponse, error) {
		return daemon.AgentSpawnResponse{AgentID: "agt-1", Status: "running"}, nil
	}
	agentListRPC = func(context.Context, string, string) (daemon.AgentListResponse, error) {
		return daemon.AgentListResponse{}, nil
	}
	agentGetRPC = func(context.Context, string, string, string) (daemon.AgentGetResponse, error) {
		return daemon.AgentGetResponse{AgentID: "agt-1", WorkspaceID: "sess-1", Command: "claude-code", Status: "running", StartedAt: "now"}, nil
	}
	agentAttachRPC = func(context.Context, string, daemon.AgentAttachRequest) (daemon.AgentAttachResponse, error) {
		return daemon.AgentAttachResponse{Token: "tok-1", Status: "pending"}, nil
	}
	agentDetachRPC = func(context.Context, string, daemon.AgentDetachRequest) (daemon.AgentDetachResponse, error) {
		return daemon.AgentDetachResponse{Status: "detached"}, nil
	}
	agentSendRPC = func(context.Context, string, daemon.AgentSendRequest) (daemon.AgentSendResponse, error) {
		return daemon.AgentSendResponse{Status: "sent"}, nil
	}
	agentOutputRPC = func(context.Context, string, string, string) (daemon.AgentOutputResponse, error) {
		return daemon.AgentOutputResponse{Output: "ok\n"}, nil
	}
	agentStopRPC = func(context.Context, string, string, string) (daemon.AgentStopResponse, error) {
		return daemon.AgentStopResponse{Status: "stopping"}, nil
	}
	agentAttachTerminalSize = func(*cobra.Command) (uint16, uint16) { return 120, 40 }
	agentAttachRunSession = func(context.Context, io.Reader, io.Writer, string, string, uint16, uint16, <-chan os.Signal, func() (uint16, uint16)) (attachSessionOutcome, error) {
		return attachSessionOutcome{Detached: true}, nil
	}

	t.Cleanup(func() {
		commandResolveSessionTarget = originalResolveTarget
		agentReadActiveSession = originalReadActive
		agentSpawnRPC = originalSpawn
		agentListRPC = originalList
		agentGetRPC = originalGet
		agentAttachRPC = originalAttach
		agentDetachRPC = originalDetach
		agentSendRPC = originalSend
		agentOutputRPC = originalOutput
		agentStopRPC = originalStop
		agentAttachTerminalSize = originalAttachTerminalSize
		agentAttachRunSession = originalAttachRunSession
	})

	tests := []struct {
		name string
		args []string
	}{
		{name: "spawn", args: []string{"agent", "spawn", "--workspace", "alpha", "--harness", "opencode", "--", "--resume"}},
		{name: "list", args: []string{"agent", "list", "--workspace", "alpha"}},
		{name: "show", args: []string{"agent", "show", "claude", "--workspace", "alpha"}},
		{name: "attach", args: []string{"agent", "attach", "claude", "--workspace", "alpha"}},
		{name: "detach", args: []string{"agent", "detach", "claude", "--workspace", "alpha"}},
		{name: "send", args: []string{"agent", "send", "claude", "--workspace", "alpha", "--input", "hello"}},
		{name: "output", args: []string{"agent", "output", "claude", "--workspace", "alpha"}},
		{name: "stop", args: []string{"agent", "stop", "claude", "--workspace", "alpha"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotLookup := ""
			commandResolveSessionTarget = func(_ context.Context, _ string, idOrName string) (resolvedSessionTarget, error) {
				gotLookup = idOrName
				return resolvedSessionTarget{WorkspaceID: "sess-override", Session: &daemon.WorkspaceGetResponse{WorkspaceID: "sess-override", OriginRoot: t.TempDir()}}, nil
			}

			_, err := executeRootCommand(tc.args...)
			if err != nil {
				t.Fatalf("execute agent %s with --workspace: %v", tc.name, err)
			}
			if gotLookup != "alpha" {
				t.Fatalf("session lookup argument = %q, want %q", gotLookup, "alpha")
			}
		})
	}
}

func executeRootCommandWithInput(stdin string, args ...string) (string, error) {
	originalCommandEnsure := commandEnsureDaemonRunning
	originalAgentEnsure := agentEnsureDaemonRunning
	originalSessionEnsure := workspaceEnsureDaemonRunning
	originalResolveTarget := commandResolveSessionTarget
	originalCommandScope := commandEnsureWorkspaceScope
	originalAgentScope := agentEnsureWorkspaceScope
	commandEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	agentEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	workspaceEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	commandResolveSessionTarget = func(ctx context.Context, socketPath, idOrName string) (resolvedSessionTarget, error) {
		sessionID, err := commandResolveSessionIdentifier(ctx, socketPath, idOrName)
		if err != nil {
			return resolvedSessionTarget{}, err
		}
		return resolvedSessionTarget{WorkspaceID: sessionID, Session: &daemon.WorkspaceGetResponse{WorkspaceID: sessionID}}, nil
	}
	commandEnsureWorkspaceScope = func(*daemon.WorkspaceGetResponse, string) error { return nil }
	agentEnsureWorkspaceScope = func(*daemon.WorkspaceGetResponse, string) error { return nil }
	defer func() {
		commandEnsureDaemonRunning = originalCommandEnsure
		agentEnsureDaemonRunning = originalAgentEnsure
		workspaceEnsureDaemonRunning = originalSessionEnsure
		commandResolveSessionTarget = originalResolveTarget
		commandEnsureWorkspaceScope = originalCommandScope
		agentEnsureWorkspaceScope = originalAgentScope
	}()

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(stdin))
	root.SetContext(context.Background())
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}
