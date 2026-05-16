package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

func TestRollbackInitRemovesAriOwnedStateAndAppendsRecord(t *testing.T) {
	stubBootstrap(t)
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	configPath := filepath.Join(t.TempDir(), "config.json")
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", configPath, "defaults", "test-version")
	d.setHarnessFactoryForTest("codex", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return newFakeHarness("codex", nil), nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	root := filepath.Join(t.TempDir(), "Projects")
	applied := callMethod[InitApplyResponse](t, registry, "init.apply", InitApplyRequest{Harness: "codex", Model: "gpt-5.5", Root: root})
	if !applied.HomeWorkspaceReady || !applied.HomeHelperReady {
		t.Fatalf("init apply response = %#v, want home workspace/helper ready", applied)
	}
	records, err := store.ListOperationRecords(context.Background(), "")
	if err != nil {
		t.Fatalf("ListOperationRecords returned error: %v", err)
	}
	var rollbackPointID string
	for _, record := range records {
		if record.OperationType == daemonOperationTypeInitApplied {
			rollbackPointID = record.RollbackPointID
		}
	}
	if rollbackPointID == "" {
		t.Fatalf("operation records = %#v, want init rollback point", records)
	}

	rolledBack := callMethod[RollbackApplyResponse](t, registry, "rollback.apply", RollbackApplyRequest{RollbackPointID: rollbackPointID})
	if rolledBack.Status != daemonOperationResultSucceeded || rolledBack.RollbackOperationID == "" {
		t.Fatalf("rollback response = %#v, want successful appended operation", rolledBack)
	}
	var persisted map[string]string
	if err := readJSONFile(configPath, &persisted); err != nil {
		t.Fatalf("read config: %v", err)
	}
	if persisted["default_harness"] != "" || persisted["preferred_model"] != "" || persisted["default_workspace_root"] != "" {
		t.Fatalf("config after rollback = %#v, want init-owned config cleared", persisted)
	}
	sessions, err := store.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions returned error: %v", err)
	}
	for _, session := range sessions {
		if session.OriginRoot == root {
			t.Fatalf("home workspace still present after rollback: %#v", session)
		}
	}
	records, err = store.ListOperationRecords(context.Background(), "")
	if err != nil {
		t.Fatalf("ListOperationRecords after rollback returned error: %v", err)
	}
	foundRollback := false
	foundInitHistory := false
	for _, record := range records {
		if record.OperationID == rolledBack.RollbackOperationID && record.OperationType == daemonOperationTypeRollbackApplied && strings.Contains(record.RollbackDataJSON, "ari_owned_state_only") {
			foundRollback = true
		}
		if record.OperationType == daemonOperationTypeInitApplied {
			foundInitHistory = true
		}
	}
	if !foundRollback || !foundInitHistory {
		t.Fatalf("operation records after rollback = %#v, want appended rollback and preserved init history", records)
	}
	if _, err := os.Stat(root); err != nil && !os.IsNotExist(err) {
		t.Fatalf("stat root: %v", err)
	}
}

