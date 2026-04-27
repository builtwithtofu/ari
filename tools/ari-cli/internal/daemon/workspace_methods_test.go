package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
	_ "modernc.org/sqlite"
)

func TestWorkspaceMethodsCreateAndGet(t *testing.T) {
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

	repoRoot := t.TempDir()
	if err := makeGitRoot(repoRoot); err != nil {
		t.Fatalf("makeGitRoot returned error: %v", err)
	}

	createResp := callMethod[WorkspaceCreateResponse](t, registry, "workspace.create", WorkspaceCreateRequest{
		Name:          "alpha",
		Folder:        repoRoot,
		OriginRoot:    t.TempDir(),
		CleanupPolicy: "manual",
	})

	if createResp.WorkspaceID == "" {
		t.Fatal("WorkspaceCreateResponse.WorkspaceID is empty")
	}
	if createResp.VCSType != "git" {
		t.Fatalf("WorkspaceCreateResponse.VCSType = %q, want %q", createResp.VCSType, "git")
	}

	getResp := callMethod[WorkspaceGetResponse](t, registry, "workspace.get", WorkspaceGetRequest{WorkspaceID: createResp.WorkspaceID})
	if getResp.Name != "alpha" {
		t.Fatalf("WorkspaceGetResponse.Name = %q, want %q", getResp.Name, "alpha")
	}
	if len(getResp.Folders) != 1 {
		t.Fatalf("WorkspaceGetResponse folders len = %d, want 1", len(getResp.Folders))
	}
	help, err := store.GetDefaultHelperProfile(context.Background(), createResp.WorkspaceID)
	if err != nil {
		t.Fatalf("GetDefaultHelperProfile returned error: %v", err)
	}
	if help.Name != "helper" || help.WorkspaceID != createResp.WorkspaceID || help.Prompt != helperPrompt() {
		t.Fatalf("project helper = %#v", help)
	}
}

