package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDaemonSmokeLifecycle(t *testing.T) {
	stubBootstrap(t)

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

	status := StatusResponse{}
	callDaemonMethod(t, socketPath, "daemon.status", StatusRequest{}, &status)

	pid, err := ReadPIDFile(pidPath)
	if err != nil {
		t.Fatalf("ReadPIDFile returned error: %v", err)
	}
	if pid != status.PID {
		t.Fatalf("pid file value = %d, want daemon pid %d", pid, status.PID)
	}

	if status.Version != "0.3.0-dev" {
		t.Fatalf("status version = %q, want 0.3.0-dev", status.Version)
	}
	if status.PID <= 0 {
		t.Fatalf("status pid = %d, want positive", status.PID)
	}
	if status.UptimeSeconds < 0 {
		t.Fatalf("status uptime = %d, want non-negative", status.UptimeSeconds)
	}
	if status.SocketPath != socketPath {
		t.Fatalf("status socket = %q, want %q", status.SocketPath, socketPath)
	}
	if status.DatabasePath != dbPath {
		t.Fatalf("status db path = %q, want %q", status.DatabasePath, dbPath)
	}
	if status.DatabaseState != "healthy" {
		t.Fatalf("status db state = %q, want healthy", status.DatabaseState)
	}

	stop := StopResponse{}
	stopErr := tryDaemonMethod(context.Background(), socketPath, "daemon.stop", StopRequest{}, &stop)
	if stopErr == nil && stop.Status != "stopping" {
		t.Fatalf("stop status = %q, want stopping", stop.Status)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("daemon start returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		if stopErr != nil {
			t.Fatalf("daemon.stop returned %v and daemon did not exit", stopErr)
		}
		t.Fatal("timed out waiting for daemon to exit")
	}

	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("socket path stat error = %v, want removed", err)
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("pid path stat error = %v, want removed", err)
	}
}
