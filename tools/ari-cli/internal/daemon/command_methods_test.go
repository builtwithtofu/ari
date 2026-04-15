package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/process"
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
		WorkspaceID: "sess-1",
		Command:     "/bin/sh",
		Args:        []string{"-c", "printf 'hello-output'; exit 7"},
	})

	if runResp.CommandID == "" {
		t.Fatal("command.run returned empty command_id")
	}

	waitForCommandStatus(t, registry, "sess-1", runResp.CommandID, "exited")

	getResp := callMethod[CommandGetResponse](t, registry, "command.get", CommandGetRequest{WorkspaceID: "sess-1", CommandID: runResp.CommandID})
	if getResp.Status != "exited" {
		t.Fatalf("command.get status = %q, want %q", getResp.Status, "exited")
	}
	if getResp.ExitCode == nil || *getResp.ExitCode != 7 {
		t.Fatalf("command.get exit_code = %v, want 7", getResp.ExitCode)
	}

	outputResp := callMethod[CommandOutputResponse](t, registry, "command.output", CommandOutputRequest{WorkspaceID: "sess-1", CommandID: runResp.CommandID})
	if !strings.Contains(outputResp.Output, "hello-output") {
		t.Fatalf("command.output output = %q, want contains %q", outputResp.Output, "hello-output")
	}
}

