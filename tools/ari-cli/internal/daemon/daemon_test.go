package daemon

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/sourcegraph/jsonrpc2"
)

func TestDaemonStatusAndStopOverRPC(t *testing.T) {
	stubBootstrap(t)

	socketPath := testSocketPath(t)
	dbPath := filepath.Join(t.TempDir(), "ari.db")
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")
	d := New(socketPath, dbPath, pidPath, "defaults", "defaults", "test-version")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- d.Start(ctx)
	}()

	var status StatusResponse
	callDaemonMethod(t, socketPath, "daemon.status", StatusRequest{}, &status)

	if status.Version != "test-version" {
		t.Fatalf("unexpected version: %q", status.Version)
	}

	if status.PID <= 0 {
		t.Fatalf("expected pid, got %d", status.PID)
	}

	if status.SocketPath != socketPath {
		t.Fatalf("unexpected socket path: %q", status.SocketPath)
	}
	if status.DatabasePath != dbPath {
		t.Fatalf("unexpected database path: %q", status.DatabasePath)
	}
	if status.DatabaseState != "healthy" {
		t.Fatalf("unexpected database state: %q", status.DatabaseState)
	}
	if status.ConfigPath != "defaults" {
		t.Fatalf("unexpected config path: %q", status.ConfigPath)
	}
	if status.ConfigSource != "defaults" {
		t.Fatalf("unexpected config source: %q", status.ConfigSource)
	}

	var stop StopResponse
	callDaemonMethod(t, socketPath, "daemon.stop", StopRequest{}, &stop)

	if stop.Status != "stopping" {
		t.Fatalf("unexpected stop status: %q", stop.Status)
	}

	if err := <-errCh; err != nil {
		t.Fatalf("daemon start returned error: %v", err)
	}

	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("socket path stat error = %v, want removed after stop", err)
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("pid path stat error = %v, want removed after stop", err)
	}
}

func TestDaemonConcurrentStartDoesNotLeakSocketBindError(t *testing.T) {
	stubBootstrap(t)

	socketPath := testSocketPath(t)
	dbPath := filepath.Join(t.TempDir(), "ari.db")
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")
	d := New(socketPath, dbPath, pidPath, "defaults", "defaults", "test-version")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const workers = 8
	errCh := make(chan error, workers)
	startBarrier := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-startBarrier
			errCh <- d.Start(ctx)
		}()
	}

	close(startBarrier)
	time.Sleep(100 * time.Millisecond)
	cancel()
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err == nil || errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "daemon is already running") {
			continue
		}
		if strings.Contains(err.Error(), "listen on unix socket") {
			t.Fatalf("unexpected socket bind race error: %v", err)
		}
	}
}

func TestDaemonDefaultVersionIsDev(t *testing.T) {
	d := New("/tmp/ari-daemon.sock", "/tmp/ari.db", "/tmp/ari-daemon.pid", "defaults", "defaults", "")

	status := d.status()
	if status.Version != "dev" {
		t.Fatalf("unexpected default version: %q", status.Version)
	}
}

func TestDaemonStartAlreadyRunningSkipsBootstrapSideEffects(t *testing.T) {
	socketPath := testSocketPath(t)
	dbPath := filepath.Join(t.TempDir(), "ari.db")
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")
	d := New(socketPath, dbPath, pidPath, "defaults", "defaults", "test-version")

	original := bootstrapDatabase
	var bootstrapCalls int32
	bootstrapDatabase = func(_ context.Context, dbPath string) error {
		atomic.AddInt32(&bootstrapCalls, 1)
		if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
			return err
		}
		dbConn, err := sql.Open("sqlite", dbPath)
		if err != nil {
			return err
		}
		defer func() {
			_ = dbConn.Close()
		}()
		_, err = dbConn.Exec(`CREATE TABLE IF NOT EXISTS daemon_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`)
		return err
	}
	t.Cleanup(func() {
		bootstrapDatabase = original
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- d.Start(ctx)
	}()

	callDaemonMethod(t, socketPath, "daemon.status", StatusRequest{}, &StatusResponse{})

	err := d.Start(context.Background())
	if err == nil {
		t.Fatal("second Start returned nil error")
	}
	if !strings.Contains(err.Error(), "daemon is already running") {
		t.Fatalf("second Start error = %v, want already running", err)
	}
	if got := atomic.LoadInt32(&bootstrapCalls); got != 1 {
		t.Fatalf("bootstrap call count = %d, want 1", got)
	}

	d.Stop()
	if err := <-errCh; err != nil {
		t.Fatalf("first Start returned error: %v", err)
	}
}

