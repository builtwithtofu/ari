package sandbox

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestListWorkspacesSortedByNewestFirst(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	workspacesRoot := filepath.Join(repoRoot, ".sandbox", "workspaces")

	older := filepath.Join(workspacesRoot, "20260218-100000-feature-x")
	newer := filepath.Join(workspacesRoot, "20260218-110000-urgent-bug")

	if err := os.MkdirAll(older, 0o755); err != nil {
		t.Fatalf("MkdirAll older failed: %v", err)
	}
	if err := os.MkdirAll(newer, 0o755); err != nil {
		t.Fatalf("MkdirAll newer failed: %v", err)
	}

	oldTime := time.Date(2026, time.February, 18, 10, 0, 0, 0, time.UTC)
	newTime := time.Date(2026, time.February, 18, 11, 0, 0, 0, time.UTC)
	if err := os.Chtimes(older, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes older failed: %v", err)
	}
	if err := os.Chtimes(newer, newTime, newTime); err != nil {
		t.Fatalf("Chtimes newer failed: %v", err)
	}

	listed, err := ListWorkspaces(repoRoot)
	if err != nil {
		t.Fatalf("ListWorkspaces returned error: %v", err)
	}

	if len(listed) != 2 {
		t.Fatalf("expected 2 workspaces, got %d", len(listed))
	}

	if listed[0].Name != "20260218-110000-urgent-bug" {
		t.Fatalf("expected newest workspace first, got %s", listed[0].Name)
	}
}
