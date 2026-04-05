package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sourcegraph/jsonrpc2"
)

var commandSmokeGet = func(ctx context.Context, socketPath, sessionID, commandID string) (CommandGetResponse, error) {
	resp := CommandGetResponse{}
	err := tryDaemonMethod(ctx, socketPath, "command.get", CommandGetRequest{SessionID: sessionID, CommandID: commandID}, &resp)
	return resp, err
}

func TestCommandSmokeLifecycleOverRPC(t *testing.T) {
	stubSessionBootstrap(t)

	socketPath := testSocketPath(t)
	dbPath := filepath.Join(t.TempDir(), "ari.db")
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")
	d := New(socketPath, dbPath, pidPath, "defaults", "defaults", "0.3.0-dev")

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	errCh := make(chan error, 1)
	go func() {
		errCh <- d.Start(ctx)
	}()

	waitForDaemonReady(t, socketPath)

	repoRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatalf("create git root: %v", err)
	}
	originRoot := t.TempDir()

	create := SessionCreateResponse{}
	callDaemonMethod(t, socketPath, "session.create", SessionCreateRequest{
		Name:          "alpha",
		Folder:        repoRoot,
		OriginRoot:    originRoot,
		CleanupPolicy: "manual",
	}, &create)

	run := CommandRunResponse{}
	callDaemonMethod(t, socketPath, "command.run", CommandRunRequest{
		SessionID: create.SessionID,
		Command:   "/bin/sh",
		Args:      []string{"-c", "printf 'smoke-output'; exit 0"},
	}, &run)
	if run.CommandID == "" {
		t.Fatal("command.run returned empty command_id")
	}

	waitForCommandExited(t, socketPath, create.SessionID, run.CommandID)

	list := CommandListResponse{}
	callDaemonMethod(t, socketPath, "command.list", CommandListRequest{SessionID: create.SessionID}, &list)
	if len(list.Commands) == 0 {
		t.Fatal("command.list returned no commands")
	}

	waitForCommandOutputContains(t, socketPath, create.SessionID, run.CommandID, "smoke-output")

	stop := StopResponse{}
	callDaemonMethod(t, socketPath, "daemon.stop", StopRequest{}, &stop)
	if stop.Status != "stopping" {
		t.Fatalf("daemon.stop status = %q, want %q", stop.Status, "stopping")
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("daemon start returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for daemon to exit")
	}
}

func waitForCommandExited(t *testing.T, socketPath, sessionID, commandID string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), boundedTestTimeout(t, 4*time.Second))
	defer cancel()

	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()

	lastStatus := ""
	var lastErr error
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("command %s did not reach exited state before timeout (last status=%q, last error=%v)", commandID, lastStatus, lastErr)
		case <-ticker.C:
			attemptCtx, attemptCancel := context.WithTimeout(ctx, 350*time.Millisecond)
			get, err := commandSmokeGet(attemptCtx, socketPath, sessionID, commandID)
			attemptCancel()
			if err != nil {
				lastErr = err
				if isTransientRPCError(err) {
					continue
				}
				continue
			}

			lastErr = nil
			lastStatus = get.Status
			if get.Status == "exited" {
				return
			}
		}
	}
}

func waitForCommandOutputContains(t *testing.T, socketPath, sessionID, commandID, want string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), boundedTestTimeout(t, 4*time.Second))
	defer cancel()

	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()

	lastOutput := ""
	var lastErr error
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("command %s output did not include %q before timeout (last output=%q, last error=%v)", commandID, want, lastOutput, lastErr)
		case <-ticker.C:
			attemptCtx, attemptCancel := context.WithTimeout(ctx, 350*time.Millisecond)
			output := CommandOutputResponse{}
			err := tryDaemonMethod(attemptCtx, socketPath, "command.output", CommandOutputRequest{SessionID: sessionID, CommandID: commandID}, &output)
			attemptCancel()
			if err != nil {
				lastErr = err
				if isTransientRPCError(err) {
					continue
				}
				continue
			}

			lastErr = nil
			lastOutput = output.Output
			if strings.Contains(output.Output, want) {
				return
			}
		}
	}
}

func waitForDaemonReady(t *testing.T, socketPath string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), boundedTestTimeout(t, 3*time.Second))
	defer cancel()

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	var lastErr error
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("daemon did not become ready before timeout: %v", lastErr)
		case <-ticker.C:
			status := StatusResponse{}
			if err := tryDaemonMethod(ctx, socketPath, "daemon.status", StatusRequest{}, &status); err != nil {
				lastErr = err
				continue
			}
			return
		}
	}
}

func isTransientRPCError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}

	var rpcErr *jsonrpc2.Error
	if errors.As(err, &rpcErr) && rpcErr.Code == int64(jsonrpc2.CodeInternalError) {
		return true
	}

	return false
}

func boundedTestTimeout(t *testing.T, max time.Duration) time.Duration {
	t.Helper()

	if deadline, ok := t.Deadline(); ok {
		remaining := time.Until(deadline) - 250*time.Millisecond
		if remaining < 100*time.Millisecond {
			return 100 * time.Millisecond
		}
		if remaining < max {
			return remaining
		}
	}

	return max
}

func TestWaitForCommandExitedRetriesTransientInternalError(t *testing.T) {
	original := commandSmokeGet
	t.Cleanup(func() {
		commandSmokeGet = original
	})

	attempts := 0
	commandSmokeGet = func(context.Context, string, string, string) (CommandGetResponse, error) {
		attempts++
		switch attempts {
		case 1:
			return CommandGetResponse{}, &jsonrpc2.Error{Code: int64(jsonrpc2.CodeInternalError), Message: "Internal error"}
		case 2:
			return CommandGetResponse{Status: "running"}, nil
		default:
			return CommandGetResponse{Status: "exited"}, nil
		}
	}

	waitForCommandExited(t, "unused", "sess-1", "cmd-1")
	if attempts < 3 {
		t.Fatalf("command.get attempts = %d, want at least 3", attempts)
	}
}
