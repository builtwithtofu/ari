package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
	_ "modernc.org/sqlite"
)

func TestWorkspaceCreateCanStartEmptyWithoutProfilesOrHarnessSessions(t *testing.T) {
	store := newSessionMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"default_harness":"codex"}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", configPath, "defaults", "test-version")

	err := d.registerWorkspaceMethods(registry, store)
	if err != nil {
		t.Fatalf("registerWorkspaceMethods returned error: %v", err)
	}

	createResp := callMethod[WorkspaceCreateResponse](t, registry, "workspace.create", WorkspaceCreateRequest{
		Name:          "alpha",
		CleanupPolicy: "manual",
	})

	if createResp.WorkspaceID == "" {
		t.Fatal("WorkspaceCreateResponse.WorkspaceID is empty")
	}
	getResp := callMethod[WorkspaceGetResponse](t, registry, "workspace.get", WorkspaceGetRequest{WorkspaceID: createResp.WorkspaceID})
	if getResp.Name != "alpha" {
		t.Fatalf("WorkspaceGetResponse.Name = %q, want %q", getResp.Name, "alpha")
	}
	if len(getResp.Folders) != 0 {
		t.Fatalf("WorkspaceGetResponse folders len = %d, want 0", len(getResp.Folders))
	}
	profiles, err := store.ListProfiles(context.Background(), createResp.WorkspaceID)
	if err != nil {
		t.Fatalf("ListProfiles returned error: %v", err)
	}
	if len(profiles) != 0 {
		t.Fatalf("workspace profiles = %#v, want none", profiles)
	}
	sessions, err := store.ListHarnessSessions(context.Background(), createResp.WorkspaceID)
	if err != nil {
		t.Fatalf("ListHarnessSessions returned error: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("harness sessions = %#v, want none", sessions)
	}
}

