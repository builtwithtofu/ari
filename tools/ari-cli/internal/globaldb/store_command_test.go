package globaldb

import (
	"context"
	"errors"
	"testing"
)

func TestCommandStoreLifecycleAndReconciliation(t *testing.T) {
	store := newCommandTestStore(t)
	ctx := context.Background()

	if err := store.CreateSession(ctx, "sess-1", "alpha", "/tmp/origin", "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}

	createReq := CreateCommandParams{
		CommandID:   "cmd-1",
		WorkspaceID: "sess-1",
		Command:     "go test ./...",
		Args:        `["./..."]`,
		Status:      "running",
		StartedAt:   "2026-04-03T00:00:00Z",
	}
	if err := store.CreateCommand(ctx, createReq); err != nil {
		t.Fatalf("CreateCommand returned error: %v", err)
	}

	got, err := store.GetCommand(ctx, "sess-1", "cmd-1")
	if err != nil {
		t.Fatalf("GetCommand returned error: %v", err)
	}
	if got.CommandID != "cmd-1" {
		t.Fatalf("GetCommand CommandID = %q, want %q", got.CommandID, "cmd-1")
	}
	if got.Status != "running" {
		t.Fatalf("GetCommand Status = %q, want %q", got.Status, "running")
	}

	list, err := store.ListCommands(ctx, "sess-1")
	if err != nil {
		t.Fatalf("ListCommands returned error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ListCommands len = %d, want 1", len(list))
	}
	if list[0].Command != "go test ./..." {
		t.Fatalf("ListCommands[0].Command = %q, want %q", list[0].Command, "go test ./...")
	}

	updateReq := UpdateCommandStatusParams{
		WorkspaceID: "sess-1",
		CommandID:   "cmd-1",
		Status:      "exited",
		ExitCode:    intPtr(0),
		FinishedAt:  stringPtr("2026-04-03T00:00:05Z"),
	}
	if err := store.UpdateCommandStatus(ctx, updateReq); err != nil {
		t.Fatalf("UpdateCommandStatus returned error: %v", err)
	}

	updated, err := store.GetCommand(ctx, "sess-1", "cmd-1")
	if err != nil {
		t.Fatalf("GetCommand after update returned error: %v", err)
	}
	if updated.Status != "exited" {
		t.Fatalf("GetCommand after update Status = %q, want %q", updated.Status, "exited")
	}
	if updated.ExitCode == nil || *updated.ExitCode != 0 {
		t.Fatalf("GetCommand after update ExitCode = %v, want 0", updated.ExitCode)
	}

	if err := store.CreateCommand(ctx, CreateCommandParams{
		CommandID:   "cmd-2",
		WorkspaceID: "sess-1",
		Command:     "sleep 30",
		Args:        `[]`,
		Status:      "running",
		StartedAt:   "2026-04-03T00:01:00Z",
	}); err != nil {
		t.Fatalf("CreateCommand cmd-2 returned error: %v", err)
	}

	if err := store.MarkRunningCommandsLost(ctx); err != nil {
		t.Fatalf("MarkRunningCommandsLost returned error: %v", err)
	}

	lost, err := store.GetCommand(ctx, "sess-1", "cmd-2")
	if err != nil {
		t.Fatalf("GetCommand cmd-2 returned error: %v", err)
	}
	if lost.Status != "lost" {
		t.Fatalf("GetCommand cmd-2 Status = %q, want %q", lost.Status, "lost")
	}
}

func TestGetCommandMissingReturnsNotFound(t *testing.T) {
	store := newCommandTestStore(t)
	ctx := context.Background()

	if err := store.CreateSession(ctx, "sess-1", "alpha", "/tmp/origin", "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}

	_, err := store.GetCommand(ctx, "sess-1", "missing")
	if err == nil {
		t.Fatal("GetCommand returned nil error for missing command")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetCommand error = %v, want ErrNotFound", err)
	}
}

func intPtr(v int) *int { return &v }

func stringPtr(v string) *string { return &v }

func newCommandTestStore(t *testing.T) *Store {
	return newMigratedGlobalDBStore(t, "command-store")
}
