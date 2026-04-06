package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func TestCommandListRejectsActiveSessionOutsideWorkspace(t *testing.T) {
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
	originalReadActive := commandReadActiveSession
	originalEnsure := commandEnsureDaemonRunning
	originalSessionGet := sessionGetRPC
	originalList := commandListRPC

	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	commandReadActiveSession = func() (string, error) {
		return "sess-1", nil
	}
	commandEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	sessionGetRPC = func(context.Context, string, string) (daemon.SessionGetResponse, error) {
		return daemon.SessionGetResponse{SessionID: "sess-1", OriginRoot: t.TempDir()}, nil
	}
	commandListRPC = func(context.Context, string, string) (daemon.CommandListResponse, error) {
		return daemon.CommandListResponse{}, errors.New("command list should not be called")
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		commandReadActiveSession = originalReadActive
		commandEnsureDaemonRunning = originalEnsure
		sessionGetRPC = originalSessionGet
		commandListRPC = originalList
	})

	_, err = executeRootCommandRaw("command", "list")
	if err == nil {
		t.Fatal("command list returned nil error for cross-workspace active session")
	}
	if err.Error() != "Active workspace session belongs to a different workspace; use --session <id-or-name> to override" {
		t.Fatalf("command list error = %q, want %q", err.Error(), "Active workspace session belongs to a different workspace; use --session <id-or-name> to override")
	}
}

func TestCommandListSessionOverrideBypassesWorkspaceSafety(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolve := commandResolveSessionIdentifier
	originalReadActive := commandReadActiveSession
	originalEnsure := commandEnsureDaemonRunning
	originalSessionGet := sessionGetRPC
	originalList := commandListRPC

	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	commandReadActiveSession = func() (string, error) {
		return "", errors.New("active session should not be read when --session is provided")
	}
	commandEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	sessionGetRPC = func(context.Context, string, string) (daemon.SessionGetResponse, error) {
		return daemon.SessionGetResponse{SessionID: "sess-1", OriginRoot: t.TempDir()}, nil
	}
	called := false
	commandListRPC = func(context.Context, string, string) (daemon.CommandListResponse, error) {
		called = true
		return daemon.CommandListResponse{}, nil
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		commandReadActiveSession = originalReadActive
		commandEnsureDaemonRunning = originalEnsure
		sessionGetRPC = originalSessionGet
		commandListRPC = originalList
	})

	_, err := executeRootCommand("command", "list", "--session", "alpha")
	if err != nil {
		t.Fatalf("command list with --session returned error: %v", err)
	}
	if !called {
		t.Fatal("command list RPC not called with --session override")
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

	originalResolve := commandResolveSessionIdentifier
	originalReadActive := commandReadActiveSession
	originalEnsure := commandEnsureDaemonRunning
	originalSessionGet := sessionGetRPC
	originalList := commandListRPC

	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	commandReadActiveSession = func() (string, error) {
		return "sess-1", nil
	}
	commandEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	sessionGetRPC = func(context.Context, string, string) (daemon.SessionGetResponse, error) {
		return daemon.SessionGetResponse{
			SessionID:  "sess-1",
			OriginRoot: workspaceRoot,
			Folders:    []daemon.SessionFolderInfo{{Path: filepath.Join(workspaceRoot, "repo-a")}},
		}, nil
	}
	called := false
	commandListRPC = func(context.Context, string, string) (daemon.CommandListResponse, error) {
		called = true
		return daemon.CommandListResponse{}, nil
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		commandReadActiveSession = originalReadActive
		commandEnsureDaemonRunning = originalEnsure
		sessionGetRPC = originalSessionGet
		commandListRPC = originalList
	})

	_, err = executeRootCommand("command", "list")
	if err != nil {
		t.Fatalf("command list returned error: %v", err)
	}
	if !called {
		t.Fatal("command list RPC not called when cwd is within origin root")
	}
}

func TestCommandListEnvActiveSessionBypassesWorkspaceSafety(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ARI_ACTIVE_SESSION", "sess-env")

	originalResolve := commandResolveSessionIdentifier
	originalReadActive := commandReadActiveSession
	originalEnsure := commandEnsureDaemonRunning
	originalSessionGet := sessionGetRPC
	originalList := commandListRPC

	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-env", nil
	}
	commandReadActiveSession = func() (string, error) {
		return "sess-env", nil
	}
	commandEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	sessionGetRPC = func(context.Context, string, string) (daemon.SessionGetResponse, error) {
		return daemon.SessionGetResponse{SessionID: "sess-env", OriginRoot: t.TempDir()}, nil
	}
	called := false
	commandListRPC = func(context.Context, string, string) (daemon.CommandListResponse, error) {
		called = true
		return daemon.CommandListResponse{}, nil
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		commandReadActiveSession = originalReadActive
		commandEnsureDaemonRunning = originalEnsure
		sessionGetRPC = originalSessionGet
		commandListRPC = originalList
	})

	_, err := executeRootCommandRaw("command", "list")
	if err != nil {
		t.Fatalf("command list with env override returned error: %v", err)
	}
	if !called {
		t.Fatal("command list RPC not called with env active-session override")
	}
}

