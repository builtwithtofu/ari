package globaldb

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestSessionCreateAndLookup(t *testing.T) {
	store := newSessionTestStore(t)
	ctx := context.Background()

	err := store.CreateSession(ctx, "sess-1", "alpha", "/tmp/origin", "manual", "auto")
	if err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}

	gotByID, err := store.GetSession(ctx, "sess-1")
	if err != nil {
		t.Fatalf("GetSession returned error: %v", err)
	}
	if gotByID.ID != "sess-1" {
		t.Fatalf("GetSession ID = %q, want %q", gotByID.ID, "sess-1")
	}
	if gotByID.Name != "alpha" {
		t.Fatalf("GetSession Name = %q, want %q", gotByID.Name, "alpha")
	}
	if gotByID.Status != "active" {
		t.Fatalf("GetSession Status = %q, want %q", gotByID.Status, "active")
	}
	if gotByID.VCSPreference != "auto" {
		t.Fatalf("GetSession VCSPreference = %q, want %q", gotByID.VCSPreference, "auto")
	}

	gotByName, err := store.GetSessionByName(ctx, "alpha")
	if err != nil {
		t.Fatalf("GetSessionByName returned error: %v", err)
	}
	if gotByName.ID != "sess-1" {
		t.Fatalf("GetSessionByName ID = %q, want %q", gotByName.ID, "sess-1")
	}

	err = store.CreateSession(ctx, "sess-2", "alpha", "/tmp/origin2", "manual", "auto")
	if err == nil {
		t.Fatal("CreateSession returned nil error for duplicate name")
	}
}

func TestSessionCreateStoresExplicitVCSPreference(t *testing.T) {
	store := newSessionTestStore(t)
	ctx := context.Background()

	err := store.CreateSession(ctx, "sess-1", "alpha", "/tmp/origin", "manual", "git")
	if err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}

	got, err := store.GetSession(ctx, "sess-1")
	if err != nil {
		t.Fatalf("GetSession returned error: %v", err)
	}
	if got.VCSPreference != "git" {
		t.Fatalf("GetSession VCSPreference = %q, want %q", got.VCSPreference, "git")
	}
}

func TestEnsureSystemWorkspaceCreatesSingletonWithoutFolder(t *testing.T) {
	store := newSessionTestStore(t)
	ctx := context.Background()

	first, err := store.EnsureSystemWorkspace(ctx, "system-1")
	if err != nil {
		t.Fatalf("EnsureSystemWorkspace returned error: %v", err)
	}
	second, err := store.EnsureSystemWorkspace(ctx, "system-2")
	if err != nil {
		t.Fatalf("EnsureSystemWorkspace second call returned error: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("system workspace IDs differ: %q vs %q", first.ID, second.ID)
	}
	if first.Name != "system" || first.Kind != "system" || first.OriginRoot != "" {
		t.Fatalf("unexpected system workspace: %#v", first)
	}
	folders, err := store.ListFolders(ctx, first.ID)
	if err != nil {
		t.Fatalf("ListFolders returned error: %v", err)
	}
	if len(folders) != 0 {
		t.Fatalf("system folder count = %d, want 0", len(folders))
	}
}

func TestEnsureSystemWorkspaceRejectsLegacyProjectNameCollision(t *testing.T) {
	store := newSessionTestStore(t)
	ctx := context.Background()
	if err := store.CreateSession(ctx, "project-system", "system", "/tmp/origin", "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	_, err := store.EnsureSystemWorkspace(ctx, "system-1")
	if err == nil {
		t.Fatal("EnsureSystemWorkspace returned nil error for project named system")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("EnsureSystemWorkspace error = %v, want ErrInvalidInput", err)
	}
}

func TestEnsureSystemWorkspaceConcurrentCallsReturnSingleton(t *testing.T) {
	store := newSessionTestStore(t)
	ctx := context.Background()
	const workers = 2
	results := make(chan string, workers)
	errs := make(chan error, workers)
	var start sync.WaitGroup
	start.Add(1)
	for i := 0; i < workers; i++ {
		go func(id string) {
			start.Wait()
			system, err := store.EnsureSystemWorkspace(ctx, id)
			if err != nil {
				errs <- err
				return
			}
			results <- system.ID
		}(fmt.Sprintf("system-%d", i))
	}
	start.Done()
	for i := 0; i < workers; i++ {
		select {
		case err := <-errs:
			t.Fatalf("EnsureSystemWorkspace returned error: %v", err)
		case <-results:
		}
	}
}

func TestSystemWorkspaceRejectsFoldersAndStatusChanges(t *testing.T) {
	store := newSessionTestStore(t)
	ctx := context.Background()
	systemWorkspace, err := store.EnsureSystemWorkspace(ctx, "system-1")
	if err != nil {
		t.Fatalf("EnsureSystemWorkspace returned error: %v", err)
	}
	if err := store.AddFolder(ctx, systemWorkspace.ID, "/tmp/repo", "git", true); err == nil || !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("AddFolder system error = %v, want ErrInvalidInput", err)
	}
	if err := store.UpdateSessionStatus(ctx, systemWorkspace.ID, statusSuspended); err == nil || !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("UpdateSessionStatus system error = %v, want ErrInvalidInput", err)
	}
}