func TestDaemonStopResetsStartTime(t *testing.T) {
	stubBootstrap(t)

	socketPath := testSocketPath(t)
	dbPath := filepath.Join(t.TempDir(), "ari.db")
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")
	d := New(socketPath, dbPath, pidPath, "defaults", "defaults", "test-version")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- d.Start(ctx)
	}()

	callDaemonMethod(t, socketPath, "daemon.status", StatusRequest{}, &StatusResponse{})
	d.Stop()

	if err := <-errCh; err != nil {
		t.Fatalf("daemon start returned error: %v", err)
	}

	d.mu.RLock()
	startedAt := d.startedAt
	d.mu.RUnlock()

	if !startedAt.IsZero() {
		t.Fatalf("daemon startedAt = %v, want zero after stop", startedAt)
	}
}

func TestDaemonStartWritesAndRemovesPIDFile(t *testing.T) {
	stubBootstrap(t)

	socketPath := testSocketPath(t)
	dbPath := filepath.Join(t.TempDir(), "ari.db")
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")
	d := New(socketPath, dbPath, pidPath, "defaults", "defaults", "test-version")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- d.Start(ctx)
	}()

	callDaemonMethod(t, socketPath, "daemon.status", StatusRequest{}, &StatusResponse{})

	pid, err := ReadPIDFile(pidPath)
	if err != nil {
		t.Fatalf("ReadPIDFile returned error: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("pid in file = %d, want positive", pid)
	}

	d.Stop()
	if err := <-errCh; err != nil {
		t.Fatalf("daemon start returned error: %v", err)
	}

	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("pid path stat error = %v, want removed on shutdown", err)
	}
}

func TestDaemonSignalChannelTriggersShutdownAndRemovesPIDFile(t *testing.T) {
	stubBootstrap(t)

	socketPath := testSocketPath(t)
	dbPath := filepath.Join(t.TempDir(), "ari.db")
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")
	sigCh := make(chan os.Signal, 1)
	d := NewWithSignalChannel(socketPath, dbPath, pidPath, "defaults", "defaults", "test-version", sigCh)

	errCh := make(chan error, 1)
	go func() {
		errCh <- d.Start(context.Background())
	}()

	callDaemonMethod(t, socketPath, "daemon.status", StatusRequest{}, &StatusResponse{})

	if _, err := os.Stat(pidPath); err != nil {
		t.Fatalf("pid path stat before signal = %v", err)
	}

	sigCh <- syscall.SIGTERM

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("daemon start returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for daemon shutdown from signal")
	}

	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("pid path stat after signal = %v, want removed", err)
	}
}

func callDaemonMethod(t *testing.T, socketPath, method string, params any, result any) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var dialErr error
	var conn net.Conn
	for i := 0; i < 50; i++ {
		conn, dialErr = net.Dial("unix", socketPath)
		if dialErr == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if dialErr != nil {
		t.Fatalf("dial daemon socket: %v", dialErr)
	}
	defer func() {
		_ = conn.Close()
	}()

	//nolint:staticcheck // Ari standardizes on PlainObjectCodec framing for local RPC.
	stream := jsonrpc2.NewBufferedStream(conn, jsonrpc2.PlainObjectCodec{})
	rpcConn := jsonrpc2.NewConn(ctx, stream, jsonrpc2.HandlerWithError(func(context.Context, *jsonrpc2.Conn, *jsonrpc2.Request) (interface{}, error) {
		return nil, nil
	}))
	defer func() {
		_ = rpcConn.Close()
	}()

	if err := rpcConn.Call(ctx, method, params, result); err != nil {
		t.Fatalf("call %s: %v", method, err)
	}
}

func testSocketPath(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("/tmp", "ari-daemon-")
	if err != nil {
		t.Fatalf("create temp socket dir: %v", err)
	}

	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})

	return filepath.Join(dir, "s.sock")
}

func stubBootstrap(t *testing.T) {
	t.Helper()

	original := bootstrapDatabase
	bootstrapDatabase = func(_ context.Context, dbPath string) error {
		if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
			return err
		}

		dbConn, err := sql.Open("sqlite", dbPath)
		if err != nil {
			return err
		}
		defer func() {
			_ = dbConn.Close()
		}()

		if _, err := dbConn.Exec(`CREATE TABLE IF NOT EXISTS daemon_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
			return err
		}

		return nil
	}

	t.Cleanup(func() {
		bootstrapDatabase = original
	})
}
