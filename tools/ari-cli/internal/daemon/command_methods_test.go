package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
	_ "modernc.org/sqlite"
)

func TestCommandRunOutputAndWaiterPersistence(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerCommandMethods(registry, store); err != nil {
		t.Fatalf("registerCommandMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)

	runResp := callMethod[CommandRunResponse](t, registry, "command.run", CommandRunRequest{
		SessionID: "sess-1",
		Command:   "/bin/sh",
		Args:      []string{"-c", "printf 'hello-output'; exit 7"},
	})

	if runResp.CommandID == "" {
		t.Fatal("command.run returned empty command_id")
	}

	waitForCommandStatus(t, registry, "sess-1", runResp.CommandID, "exited")

	getResp := callMethod[CommandGetResponse](t, registry, "command.get", CommandGetRequest{SessionID: "sess-1", CommandID: runResp.CommandID})
	if getResp.Status != "exited" {
		t.Fatalf("command.get status = %q, want %q", getResp.Status, "exited")
	}
	if getResp.ExitCode == nil || *getResp.ExitCode != 7 {
		t.Fatalf("command.get exit_code = %v, want 7", getResp.ExitCode)
	}

	outputResp := callMethod[CommandOutputResponse](t, registry, "command.output", CommandOutputRequest{SessionID: "sess-1", CommandID: runResp.CommandID})
	if !strings.Contains(outputResp.Output, "hello-output") {
		t.Fatalf("command.output output = %q, want contains %q", outputResp.Output, "hello-output")
	}
}

func TestCommandRunUsesSessionPrimaryFolderAsCWD(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerCommandMethods(registry, store); err != nil {
		t.Fatalf("registerCommandMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)

	runResp := callMethod[CommandRunResponse](t, registry, "command.run", CommandRunRequest{
		SessionID: "sess-1",
		Command:   "/bin/sh",
		Args:      []string{"-c", "pwd"},
	})

	waitForCommandStatus(t, registry, "sess-1", runResp.CommandID, "exited")

	outputResp := callMethod[CommandOutputResponse](t, registry, "command.output", CommandOutputRequest{SessionID: "sess-1", CommandID: runResp.CommandID})
	if !strings.Contains(outputResp.Output, workspace) {
		t.Fatalf("command.output output = %q, want contains workspace %q", outputResp.Output, workspace)
	}
}

func TestCommandRunInvalidSessionStateAndFolderGuards(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, store *globaldb.Store)
	}{
		{
			name: "missing primary folder",
			setup: func(t *testing.T, store *globaldb.Store) {
				t.Helper()
				if err := store.CreateSession(context.Background(), "sess-1", "alpha", t.TempDir(), "manual", "auto"); err != nil {
					t.Fatalf("CreateSession returned error: %v", err)
				}
			},
		},
		{
			name: "closed session",
			setup: func(t *testing.T, store *globaldb.Store) {
				t.Helper()
				workspace := t.TempDir()
				seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)
				if err := store.UpdateSessionStatus(context.Background(), "sess-1", "closed"); err != nil {
					t.Fatalf("UpdateSessionStatus returned error: %v", err)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := newCommandMethodTestStore(t)
			registry := rpc.NewMethodRegistry()
			d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

			if err := d.registerCommandMethods(registry, store); err != nil {
				t.Fatalf("registerCommandMethods returned error: %v", err)
			}

			tc.setup(t, store)

			spec, ok := registry.Get("command.run")
			if !ok {
				t.Fatal("command.run method not registered")
			}

			raw, err := json.Marshal(CommandRunRequest{SessionID: "sess-1", Command: "/bin/sh", Args: []string{"-c", "echo hi"}})
			if err != nil {
				t.Fatalf("marshal params: %v", err)
			}

			_, err = spec.Call(context.Background(), raw)
			if err == nil {
				t.Fatal("command.run returned nil error for invalid session state")
			}

			var rpcErr *rpc.HandlerError
			if !errors.As(err, &rpcErr) {
				t.Fatalf("command.run error type = %T, want *rpc.HandlerError", err)
			}
			if rpcErr.Code != rpc.InvalidParams {
				t.Fatalf("command.run error code = %d, want %d", rpcErr.Code, rpc.InvalidParams)
			}
		})
	}
}

func TestCommandListReturnsCommandsForSession(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerCommandMethods(registry, store); err != nil {
		t.Fatalf("registerCommandMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)
	seedSessionWithPrimaryFolder(t, store, "sess-2", workspace)

	if err := store.CreateCommand(context.Background(), globaldb.CreateCommandParams{CommandID: "cmd-1", SessionID: "sess-1", Command: "echo one", Args: "[]", Status: "running", StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
		t.Fatalf("CreateCommand cmd-1 returned error: %v", err)
	}
	if err := store.CreateCommand(context.Background(), globaldb.CreateCommandParams{CommandID: "cmd-2", SessionID: "sess-2", Command: "echo two", Args: "[]", Status: "running", StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
		t.Fatalf("CreateCommand cmd-2 returned error: %v", err)
	}

	resp := callMethod[CommandListResponse](t, registry, "command.list", CommandListRequest{SessionID: "sess-1"})
	if len(resp.Commands) != 1 {
		t.Fatalf("command.list len = %d, want 1", len(resp.Commands))
	}
	if resp.Commands[0].CommandID != "cmd-1" {
		t.Fatalf("command.list[0].command_id = %q, want %q", resp.Commands[0].CommandID, "cmd-1")
	}
}

func TestCommandStopReturnsStoredStatusForExitedCommand(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerCommandMethods(registry, store); err != nil {
		t.Fatalf("registerCommandMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)

	runResp := callMethod[CommandRunResponse](t, registry, "command.run", CommandRunRequest{
		SessionID: "sess-1",
		Command:   "/bin/sh",
		Args:      []string{"-c", "exit 0"},
	})

	waitForCommandStatus(t, registry, "sess-1", runResp.CommandID, "exited")

	stopResp := callMethod[CommandStopResponse](t, registry, "command.stop", CommandStopRequest{SessionID: "sess-1", CommandID: runResp.CommandID})
	if stopResp.Status != "exited" {
		t.Fatalf("command.stop status = %q, want %q", stopResp.Status, "exited")
	}
}

func waitForCommandStatus(t *testing.T, registry *rpc.MethodRegistry, sessionID, commandID, want string) {
	t.Helper()

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		resp := callMethod[CommandGetResponse](t, registry, "command.get", CommandGetRequest{SessionID: sessionID, CommandID: commandID})
		if resp.Status == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("command %s status did not reach %q before timeout", commandID, want)
}

func seedSessionWithPrimaryFolder(t *testing.T, store *globaldb.Store, sessionID, folder string) {
	t.Helper()

	if err := store.CreateSession(context.Background(), sessionID, sessionID, t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateSession(%s) returned error: %v", sessionID, err)
	}
	if err := store.AddFolder(context.Background(), sessionID, folder, "git", true); err != nil {
		t.Fatalf("AddFolder(%s) returned error: %v", sessionID, err)
	}
}

func newCommandMethodTestStore(t *testing.T) *globaldb.Store {
	t.Helper()

	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	if _, err := db.Exec(`
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
`); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	store, err := globaldb.NewSQLStore(db)
	if err != nil {
		t.Fatalf("NewSQLStore returned error: %v", err)
	}

	return store
}