func TestCommandSubcommandsRejectActiveSessionOutsideWorkspace(t *testing.T) {
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
	originalReadActive := commandReadActiveSession
	originalEnsure := commandEnsureDaemonRunning
	originalSessionGet := sessionGetRPC
	originalRun := commandRunRPC
	originalList := commandListRPC
	originalShow := commandGetRPC
	originalOutput := commandOutputRPC
	originalStop := commandStopRPC

	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	commandReadActiveSession = func() (string, error) {
		return "sess-1", nil
	}
	commandEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	sessionGetRPC = func(context.Context, string, string) (daemon.SessionGetResponse, error) {
		return daemon.SessionGetResponse{SessionID: "sess-1", OriginRoot: t.TempDir()}, nil
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
		commandResolveSessionIdentifier = originalResolve
		commandReadActiveSession = originalReadActive
		commandEnsureDaemonRunning = originalEnsure
		sessionGetRPC = originalSessionGet
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
		{name: "run", args: []string{"command", "run", "--", "echo", "hi"}},
		{name: "list", args: []string{"command", "list"}},
		{name: "show", args: []string{"command", "show", "cmd-1"}},
		{name: "output", args: []string{"command", "output", "cmd-1"}},
		{name: "stop", args: []string{"command", "stop", "cmd-1"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := executeRootCommandRaw(tc.args...)
			if err == nil {
				t.Fatalf("%s returned nil error", tc.name)
			}
			if err.Error() != "Active workspace session belongs to a different workspace; use --session <id-or-name> to override" {
				t.Fatalf("%s error = %q, want workspace mismatch error", tc.name, err.Error())
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
	originalReadActive := commandReadActiveSession
	originalRun := commandRunRPC
	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	commandReadActiveSession = func() (string, error) {
		return "sess-1", nil
	}
	var gotReq daemon.CommandRunRequest
	commandRunRPC = func(_ context.Context, _ string, req daemon.CommandRunRequest) (daemon.CommandRunResponse, error) {
		gotReq = req
		return daemon.CommandRunResponse{CommandID: "cmd-1", Status: "running"}, nil
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		commandReadActiveSession = originalReadActive
		commandRunRPC = originalRun
	})

	out, err := executeRootCommand("command", "run", "--", "go", "test", "./...")
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
	originalReadActive := commandReadActiveSession
	originalList := commandListRPC
	originalShow := commandGetRPC
	originalOutput := commandOutputRPC
	originalStop := commandStopRPC
	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	commandReadActiveSession = func() (string, error) {
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
		commandReadActiveSession = originalReadActive
		commandListRPC = originalList
		commandGetRPC = originalShow
		commandOutputRPC = originalOutput
		commandStopRPC = originalStop
	})

	listOut, err := executeRootCommand("command", "list")
	if err != nil {
		t.Fatalf("execute command list: %v", err)
	}
	if !strings.Contains(listOut, "cmd-1") {
		t.Fatalf("command list output = %q, want command id", listOut)
	}

	showOut, err := executeRootCommand("command", "show", "cmd-1")
	if err != nil {
		t.Fatalf("execute command show: %v", err)
	}
	if !strings.Contains(showOut, "Status: exited") {
		t.Fatalf("command show output = %q, want status", showOut)
	}

	outputOut, err := executeRootCommand("command", "output", "cmd-1")
	if err != nil {
		t.Fatalf("execute command output: %v", err)
	}
	if !strings.Contains(outputOut, "ok") {
		t.Fatalf("command output output = %q, want output content", outputOut)
	}

	stopOut, err := executeRootCommand("command", "stop", "cmd-1")
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
	originalReadActive := commandReadActiveSession
	originalShow := commandGetRPC
	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	commandReadActiveSession = func() (string, error) {
		return "sess-1", nil
	}
	commandGetRPC = func(context.Context, string, string, string) (daemon.CommandGetResponse, error) {
		return daemon.CommandGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.CommandNotFound), Message: "command not found"}
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		commandReadActiveSession = originalReadActive
		commandGetRPC = originalShow
	})

	_, err := executeRootCommand("command", "show", "missing")
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

	originalResolve := commandResolveSessionIdentifier
	originalReadActive := commandReadActiveSession
	originalShow := commandGetRPC
	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-missing", nil
	}
	commandReadActiveSession = func() (string, error) {
		return "sess-missing", nil
	}
	commandGetRPC = func(context.Context, string, string, string) (daemon.CommandGetResponse, error) {
		return daemon.CommandGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "session not found"}
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		commandReadActiveSession = originalReadActive
		commandGetRPC = originalShow
	})

	_, err := executeRootCommand("command", "show", "cmd-1")
	if err == nil {
		t.Fatal("command show returned nil error for missing session")
	}
	if err.Error() != "Session not found" {
		t.Fatalf("command show error = %q, want %q", err.Error(), "Session not found")
	}
}

func TestCommandListUsesSessionFlagOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolve := commandResolveSessionIdentifier
	originalReadActive := commandReadActiveSession
	originalList := commandListRPC

	var gotLookup string
	commandResolveSessionIdentifier = func(_ context.Context, _ string, idOrName string) (string, error) {
		gotLookup = idOrName
		return "sess-override", nil
	}
	commandReadActiveSession = func() (string, error) {
		return "sess-active", nil
	}
	commandListRPC = func(context.Context, string, string) (daemon.CommandListResponse, error) {
		return daemon.CommandListResponse{}, nil
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		commandReadActiveSession = originalReadActive
		commandListRPC = originalList
	})

	_, err := executeRootCommand("command", "list", "--session", "alpha")
	if err != nil {
		t.Fatalf("execute command list with --session: %v", err)
	}
	if gotLookup != "alpha" {
		t.Fatalf("session lookup argument = %q, want %q", gotLookup, "alpha")
	}
}

func TestCommandListRequiresActiveWorkspaceWhenSessionNotProvided(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalReadActive := commandReadActiveSession
	commandReadActiveSession = func() (string, error) {
		return "", nil
	}
	t.Cleanup(func() {
		commandReadActiveSession = originalReadActive
	})

	_, err := executeRootCommand("command", "list")
	if err == nil {
		t.Fatal("command list returned nil error without active session")
	}
	if err.Error() != "No active workspace session is set" {
		t.Fatalf("command list error = %q, want %q", err.Error(), "No active workspace session is set")
	}
}

func TestCommandListMissingActiveSessionDoesNotCallEnsure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalReadActive := commandReadActiveSession
	originalEnsure := commandEnsureDaemonRunning
	commandReadActiveSession = func() (string, error) {
		return "", nil
	}
	commandEnsureDaemonRunning = func(context.Context, *config.Config) error {
		return userFacingError{message: "ensure called unexpectedly"}
	}
	t.Cleanup(func() {
		commandReadActiveSession = originalReadActive
		commandEnsureDaemonRunning = originalEnsure
	})

	_, err := executeRootCommandRaw("command", "list")
	if err == nil {
		t.Fatal("command list returned nil error without active session")
	}
	if err.Error() != "No active workspace session is set" {
		t.Fatalf("command list error = %q, want %q", err.Error(), "No active workspace session is set")
	}
}

func TestCommandSubcommandsUseSessionFlagOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolve := commandResolveSessionIdentifier
	originalReadActive := commandReadActiveSession
	originalRun := commandRunRPC
	originalList := commandListRPC
	originalShow := commandGetRPC
	originalOutput := commandOutputRPC
	originalStop := commandStopRPC

	commandReadActiveSession = func() (string, error) {
		return "", errors.New("active session should not be read when --session is provided")
	}
	commandRunRPC = func(context.Context, string, daemon.CommandRunRequest) (daemon.CommandRunResponse, error) {
		return daemon.CommandRunResponse{CommandID: "cmd-1", Status: "running"}, nil
	}
	commandListRPC = func(context.Context, string, string) (daemon.CommandListResponse, error) {
		return daemon.CommandListResponse{}, nil
	}
	commandGetRPC = func(context.Context, string, string, string) (daemon.CommandGetResponse, error) {
		return daemon.CommandGetResponse{CommandID: "cmd-1", SessionID: "sess-1", Command: "echo", Status: "running", StartedAt: "now"}, nil
	}
	commandOutputRPC = func(context.Context, string, string, string) (daemon.CommandOutputResponse, error) {
		return daemon.CommandOutputResponse{Output: "ok\n"}, nil
	}
	commandStopRPC = func(context.Context, string, string, string) (daemon.CommandStopResponse, error) {
		return daemon.CommandStopResponse{Status: "stopping"}, nil
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		commandReadActiveSession = originalReadActive
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
		{name: "run", args: []string{"command", "run", "--session", "alpha", "--", "echo", "hi"}},
		{name: "list", args: []string{"command", "list", "--session", "alpha"}},
		{name: "show", args: []string{"command", "show", "cmd-1", "--session", "alpha"}},
		{name: "output", args: []string{"command", "output", "cmd-1", "--session", "alpha"}},
		{name: "stop", args: []string{"command", "stop", "cmd-1", "--session", "alpha"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotLookup := ""
			commandResolveSessionIdentifier = func(_ context.Context, _ string, idOrName string) (string, error) {
				gotLookup = idOrName
				return "sess-override", nil
			}

			_, err := executeRootCommand(tc.args...)
			if err != nil {
				t.Fatalf("execute command %s with --session: %v", tc.name, err)
			}
			if gotLookup != "alpha" {
				t.Fatalf("session lookup argument = %q, want %q", gotLookup, "alpha")
			}
		})
	}
}