func TestSessionCreateRejectsInvalidVCSPreference(t *testing.T) {
	store := newSessionTestStore(t)
	ctx := context.Background()

	err := store.CreateSession(ctx, "sess-1", "alpha", "/tmp/origin", "manual", "gti")
	if err == nil {
		t.Fatal("CreateSession returned nil error for invalid vcs preference")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("CreateSession error = %v, want ErrInvalidInput", err)
	}
}

func TestSessionStatusTransitions(t *testing.T) {
	store := newSessionTestStore(t)
	ctx := context.Background()

	err := store.CreateSession(ctx, "sess-1", "alpha", "/tmp/origin", "manual", "auto")
	if err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}

	tests := []struct {
		name       string
		toStatus   string
		wantErr    bool
		wantClosed bool
	}{
		{name: "active to suspended", toStatus: "suspended"},
		{name: "suspended to active", toStatus: "active"},
		{name: "active to closed", toStatus: "closed", wantClosed: true},
		{name: "closed to active rejected", toStatus: "active", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := store.UpdateSessionStatus(ctx, "sess-1", tc.toStatus)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("UpdateSessionStatus returned nil error, want error")
				}
				if !errors.Is(err, ErrSessionClosed) {
					t.Fatalf("UpdateSessionStatus error = %v, want ErrSessionClosed", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("UpdateSessionStatus returned error: %v", err)
			}

			got, err := store.GetSession(ctx, "sess-1")
			if err != nil {
				t.Fatalf("GetSession returned error: %v", err)
			}
			if got.Status != tc.toStatus {
				t.Fatalf("GetSession Status = %q, want %q", got.Status, tc.toStatus)
			}
		})
	}
}

