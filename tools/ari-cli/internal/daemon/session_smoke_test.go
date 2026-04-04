package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/client"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
	"github.com/sourcegraph/jsonrpc2"
)

func TestSessionSmokeLifecycleOverRPC(t *testing.T) {
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
	if create.SessionID == "" {
		t.Fatal("session.create returned empty session_id")
	}
	if create.VCSType != "git" {
		t.Fatalf("session.create vcs_type = %q, want %q", create.VCSType, "git")
	}

	list := SessionListResponse{}
	callDaemonMethod(t, socketPath, "session.list", SessionListRequest{}, &list)
	if len(list.Sessions) != 1 {
		t.Fatalf("session.list len = %d, want 1", len(list.Sessions))
	}
	if list.Sessions[0].SessionID != create.SessionID {
		t.Fatalf("session.list id = %q, want %q", list.Sessions[0].SessionID, create.SessionID)
	}

	get := SessionGetResponse{}
	callDaemonMethod(t, socketPath, "session.get", SessionGetRequest{SessionID: create.SessionID}, &get)
	if get.Name != "alpha" {
		t.Fatalf("session.get name = %q, want %q", get.Name, "alpha")
	}
	if len(get.Folders) != 1 {
		t.Fatalf("session.get folders len = %d, want 1", len(get.Folders))
	}

	suspend := SessionSuspendResponse{}
	callDaemonMethod(t, socketPath, "session.suspend", SessionSuspendRequest{SessionID: create.SessionID}, &suspend)
	if suspend.Status != "suspended" {
		t.Fatalf("session.suspend status = %q, want %q", suspend.Status, "suspended")
	}

	resume := SessionResumeResponse{}
	callDaemonMethod(t, socketPath, "session.resume", SessionResumeRequest{SessionID: create.SessionID}, &resume)
	if resume.Status != "active" {
		t.Fatalf("session.resume status = %q, want %q", resume.Status, "active")
	}

	closeResp := SessionCloseResponse{}
	callDaemonMethod(t, socketPath, "session.close", SessionCloseRequest{SessionID: create.SessionID}, &closeResp)
	if closeResp.Status != "closed" {
		t.Fatalf("session.close status = %q, want %q", closeResp.Status, "closed")
	}

	cli := client.New(socketPath)
	missing := SessionGetResponse{}
	err := cli.Call(context.Background(), "session.get", SessionGetRequest{SessionID: "missing"}, &missing)
	if err == nil {
		t.Fatal("session.get missing returned nil error")
	}

	var rpcErr *jsonrpc2.Error
	if !errors.As(err, &rpcErr) {
		t.Fatalf("session.get missing error type = %T, want *jsonrpc2.Error", err)
	}
	if rpcErr.Code != int64(rpc.SessionNotFound) {
		t.Fatalf("session.get missing error code = %d, want %d", rpcErr.Code, rpc.SessionNotFound)
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

func stubSessionBootstrap(t *testing.T) {
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
