package query

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestListSessionsSortedByNewest(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	if err := writeJSON(
		filepath.Join(repoRoot, ".gaia", "runtime", "s-old", "state.json"),
		map[string]any{
			"session_id":           "s-old",
			"current_stream_id":    "feature-a",
			"active_work_units":    []string{"u1"},
			"completed_work_units": []string{},
			"blocked_work_units":   []string{},
		},
	); err != nil {
		t.Fatalf("write old session state failed: %v", err)
	}

	if err := writeJSON(
		filepath.Join(repoRoot, ".gaia", "runtime", "s-new", "state.json"),
		map[string]any{
			"session_id":           "s-new",
			"current_stream_id":    "feature-b",
			"active_work_units":    []string{"u2"},
			"completed_work_units": []string{"u1"},
			"blocked_work_units":   []string{"u3"},
		},
	); err != nil {
		t.Fatalf("write new session state failed: %v", err)
	}

	oldTime := time.Date(2026, time.February, 19, 10, 0, 0, 0, time.UTC)
	newTime := time.Date(2026, time.February, 19, 11, 0, 0, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(repoRoot, ".gaia", "runtime", "s-old", "state.json"), oldTime, oldTime); err != nil {
		t.Fatalf("set old mtime failed: %v", err)
	}
	if err := os.Chtimes(filepath.Join(repoRoot, ".gaia", "runtime", "s-new", "state.json"), newTime, newTime); err != nil {
		t.Fatalf("set new mtime failed: %v", err)
	}

	sessions, err := ListSessions(repoRoot)
	if err != nil {
		t.Fatalf("ListSessions returned error: %v", err)
	}

	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}

	if sessions[0].SessionID != "s-new" {
		t.Fatalf("expected newest session first, got %s", sessions[0].SessionID)
	}

	if sessions[0].BlockedCount != 1 || sessions[0].CompletedCount != 1 || sessions[0].ActiveCount != 1 {
		t.Fatalf("unexpected count values for s-new: %+v", sessions[0])
	}
}

func TestResolveSessionIDReturnsLatestWhenEmpty(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	if err := writeJSON(
		filepath.Join(repoRoot, ".gaia", "runtime", "s-latest", "state.json"),
		map[string]any{"session_id": "s-latest"},
	); err != nil {
		t.Fatalf("write state failed: %v", err)
	}

	resolved, err := ResolveSessionID(repoRoot, "")
	if err != nil {
		t.Fatalf("ResolveSessionID returned error: %v", err)
	}

	if resolved != "s-latest" {
		t.Fatalf("expected s-latest, got %s", resolved)
	}
}

func TestResolveSessionIDFailsWhenNoSessions(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	_, err := ResolveSessionID(repoRoot, "")
	if !errors.Is(err, ErrNoSessionState) {
		t.Fatalf("expected ErrNoSessionState, got %v", err)
	}
}

func TestReadSessionAndLifecycleState(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	if err := writeJSON(
		filepath.Join(repoRoot, ".gaia", "runtime", "s-1", "state.json"),
		map[string]any{"session_id": "s-1", "current_stream_id": "feature-x"},
	); err != nil {
		t.Fatalf("write runtime state failed: %v", err)
	}

	if err := writeJSON(
		filepath.Join(repoRoot, ".gaia", "lifecycle", "s-1.json"),
		map[string]any{"session_id": "s-1", "state": "planning_ready"},
	); err != nil {
		t.Fatalf("write lifecycle state failed: %v", err)
	}

	runtimeState, resolved, err := ReadSessionState(repoRoot, "s-1")
	if err != nil {
		t.Fatalf("ReadSessionState returned error: %v", err)
	}

	if resolved != "s-1" {
		t.Fatalf("expected resolved session s-1, got %s", resolved)
	}

	if runtimeState["current_stream_id"] != "feature-x" {
		t.Fatalf("unexpected runtime stream value: %v", runtimeState["current_stream_id"])
	}

	lifecycleState, resolvedLifecycle, err := ReadLifecycleState(repoRoot, "s-1")
	if err != nil {
		t.Fatalf("ReadLifecycleState returned error: %v", err)
	}

	if resolvedLifecycle != "s-1" {
		t.Fatalf("expected lifecycle session s-1, got %s", resolvedLifecycle)
	}

	if lifecycleState["state"] != "planning_ready" {
		t.Fatalf("unexpected lifecycle state value: %v", lifecycleState["state"])
	}
}

func TestReadFlowState(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	if err := writeJSON(
		filepath.Join(repoRoot, ".gaia", "runtime", "s-flow", "state.json"),
		map[string]any{"session_id": "s-flow", "current_stream_id": "feature-x"},
	); err != nil {
		t.Fatalf("write runtime state failed: %v", err)
	}

	if err := writeJSON(
		filepath.Join(repoRoot, ".gaia", "flows", "s-flow.json"),
		map[string]any{"session_id": "s-flow", "state": "planning_ready", "iteration": 2},
	); err != nil {
		t.Fatalf("write flow state failed: %v", err)
	}

	flowState, resolved, err := ReadFlowState(repoRoot, "")
	if err != nil {
		t.Fatalf("ReadFlowState returned error: %v", err)
	}

	if resolved != "s-flow" {
		t.Fatalf("expected resolved session s-flow, got %s", resolved)
	}

	if flowState["iteration"] != float64(2) {
		t.Fatalf("unexpected flow iteration: %v", flowState["iteration"])
	}
}

func TestReadActivePlanState(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	if err := writeJSON(
		filepath.Join(repoRoot, ".gaia", "runtime", "s-active", "state.json"),
		map[string]any{"session_id": "s-active", "current_stream_id": "feature-x"},
	); err != nil {
		t.Fatalf("write runtime state failed: %v", err)
	}

	if err := writeJSON(
		filepath.Join(repoRoot, ".gaia", "runtime", "s-active", "active-plan.json"),
		map[string]any{
			"session_id": "s-active",
			"work_unit":  "unit-42",
			"stream_id":  "feature-x",
			"risk_level": "medium",
			"status":     "ok",
		},
	); err != nil {
		t.Fatalf("write active plan failed: %v", err)
	}

	activePlan, resolved, err := ReadActivePlanState(repoRoot, "")
	if err != nil {
		t.Fatalf("ReadActivePlanState returned error: %v", err)
	}

	if resolved != "s-active" {
		t.Fatalf("expected resolved session s-active, got %s", resolved)
	}

	if activePlan["work_unit"] != "unit-42" {
		t.Fatalf("unexpected active plan work unit: %v", activePlan["work_unit"])
	}
}

func TestReadSurfaceRegistry(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	if err := writeJSON(
		filepath.Join(repoRoot, ".gaia", "surfaces", "registry.json"),
		map[string]any{
			"version": "1.0",
			"surfaces": []any{
				map[string]any{"surface_id": "gaia.cli", "stability_lane": "experimental"},
			},
		},
	); err != nil {
		t.Fatalf("write surface registry failed: %v", err)
	}

	registry, err := ReadSurfaceRegistry(repoRoot)
	if err != nil {
		t.Fatalf("ReadSurfaceRegistry returned error: %v", err)
	}

	if registry["version"] != "1.0" {
		t.Fatalf("unexpected registry version: %v", registry["version"])
	}
}

func writeJSON(path string, payload map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	return os.WriteFile(path, encoded, 0o644)
}