func TestWorkspaceSetupExistingCreatesSelectsAndRecordsRollbackPoint(t *testing.T) {
	store := newSessionMethodTestStore(t)
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
	if setup.WorkspaceID == "" || setup.ActiveWorkspace != setup.WorkspaceID || setup.RollbackPointID == "" {
		t.Fatalf("setup response = %#v, want workspace, active context, rollback point", setup)
	}
	get := callMethod[WorkspaceGetResponse](t, registry, "workspace.get", WorkspaceGetRequest{WorkspaceID: setup.WorkspaceID})
	if get.Name != "project" || len(get.Folders) != 1 || get.Folders[0].Path != repoRoot {
		t.Fatalf("workspace after setup = %#v, want project with existing folder", get)
	}
	profiles, err := store.ListProfiles(context.Background(), setup.WorkspaceID)
	if err != nil {
		t.Fatalf("ListProfiles returned error: %v", err)
	}
	if len(profiles) != 0 {
		t.Fatalf("workspace profiles = %#v, want setup to leave profile creation explicit", profiles)
	}
	active := callMethod[ContextGetResponse](t, registry, "context.get", ContextGetRequest{})
	if active.Current.WorkspaceID != setup.WorkspaceID {
		t.Fatalf("active context = %#v, want %q", active.Current, setup.WorkspaceID)
	}
	records, err := store.ListOperationRecords(context.Background(), "")
	if err != nil {
		t.Fatalf("ListOperationRecords returned error: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("operation records = %#v, want checkpoint and visible setup operation", records)
	}
	setupRecord := records[0]
	checkpoint := records[1]
	if checkpoint.OperationType != daemonOperationTypeRollbackCheckpoint || setupRecord.OperationType != "workspace_project_setup" || setupRecord.RollbackPointID != checkpoint.OperationID {
		t.Fatalf("operation records = %#v, want setup linked to checkpoint %#v", records, checkpoint)
	}
}

func TestWorkspaceSetupExistingRejectsInvalidFolderBeforeDurableWrites(t *testing.T) {
	store := newSessionMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	nonRepo := t.TempDir()

	err := callMethodError(registry, "workspace.setup_existing", WorkspaceSetupExistingRequest{Name: "project", Folder: nonRepo})
	if err == nil {
		t.Fatal("workspace.setup_existing returned nil error for non-VCS folder")
	}
	if _, lookupErr := store.GetWorkspaceByName(context.Background(), "project"); !errors.Is(lookupErr, globaldb.ErrNotFound) {
		t.Fatalf("GetWorkspaceByName after failed setup error = %v, want ErrNotFound", lookupErr)
	}
	records, err := store.ListOperationRecords(context.Background(), "")
	if err != nil {
		t.Fatalf("ListOperationRecords returned error: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("operation records after failed setup = %#v, want none", records)
	}
	active := callMethod[ContextGetResponse](t, registry, "context.get", ContextGetRequest{})
	if active.Current.WorkspaceID != "" {
		t.Fatalf("active context after failed setup = %#v, want empty", active.Current)
	}
}

func TestWorkspaceSetupExistingRejectsDuplicateNameBeforeCheckpoint(t *testing.T) {
	store := newSessionMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	ctx := context.Background()
	if err := store.CreateWorkspace(ctx, "ws-existing", "project", "", "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	repoRoot := t.TempDir()
	if err := makeGitRoot(repoRoot); err != nil {
		t.Fatalf("makeGitRoot returned error: %v", err)
	}

	err := callMethodError(registry, "workspace.setup_existing", WorkspaceSetupExistingRequest{Name: "project", Folder: repoRoot})
	if err == nil {
		t.Fatal("workspace.setup_existing returned nil error for duplicate name")
	}
	records, err := store.ListOperationRecords(ctx, "")
	if err != nil {
		t.Fatalf("ListOperationRecords returned error: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("operation records after duplicate setup = %#v, want none", records)
	}
	active := callMethod[ContextGetResponse](t, registry, "context.get", ContextGetRequest{})
	if active.Current.WorkspaceID != "" {
		t.Fatalf("active context after duplicate setup = %#v, want empty", active.Current)
	}
}

func TestWorkspaceSetupExistingRejectsOwnedFolderBeforeCheckpoint(t *testing.T) {
	store := newSessionMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	ctx := context.Background()
	repoRoot := t.TempDir()
	if err := makeGitRoot(repoRoot); err != nil {
		t.Fatalf("makeGitRoot returned error: %v", err)
	}
	if err := store.CreateWorkspace(ctx, "ws-existing", "existing", repoRoot, "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	if err := store.AddFolder(ctx, "ws-existing", repoRoot, "git", true); err != nil {
		t.Fatalf("AddFolder returned error: %v", err)
	}

	err := callMethodError(registry, "workspace.setup_existing", WorkspaceSetupExistingRequest{Name: "project", Folder: repoRoot})
	if err == nil {
		t.Fatal("workspace.setup_existing returned nil error for owned folder")
	}
	if _, lookupErr := store.GetWorkspaceByName(ctx, "project"); !errors.Is(lookupErr, globaldb.ErrNotFound) {
		t.Fatalf("GetWorkspaceByName after owned folder setup error = %v, want ErrNotFound", lookupErr)
	}
	records, err := store.ListOperationRecords(ctx, "")
	if err != nil {
		t.Fatalf("ListOperationRecords returned error: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("operation records after owned folder setup = %#v, want none", records)
	}
}

func TestWorkspaceAddFolderMaintainsOnePrimaryAcrossMultipleFolders(t *testing.T) {
	store := newSessionMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", configPath, "defaults", "test-version")
	if err := d.registerWorkspaceMethods(registry, store); err != nil {
		t.Fatalf("registerWorkspaceMethods returned error: %v", err)
	}
	createResp := callMethod[WorkspaceCreateResponse](t, registry, "workspace.create", WorkspaceCreateRequest{Name: "alpha", CleanupPolicy: "manual"})
	repoRoot := t.TempDir()
	if err := makeGitRoot(repoRoot); err != nil {
		t.Fatalf("makeGitRoot returned error: %v", err)
	}
	secondRoot := t.TempDir()
	if err := makeGitRoot(secondRoot); err != nil {
		t.Fatalf("makeGitRoot second returned error: %v", err)
	}
	addResp := callMethod[WorkspaceAddFolderResponse](t, registry, "workspace.add_folder", WorkspaceAddFolderRequest{WorkspaceID: createResp.WorkspaceID, FolderPath: repoRoot})
	if addResp.FolderPath != repoRoot || addResp.VCSType != "git" {
		t.Fatalf("WorkspaceAddFolderResponse = %#v, want git folder", addResp)
	}
	secondResp := callMethod[WorkspaceAddFolderResponse](t, registry, "workspace.add_folder", WorkspaceAddFolderRequest{WorkspaceID: createResp.WorkspaceID, FolderPath: secondRoot})
	if secondResp.FolderPath != secondRoot || secondResp.VCSType != "git" {
		t.Fatalf("second WorkspaceAddFolderResponse = %#v, want git folder", secondResp)
	}
	getResp := callMethod[WorkspaceGetResponse](t, registry, "workspace.get", WorkspaceGetRequest{WorkspaceID: createResp.WorkspaceID})
	if len(getResp.Folders) != 2 || !getResp.Folders[0].IsPrimary || getResp.Folders[0].Path != repoRoot || getResp.Folders[1].IsPrimary || getResp.Folders[1].Path != secondRoot {
		t.Fatalf("folders = %#v, want first added folder primary and second non-primary", getResp.Folders)
	}
	profiles, err := store.ListProfiles(context.Background(), createResp.WorkspaceID)
	if err != nil {
		t.Fatalf("ListProfiles returned error: %v", err)
	}
	if len(profiles) != 0 {
		t.Fatalf("profiles after add folder = %#v, want none", profiles)
	}
	sessions, err := store.ListHarnessSessions(context.Background(), createResp.WorkspaceID)
	if err != nil {
		t.Fatalf("ListHarnessSessions returned error: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("harness sessions after add folder = %#v, want none", sessions)
	}
	callMethod[WorkspaceRemoveFolderResponse](t, registry, "workspace.remove_folder", WorkspaceRemoveFolderRequest{WorkspaceID: createResp.WorkspaceID, FolderPath: repoRoot})
	callMethod[WorkspaceRemoveFolderResponse](t, registry, "workspace.remove_folder", WorkspaceRemoveFolderRequest{WorkspaceID: createResp.WorkspaceID, FolderPath: secondRoot})
	getResp = callMethod[WorkspaceGetResponse](t, registry, "workspace.get", WorkspaceGetRequest{WorkspaceID: createResp.WorkspaceID})
	if len(getResp.Folders) != 0 {
		t.Fatalf("folders after removing all = %#v, want empty workspace", getResp.Folders)
	}
}

func TestWorkspaceGetMissingReturnsWorkspaceNotFound(t *testing.T) {
	store := newSessionMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	err := d.registerWorkspaceMethods(registry, store)
	if err != nil {
		t.Fatalf("registerWorkspaceMethods returned error: %v", err)
	}

	spec, ok := registry.Get("workspace.get")
	if !ok {
		t.Fatal("workspace.get method not registered")
	}

	raw, err := json.Marshal(WorkspaceGetRequest{WorkspaceID: "missing"})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	_, err = spec.Call(context.Background(), raw)
	if err == nil {
		t.Fatal("workspace.get returned nil error for missing workspace")
	}

	var rpcErr *rpc.HandlerError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("workspace.get error type = %T, want *rpc.HandlerError", err)
	}
	if rpcErr.Code != rpc.SessionNotFound {
		t.Fatalf("workspace.get error code = %d, want %d", rpcErr.Code, rpc.SessionNotFound)
	}
}

func TestWorkspaceAddFolderRejectsSubdirectoryOfVCSRoot(t *testing.T) {
	store := newSessionMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	err := d.registerWorkspaceMethods(registry, store)
	if err != nil {
		t.Fatalf("registerWorkspaceMethods returned error: %v", err)
	}

	repoRoot := t.TempDir()
	if err := makeGitRoot(repoRoot); err != nil {
		t.Fatalf("makeGitRoot returned error: %v", err)
	}

	createResp := callMethod[WorkspaceCreateResponse](t, registry, "workspace.create", WorkspaceCreateRequest{
		Name:          "alpha",
		OriginRoot:    t.TempDir(),
		CleanupPolicy: "manual",
	})

	subdir := filepath.Join(repoRoot, "nested")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("create subdir: %v", err)
	}

	spec, ok := registry.Get("workspace.add_folder")
	if !ok {
		t.Fatal("workspace.add_folder method not registered")
	}

	raw, err := json.Marshal(WorkspaceAddFolderRequest{WorkspaceID: createResp.WorkspaceID, FolderPath: subdir})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	_, err = spec.Call(context.Background(), raw)
	if err == nil {
		t.Fatal("workspace.add_folder returned nil error for subdirectory input")
	}
}

func TestSessionGetResolvesByName(t *testing.T) {
	store := newSessionMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	err := d.registerWorkspaceMethods(registry, store)
	if err != nil {
		t.Fatalf("registerWorkspaceMethods returned error: %v", err)
	}

	repoRoot := t.TempDir()
	if err := makeGitRoot(repoRoot); err != nil {
		t.Fatalf("makeGitRoot returned error: %v", err)
	}

	createResp := callMethod[WorkspaceCreateResponse](t, registry, "workspace.create", WorkspaceCreateRequest{
		Name:          "alpha",
		OriginRoot:    t.TempDir(),
		CleanupPolicy: "manual",
	})

	getResp := callMethod[WorkspaceGetResponse](t, registry, "workspace.get", WorkspaceGetRequest{WorkspaceID: "alpha"})
	if getResp.WorkspaceID != createResp.WorkspaceID {
		t.Fatalf("workspace.get by name id = %q, want %q", getResp.WorkspaceID, createResp.WorkspaceID)
	}
}

func TestSessionRemoveFolderMissingReturnsInvalidParams(t *testing.T) {
	store := newSessionMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	err := d.registerWorkspaceMethods(registry, store)
	if err != nil {
		t.Fatalf("registerWorkspaceMethods returned error: %v", err)
	}

	repoRoot := t.TempDir()
	if err := makeGitRoot(repoRoot); err != nil {
		t.Fatalf("makeGitRoot returned error: %v", err)
	}

	createResp := callMethod[WorkspaceCreateResponse](t, registry, "workspace.create", WorkspaceCreateRequest{
		Name:          "alpha",
		OriginRoot:    t.TempDir(),
		CleanupPolicy: "manual",
	})

	spec, ok := registry.Get("workspace.remove_folder")
	if !ok {
		t.Fatal("workspace.remove_folder method not registered")
	}

	raw, err := json.Marshal(WorkspaceRemoveFolderRequest{WorkspaceID: createResp.WorkspaceID, FolderPath: filepath.Join(repoRoot, "missing")})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	_, err = spec.Call(context.Background(), raw)
	if err == nil {
		t.Fatal("workspace.remove_folder returned nil error for missing folder")
	}

	var rpcErr *rpc.HandlerError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("workspace.remove_folder error type = %T, want *rpc.HandlerError", err)
	}
	if rpcErr.Code != rpc.InvalidParams {
		t.Fatalf("workspace.remove_folder error code = %d, want %d", rpcErr.Code, rpc.InvalidParams)
	}
}

func TestSessionRemoveFolderMissingSessionReturnsSessionNotFound(t *testing.T) {
	store := newSessionMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	err := d.registerWorkspaceMethods(registry, store)
	if err != nil {
		t.Fatalf("registerWorkspaceMethods returned error: %v", err)
	}

	spec, ok := registry.Get("workspace.remove_folder")
	if !ok {
		t.Fatal("workspace.remove_folder method not registered")
	}

	raw, err := json.Marshal(WorkspaceRemoveFolderRequest{WorkspaceID: "missing", FolderPath: "/tmp/repo"})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	_, err = spec.Call(context.Background(), raw)
	if err == nil {
		t.Fatal("workspace.remove_folder returned nil error for missing workspace")
	}

	var rpcErr *rpc.HandlerError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("workspace.remove_folder error type = %T, want *rpc.HandlerError", err)
	}
	if rpcErr.Code != rpc.SessionNotFound {
		t.Fatalf("workspace.remove_folder error code = %d, want %d", rpcErr.Code, rpc.SessionNotFound)
	}
}

func TestNormalizeAndValidateVCSRootUsesPreferenceWhenBothMarkersExist(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoRoot, ".jj"), 0o755); err != nil {
		t.Fatalf("create .jj dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, ".git"), []byte("gitdir: /tmp/worktree/.git/worktrees/test"), 0o644); err != nil {
		t.Fatalf("create .git marker file: %v", err)
	}

	_, vcsType, err := normalizeAndValidateVCSRoot(repoRoot, "git")
	if err != nil {
		t.Fatalf("normalizeAndValidateVCSRoot(git) returned error: %v", err)
	}
	if vcsType != "git" {
		t.Fatalf("vcsType with git preference = %q, want %q", vcsType, "git")
	}

	_, vcsType, err = normalizeAndValidateVCSRoot(repoRoot, "jj")
	if err != nil {
		t.Fatalf("normalizeAndValidateVCSRoot(jj) returned error: %v", err)
	}
	if vcsType != "jj" {
		t.Fatalf("vcsType with jj preference = %q, want %q", vcsType, "jj")
	}

	_, vcsType, err = normalizeAndValidateVCSRoot(repoRoot, "auto")
	if err != nil {
		t.Fatalf("normalizeAndValidateVCSRoot(auto) returned error: %v", err)
	}
	if vcsType != "jj" {
		t.Fatalf("vcsType with auto preference = %q, want %q", vcsType, "jj")
	}
}

func TestSessionCreateRejectsInvalidVCSPreference(t *testing.T) {
	store := newSessionMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	err := d.registerWorkspaceMethods(registry, store)
	if err != nil {
		t.Fatalf("registerWorkspaceMethods returned error: %v", err)
	}

	spec, ok := registry.Get("workspace.create")
	if !ok {
		t.Fatal("workspace.create method not registered")
	}

	raw, err := json.Marshal(WorkspaceCreateRequest{
		Name:          "alpha",
		OriginRoot:    t.TempDir(),
		CleanupPolicy: "manual",
		VCSPreference: "gti",
	})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	_, err = spec.Call(context.Background(), raw)
	if err == nil {
		t.Fatal("workspace.create returned nil error for invalid vcs preference")
	}

	var rpcErr *rpc.HandlerError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("workspace.create error type = %T, want *rpc.HandlerError", err)
	}
	if rpcErr.Code != rpc.InvalidParams {
		t.Fatalf("workspace.create error code = %d, want %d", rpcErr.Code, rpc.InvalidParams)
	}
}

func TestWorkspaceCreateDoesNotReadDefaultHarnessConfig(t *testing.T) {
	store := newSessionMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"default_harness":"bad-harness"}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", configPath, "defaults", "test-version")

	err := d.registerWorkspaceMethods(registry, store)
	if err != nil {
		t.Fatalf("registerWorkspaceMethods returned error: %v", err)
	}

	resp := callMethod[WorkspaceCreateResponse](t, registry, "workspace.create", WorkspaceCreateRequest{Name: "alpha", OriginRoot: t.TempDir(), CleanupPolicy: "manual", VCSPreference: "auto"})
	profiles, err := store.ListProfiles(context.Background(), resp.WorkspaceID)
	if err != nil {
		t.Fatalf("ListProfiles returned error: %v", err)
	}
	if len(profiles) != 0 {
		t.Fatalf("profiles = %#v, want no implicit helper profile", profiles)
	}
}

func newSessionMethodTestStore(t *testing.T) *globaldb.Store {
	t.Helper()
	return newCommandMethodTestStore(t)
}

func makeGitRoot(root string) error {
	return os.MkdirAll(filepath.Join(root, ".git"), 0o755)
}
