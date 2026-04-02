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

func TestSessionMethodsCreateAndGet(t *testing.T) {
	store := newSessionMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	err := d.registerSessionMethods(registry, store)
	if err != nil {
		t.Fatalf("registerSessionMethods returned error: %v", err)
	}

	repoRoot := t.TempDir()
	if err := makeGitRoot(repoRoot); err != nil {
		t.Fatalf("makeGitRoot returned error: %v", err)
	}

	createResp := callMethod[SessionCreateResponse](t, registry, "session.create", SessionCreateRequest{
		Name:          "alpha",
		Folder:        repoRoot,
		OriginRoot:    t.TempDir(),
		CleanupPolicy: "manual",
	})

	if createResp.SessionID == "" {
		t.Fatal("SessionCreateResponse.SessionID is empty")
	}
	if createResp.VCSType != "git" {
		t.Fatalf("SessionCreateResponse.VCSType = %q, want %q", createResp.VCSType, "git")
	}

	getResp := callMethod[SessionGetResponse](t, registry, "session.get", SessionGetRequest{SessionID: createResp.SessionID})
	if getResp.Name != "alpha" {
		t.Fatalf("SessionGetResponse.Name = %q, want %q", getResp.Name, "alpha")
	}
	if len(getResp.Folders) != 1 {
		t.Fatalf("SessionGetResponse folders len = %d, want 1", len(getResp.Folders))
	}
}

func TestSessionGetMissingReturnsSessionNotFound(t *testing.T) {
	store := newSessionMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	err := d.registerSessionMethods(registry, store)
	if err != nil {
		t.Fatalf("registerSessionMethods returned error: %v", err)
	}

	spec, ok := registry.Get("session.get")
	if !ok {
		t.Fatal("session.get method not registered")
	}

	raw, err := json.Marshal(SessionGetRequest{SessionID: "missing"})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	_, err = spec.Call(context.Background(), raw)
	if err == nil {
		t.Fatal("session.get returned nil error for missing session")
	}

	var rpcErr *rpc.HandlerError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("session.get error type = %T, want *rpc.HandlerError", err)
	}
	if rpcErr.Code != rpc.SessionNotFound {
		t.Fatalf("session.get error code = %d, want %d", rpcErr.Code, rpc.SessionNotFound)
	}
}

func TestSessionAddFolderRejectsSubdirectoryOfVCSRoot(t *testing.T) {
	store := newSessionMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	err := d.registerSessionMethods(registry, store)
	if err != nil {
		t.Fatalf("registerSessionMethods returned error: %v", err)
	}

	repoRoot := t.TempDir()
	if err := makeGitRoot(repoRoot); err != nil {
		t.Fatalf("makeGitRoot returned error: %v", err)
	}

	createResp := callMethod[SessionCreateResponse](t, registry, "session.create", SessionCreateRequest{
		Name:          "alpha",
		Folder:        repoRoot,
		OriginRoot:    t.TempDir(),
		CleanupPolicy: "manual",
	})

	subdir := filepath.Join(repoRoot, "nested")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("create subdir: %v", err)
	}

	spec, ok := registry.Get("session.add_folder")
	if !ok {
		t.Fatal("session.add_folder method not registered")
	}

	raw, err := json.Marshal(SessionAddFolderRequest{SessionID: createResp.SessionID, FolderPath: subdir})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	_, err = spec.Call(context.Background(), raw)
	if err == nil {
		t.Fatal("session.add_folder returned nil error for subdirectory input")
	}
}

func TestSessionGetResolvesByName(t *testing.T) {
	store := newSessionMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	err := d.registerSessionMethods(registry, store)
	if err != nil {
		t.Fatalf("registerSessionMethods returned error: %v", err)
	}

	repoRoot := t.TempDir()
	if err := makeGitRoot(repoRoot); err != nil {
		t.Fatalf("makeGitRoot returned error: %v", err)
	}

	createResp := callMethod[SessionCreateResponse](t, registry, "session.create", SessionCreateRequest{
		Name:          "alpha",
		Folder:        repoRoot,
		OriginRoot:    t.TempDir(),
		CleanupPolicy: "manual",
	})

	getResp := callMethod[SessionGetResponse](t, registry, "session.get", SessionGetRequest{SessionID: "alpha"})
	if getResp.SessionID != createResp.SessionID {
		t.Fatalf("session.get by name id = %q, want %q", getResp.SessionID, createResp.SessionID)
	}
}