func TestCommandOutputPrefersRetainedSnapshotForExitedCommand(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerCommandMethods(registry, store); err != nil {
		t.Fatalf("registerCommandMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)

	if err := store.CreateCommand(context.Background(), globaldb.CreateCommandParams{
		CommandID:   "cmd-1",
		WorkspaceID: "sess-1",
		Command:     "echo hi",
		Args:        `[]`,
		Status:      "exited",
		StartedAt:   time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("CreateCommand returned error: %v", err)
	}

	d.setCommandOutput("cmd-1", "retained-output")
	d.setCommandProcess("cmd-1", &process.Process{})

	resp := callMethod[CommandOutputResponse](t, registry, "command.output", CommandOutputRequest{WorkspaceID: "sess-1", CommandID: "cmd-1"})
	if resp.Output != "retained-output" {
		t.Fatalf("command.output = %q, want %q", resp.Output, "retained-output")
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
		WorkspaceID: "sess-1",
		Command:     "/bin/sh",
		Args:        []string{"-c", "pwd"},
	})

	waitForCommandStatus(t, registry, "sess-1", runResp.CommandID, "exited")

	outputResp := callMethod[CommandOutputResponse](t, registry, "command.output", CommandOutputRequest{WorkspaceID: "sess-1", CommandID: runResp.CommandID})
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

			raw, err := json.Marshal(CommandRunRequest{WorkspaceID: "sess-1", Command: "/bin/sh", Args: []string{"-c", "echo hi"}})
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

	if err := store.CreateCommand(context.Background(), globaldb.CreateCommandParams{CommandID: "cmd-1", WorkspaceID: "sess-1", Command: "echo one", Args: "[]", Status: "running", StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
		t.Fatalf("CreateCommand cmd-1 returned error: %v", err)
	}
	if err := store.CreateCommand(context.Background(), globaldb.CreateCommandParams{CommandID: "cmd-2", WorkspaceID: "sess-2", Command: "echo two", Args: "[]", Status: "running", StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
		t.Fatalf("CreateCommand cmd-2 returned error: %v", err)
	}

	resp := callMethod[CommandListResponse](t, registry, "command.list", CommandListRequest{WorkspaceID: "sess-1"})
	if len(resp.Commands) != 1 {
		t.Fatalf("command.list len = %d, want 1", len(resp.Commands))
	}
	if resp.Commands[0].CommandID != "cmd-1" {
		t.Fatalf("command.list[0].command_id = %q, want %q", resp.Commands[0].CommandID, "cmd-1")
	}
}

func TestCommandListRejectsMalformedStoredArgs(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerCommandMethods(registry, store); err != nil {
		t.Fatalf("registerCommandMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)

	if err := store.CreateCommand(context.Background(), globaldb.CreateCommandParams{CommandID: "cmd-bad", WorkspaceID: "sess-1", Command: "echo one", Args: `{"bad":true}`, Status: "running", StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
		t.Fatalf("CreateCommand cmd-bad returned error: %v", err)
	}

	spec, ok := registry.Get("command.list")
	if !ok {
		t.Fatal("command.list method not registered")
	}
	raw, err := json.Marshal(CommandListRequest{WorkspaceID: "sess-1"})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	_, err = spec.Call(context.Background(), raw)
	if err == nil {
		t.Fatal("command.list returned nil error for malformed args")
	}
	if !strings.Contains(err.Error(), "map command record to tool: decode command args") {
		t.Fatalf("command.list error = %q, want decode args mapping error", err.Error())
	}
}

func TestCommandGetRejectsMalformedStoredArgs(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerCommandMethods(registry, store); err != nil {
		t.Fatalf("registerCommandMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)

	if err := store.CreateCommand(context.Background(), globaldb.CreateCommandParams{CommandID: "cmd-bad", WorkspaceID: "sess-1", Command: "echo one", Args: `{"bad":true}`, Status: "running", StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
		t.Fatalf("CreateCommand cmd-bad returned error: %v", err)
	}

	spec, ok := registry.Get("command.get")
	if !ok {
		t.Fatal("command.get method not registered")
	}
	raw, err := json.Marshal(CommandGetRequest{WorkspaceID: "sess-1", CommandID: "cmd-bad"})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	_, err = spec.Call(context.Background(), raw)
	if err == nil {
		t.Fatal("command.get returned nil error for malformed args")
	}
	if !strings.Contains(err.Error(), "map command record to tool: decode command args") {
		t.Fatalf("command.get error = %q, want decode args mapping error", err.Error())
	}
}

func TestWorkspaceCommandDefinitionMethodsLifecycle(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerCommandMethods(registry, store); err != nil {
		t.Fatalf("registerCommandMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)

	createResp := callMethod[WorkspaceCommandCreateResponse](t, registry, "workspace.command.create", WorkspaceCommandCreateRequest{
		WorkspaceID: "sess-1",
		Name:        "test",
		Command:     "go",
		Args:        []string{"test", "./..."},
	})
	if createResp.CommandID == "" {
		t.Fatal("workspace.command.create returned empty command_id")
	}
	if createResp.Name != "test" {
		t.Fatalf("workspace.command.create name = %q, want %q", createResp.Name, "test")
	}

	listResp := callMethod[WorkspaceCommandListResponse](t, registry, "workspace.command.list", WorkspaceCommandListRequest{WorkspaceID: "sess-1"})
	if len(listResp.Commands) != 1 {
		t.Fatalf("workspace.command.list len = %d, want 1", len(listResp.Commands))
	}
	if listResp.Commands[0].Name != "test" {
		t.Fatalf("workspace.command.list[0].name = %q, want %q", listResp.Commands[0].Name, "test")
	}
	if len(listResp.Commands[0].Args) != 2 || listResp.Commands[0].Args[0] != "test" || listResp.Commands[0].Args[1] != "./..." {
		t.Fatalf("workspace.command.list[0].args = %#v, want [test ./...]", listResp.Commands[0].Args)
	}

	getResp := callMethod[WorkspaceCommandGetResponse](t, registry, "workspace.command.get", WorkspaceCommandGetRequest{WorkspaceID: "sess-1", CommandIDOrName: createResp.CommandID})
	if getResp.CommandID != createResp.CommandID {
		t.Fatalf("workspace.command.get command_id = %q, want %q", getResp.CommandID, createResp.CommandID)
	}
	getByNameResp := callMethod[WorkspaceCommandGetResponse](t, registry, "workspace.command.get", WorkspaceCommandGetRequest{WorkspaceID: "sess-1", CommandIDOrName: "test"})
	if getByNameResp.CommandID != createResp.CommandID {
		t.Fatalf("workspace.command.get by name command_id = %q, want %q", getByNameResp.CommandID, createResp.CommandID)
	}

	removeResp := callMethod[WorkspaceCommandRemoveResponse](t, registry, "workspace.command.remove", WorkspaceCommandRemoveRequest{WorkspaceID: "sess-1", CommandIDOrName: createResp.CommandID})
	if removeResp.Status != "removed" {
		t.Fatalf("workspace.command.remove status = %q, want %q", removeResp.Status, "removed")
	}

	listAfter := callMethod[WorkspaceCommandListResponse](t, registry, "workspace.command.list", WorkspaceCommandListRequest{WorkspaceID: "sess-1"})
	if len(listAfter.Commands) != 0 {
		t.Fatalf("workspace.command.list after remove len = %d, want 0", len(listAfter.Commands))
	}
}

func TestWorkspaceCommandDefinitionCreateRejectsNameThatCollidesWithID(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerCommandMethods(registry, store); err != nil {
		t.Fatalf("registerCommandMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)

	createResp := callMethod[WorkspaceCommandCreateResponse](t, registry, "workspace.command.create", WorkspaceCommandCreateRequest{
		WorkspaceID: "sess-1",
		Name:        "first",
		Command:     "go",
		Args:        []string{"test", "./..."},
	})

	spec, ok := registry.Get("workspace.command.create")
	if !ok {
		t.Fatal("workspace.command.create method not registered")
	}
	raw, err := json.Marshal(WorkspaceCommandCreateRequest{
		WorkspaceID: "sess-1",
		Name:        createResp.CommandID,
		Command:     "go",
		Args:        []string{"test", "./..."},
	})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	_, err = spec.Call(context.Background(), raw)
	if err == nil {
		t.Fatal("workspace.command.create returned nil error for colliding name")
	}
	var rpcErr *rpc.HandlerError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("workspace.command.create error type = %T, want *rpc.HandlerError", err)
	}
	if rpcErr.Code != rpc.InvalidParams {
		t.Fatalf("workspace.command.create error code = %d, want %d", rpcErr.Code, rpc.InvalidParams)
	}
}

func TestWorkspaceCommandDefinitionCreateDefaultsMissingArgsToEmptyList(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerCommandMethods(registry, store); err != nil {
		t.Fatalf("registerCommandMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)

	resp := callMethod[WorkspaceCommandCreateResponse](t, registry, "workspace.command.create", WorkspaceCommandCreateRequest{
		WorkspaceID: "sess-1",
		Name:        "test",
		Command:     "go",
		Args:        nil,
	})
	if len(resp.Args) != 0 {
		t.Fatalf("workspace.command.create args len = %d, want 0", len(resp.Args))
	}
}

func TestWorkspaceCommandDefinitionCreateRejectsDuplicateName(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerCommandMethods(registry, store); err != nil {
		t.Fatalf("registerCommandMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)

	_ = callMethod[WorkspaceCommandCreateResponse](t, registry, "workspace.command.create", WorkspaceCommandCreateRequest{
		WorkspaceID: "sess-1",
		Name:        "test",
		Command:     "go",
		Args:        []string{"test", "./..."},
	})

	spec, ok := registry.Get("workspace.command.create")
	if !ok {
		t.Fatal("workspace.command.create method not registered")
	}
	raw, err := json.Marshal(WorkspaceCommandCreateRequest{
		WorkspaceID: "sess-1",
		Name:        "test",
		Command:     "go",
		Args:        []string{"test", "./..."},
	})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	_, err = spec.Call(context.Background(), raw)
	if err == nil {
		t.Fatal("workspace.command.create returned nil error for duplicate name")
	}
	var rpcErr *rpc.HandlerError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("workspace.command.create error type = %T, want *rpc.HandlerError", err)
	}
	if rpcErr.Code != rpc.InvalidParams {
		t.Fatalf("workspace.command.create error code = %d, want %d", rpcErr.Code, rpc.InvalidParams)
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
		WorkspaceID: "sess-1",
		Command:     "/bin/sh",
		Args:        []string{"-c", "exit 0"},
	})

	waitForCommandStatus(t, registry, "sess-1", runResp.CommandID, "exited")

	stopResp := callMethod[CommandStopResponse](t, registry, "command.stop", CommandStopRequest{WorkspaceID: "sess-1", CommandID: runResp.CommandID})
	if stopResp.Status != "exited" {
		t.Fatalf("command.stop status = %q, want %q", stopResp.Status, "exited")
	}
}

func TestCommandStopReturnsWithoutWaitingForStopPath(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerCommandMethods(registry, store); err != nil {
		t.Fatalf("registerCommandMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)

	runResp := callMethod[CommandRunResponse](t, registry, "command.run", CommandRunRequest{
		WorkspaceID: "sess-1",
		Command:     "/bin/sh",
		Args:        []string{"-c", "while true; do sleep 1; done"},
	})

	originalStop := stopCommandProcess
	stopCommandProcess = func(*process.Process) error {
		time.Sleep(300 * time.Millisecond)
		return nil
	}
	t.Cleanup(func() {
		stopCommandProcess = originalStop
	})

	start := time.Now()
	stopResp := callMethod[CommandStopResponse](t, registry, "command.stop", CommandStopRequest{WorkspaceID: "sess-1", CommandID: runResp.CommandID})
	if stopResp.Status != "stopping" {
		t.Fatalf("command.stop status = %q, want %q", stopResp.Status, "stopping")
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("command.stop elapsed = %s, want <= 200ms", elapsed)
	}
}

func TestCommandWaiterRetriesPersistingExitStatus(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerCommandMethods(registry, store); err != nil {
		t.Fatalf("registerCommandMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)

	originalUpdate := updateCommandStatus
	attempts := 0
	updateCommandStatus = func(store *globaldb.Store, ctx context.Context, params globaldb.UpdateCommandStatusParams) error {
		attempts++
		if attempts <= 8 {
			return errors.New("transient write failure")
		}
		return originalUpdate(store, ctx, params)
	}
	t.Cleanup(func() {
		updateCommandStatus = originalUpdate
	})

	runResp := callMethod[CommandRunResponse](t, registry, "command.run", CommandRunRequest{
		WorkspaceID: "sess-1",
		Command:     "/bin/sh",
		Args:        []string{"-c", "exit 9"},
	})

	waitForCommandStatus(t, registry, "sess-1", runResp.CommandID, "exited")

	if attempts < 9 {
		t.Fatalf("updateCommandStatus attempts = %d, want >= 9", attempts)
	}

	getResp := callMethod[CommandGetResponse](t, registry, "command.get", CommandGetRequest{WorkspaceID: "sess-1", CommandID: runResp.CommandID})
	if getResp.ExitCode == nil || *getResp.ExitCode != 9 {
		t.Fatalf("command.get exit_code = %v, want 9", getResp.ExitCode)
	}
}

func TestCommandOutputRetentionEvictsOldestSnapshots(t *testing.T) {
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	for i := 0; i < maxRetainedCommandLogs+1; i++ {
		commandID := fmt.Sprintf("cmd-%03d", i)
		d.setCommandOutput(commandID, commandID+"-output")
	}

	if _, ok := d.getCommandOutput("cmd-000"); ok {
		t.Fatal("expected oldest command output to be evicted")
	}
	if output, ok := d.getCommandOutput(fmt.Sprintf("cmd-%03d", maxRetainedCommandLogs)); !ok || output == "" {
		t.Fatal("expected newest command output to be retained")
	}
}

func waitForCommandStatus(t *testing.T, registry *rpc.MethodRegistry, sessionID, commandID, want string) {
	t.Helper()

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		resp := callMethod[CommandGetResponse](t, registry, "command.get", CommandGetRequest{WorkspaceID: sessionID, CommandID: commandID})
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

	dbPath := filepath.Join(t.TempDir(), fmt.Sprintf("command-method-%d.db", time.Now().UnixNano()))
	if err := applyMigrationSQLFiles(dbPath); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		t.Fatalf("set busy timeout: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	store, err := globaldb.NewSQLStore(db)
	if err != nil {
		t.Fatalf("NewSQLStore returned error: %v", err)
	}

	return store
}