func TestWorkspaceCreateWithoutDefaultHarnessDoesNotCreateUnusableHelper(t *testing.T) {
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
	repoRoot := t.TempDir()
	if err := makeGitRoot(repoRoot); err != nil {
		t.Fatalf("makeGitRoot returned error: %v", err)
	}

	createResp := callMethod[WorkspaceCreateResponse](t, registry, "workspace.create", WorkspaceCreateRequest{Name: "alpha", Folder: repoRoot, OriginRoot: t.TempDir(), CleanupPolicy: "manual"})
	_, err := store.GetDefaultHelperProfile(context.Background(), createResp.WorkspaceID)
	if !errors.Is(err, globaldb.ErrNotFound) {
		t.Fatalf("GetDefaultHelperProfile error = %v, want ErrNotFound", err)
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
		Folder:        repoRoot,
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
		Folder:        repoRoot,
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
		Folder:        repoRoot,
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

	repoRoot := t.TempDir()
	if err := makeGitRoot(repoRoot); err != nil {
		t.Fatalf("makeGitRoot returned error: %v", err)
	}

	spec, ok := registry.Get("workspace.create")
	if !ok {
		t.Fatal("workspace.create method not registered")
	}

	raw, err := json.Marshal(WorkspaceCreateRequest{
		Name:          "alpha",
		Folder:        repoRoot,
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

func TestSessionCreateRollsBackWhenFolderInsertFails(t *testing.T) {
	store := newSessionMethodStoreWithoutFolders(t)
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

	spec, ok := registry.Get("workspace.create")
	if !ok {
		t.Fatal("workspace.create method not registered")
	}

	raw, err := json.Marshal(WorkspaceCreateRequest{
		Name:          "alpha",
		Folder:        repoRoot,
		OriginRoot:    t.TempDir(),
		CleanupPolicy: "manual",
		VCSPreference: "auto",
	})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	_, err = spec.Call(context.Background(), raw)
	if err == nil {
		t.Fatal("workspace.create returned nil error when folder insert fails")
	}

	_, lookupErr := store.GetSessionByName(context.Background(), "alpha")
	if !errors.Is(lookupErr, globaldb.ErrNotFound) {
		t.Fatalf("GetSessionByName after failed create error = %v, want ErrNotFound", lookupErr)
	}
}

func TestSessionCreateRollsBackWhenHelperHarnessConfigIsInvalid(t *testing.T) {
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

	repoRoot := t.TempDir()
	if err := makeGitRoot(repoRoot); err != nil {
		t.Fatalf("makeGitRoot returned error: %v", err)
	}

	err = callMethodError(registry, "workspace.create", WorkspaceCreateRequest{Name: "alpha", Folder: repoRoot, OriginRoot: t.TempDir(), CleanupPolicy: "manual", VCSPreference: "auto"})
	if err == nil {
		t.Fatal("workspace.create returned nil error for invalid helper harness config")
	}

	_, lookupErr := store.GetSessionByName(context.Background(), "alpha")
	if !errors.Is(lookupErr, globaldb.ErrNotFound) {
		t.Fatalf("GetSessionByName after failed create error = %v, want ErrNotFound", lookupErr)
	}
}

func callMethod[T any](t *testing.T, registry *rpc.MethodRegistry, methodName string, params any) T {
	t.Helper()

	spec, ok := registry.Get(methodName)
	if !ok {
		t.Fatalf("method %s not registered", methodName)
	}

	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params for %s: %v", methodName, err)
	}

	resultAny, err := spec.Call(context.Background(), raw)
	if err != nil {
		t.Fatalf("call %s returned error: %v", methodName, err)
	}

	result, ok := resultAny.(T)
	if !ok {
		t.Fatalf("call %s result type = %T, want %T", methodName, resultAny, *new(T))
	}

	return result
}

func newSessionMethodTestStore(t *testing.T) *globaldb.Store {
	t.Helper()

	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	if _, err := db.Exec(`
CREATE TABLE workspaces (
	workspace_id TEXT PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	status TEXT NOT NULL DEFAULT 'active',
	vcs_preference TEXT NOT NULL DEFAULT 'auto',
	origin_root TEXT NOT NULL,
	cleanup_policy TEXT NOT NULL DEFAULT 'manual',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	workspace_kind TEXT NOT NULL DEFAULT 'project'
);
CREATE UNIQUE INDEX workspaces_single_system_uq
	ON workspaces (workspace_kind)
	WHERE workspace_kind = 'system';
CREATE TABLE workspace_folders (
	workspace_id TEXT NOT NULL,
	folder_path TEXT NOT NULL,
	vcs_type TEXT NOT NULL DEFAULT 'unknown',
	is_primary INTEGER NOT NULL DEFAULT 0,
	added_at TEXT NOT NULL,
	PRIMARY KEY (workspace_id, folder_path),
	FOREIGN KEY(workspace_id) REFERENCES workspaces(workspace_id) ON DELETE CASCADE
);
CREATE TABLE agent_profiles (
	profile_id TEXT PRIMARY KEY,
	workspace_id TEXT,
	name TEXT NOT NULL,
	harness TEXT,
	model TEXT,
	prompt TEXT,
	invocation_class TEXT,
	defaults_json TEXT NOT NULL DEFAULT '{}',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	UNIQUE(workspace_id, name),
	FOREIGN KEY(workspace_id) REFERENCES workspaces(workspace_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX agent_profiles_global_name_idx
	ON agent_profiles(name)
	WHERE workspace_id IS NULL;
`); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	store, err := globaldb.NewSQLStore(db)
	if err != nil {
		t.Fatalf("NewSQLStore returned error: %v", err)
	}

	return store
}

func newSessionMethodStoreWithoutFolders(t *testing.T) *globaldb.Store {
	t.Helper()

	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	if _, err := db.Exec(`
CREATE TABLE workspaces (
	workspace_id TEXT PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	status TEXT NOT NULL DEFAULT 'active',
	vcs_preference TEXT NOT NULL DEFAULT 'auto',
	origin_root TEXT NOT NULL,
	cleanup_policy TEXT NOT NULL DEFAULT 'manual',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	workspace_kind TEXT NOT NULL DEFAULT 'project'
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

func makeGitRoot(root string) error {
	return os.MkdirAll(filepath.Join(root, ".git"), 0o755)
}
