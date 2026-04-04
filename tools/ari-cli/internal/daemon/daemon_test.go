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
	bootstrapDatabase = func(ctx context.Context, dbPath string) error {
		_ = ctx
		atomic.AddInt32(&bootstrapCalls, 1)
		return applyMigrationSQLFiles(dbPath)
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

func TestDaemonStartMarksRunningCommandsLost(t *testing.T) {
	socketPath := testSocketPath(t)
	dbPath := filepath.Join(t.TempDir(), "ari.db")
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	if _, err := db.Exec(`
CREATE TABLE daemon_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE sessions (
	session_id TEXT PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	status TEXT NOT NULL DEFAULT 'active',
	vcs_preference TEXT NOT NULL DEFAULT 'auto',
	origin_root TEXT NOT NULL,
	cleanup_policy TEXT NOT NULL DEFAULT 'manual',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE TABLE session_folders (
	session_id TEXT NOT NULL,
	folder_path TEXT NOT NULL,
	vcs_type TEXT NOT NULL DEFAULT 'unknown',
	is_primary INTEGER NOT NULL DEFAULT 0,
	added_at TEXT NOT NULL,
	PRIMARY KEY (session_id, folder_path),
	FOREIGN KEY(session_id) REFERENCES sessions(session_id) ON DELETE CASCADE
);
CREATE TABLE commands (
	command_id TEXT PRIMARY KEY,
	session_id TEXT NOT NULL,
	command TEXT NOT NULL,
	args TEXT NOT NULL DEFAULT '[]',
	status TEXT NOT NULL DEFAULT 'running',
	exit_code INTEGER,
	started_at TEXT NOT NULL,
	finished_at TEXT,
	FOREIGN KEY(session_id) REFERENCES sessions(session_id) ON DELETE CASCADE
);
CREATE TABLE agents (
	agent_id TEXT PRIMARY KEY,
	session_id TEXT NOT NULL,
	name TEXT,
	command TEXT NOT NULL,
	args TEXT NOT NULL DEFAULT '[]',
	status TEXT NOT NULL DEFAULT 'running',
	exit_code INTEGER,
	started_at TEXT NOT NULL,
	stopped_at TEXT,
	FOREIGN KEY(session_id) REFERENCES sessions(session_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX agents_session_id_name_uq
	ON agents (session_id, name)
	WHERE name IS NOT NULL;
INSERT INTO sessions (session_id, name, status, vcs_preference, origin_root, cleanup_policy, created_at, updated_at)
VALUES ('sess-1', 'alpha', 'active', 'auto', '/tmp', 'manual', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
INSERT INTO commands (command_id, session_id, command, args, status, started_at)
VALUES ('cmd-1', 'sess-1', 'sleep 30', '[]', 'running', '2026-01-01T00:00:00Z');
`); err != nil {
		_ = db.Close()
		t.Fatalf("seed schema: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}

	originalBootstrap := bootstrapDatabase
	bootstrapDatabase = func(_ context.Context, _ string) error { return nil }
	t.Cleanup(func() {
		bootstrapDatabase = originalBootstrap
	})

	d := New(socketPath, dbPath, pidPath, "defaults", "defaults", "test-version")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- d.Start(ctx)
	}()

	callDaemonMethod(t, socketPath, "daemon.status", StatusRequest{}, &StatusResponse{})

	verifyDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		d.Stop()
		<-errCh
		t.Fatalf("open verify db: %v", err)
	}
	defer func() {
		_ = verifyDB.Close()
	}()

	var status string
	if err := verifyDB.QueryRow(`SELECT status FROM commands WHERE command_id = 'cmd-1'`).Scan(&status); err != nil {
		d.Stop()
		<-errCh
		t.Fatalf("query command status: %v", err)
	}
	if status != "lost" {
		d.Stop()
		<-errCh
		t.Fatalf("command status = %q, want %q", status, "lost")
	}

	d.Stop()
	if err := <-errCh; err != nil {
		t.Fatalf("daemon start returned error: %v", err)
	}
}

func TestDaemonStartMarksRunningAgentsLost(t *testing.T) {
	socketPath := testSocketPath(t)
	dbPath := filepath.Join(t.TempDir(), "ari.db")
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")

	if err := applyMigrationSQLFiles(dbPath); err != nil {
		t.Fatalf("apply migration files: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	if _, err := db.Exec(`
INSERT INTO sessions (session_id, name, status, vcs_preference, origin_root, cleanup_policy, created_at, updated_at)
VALUES ('sess-1', 'alpha', 'active', 'auto', '/tmp', 'manual', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
INSERT INTO agents (agent_id, session_id, name, command, args, status, started_at)
VALUES ('agt-1', 'sess-1', 'claude', 'claude-code', '[]', 'running', '2026-01-01T00:00:00Z');
`); err != nil {
		_ = db.Close()
		t.Fatalf("seed agent row: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}

	originalBootstrap := bootstrapDatabase
	bootstrapDatabase = func(_ context.Context, _ string) error { return nil }
	t.Cleanup(func() {
		bootstrapDatabase = originalBootstrap
	})

	d := New(socketPath, dbPath, pidPath, "defaults", "defaults", "test-version")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- d.Start(ctx)
	}()

	callDaemonMethod(t, socketPath, "daemon.status", StatusRequest{}, &StatusResponse{})

	verifyDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		d.Stop()
		<-errCh
		t.Fatalf("open verify db: %v", err)
	}
	defer func() {
		_ = verifyDB.Close()
	}()

	var status string
	if err := verifyDB.QueryRow(`SELECT status FROM agents WHERE agent_id = 'agt-1'`).Scan(&status); err != nil {
		d.Stop()
		<-errCh
		t.Fatalf("query agent status: %v", err)
	}
	if status != "lost" {
		d.Stop()
		<-errCh
		t.Fatalf("agent status = %q, want %q", status, "lost")
	}

	d.Stop()
	if err := <-errCh; err != nil {
		t.Fatalf("daemon start returned error: %v", err)
	}
}

func callDaemonMethod(t *testing.T, socketPath, method string, params any, result any) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := tryDaemonMethod(ctx, socketPath, method, params, result); err != nil {
		t.Fatalf("call %s: %v", method, err)
	}
}

func tryDaemonMethod(ctx context.Context, socketPath, method string, params any, result any) error {
	if ctx == nil {
		ctx = context.Background()
	}

	var dialErr error
	var conn net.Conn
	dialer := &net.Dialer{}
	for i := 0; i < 50; i++ {
		conn, dialErr = dialer.DialContext(ctx, "unix", socketPath)
		if dialErr == nil {
			break
		}
		select {
		case <-ctx.Done():
			return dialErr
		case <-time.After(10 * time.Millisecond):
		}
	}

	if dialErr != nil {
		return dialErr
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
		return err
	}

	return nil
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
	bootstrapDatabase = func(ctx context.Context, dbPath string) error {
		_ = ctx
		return applyMigrationSQLFiles(dbPath)
	}

	t.Cleanup(func() {
		bootstrapDatabase = original
	})
}