func TestSessionRemoveFolderMissingReturnsInvalidParams(t *testing.T) {
	store := newSessionMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	err := d.registerSessionMethods(registry, store)
	if err != nil {
		t.Fatalf("registerSessionMethods returned error: %v", err)
	}

	repoRoot := t.TempDir()
	if err := makeGitRoot(repoRoot); err != nil {
		t.Fatalf("makeGitRoot returned error: %v", err)
	}

	createResp := callMethod[SessionCreateResponse](t, registry, "session.create", SessionCreateRequest{
		Name:          "alpha",
		Folder:        repoRoot,
		OriginRoot:    t.TempDir(),
		CleanupPolicy: "manual",
	})

	spec, ok := registry.Get("session.remove_folder")
	if !ok {
		t.Fatal("session.remove_folder method not registered")
	}

	raw, err := json.Marshal(SessionRemoveFolderRequest{SessionID: createResp.SessionID, FolderPath: filepath.Join(repoRoot, "missing")})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	_, err = spec.Call(context.Background(), raw)
	if err == nil {
		t.Fatal("session.remove_folder returned nil error for missing folder")
	}

	var rpcErr *rpc.HandlerError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("session.remove_folder error type = %T, want *rpc.HandlerError", err)
	}
	if rpcErr.Code != rpc.InvalidParams {
		t.Fatalf("session.remove_folder error code = %d, want %d", rpcErr.Code, rpc.InvalidParams)
	}
}

func TestSessionRemoveFolderMissingSessionReturnsSessionNotFound(t *testing.T) {
	store := newSessionMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	err := d.registerSessionMethods(registry, store)
	if err != nil {
		t.Fatalf("registerSessionMethods returned error: %v", err)
	}

	spec, ok := registry.Get("session.remove_folder")
	if !ok {
		t.Fatal("session.remove_folder method not registered")
	}

	raw, err := json.Marshal(SessionRemoveFolderRequest{SessionID: "missing", FolderPath: "/tmp/repo"})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	_, err = spec.Call(context.Background(), raw)
	if err == nil {
		t.Fatal("session.remove_folder returned nil error for missing session")
	}

	var rpcErr *rpc.HandlerError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("session.remove_folder error type = %T, want *rpc.HandlerError", err)
	}
	if rpcErr.Code != rpc.SessionNotFound {
		t.Fatalf("session.remove_folder error code = %d, want %d", rpcErr.Code, rpc.SessionNotFound)
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

	err := d.registerSessionMethods(registry, store)
	if err != nil {
		t.Fatalf("registerSessionMethods returned error: %v", err)
	}

	repoRoot := t.TempDir()
	if err := makeGitRoot(repoRoot); err != nil {
		t.Fatalf("makeGitRoot returned error: %v", err)
	}

	spec, ok := registry.Get("session.create")
	if !ok {
		t.Fatal("session.create method not registered")
	}

	raw, err := json.Marshal(SessionCreateRequest{
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
		t.Fatal("session.create returned nil error for invalid vcs preference")
	}

	var rpcErr *rpc.HandlerError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("session.create error type = %T, want *rpc.HandlerError", err)
	}
	if rpcErr.Code != rpc.InvalidParams {
		t.Fatalf("session.create error code = %d, want %d", rpcErr.Code, rpc.InvalidParams)
	}
}

func TestSessionCreateRollsBackWhenFolderInsertFails(t *testing.T) {
	store := newSessionMethodStoreWithoutFolders(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	err := d.registerSessionMethods(registry, store)
	if err != nil {
		t.Fatalf("registerSessionMethods returned error: %v", err)
	}

	repoRoot := t.TempDir()
	if err := makeGitRoot(repoRoot); err != nil {
		t.Fatalf("makeGitRoot returned error: %v", err)
	}

	spec, ok := registry.Get("session.create")
	if !ok {
		t.Fatal("session.create method not registered")
	}

	raw, err := json.Marshal(SessionCreateRequest{
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
		t.Fatal("session.create returned nil error when folder insert fails")
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