func TestSessionFolderOperationsAndGuards(t *testing.T) {
	store := newSessionTestStore(t)
	ctx := context.Background()

	err := store.CreateSession(ctx, "sess-1", "alpha", "/tmp/origin", "manual", "auto")
	if err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}

	err = store.AddFolder(ctx, "sess-1", "/tmp/repo-a", "git", true)
	if err != nil {
		t.Fatalf("AddFolder returned error: %v", err)
	}
	err = store.AddFolder(ctx, "sess-1", "/tmp/repo-b", "jj", false)
	if err != nil {
		t.Fatalf("AddFolder returned error: %v", err)
	}

	folders, err := store.ListFolders(ctx, "sess-1")
	if err != nil {
		t.Fatalf("ListFolders returned error: %v", err)
	}
	if len(folders) != 2 {
		t.Fatalf("ListFolders len = %d, want 2", len(folders))
	}

	err = store.RemoveFolder(ctx, "sess-1", "/tmp/repo-b")
	if err != nil {
		t.Fatalf("RemoveFolder returned error: %v", err)
	}

	folders, err = store.ListFolders(ctx, "sess-1")
	if err != nil {
		t.Fatalf("ListFolders after remove returned error: %v", err)
	}
	if len(folders) != 1 {
		t.Fatalf("ListFolders after remove len = %d, want 1", len(folders))
	}
	if !folders[0].IsPrimary {
		t.Fatal("remaining folder is not marked primary")
	}

	err = store.RemoveFolder(ctx, "sess-1", "/tmp/missing")
	if err == nil {
		t.Fatal("RemoveFolder returned nil error for missing folder")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("RemoveFolder missing folder error = %v, want ErrNotFound", err)
	}

	err = store.RemoveFolder(ctx, "sess-1", "/tmp/repo-a")
	if err == nil {
		t.Fatal("RemoveFolder returned nil error for last folder")
	}
	if !errors.Is(err, ErrLastFolder) {
		t.Fatalf("RemoveFolder error = %v, want ErrLastFolder", err)
	}

	err = store.UpdateSessionStatus(ctx, "sess-1", "closed")
	if err != nil {
		t.Fatalf("UpdateSessionStatus returned error: %v", err)
	}

	err = store.AddFolder(ctx, "sess-1", "/tmp/repo-c", "git", false)
	if err == nil {
		t.Fatal("AddFolder returned nil error for closed session")
	}
	if !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("AddFolder error = %v, want ErrSessionClosed", err)
	}
}

func TestAddFolderRejectsFolderAlreadyOwnedByAnotherWorkspace(t *testing.T) {
	store := newSessionTestStore(t)
	ctx := context.Background()

	if err := store.CreateSession(ctx, "sess-1", "alpha", "/tmp/origin-a", "manual", "auto"); err != nil {
		t.Fatalf("CreateSession sess-1 returned error: %v", err)
	}
	if err := store.CreateSession(ctx, "sess-2", "beta", "/tmp/origin-b", "manual", "auto"); err != nil {
		t.Fatalf("CreateSession sess-2 returned error: %v", err)
	}
	if err := store.AddFolder(ctx, "sess-1", "/tmp/repo-a", "git", true); err != nil {
		t.Fatalf("AddFolder sess-1 returned error: %v", err)
	}

	err := store.AddFolder(ctx, "sess-2", "/tmp/repo-a", "git", true)
	if err == nil {
		t.Fatal("AddFolder returned nil error for folder owned by another workspace")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("AddFolder duplicate folder error = %v, want ErrInvalidInput", err)
	}
}

func TestAddFolderRejectsAnyActiveHistoricalOwner(t *testing.T) {
	store := newSessionTestStore(t)
	ctx := context.Background()

	if err := store.CreateSession(ctx, "sess-1", "alpha", "/tmp/origin-a", "manual", "auto"); err != nil {
		t.Fatalf("CreateSession sess-1 returned error: %v", err)
	}
	if err := store.CreateSession(ctx, "sess-2", "beta", "/tmp/origin-b", "manual", "auto"); err != nil {
		t.Fatalf("CreateSession sess-2 returned error: %v", err)
	}
	if err := store.CreateSession(ctx, "sess-3", "gamma", "/tmp/origin-c", "manual", "auto"); err != nil {
		t.Fatalf("CreateSession sess-3 returned error: %v", err)
	}
	if err := store.UpdateSessionStatus(ctx, "sess-1", statusClosed); err != nil {
		t.Fatalf("UpdateSessionStatus returned error: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, insertSessionFolderQuery, "sess-1", "/tmp/repo-a", "git", 1, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("direct closed owner insert returned error: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, insertSessionFolderQuery, "sess-2", "/tmp/repo-a", "git", 1, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("direct active owner insert returned error: %v", err)
	}

	err := store.AddFolder(ctx, "sess-3", "/tmp/repo-a", "git", true)
	if err == nil {
		t.Fatal("AddFolder returned nil error with an active historical owner")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("AddFolder error = %v, want ErrInvalidInput", err)
	}
}

func TestAddFolderAllowsReuseFromClosedWorkspace(t *testing.T) {
	store := newSessionTestStore(t)
	ctx := context.Background()

	if err := store.CreateSession(ctx, "sess-1", "alpha", "/tmp/origin-a", "manual", "auto"); err != nil {
		t.Fatalf("CreateSession sess-1 returned error: %v", err)
	}
	if err := store.CreateSession(ctx, "sess-2", "beta", "/tmp/origin-b", "manual", "auto"); err != nil {
		t.Fatalf("CreateSession sess-2 returned error: %v", err)
	}
	if err := store.AddFolder(ctx, "sess-1", "/tmp/repo-a", "git", true); err != nil {
		t.Fatalf("AddFolder sess-1 returned error: %v", err)
	}
	if err := store.UpdateSessionStatus(ctx, "sess-1", statusClosed); err != nil {
		t.Fatalf("UpdateSessionStatus returned error: %v", err)
	}

	if err := store.AddFolder(ctx, "sess-2", "/tmp/repo-a", "git", true); err != nil {
		t.Fatalf("AddFolder should allow closed workspace folder reuse, got: %v", err)
	}
}

func TestAddFolderUsesImmediateTransactionForOwnershipCheck(t *testing.T) {
	db := &recordingDB{queryRows: &testRows{items: [][]any{{"sess-1", "alpha", "active", "auto", "/tmp/origin", "manual", "2026-04-25T00:00:00Z", "2026-04-25T00:00:00Z"}}}}
	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}

	if err := store.AddFolder(context.Background(), "sess-1", "/tmp/repo-a", "git", true); err != nil {
		t.Fatalf("AddFolder returned error: %v", err)
	}
	if !db.immediateTransactionStarted {
		t.Fatal("AddFolder did not use an immediate transaction for folder ownership check")
	}
}

