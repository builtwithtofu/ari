package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
	"github.com/sourcegraph/jsonrpc2"
)

func TestRootRegistersCommandCommand(t *testing.T) {
	root := NewRootCmd()
	commandCmd, _, err := root.Find([]string{"command"})
	if err != nil {
		t.Fatalf("find command command: %v", err)
	}

	if commandCmd == nil {
		t.Fatal("expected command command to be registered")
	}
	if commandCmd.Name() != "command" {
		t.Fatalf("command name = %q, want %q", commandCmd.Name(), "command")
	}
}

func TestCommandSubcommandsExist(t *testing.T) {
	command := NewCommandCmd()

	run, _, err := command.Find([]string{"run"})
	if err != nil {
		t.Fatalf("find command run: %v", err)
	}
	list, _, err := command.Find([]string{"list"})
	if err != nil {
		t.Fatalf("find command list: %v", err)
	}
	show, _, err := command.Find([]string{"show"})
	if err != nil {
		t.Fatalf("find command show: %v", err)
	}
	output, _, err := command.Find([]string{"output"})
	if err != nil {
		t.Fatalf("find command output: %v", err)
	}
	stop, _, err := command.Find([]string{"stop"})
	if err != nil {
		t.Fatalf("find command stop: %v", err)
	}

	if run == nil || list == nil || show == nil || output == nil || stop == nil {
		t.Fatal("expected command subcommands to be registered")
	}
}

func TestCommandRunUsesSeparatorArguments(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolve := commandResolveSessionIdentifier
	originalRun := commandRunRPC
	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	var gotReq daemon.CommandRunRequest
	commandRunRPC = func(_ context.Context, _ string, req daemon.CommandRunRequest) (daemon.CommandRunResponse, error) {
		gotReq = req
		return daemon.CommandRunResponse{CommandID: "cmd-1", Status: "running"}, nil
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		commandRunRPC = originalRun
	})

	out, err := executeRootCommand("command", "run", "alpha", "--", "go", "test", "./...")
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
}

func TestCommandListShowOutputStop(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolve := commandResolveSessionIdentifier
	originalList := commandListRPC
	originalShow := commandGetRPC
	originalOutput := commandOutputRPC
	originalStop := commandStopRPC
	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	commandListRPC = func(context.Context, string, string) (daemon.CommandListResponse, error) {
		return daemon.CommandListResponse{Commands: []daemon.CommandSummary{{CommandID: "cmd-1", Command: "go test", Status: "running", StartedAt: "now"}}}, nil
	}
	commandGetRPC = func(context.Context, string, string, string) (daemon.CommandGetResponse, error) {
		exitCode := 0
		return daemon.CommandGetResponse{CommandID: "cmd-1", SessionID: "sess-1", Command: "go test", Args: `["./..."]`, Status: "exited", ExitCode: &exitCode, StartedAt: "now", FinishedAt: "later"}, nil
	}
	commandOutputRPC = func(context.Context, string, string, string) (daemon.CommandOutputResponse, error) {
		return daemon.CommandOutputResponse{Output: "ok\n"}, nil
	}
	commandStopRPC = func(context.Context, string, string, string) (daemon.CommandStopResponse, error) {
		return daemon.CommandStopResponse{Status: "stopping"}, nil
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		commandListRPC = originalList
		commandGetRPC = originalShow
		commandOutputRPC = originalOutput
		commandStopRPC = originalStop
	})

	listOut, err := executeRootCommand("command", "list", "alpha")
	if err != nil {
		t.Fatalf("execute command list: %v", err)
	}
	if !strings.Contains(listOut, "cmd-1") {
		t.Fatalf("command list output = %q, want command id", listOut)
	}

	showOut, err := executeRootCommand("command", "show", "alpha", "cmd-1")
	if err != nil {
		t.Fatalf("execute command show: %v", err)
	}
	if !strings.Contains(showOut, "Status: exited") {
		t.Fatalf("command show output = %q, want status", showOut)
	}

	outputOut, err := executeRootCommand("command", "output", "alpha", "cmd-1")
	if err != nil {
		t.Fatalf("execute command output: %v", err)
	}
	if !strings.Contains(outputOut, "ok") {
		t.Fatalf("command output output = %q, want output content", outputOut)
	}

	stopOut, err := executeRootCommand("command", "stop", "alpha", "cmd-1")
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

	originalResolve := commandResolveSessionIdentifier
	originalShow := commandGetRPC
	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	commandGetRPC = func(context.Context, string, string, string) (daemon.CommandGetResponse, error) {
		return daemon.CommandGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.CommandNotFound), Message: "command not found"}
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		commandGetRPC = originalShow
	})

	_, err := executeRootCommand("command", "show", "alpha", "missing")
	if err == nil {
		t.Fatal("command show returned nil error for missing command")
	}
	if err.Error() != "Command not found" {
		t.Fatalf("command show error = %q, want %q", err.Error(), "Command not found")
	}
}
