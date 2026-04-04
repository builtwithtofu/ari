package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
	"github.com/sourcegraph/jsonrpc2"
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
