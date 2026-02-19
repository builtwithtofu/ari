package status

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadLatestRuntimeSummary(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()

	oldSessionDir := filepath.Join(repoRoot, ".gaia", "runtime", "s-old")
	if err := os.MkdirAll(oldSessionDir, 0o755); err != nil {
		t.Fatalf("MkdirAll old session failed: %v", err)
	}

	oldState := []byte(`{"session_id":"s-old","current_stream_id":"feature-a","active_work_units":["u1"],"completed_work_units":[],"blocked_work_units":[]}`)
	oldStatePath := filepath.Join(oldSessionDir, "state.json")
	if err := os.WriteFile(oldStatePath, oldState, 0o644); err != nil {
		t.Fatalf("WriteFile old state failed: %v", err)
	}

	newSessionDir := filepath.Join(repoRoot, ".gaia", "runtime", "s-new")
	if err := os.MkdirAll(newSessionDir, 0o755); err != nil {
		t.Fatalf("MkdirAll new session failed: %v", err)
	}

	newState := []byte(`{"session_id":"s-new","current_stream_id":"feature-b","active_work_units":["u2"],"completed_work_units":["u1"],"blocked_work_units":["u3"]}`)
	newStatePath := filepath.Join(newSessionDir, "state.json")
	if err := os.WriteFile(newStatePath, newState, 0o644); err != nil {
		t.Fatalf("WriteFile new state failed: %v", err)
	}

	oldTime := time.Date(2026, time.February, 18, 11, 0, 0, 0, time.UTC)
	newTime := time.Date(2026, time.February, 18, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(oldStatePath, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes old state failed: %v", err)
	}
	if err := os.Chtimes(newStatePath, newTime, newTime); err != nil {
		t.Fatalf("Chtimes new state failed: %v", err)
	}

	summary, err := LoadRuntimeSummary(repoRoot, "")
	if err != nil {
		t.Fatalf("LoadRuntimeSummary returned error: %v", err)
	}

	if summary.SessionID != "s-new" {
		t.Fatalf("expected s-new, got %s", summary.SessionID)
	}
	if summary.CurrentStreamID != "feature-b" {
		t.Fatalf("expected feature-b, got %s", summary.CurrentStreamID)
	}
	if summary.ActiveCount != 1 || summary.CompletedCount != 1 || summary.BlockedCount != 1 {
		t.Fatalf("unexpected counts: active=%d completed=%d blocked=%d", summary.ActiveCount, summary.CompletedCount, summary.BlockedCount)
	}
}
