package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
)

func TestInitMethodsExposeOptionsStateAndApplyThroughRPC(t *testing.T) {
	stubBootstrap(t)

	configPath := filepath.Join(t.TempDir(), "config.json")
	socketPath := testSocketPath(t)
	dbPath := filepath.Join(t.TempDir(), "ari.db")
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")
	d := New(socketPath, dbPath, pidPath, configPath, "test", "test-version")
	d.setHarnessFactoryForTest("codex", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (HarnessAdapter, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return newFakeHarness("codex", nil), nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- d.Start(ctx)
	}()

	var options InitOptionsResponse
	callDaemonMethod(t, socketPath, "init.options", InitOptionsRequest{}, &options)
	if len(options.Harnesses) != 3 {
		t.Fatalf("unexpected harness options: %#v", options.Harnesses)
	}
	if options.Harnesses[0].Name != "claude-code" || options.Harnesses[1].Name != "codex" || options.Harnesses[2].Name != "opencode" {
		t.Fatalf("unexpected harness order: %#v", options.Harnesses)
	}
	if len(options.Models) == 0 || len(options.Roots) == 0 || options.Roots[0].Path != "~/" {
		t.Fatalf("unexpected model/root options: %#v %#v", options.Models, options.Roots)
	}

	var before InitStateResponse
	callDaemonMethod(t, socketPath, "init.state", InitStateRequest{}, &before)
	if before.Initialized || before.DefaultHarness != "" || before.PreferredModel != "" || before.DefaultRoot != filepath.Clean(os.Getenv("HOME")) || before.HomeWorkspaceReady || before.HomeHelperReady {
		t.Fatalf("unexpected initial state: %#v", before)
	}

	var applied InitApplyResponse
	callDaemonMethod(t, socketPath, "init.apply", InitApplyRequest{Harness: "codex", Model: "gpt-5.5", Root: "~/Projects"}, &applied)
	wantRoot := filepath.Join(os.Getenv("HOME"), "Projects")
	if !applied.Initialized || applied.DefaultHarness != "codex" || applied.PreferredModel != "gpt-5.5" || applied.DefaultRoot != wantRoot || !applied.DefaultHarnessSet || !applied.HomeHelperReady {
		t.Fatalf("unexpected apply response: %#v", applied)
	}

	var after InitStateResponse
	callDaemonMethod(t, socketPath, "init.state", InitStateRequest{}, &after)
	if !after.Initialized || after.DefaultHarness != "codex" || after.PreferredModel != "gpt-5.5" || after.DefaultRoot != wantRoot || !after.HomeWorkspaceReady || !after.HomeHelperReady {
		t.Fatalf("unexpected state after apply: %#v", after)
	}

	var persisted map[string]string
	if err := readJSONFile(configPath, &persisted); err != nil {
		t.Fatalf("read config: %v", err)
	}
	if persisted["default_harness"] != "codex" {
		t.Fatalf("default_harness = %q, want codex", persisted["default_harness"])
	}
	if persisted["preferred_model"] != "gpt-5.5" || persisted["default_workspace_root"] != wantRoot {
		t.Fatalf("persisted model/root = %#v, want gpt-5.5/%s", persisted, wantRoot)
	}

	store := openInitMethodTestStore(t, dbPath)
	workspaces, err := store.ListWorkspaces(context.Background())
	if err != nil {
		t.Fatalf("ListSessions returned error: %v", err)
	}
	var homeWorkspaceID string
	for _, workspace := range workspaces {
		if workspace.OriginRoot == wantRoot {
			homeWorkspaceID = workspace.ID
		}
	}
	if homeWorkspaceID == "" {
		t.Fatalf("home workspace for %s was not persisted: %#v", wantRoot, workspaces)
	}
	current, err := readActiveWorkspaceContext(context.Background(), store)
	if err != nil {
		t.Fatalf("readActiveWorkspaceContext returned error: %v", err)
	}
	if current.WorkspaceID != homeWorkspaceID {
		t.Fatalf("active context = %#v, want home workspace %q", current, homeWorkspaceID)
	}
	helper, err := store.GetDefaultHelperProfile(context.Background(), homeWorkspaceID)
	if err != nil {
		t.Fatalf("GetDefaultHelperProfile returned error: %v", err)
	}
	if helper.Name != globaldb.DefaultHelperProfileName || helper.WorkspaceID != homeWorkspaceID || helper.Harness != "codex" {
		t.Fatalf("helper profile = %#v, want codex helper tied to home workspace", helper)
	}
	helpers, err := store.ListHarnessSessions(context.Background(), homeWorkspaceID)
	if err != nil {
		t.Fatalf("ListHarnessSessions returned error: %v", err)
	}
	if !hasLiveHelperSession(helpers, helper.ProfileID) {
		t.Fatalf("agent sessions = %#v, want live helper session for profile %q", helpers, helper.ProfileID)
	}

	records, err := store.ListOperationRecords(context.Background(), "")
	if err != nil {
		t.Fatalf("ListOperationRecords returned error: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("operation records = %#v, want checkpoint and init operation", records)
	}
	initRecord := records[0]
	checkpoint := records[1]
	if checkpoint.OperationType != daemonOperationTypeRollbackCheckpoint || checkpoint.Scope != globaldb.OperationScopeGlobal {
		t.Fatalf("checkpoint record = %#v, want global rollback checkpoint", checkpoint)
	}
	if initRecord.OperationType != daemonOperationTypeInitApplied || initRecord.Source != daemonOperationSourceDaemon || initRecord.Result != daemonOperationResultSucceeded {
		t.Fatalf("init operation record = %#v, want semantic daemon success", initRecord)
	}
	if initRecord.ParentOperationID != checkpoint.OperationID || initRecord.CheckpointOperationID != checkpoint.OperationID || initRecord.RollbackPointID != checkpoint.OperationID {
		t.Fatalf("init operation links = %#v, want checkpoint %q", initRecord, checkpoint.OperationID)
	}
	if !strings.Contains(initRecord.PayloadHash, "sha256:") || !strings.Contains(initRecord.PayloadSnapshotJSON, `"model":"gpt-5.5"`) || !strings.Contains(initRecord.PayloadSnapshotJSON, `"root":"`+wantRoot+`"`) || initRecord.RollbackDataJSON == "{}" {
		t.Fatalf("init operation metadata = %#v, want payload hash/snapshot and rollback data", initRecord)
	}

	var stop StopResponse
	tryStopDaemonMethod(t, socketPath, &stop)
	if err := <-errCh; err != nil {
		t.Fatalf("daemon start returned error: %v", err)
	}
}

func openInitMethodTestStore(t *testing.T, dbPath string) *globaldb.Store {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() {
		_ = db.Close()
	})

	store, err := globaldb.NewSQLStore(db)
	if err != nil {
		t.Fatalf("NewSQLStore returned error: %v", err)
	}
	return store
}

func tryStopDaemonMethod(t *testing.T, socketPath string, stop *StopResponse) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := tryDaemonMethod(ctx, socketPath, "daemon.stop", StopRequest{}, stop); err != nil && !strings.Contains(err.Error(), "connection is closed") {
		t.Fatalf("call daemon.stop: %v", err)
	}
}

func TestInitApplyRejectsInvalidHarnessAndPreservesConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"preferred_model":"keep-me","default_harness":"codex"}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	d := New("/tmp/ari.sock", "/tmp/ari.db", "/tmp/ari.pid", configPath, "test", "test-version")

	_, err := d.applyInit(context.Background(), nil, InitApplyRequest{Harness: "unknown"})
	if err == nil {
		t.Fatal("applyInit returned nil error for invalid harness")
	}

	var persisted map[string]string
	if readErr := readJSONFile(configPath, &persisted); readErr != nil {
		t.Fatalf("read config: %v", readErr)
	}
	if persisted["default_harness"] != "codex" || persisted["preferred_model"] != "keep-me" {
		t.Fatalf("config was not preserved: %#v", persisted)
	}
}

func readJSONFile(path string, out any) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}