func TestRollbackInitPreservesPreExistingWorkspaceAtRoot(t *testing.T) {
	stubBootstrap(t)
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	configPath := filepath.Join(t.TempDir(), "config.json")
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", configPath, "defaults", "test-version")
	d.setHarnessFactoryForTest("codex", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return newFakeHarness("codex", nil), nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	root := t.TempDir()
	if err := store.CreateSession(context.Background(), "ws-existing", "existing", root, "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	if err := store.AddFolder(context.Background(), "ws-existing", root, "git", true); err != nil {
		t.Fatalf("AddFolder returned error: %v", err)
	}
	callMethod[InitApplyResponse](t, registry, "init.apply", InitApplyRequest{Harness: "codex", Model: "gpt-5.5", Root: root})
	records, err := store.ListOperationRecords(context.Background(), "")
	if err != nil {
		t.Fatalf("ListOperationRecords returned error: %v", err)
	}
	var rollbackPointID string
	for _, record := range records {
		if record.OperationType == daemonOperationTypeInitApplied {
			rollbackPointID = record.RollbackPointID
		}
	}
	callMethod[RollbackApplyResponse](t, registry, "rollback.apply", RollbackApplyRequest{RollbackPointID: rollbackPointID})
	if _, err := store.GetSession(context.Background(), "ws-existing"); err != nil {
		t.Fatalf("pre-existing workspace was removed: %v", err)
	}
}

func TestRollbackInitRestoresPreviousActiveWorkspace(t *testing.T) {
	stubBootstrap(t)
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	configPath := filepath.Join(t.TempDir(), "config.json")
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", configPath, "defaults", "test-version")
	d.setHarnessFactoryForTest("codex", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return newFakeHarness("codex", nil), nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-previous", t.TempDir())
	callMethod[ContextSetResponse](t, registry, "context.set", ContextSetRequest{WorkspaceID: "ws-previous"})
	callMethod[InitApplyResponse](t, registry, "init.apply", InitApplyRequest{Harness: "codex", Model: "gpt-5.5", Root: filepath.Join(t.TempDir(), "home")})
	records, err := store.ListOperationRecords(context.Background(), "")
	if err != nil {
		t.Fatalf("ListOperationRecords returned error: %v", err)
	}
	var rollbackPointID string
	for _, record := range records {
		if record.OperationType == daemonOperationTypeInitApplied {
			rollbackPointID = record.RollbackPointID
		}
	}
	callMethod[RollbackApplyResponse](t, registry, "rollback.apply", RollbackApplyRequest{RollbackPointID: rollbackPointID})
	active := callMethod[ContextGetResponse](t, registry, "context.get", ContextGetRequest{})
	if active.Current.WorkspaceID != "ws-previous" {
		t.Fatalf("active context after init rollback = %#v, want previous workspace", active.Current)
	}
	var persisted map[string]string
	if err := readJSONFile(configPath, &persisted); err != nil {
		t.Fatalf("read config: %v", err)
	}
	if persisted["active_workspace"] != "ws-previous" {
		t.Fatalf("active_workspace after init rollback = %q, want previous workspace", persisted["active_workspace"])
	}
}

func TestRollbackProjectSetupRemovesWorkspaceAndClearsActiveContext(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"default_harness":"codex"}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", configPath, "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	repoRoot := t.TempDir()
	if err := makeGitRoot(repoRoot); err != nil {
		t.Fatalf("makeGitRoot returned error: %v", err)
	}
	setup := callMethod[WorkspaceSetupExistingResponse](t, registry, "workspace.setup_existing", WorkspaceSetupExistingRequest{Name: "project", Folder: repoRoot})
	active := callMethod[ContextGetResponse](t, registry, "context.get", ContextGetRequest{})
	if active.Current.WorkspaceID != setup.WorkspaceID {
		t.Fatalf("active context before rollback = %#v, want setup workspace", active.Current)
	}

	rolledBack := callMethod[RollbackApplyResponse](t, registry, "rollback.apply", RollbackApplyRequest{RollbackPointID: setup.RollbackPointID})
	if rolledBack.Status != daemonOperationResultSucceeded || rolledBack.RollbackOperationID == "" {
		t.Fatalf("rollback response = %#v, want success", rolledBack)
	}
	if _, err := store.GetSession(context.Background(), setup.WorkspaceID); err == nil {
		t.Fatalf("workspace %q still exists after rollback", setup.WorkspaceID)
	}
	active = callMethod[ContextGetResponse](t, registry, "context.get", ContextGetRequest{})
	if active.Current.WorkspaceID != "" {
		t.Fatalf("active context after rollback = %#v, want cleared", active.Current)
	}
	var persisted map[string]string
	if err := readJSONFile(configPath, &persisted); err != nil {
		t.Fatalf("read config: %v", err)
	}
	if persisted["active_workspace"] != "" {
		t.Fatalf("active_workspace after rollback = %q, want cleared", persisted["active_workspace"])
	}
	dashboard := callMethod[DashboardGetResponse](t, registry, "dashboard.get", DashboardGetRequest{})
	if dashboard.State != "workspace_picker" || dashboard.EffectiveWorkspaceID != "" {
		t.Fatalf("dashboard after rollback = %#v, want picker without stale workspace", dashboard)
	}
	if _, err := os.Stat(repoRoot); err != nil {
		t.Fatalf("repo root was affected by rollback: %v", err)
	}
	records, err := store.ListOperationRecords(context.Background(), "")
	if err != nil {
		t.Fatalf("ListOperationRecords returned error: %v", err)
	}
	foundSetup := false
	foundRollback := false
	for _, record := range records {
		if record.OperationType == "workspace_project_setup" {
			foundSetup = true
		}
		if record.OperationID == rolledBack.RollbackOperationID && record.OperationType == daemonOperationTypeRollbackApplied && strings.Contains(record.RollbackDataJSON, "ari_owned_state_only") {
			foundRollback = true
		}
	}
	if !foundSetup || !foundRollback {
		t.Fatalf("operation records after rollback = %#v, want preserved setup and appended rollback", records)
	}
	if err := callMethodError(registry, "rollback.apply", RollbackApplyRequest{RollbackPointID: setup.RollbackPointID}); err == nil || !strings.Contains(err.Error(), "already been applied") {
		t.Fatalf("second rollback error = %v, want already applied", err)
	}
}

func TestRollbackProjectSetupIgnoresFailedRollbackAttempts(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"default_harness":"codex"}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", configPath, "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	repoRoot := t.TempDir()
	if err := makeGitRoot(repoRoot); err != nil {
		t.Fatalf("makeGitRoot returned error: %v", err)
	}
	setup := callMethod[WorkspaceSetupExistingResponse](t, registry, "workspace.setup_existing", WorkspaceSetupExistingRequest{Name: "project", Folder: repoRoot})
	records, err := store.ListOperationRecords(context.Background(), setup.WorkspaceID)
	if err != nil || len(records) == 0 {
		t.Fatalf("ListOperationRecords = (%#v, %v)", records, err)
	}
	failed := records[0]
	failed.OperationID = "op-failed-rollback"
	failed.OperationType = daemonOperationTypeRollbackApplied
	failed.Result = "failed: transient"
	failed.WorkspaceID = ""
	failed.Scope = globaldb.OperationScopeGlobal
	failed.PayloadHash = "sha256:failed"
	failed.PayloadSnapshotJSON = `{"failed":"rollback"}`
	if _, err := store.AppendOperationRecord(context.Background(), failed); err != nil {
		t.Fatalf("AppendOperationRecord failed rollback returned error: %v", err)
	}

	rolledBack := callMethod[RollbackApplyResponse](t, registry, "rollback.apply", RollbackApplyRequest{RollbackPointID: setup.RollbackPointID})
	if rolledBack.Status != daemonOperationResultSucceeded {
		t.Fatalf("rollback response = %#v, want retry success", rolledBack)
	}
}