func TestWorkspaceFolderPathMigrationAllowsHistoricalDuplicates(t *testing.T) {
	store := newSessionTestStore(t)
	ctx := context.Background()

	if err := store.CreateSession(ctx, "sess-1", "alpha", "/tmp/origin-a", "manual", "auto"); err != nil {
		t.Fatalf("CreateSession sess-1 returned error: %v", err)
	}
	if err := store.CreateSession(ctx, "sess-2", "beta", "/tmp/origin-b", "manual", "auto"); err != nil {
		t.Fatalf("CreateSession sess-2 returned error: %v", err)
	}
	if err := store.AddFolder(ctx, "sess-1", "/tmp/repo-a", "git", true); err != nil {
		t.Fatalf("AddFolder sess-1 returned error: %v", err)
	}

	_, err := store.db.ExecContext(ctx, insertSessionFolderQuery, "sess-2", "/tmp/repo-a", "git", 1, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatalf("direct workspace_folders insert returned error for historical duplicate folder path: %v", err)
	}
}

func TestListSessionsNewestFirst(t *testing.T) {
	store := newSessionTestStore(t)
	ctx := context.Background()

	err := store.CreateSession(ctx, "sess-1", "alpha", "/tmp/origin-a", "manual", "auto")
	if err != nil {
		t.Fatalf("CreateSession alpha returned error: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	err = store.CreateSession(ctx, "sess-2", "beta", "/tmp/origin-b", "manual", "auto")
	if err != nil {
		t.Fatalf("CreateSession beta returned error: %v", err)
	}

	sessions, err := store.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions returned error: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("ListSessions len = %d, want 2", len(sessions))
	}
	if sessions[0].Name != "beta" {
		t.Fatalf("ListSessions[0] = %q, want %q", sessions[0].Name, "beta")
	}
	if sessions[1].Name != "alpha" {
		t.Fatalf("ListSessions[1] = %q, want %q", sessions[1].Name, "alpha")
	}
}

func TestRemovePrimaryFolderPromotesAnotherFolder(t *testing.T) {
	store := newSessionTestStore(t)
	ctx := context.Background()

	err := store.CreateSession(ctx, "sess-1", "alpha", "/tmp/origin", "manual", "auto")
	if err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}

	err = store.AddFolder(ctx, "sess-1", "/tmp/repo-a", "git", true)
	if err != nil {
		t.Fatalf("AddFolder repo-a returned error: %v", err)
	}
	err = store.AddFolder(ctx, "sess-1", "/tmp/repo-b", "jj", false)
	if err != nil {
		t.Fatalf("AddFolder repo-b returned error: %v", err)
	}

	err = store.RemoveFolder(ctx, "sess-1", "/tmp/repo-a")
	if err != nil {
		t.Fatalf("RemoveFolder primary returned error: %v", err)
	}

	folders, err := store.ListFolders(ctx, "sess-1")
	if err != nil {
		t.Fatalf("ListFolders returned error: %v", err)
	}
	if len(folders) != 1 {
		t.Fatalf("ListFolders len = %d, want 1", len(folders))
	}
	if folders[0].FolderPath != "/tmp/repo-b" {
		t.Fatalf("remaining folder path = %q, want %q", folders[0].FolderPath, "/tmp/repo-b")
	}
	if !folders[0].IsPrimary {
		t.Fatal("remaining folder is not primary after removing original primary")
	}
}

