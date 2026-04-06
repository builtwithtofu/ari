package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
	"github.com/sourcegraph/jsonrpc2"
)

func TestRootRegistersSessionCommand(t *testing.T) {
	root := NewRootCmd()
	sessionCmd, _, err := root.Find([]string{"session"})
	if err != nil {
		t.Fatalf("find session command: %v", err)
	}

	if sessionCmd == nil {
		t.Fatalf("expected session command to be registered")
	}

	if sessionCmd.Name() != "session" {
		t.Fatalf("unexpected command name: %q", sessionCmd.Name())
	}
}

func TestSessionSubcommandsExist(t *testing.T) {
	session := NewSessionCmd()

	create, _, err := session.Find([]string{"create"})
	if err != nil {
		t.Fatalf("find session create: %v", err)
	}
	list, _, err := session.Find([]string{"list"})
	if err != nil {
		t.Fatalf("find session list: %v", err)
	}
	show, _, err := session.Find([]string{"show"})
	if err != nil {
		t.Fatalf("find session show: %v", err)
	}
	closeCmd, _, err := session.Find([]string{"close"})
	if err != nil {
		t.Fatalf("find session close: %v", err)
	}
	suspend, _, err := session.Find([]string{"suspend"})
	if err != nil {
		t.Fatalf("find session suspend: %v", err)
	}
	resume, _, err := session.Find([]string{"resume"})
	if err != nil {
		t.Fatalf("find session resume: %v", err)
	}
	set, _, err := session.Find([]string{"set"})
	if err != nil {
		t.Fatalf("find session set: %v", err)
	}
	current, _, err := session.Find([]string{"current"})
	if err != nil {
		t.Fatalf("find session current: %v", err)
	}
	clear, _, err := session.Find([]string{"clear"})
	if err != nil {
		t.Fatalf("find session clear: %v", err)
	}
	folderAdd, _, err := session.Find([]string{"folder", "add"})
	if err != nil {
		t.Fatalf("find session folder add: %v", err)
	}
	folderRemove, _, err := session.Find([]string{"folder", "remove"})
	if err != nil {
		t.Fatalf("find session folder remove: %v", err)
	}

	if create == nil || list == nil || show == nil || closeCmd == nil || suspend == nil || resume == nil || set == nil || current == nil || clear == nil || folderAdd == nil || folderRemove == nil {
		t.Fatal("expected session subcommands to be registered")
	}
}

func TestSessionSetCurrentAndClear(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalGet := sessionGetRPC
	originalList := sessionListRPC
	sessionGetRPC = func(context.Context, string, string) (daemon.SessionGetResponse, error) {
		return daemon.SessionGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "session not found"}
	}
	sessionListRPC = func(context.Context, string) (daemon.SessionListResponse, error) {
		return daemon.SessionListResponse{Sessions: []daemon.SessionSummary{{SessionID: "sess-12345678", Name: "alpha", Status: "active"}}}, nil
	}
	t.Cleanup(func() {
		sessionGetRPC = originalGet
		sessionListRPC = originalList
	})

	setOut, err := executeRootCommand("session", "set", "alpha")
	if err != nil {
		t.Fatalf("execute session set: %v", err)
	}
	if !strings.Contains(setOut, "sess-12345678") {
		t.Fatalf("session set output = %q, want canonical session id", setOut)
	}

	currentOut, err := executeRootCommand("session", "current")
	if err != nil {
		t.Fatalf("execute session current: %v", err)
	}
	if !strings.Contains(currentOut, "sess-12345678") {
		t.Fatalf("session current output = %q, want stored session id", currentOut)
	}

	clearOut, err := executeRootCommand("session", "clear")
	if err != nil {
		t.Fatalf("execute session clear: %v", err)
	}
	if !strings.Contains(clearOut, "Cleared active workspace session") {
		t.Fatalf("session clear output = %q, want clear confirmation", clearOut)
	}

	_, err = executeRootCommand("session", "current")
	if err == nil {
		t.Fatal("session current after clear returned nil error")
	}
	if err.Error() != "No active workspace session is set" {
		t.Fatalf("session current after clear error = %q, want %q", err.Error(), "No active workspace session is set")
	}
}

func TestSessionCreateUsesCWDDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})

	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	originalCreate := sessionCreateRPC
	var gotReq daemon.SessionCreateRequest
	sessionCreateRPC = func(_ context.Context, _ string, req daemon.SessionCreateRequest) (daemon.SessionCreateResponse, error) {
		gotReq = req
		return daemon.SessionCreateResponse{SessionID: "sess-1", Name: req.Name, Status: "active", Folder: req.Folder, VCSType: "git", IsPrimary: true, OriginRoot: req.OriginRoot}, nil
	}
	t.Cleanup(func() {
		sessionCreateRPC = originalCreate
	})

	if err := os.MkdirAll(filepath.Join(cwd, ".git"), 0o755); err != nil {
		t.Fatalf("create .git dir: %v", err)
	}

	out, err := executeRootCommand("session", "create", "alpha")
	if err != nil {
		t.Fatalf("execute session create: %v", err)
	}

	if gotReq.Folder != cwd {
		t.Fatalf("create folder = %q, want %q", gotReq.Folder, cwd)
	}
	if gotReq.OriginRoot != cwd {
		t.Fatalf("create origin root = %q, want %q", gotReq.OriginRoot, cwd)
	}
	if gotReq.CleanupPolicy != "manual" {
		t.Fatalf("create cleanup policy = %q, want %q", gotReq.CleanupPolicy, "manual")
	}
	if gotReq.VCSPreference != "auto" {
		t.Fatalf("create vcs preference = %q, want %q", gotReq.VCSPreference, "auto")
	}
	if !strings.Contains(out, "Session created: alpha") {
		t.Fatalf("session create output = %q, want created confirmation", out)
	}
}

func TestSessionCreateUsesDetectedVCSRootForDefaultFolder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	repoRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatalf("create .git dir: %v", err)
	}
	subdir := filepath.Join(repoRoot, "nested", "work")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("create nested dir: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
	if err := os.Chdir(subdir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	originalCreate := sessionCreateRPC
	var gotReq daemon.SessionCreateRequest
	sessionCreateRPC = func(_ context.Context, _ string, req daemon.SessionCreateRequest) (daemon.SessionCreateResponse, error) {
		gotReq = req
		return daemon.SessionCreateResponse{SessionID: "sess-1", Name: req.Name, Status: "active", Folder: req.Folder, VCSType: "git", IsPrimary: true, OriginRoot: req.OriginRoot}, nil
	}
	t.Cleanup(func() {
		sessionCreateRPC = originalCreate
	})

	_, err = executeRootCommand("session", "create", "alpha")
	if err != nil {
		t.Fatalf("execute session create: %v", err)
	}

	if gotReq.Folder != repoRoot {
		t.Fatalf("create folder = %q, want repo root %q", gotReq.Folder, repoRoot)
	}
	if gotReq.OriginRoot != subdir {
		t.Fatalf("create origin root = %q, want %q", gotReq.OriginRoot, subdir)
	}
	if gotReq.VCSPreference != "auto" {
		t.Fatalf("create vcs preference = %q, want %q", gotReq.VCSPreference, "auto")
	}
}

func TestSessionFolderCommandsNormalizeRelativePaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	originalGet := sessionGetRPC
	originalList := sessionListRPC
	originalAdd := sessionAddFolderRPC
	originalRemove := sessionRemoveFolderRPC

	sessionGetRPC = func(context.Context, string, string) (daemon.SessionGetResponse, error) {
		return daemon.SessionGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "session not found"}
	}
	sessionListRPC = func(context.Context, string) (daemon.SessionListResponse, error) {
		return daemon.SessionListResponse{Sessions: []daemon.SessionSummary{{SessionID: "sess-1", Name: "alpha"}}}, nil
	}
	var addPath string
	sessionAddFolderRPC = func(_ context.Context, _ string, req daemon.SessionAddFolderRequest) (daemon.SessionAddFolderResponse, error) {
		addPath = req.FolderPath
		return daemon.SessionAddFolderResponse{FolderPath: req.FolderPath, VCSType: "git"}, nil
	}
	var removePath string
	sessionRemoveFolderRPC = func(_ context.Context, _ string, req daemon.SessionRemoveFolderRequest) error {
		removePath = req.FolderPath
		return nil
	}
	t.Cleanup(func() {
		sessionGetRPC = originalGet
		sessionListRPC = originalList
		sessionAddFolderRPC = originalAdd
		sessionRemoveFolderRPC = originalRemove
	})

	_, err = executeRootCommand("session", "folder", "add", "alpha", "relative/repo")
	if err != nil {
		t.Fatalf("execute folder add: %v", err)
	}
	if addPath != filepath.Join(cwd, "relative", "repo") {
		t.Fatalf("folder add path = %q, want %q", addPath, filepath.Join(cwd, "relative", "repo"))
	}

	_, err = executeRootCommand("session", "folder", "remove", "alpha", "relative/repo")
	if err != nil {
		t.Fatalf("execute folder remove: %v", err)
	}
	if removePath != filepath.Join(cwd, "relative", "repo") {
		t.Fatalf("folder remove path = %q, want %q", removePath, filepath.Join(cwd, "relative", "repo"))
	}
}