func TestRollbackProjectSetupRestoresPreviousActiveWorkspace(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"default_harness":"codex"}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", configPath, "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	previousRoot := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "ws-previous", previousRoot)
	callMethod[ContextSetResponse](t, registry, "context.set", ContextSetRequest{WorkspaceID: "ws-previous"})
	repoRoot := t.TempDir()
	if err := makeGitRoot(repoRoot); err != nil {
		t.Fatalf("makeGitRoot returned error: %v", err)
	}
	setup := callMethod[WorkspaceSetupExistingResponse](t, registry, "workspace.setup_existing", WorkspaceSetupExistingRequest{Name: "project", Folder: repoRoot})

	callMethod[RollbackApplyResponse](t, registry, "rollback.apply", RollbackApplyRequest{RollbackPointID: setup.RollbackPointID})
	active := callMethod[ContextGetResponse](t, registry, "context.get", ContextGetRequest{})
	if active.Current.WorkspaceID != "ws-previous" {
		t.Fatalf("active context after rollback = %#v, want previous workspace", active.Current)
	}
	var persisted map[string]string
	if err := readJSONFile(configPath, &persisted); err != nil {
		t.Fatalf("read config: %v", err)
	}
	if persisted["active_workspace"] != "ws-previous" {
		t.Fatalf("active_workspace after rollback = %q, want previous workspace", persisted["active_workspace"])
	}
}