func TestAddFolderPrimaryDemotesExistingPrimary(t *testing.T) {
	store := newSessionTestStore(t)
	ctx := context.Background()

	err := store.CreateSession(ctx, "sess-1", "alpha", "/tmp/origin", "manual", "auto")
	if err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}

	err = store.AddFolder(ctx, "sess-1", "/tmp/repo-a", "git", true)
	if err != nil {
		t.Fatalf("AddFolder repo-a returned error: %v", err)
	}
	err = store.AddFolder(ctx, "sess-1", "/tmp/repo-b", "jj", true)
	if err != nil {
		t.Fatalf("AddFolder repo-b returned error: %v", err)
	}

	folders, err := store.ListFolders(ctx, "sess-1")
	if err != nil {
		t.Fatalf("ListFolders returned error: %v", err)
	}

	primaryCount := 0
	for _, folder := range folders {
		if folder.IsPrimary {
			primaryCount++
		}
	}
	if primaryCount != 1 {
		t.Fatalf("primary folder count = %d, want 1", primaryCount)
	}
}

func TestUpdateSessionStatusRejectsClosedToClosed(t *testing.T) {
	store := newSessionTestStore(t)
	ctx := context.Background()

	err := store.CreateSession(ctx, "sess-1", "alpha", "/tmp/origin", "manual", "auto")
	if err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}

	err = store.UpdateSessionStatus(ctx, "sess-1", "closed")
	if err != nil {
		t.Fatalf("UpdateSessionStatus close returned error: %v", err)
	}

	err = store.UpdateSessionStatus(ctx, "sess-1", "closed")
	if err == nil {
		t.Fatal("UpdateSessionStatus returned nil error for closed->closed")
	}
	if !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("UpdateSessionStatus error = %v, want ErrSessionClosed", err)
	}
}

func TestConcurrentRemoveFolderKeepsOneFolder(t *testing.T) {
	store := newSessionTestStore(t)
	ctx := context.Background()

	err := store.CreateSession(ctx, "sess-1", "alpha", "/tmp/origin", "manual", "auto")
	if err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	if err := store.AddFolder(ctx, "sess-1", "/tmp/repo-a", "git", true); err != nil {
		t.Fatalf("AddFolder repo-a returned error: %v", err)
	}
	if err := store.AddFolder(ctx, "sess-1", "/tmp/repo-b", "jj", false); err != nil {
		t.Fatalf("AddFolder repo-b returned error: %v", err)
	}

	start := make(chan struct{})
	var wg sync.WaitGroup

	remove := func(path string) {
		defer wg.Done()
		<-start
		_ = store.RemoveFolder(context.Background(), "sess-1", path)
	}

	wg.Add(2)
	go remove("/tmp/repo-a")
	go remove("/tmp/repo-b")
	close(start)
	wg.Wait()

	folders, err := store.ListFolders(ctx, "sess-1")
	if err != nil {
		t.Fatalf("ListFolders returned error: %v", err)
	}
	if len(folders) != 1 {
		t.Fatalf("folder count = %d, want 1 after concurrent remove attempts", len(folders))
	}
}

func newSessionTestStore(t *testing.T) *Store {
	return newMigratedGlobalDBStore(t, "workspace-store")
}
