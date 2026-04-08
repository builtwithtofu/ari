package daemon

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/client"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
	"github.com/sourcegraph/jsonrpc2"
)

var sessionSmokeStop = func(ctx context.Context, socketPath string) (StopResponse, error) {
	stop := StopResponse{}
	err := tryDaemonMethod(ctx, socketPath, "daemon.stop", StopRequest{}, &stop)
	return stop, err
}

func TestWorkspaceSmokeLifecycleOverRPC(t *testing.T) {
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

	create := WorkspaceCreateResponse{}
	callDaemonMethod(t, socketPath, "workspace.create", WorkspaceCreateRequest{
		Name:          "alpha",
		Folder:        repoRoot,
		OriginRoot:    originRoot,
		CleanupPolicy: "manual",
	}, &create)
	if create.WorkspaceID == "" {
		t.Fatal("workspace.create returned empty workspace_id")
	}
	if create.VCSType != "git" {
		t.Fatalf("session.create vcs_type = %q, want %q", create.VCSType, "git")
	}

	list := WorkspaceListResponse{}
	callDaemonMethod(t, socketPath, "workspace.list", WorkspaceListRequest{}, &list)
	if len(list.Workspaces) != 1 {
		t.Fatalf("workspace.list len = %d, want 1", len(list.Workspaces))
	}
	if list.Workspaces[0].WorkspaceID != create.WorkspaceID {
		t.Fatalf("workspace.list id = %q, want %q", list.Workspaces[0].WorkspaceID, create.WorkspaceID)
	}

	get := WorkspaceGetResponse{}
	callDaemonMethod(t, socketPath, "workspace.get", WorkspaceGetRequest{WorkspaceID: create.WorkspaceID}, &get)
	if get.Name != "alpha" {
		t.Fatalf("session.get name = %q, want %q", get.Name, "alpha")
	}
	if len(get.Folders) != 1 {
		t.Fatalf("session.get folders len = %d, want 1", len(get.Folders))
	}

	suspend := WorkspaceSuspendResponse{}
	callDaemonMethod(t, socketPath, "workspace.suspend", WorkspaceSuspendRequest{WorkspaceID: create.WorkspaceID}, &suspend)
	if suspend.Status != "suspended" {
		t.Fatalf("session.suspend status = %q, want %q", suspend.Status, "suspended")
	}

	resume := WorkspaceResumeResponse{}
	callDaemonMethod(t, socketPath, "workspace.resume", WorkspaceResumeRequest{WorkspaceID: create.WorkspaceID}, &resume)
	if resume.Status != "active" {
		t.Fatalf("session.resume status = %q, want %q", resume.Status, "active")
	}

	closeResp := WorkspaceCloseResponse{}
	callDaemonMethod(t, socketPath, "workspace.close", WorkspaceCloseRequest{WorkspaceID: create.WorkspaceID}, &closeResp)
	if closeResp.Status != "closed" {
		t.Fatalf("session.close status = %q, want %q", closeResp.Status, "closed")
	}

	cli := client.New(socketPath)
	missing := WorkspaceGetResponse{}
	err := cli.Call(context.Background(), "workspace.get", WorkspaceGetRequest{WorkspaceID: "missing"}, &missing)
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

	requestDaemonStop(t, socketPath)

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

func requestDaemonStop(t *testing.T, socketPath string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), boundedTestTimeout(t, 2*time.Second))
	defer cancel()

	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()

	var lastErr error
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("daemon.stop did not succeed before timeout: %v", lastErr)
		case <-ticker.C:
			attemptCtx, attemptCancel := context.WithTimeout(ctx, 350*time.Millisecond)
			stopResp, err := sessionSmokeStop(attemptCtx, socketPath)
			attemptCancel()
			if err != nil {
				lastErr = err
				if isTransientRPCError(err) || isClosedConnectionError(err) {
					if isClosedConnectionError(err) {
						return
					}
					continue
				}
				continue
			}

			if stopResp.Status != "stopping" {
				t.Fatalf("daemon.stop status = %q, want %q", stopResp.Status, "stopping")
			}
			return
		}
	}
}

func isClosedConnectionError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) {
		return true
	}

	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "connection is closed") || strings.Contains(msg, "use of closed network connection")
}

func TestRequestDaemonStopRetriesTransientErrors(t *testing.T) {
	original := sessionSmokeStop
	t.Cleanup(func() {
		sessionSmokeStop = original
	})

	attempts := 0
	sessionSmokeStop = func(context.Context, string) (StopResponse, error) {
		attempts++
		if attempts == 1 {
			return StopResponse{}, context.DeadlineExceeded
		}
		return StopResponse{Status: "stopping"}, nil
	}

	requestDaemonStop(t, "unused")
	if attempts != 2 {
		t.Fatalf("daemon.stop attempts = %d, want 2", attempts)
	}
}

func TestRequestDaemonStopAcceptsClosedConnection(t *testing.T) {
	original := sessionSmokeStop
	t.Cleanup(func() {
		sessionSmokeStop = original
	})

	attempts := 0
	sessionSmokeStop = func(context.Context, string) (StopResponse, error) {
		attempts++
		return StopResponse{}, errors.New("jsonrpc2: connection is closed")
	}

	requestDaemonStop(t, "unused")
	if attempts != 1 {
		t.Fatalf("daemon.stop attempts = %d, want 1", attempts)
	}
}
