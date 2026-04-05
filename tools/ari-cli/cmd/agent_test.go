package cmd

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

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
	originalSpawn := agentSpawnRPC
	originalList := agentListRPC
	originalGet := agentGetRPC
	originalOutput := agentOutputRPC
	originalStop := agentStopRPC

	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
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
		return daemon.AgentGetResponse{AgentID: "agt-1", SessionID: "sess-1", Name: "claude", Command: "claude-code", Args: `["--resume"]`, Status: "stopped", ExitCode: &exitCode, StartedAt: "now", StoppedAt: "later"}, nil
	}
	agentOutputRPC = func(context.Context, string, string, string) (daemon.AgentOutputResponse, error) {
		return daemon.AgentOutputResponse{Output: "agent-output\n"}, nil
	}
	agentStopRPC = func(context.Context, string, string, string) (daemon.AgentStopResponse, error) {
		return daemon.AgentStopResponse{Status: "stopping"}, nil
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		agentSpawnRPC = originalSpawn
		agentListRPC = originalList
		agentGetRPC = originalGet
		agentOutputRPC = originalOutput
		agentStopRPC = originalStop
	})

	spawnOut, err := executeRootCommand("agent", "spawn", "alpha", "--name", "claude", "--", "claude-code", "--resume")
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

	listOut, err := executeRootCommand("agent", "list", "alpha")
	if err != nil {
		t.Fatalf("execute agent list: %v", err)
	}
	if !strings.Contains(listOut, "claude") {
		t.Fatalf("list output = %q, want agent name", listOut)
	}

	showOut, err := executeRootCommand("agent", "show", "alpha", "claude")
	if err != nil {
		t.Fatalf("execute agent show: %v", err)
	}
	if !strings.Contains(showOut, "Status: stopped") {
		t.Fatalf("show output = %q, want status", showOut)
	}

	outputOut, err := executeRootCommand("agent", "output", "alpha", "claude")
	if err != nil {
		t.Fatalf("execute agent output: %v", err)
	}
	if !strings.Contains(outputOut, "agent-output") {
		t.Fatalf("output output = %q, want output payload", outputOut)
	}

	stopOut, err := executeRootCommand("agent", "stop", "alpha", "claude")
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
	originalSend := agentSendRPC
	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	var got daemon.AgentSendRequest
	agentSendRPC = func(_ context.Context, _ string, req daemon.AgentSendRequest) (daemon.AgentSendResponse, error) {
		got = req
		return daemon.AgentSendResponse{Status: "sent"}, nil
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		agentSendRPC = originalSend
	})

	out, err := executeRootCommand("agent", "send", "alpha", "claude", "--input", "fix bug")
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
	originalSend := agentSendRPC
	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	var got daemon.AgentSendRequest
	agentSendRPC = func(_ context.Context, _ string, req daemon.AgentSendRequest) (daemon.AgentSendResponse, error) {
		got = req
		return daemon.AgentSendResponse{Status: "sent"}, nil
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		agentSendRPC = originalSend
	})

	out, err := executeRootCommandWithInput("hello from stdin", "agent", "send", "alpha", "claude")
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

	_, err := executeRootCommandWithInput("from-stdin", "agent", "send", "alpha", "claude", "--input", "from-flag")
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

	_, err := executeRootCommand("agent", "send", "alpha", "claude")
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
	originalGet := agentGetRPC
	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	agentGetRPC = func(context.Context, string, string, string) (daemon.AgentGetResponse, error) {
		return daemon.AgentGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.AgentNotFound), Message: "agent not found"}
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		agentGetRPC = originalGet
	})

	_, err := executeRootCommand("agent", "show", "alpha", "missing")
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
	originalSpawn := agentSpawnRPC
	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	var got daemon.AgentSpawnRequest
	agentSpawnRPC = func(_ context.Context, _ string, req daemon.AgentSpawnRequest) (daemon.AgentSpawnResponse, error) {
		got = req
		return daemon.AgentSpawnResponse{AgentID: "agt-1", Status: "running"}, nil
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		agentSpawnRPC = originalSpawn
	})

	out, err := executeRootCommand("agent", "spawn", "alpha", "--harness", "opencode", "--", "--resume")
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
	originalSpawn := agentSpawnRPC
	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	var got daemon.AgentSpawnRequest
	agentSpawnRPC = func(_ context.Context, _ string, req daemon.AgentSpawnRequest) (daemon.AgentSpawnResponse, error) {
		got = req
		return daemon.AgentSpawnResponse{AgentID: "agt-1", Status: "running"}, nil
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		agentSpawnRPC = originalSpawn
	})

	_, err := executeRootCommand("agent", "spawn", "alpha", "--harness", "opencode", "--", "review prompt")
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
	originalAttach := agentAttachRPC
	originalDetach := agentDetachRPC
	originalAttachTerminalSize := agentAttachTerminalSize
	originalAttachRunSession := agentAttachRunSession

	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
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
	agentAttachRunSession = func(_ context.Context, input io.Reader, _ io.Writer, _ string, _ string, _ uint16, _ uint16) (attachSessionOutcome, error) {
		buf := make([]byte, 8)
		_, _ = input.Read(buf)
		return attachSessionOutcome{Detached: true}, nil
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		agentAttachRPC = originalAttach
		agentDetachRPC = originalDetach
		agentAttachTerminalSize = originalAttachTerminalSize
		agentAttachRunSession = originalAttachRunSession
	})

	attachOut, err := executeRootCommandWithInput(string([]byte{0x1c}), "agent", "attach", "alpha", "claude")
	if err != nil {
		t.Fatalf("execute agent attach: %v", err)
	}
	if gotAttach.SessionID != "sess-1" || gotAttach.AgentID != "claude" {
		t.Fatalf("agent attach request = %+v, want session_id sess-1 and agent_id claude", gotAttach)
	}
	if gotAttach.InitialCols != 132 || gotAttach.InitialRows != 43 {
		t.Fatalf("agent attach initial size = %dx%d, want 132x43", gotAttach.InitialCols, gotAttach.InitialRows)
	}
	if !strings.Contains(attachOut, "Detached from agent \"claude\".") {
		t.Fatalf("attach output = %q, want detach line", attachOut)
	}

	detachOut, err := executeRootCommand("agent", "detach", "alpha", "claude")
	if err != nil {
		t.Fatalf("execute agent detach: %v", err)
	}
	if gotDetach.SessionID != "sess-1" || gotDetach.AgentID != "claude" {
		t.Fatalf("agent detach request = %+v, want session_id sess-1 and agent_id claude", gotDetach)
	}
	if !strings.Contains(detachOut, "Agent detach: detached") {
		t.Fatalf("detach output = %q, want detached line", detachOut)
	}
}

func executeRootCommandWithInput(stdin string, args ...string) (string, error) {
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
