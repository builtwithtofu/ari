package globaldb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestAgentStoreLifecycleAndReconciliation(t *testing.T) {
	store := newAgentTestStore(t)
	ctx := context.Background()

	if err := store.CreateSession(ctx, "sess-1", "alpha", "/tmp/origin", "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}

	createReq := CreateAgentParams{
		AgentID:   "agt-1",
		SessionID: "sess-1",
		Name:      stringPtr("claude"),
		Command:   "claude-code",
		Args:      `["--resume"]`,
		Status:    "running",
		StartedAt: "2026-04-04T00:00:00Z",
	}
	if err := store.CreateAgent(ctx, createReq); err != nil {
		t.Fatalf("CreateAgent returned error: %v", err)
	}

	gotByID, err := store.GetAgent(ctx, "sess-1", "agt-1")
	if err != nil {
		t.Fatalf("GetAgent returned error: %v", err)
	}
	if gotByID.AgentID != "agt-1" {
		t.Fatalf("GetAgent AgentID = %q, want %q", gotByID.AgentID, "agt-1")
	}
	if gotByID.Status != "running" {
		t.Fatalf("GetAgent Status = %q, want %q", gotByID.Status, "running")
	}

	gotByName, err := store.GetAgentByName(ctx, "sess-1", "claude")
	if err != nil {
		t.Fatalf("GetAgentByName returned error: %v", err)
	}
	if gotByName.AgentID != "agt-1" {
		t.Fatalf("GetAgentByName AgentID = %q, want %q", gotByName.AgentID, "agt-1")
	}

	list, err := store.ListAgents(ctx, "sess-1")
	if err != nil {
		t.Fatalf("ListAgents returned error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ListAgents len = %d, want 1", len(list))
	}
	if list[0].Name == nil || *list[0].Name != "claude" {
		t.Fatalf("ListAgents[0].Name = %v, want %q", list[0].Name, "claude")
	}

	updateReq := UpdateAgentStatusParams{
		SessionID: "sess-1",
		AgentID:   "agt-1",
		Status:    "stopped",
		ExitCode:  intPtr(0),
		StoppedAt: stringPtr("2026-04-04T00:00:10Z"),
	}
	if err := store.UpdateAgentStatus(ctx, updateReq); err != nil {
		t.Fatalf("UpdateAgentStatus returned error: %v", err)
	}

	updated, err := store.GetAgent(ctx, "sess-1", "agt-1")
	if err != nil {
		t.Fatalf("GetAgent after update returned error: %v", err)
	}
	if updated.Status != "stopped" {
		t.Fatalf("GetAgent after update Status = %q, want %q", updated.Status, "stopped")
	}
	if updated.ExitCode == nil || *updated.ExitCode != 0 {
		t.Fatalf("GetAgent after update ExitCode = %v, want 0", updated.ExitCode)
	}

	if err := store.CreateAgent(ctx, CreateAgentParams{
		AgentID:   "agt-2",
		SessionID: "sess-1",
		Command:   "codex",
		Args:      `[]`,
		Status:    "running",
		StartedAt: "2026-04-04T00:01:00Z",
	}); err != nil {
		t.Fatalf("CreateAgent agt-2 returned error: %v", err)
	}

	if err := store.MarkRunningAgentsLost(ctx); err != nil {
		t.Fatalf("MarkRunningAgentsLost returned error: %v", err)
	}

	lost, err := store.GetAgent(ctx, "sess-1", "agt-2")
	if err != nil {
		t.Fatalf("GetAgent agt-2 returned error: %v", err)
	}
	if lost.Status != "lost" {
		t.Fatalf("GetAgent agt-2 Status = %q, want %q", lost.Status, "lost")
	}
}

func TestGetAgentMissingReturnsNotFound(t *testing.T) {
	store := newAgentTestStore(t)
	ctx := context.Background()

	if err := store.CreateSession(ctx, "sess-1", "alpha", "/tmp/origin", "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}

	_, err := store.GetAgent(ctx, "sess-1", "missing")
	if err == nil {
		t.Fatal("GetAgent returned nil error for missing agent")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetAgent error = %v, want ErrNotFound", err)
	}

	_, err = store.GetAgentByName(ctx, "sess-1", "missing")
	if err == nil {
		t.Fatal("GetAgentByName returned nil error for missing agent")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetAgentByName error = %v, want ErrNotFound", err)
	}
}

func TestCreateAgentAllowsSameNameAcrossSessions(t *testing.T) {
	store := newAgentTestStore(t)
	ctx := context.Background()

	if err := store.CreateSession(ctx, "sess-1", "alpha", "/tmp/origin-a", "manual", "auto"); err != nil {
		t.Fatalf("CreateSession sess-1 returned error: %v", err)
	}
	if err := store.CreateSession(ctx, "sess-2", "beta", "/tmp/origin-b", "manual", "auto"); err != nil {
		t.Fatalf("CreateSession sess-2 returned error: %v", err)
	}

	if err := store.CreateAgent(ctx, CreateAgentParams{
		AgentID:   "agt-1",
		SessionID: "sess-1",
		Name:      stringPtr("claude"),
		Command:   "claude-code",
		Args:      `[]`,
		Status:    "running",
		StartedAt: "2026-04-04T00:00:00Z",
	}); err != nil {
		t.Fatalf("CreateAgent sess-1 returned error: %v", err)
	}

	if err := store.CreateAgent(ctx, CreateAgentParams{
		AgentID:   "agt-2",
		SessionID: "sess-2",
		Name:      stringPtr("claude"),
		Command:   "claude-code",
		Args:      `[]`,
		Status:    "running",
		StartedAt: "2026-04-04T00:00:10Z",
	}); err != nil {
		t.Fatalf("CreateAgent sess-2 returned error: %v", err)
	}
}

func TestNewAgentTestStoreUsesIsolatedDatabase(t *testing.T) {
	storeA := newAgentTestStore(t)
	storeB := newAgentTestStore(t)
	ctx := context.Background()

	if err := storeA.CreateSession(ctx, "sess-1", "alpha", "/tmp/origin", "manual", "auto"); err != nil {
		t.Fatalf("storeA CreateSession returned error: %v", err)
	}

	sessions, err := storeB.ListSessions(ctx)
	if err != nil {
		t.Fatalf("storeB ListSessions returned error: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("storeB sessions len = %d, want 0", len(sessions))
	}
}

func newAgentTestStore(t *testing.T) *Store {
	t.Helper()

	dbName := fmt.Sprintf("file:agent-store-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := sql.Open("sqlite", dbName)
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
CREATE TABLE agents (
	agent_id TEXT PRIMARY KEY,
	session_id TEXT NOT NULL,
	name TEXT,
	command TEXT NOT NULL,
	args TEXT NOT NULL DEFAULT '[]',
	status TEXT NOT NULL DEFAULT 'running',
	exit_code INTEGER,
	started_at TEXT NOT NULL,
	stopped_at TEXT,
	FOREIGN KEY(session_id) REFERENCES sessions(session_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX agents_session_id_name_uq
	ON agents (session_id, name)
	WHERE name IS NOT NULL;
`); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	store, err := NewSQLStore(db)
	if err != nil {
		t.Fatalf("NewSQLStore returned error: %v", err)
	}

	return store
}