func TestSessionListPrintsEntries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalList := sessionListRPC
	sessionListRPC = func(context.Context, string) (daemon.SessionListResponse, error) {
		return daemon.SessionListResponse{Sessions: []daemon.SessionSummary{
			{SessionID: "11111111-1111-1111-1111-111111111111", Name: "alpha", Status: "active", FolderCount: 2, CreatedAt: "now"},
			{SessionID: "22222222-2222-2222-2222-222222222222", Name: "beta", Status: "suspended", FolderCount: 1, CreatedAt: "later"},
		}}, nil
	}
	t.Cleanup(func() {
		sessionListRPC = originalList
	})

	out, err := executeRootCommand("session", "list")
	if err != nil {
		t.Fatalf("execute session list: %v", err)
	}

	if !strings.Contains(out, "alpha") {
		t.Fatalf("session list output = %q, want alpha", out)
	}
	if !strings.Contains(out, "beta") {
		t.Fatalf("session list output = %q, want beta", out)
	}
}

func TestResolveSessionIdentifierByNameAndPrefix(t *testing.T) {
	originalGet := sessionGetRPC
	originalList := sessionListRPC

	sessionGetRPC = func(context.Context, string, string) (daemon.SessionGetResponse, error) {
		return daemon.SessionGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "session not found"}
	}
	sessionListRPC = func(context.Context, string) (daemon.SessionListResponse, error) {
		return daemon.SessionListResponse{Sessions: []daemon.SessionSummary{
			{SessionID: "aaaaaaaa-1111-1111-1111-111111111111", Name: "alpha"},
			{SessionID: "bbbbbbbb-2222-2222-2222-222222222222", Name: "beta"},
		}}, nil
	}
	t.Cleanup(func() {
		sessionGetRPC = originalGet
		sessionListRPC = originalList
	})

	byName, err := resolveSessionIdentifier(context.Background(), "/tmp/daemon.sock", "alpha")
	if err != nil {
		t.Fatalf("resolve by name returned error: %v", err)
	}
	if byName != "aaaaaaaa-1111-1111-1111-111111111111" {
		t.Fatalf("resolve by name = %q, want %q", byName, "aaaaaaaa-1111-1111-1111-111111111111")
	}

	byPrefix, err := resolveSessionIdentifier(context.Background(), "/tmp/daemon.sock", "bbbb")
	if err != nil {
		t.Fatalf("resolve by prefix returned error: %v", err)
	}
	if byPrefix != "bbbbbbbb-2222-2222-2222-222222222222" {
		t.Fatalf("resolve by prefix = %q, want %q", byPrefix, "bbbbbbbb-2222-2222-2222-222222222222")
	}
}

func TestResolveSessionIdentifierRejectsAmbiguousPrefix(t *testing.T) {
	originalGet := sessionGetRPC
	originalList := sessionListRPC

	sessionGetRPC = func(context.Context, string, string) (daemon.SessionGetResponse, error) {
		return daemon.SessionGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.SessionNotFound), Message: "session not found"}
	}
	sessionListRPC = func(context.Context, string) (daemon.SessionListResponse, error) {
		return daemon.SessionListResponse{Sessions: []daemon.SessionSummary{
			{SessionID: "abc11111-1111-1111-1111-111111111111", Name: "alpha"},
			{SessionID: "abc22222-2222-2222-2222-222222222222", Name: "beta"},
		}}, nil
	}
	t.Cleanup(func() {
		sessionGetRPC = originalGet
		sessionListRPC = originalList
	})

	_, err := resolveSessionIdentifier(context.Background(), "/tmp/daemon.sock", "abc")
	if err == nil {
		t.Fatal("resolve ambiguous prefix returned nil error")
	}
	if err.Error() != "Session ID prefix is ambiguous" {
		t.Fatalf("resolve ambiguous prefix error = %q, want %q", err.Error(), "Session ID prefix is ambiguous")
	}
}

func TestSessionCreateAllowsVCSPreferenceOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(cwd, ".git"), 0o755); err != nil {
		t.Fatalf("create .git dir: %v", err)
	}

	originalCreate := sessionCreateRPC
	var gotReq daemon.SessionCreateRequest
	sessionCreateRPC = func(_ context.Context, _ string, req daemon.SessionCreateRequest) (daemon.SessionCreateResponse, error) {
		gotReq = req
		return daemon.SessionCreateResponse{SessionID: "sess-1", Name: req.Name, Status: "active", Folder: req.Folder, VCSType: "git", IsPrimary: true, OriginRoot: req.OriginRoot}, nil
	}
	t.Cleanup(func() {
		sessionCreateRPC = originalCreate
	})

	_, err = executeRootCommand("session", "create", "alpha", "--vcs-preference", "jj")
	if err != nil {
		t.Fatalf("execute session create: %v", err)
	}

	if gotReq.VCSPreference != "jj" {
		t.Fatalf("create vcs preference = %q, want %q", gotReq.VCSPreference, "jj")
	}
}
