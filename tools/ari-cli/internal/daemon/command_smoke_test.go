package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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

	output := CommandOutputResponse{}
	callDaemonMethod(t, socketPath, "command.output", CommandOutputRequest{SessionID: create.SessionID, CommandID: run.CommandID}, &output)
	if !strings.Contains(output.Output, "smoke-output") {
		t.Fatalf("command.output = %q, want contains %q", output.Output, "smoke-output")
	}

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

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		get := CommandGetResponse{}
		callDaemonMethod(t, socketPath, "command.get", CommandGetRequest{SessionID: sessionID, CommandID: commandID}, &get)
		if get.Status == "exited" {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}

	t.Fatalf("command %s did not reach exited state before timeout", commandID)
}
