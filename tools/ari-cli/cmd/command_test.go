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
	tests := []struct {
		name string
		path []string
		want string
	}{
		{name: "command root registered", path: []string{"command"}, want: "command"},
		{name: "daemon root still registered", path: []string{"daemon"}, want: "daemon"},
		{name: "session root still registered", path: []string{"session"}, want: "session"},
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

func TestCommandSubcommandsExist(t *testing.T) {
	command := NewCommandCmd()

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
			cmd, _, err := command.Find(tc.path)
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
