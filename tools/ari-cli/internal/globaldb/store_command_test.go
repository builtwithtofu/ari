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

func TestCreateCommandRejectsMissingWorkspace(t *testing.T) {
	store := newCommandTestStore(t)
	ctx := context.Background()

	err := store.CreateCommand(ctx, CreateCommandParams{
		CommandID:   "cmd-1",
		WorkspaceID: "missing-workspace",
		Command:     "go test ./...",
		Args:        `[]`,
		Status:      "running",
		StartedAt:   "2026-04-03T00:00:00Z",
	})
	if err == nil {
		t.Fatal("CreateCommand returned nil error for missing workspace")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("CreateCommand missing workspace error = %v, want ErrNotFound", err)
	}
}

func TestWorkspaceCommandDefinitionRejectsMissingWorkspace(t *testing.T) {
	store := newCommandTestStore(t)
	ctx := context.Background()

	err := store.CreateWorkspaceCommandDefinition(ctx, CreateWorkspaceCommandDefinitionParams{
		CommandID:   "cmd-def-1",
		WorkspaceID: "missing-workspace",
		Name:        "test",
		Command:     "go",
		Args:        `[]`,
	})
	if err == nil {
		t.Fatal("CreateWorkspaceCommandDefinition returned nil error for missing workspace")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("CreateWorkspaceCommandDefinition missing workspace error = %v, want ErrNotFound", err)
	}
}

func TestWorkspaceCommandDefinitionLifecycle(t *testing.T) {
	store := newCommandTestStore(t)
	ctx := context.Background()

	if err := store.CreateSession(ctx, "sess-1", "alpha", "/tmp/origin", "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}

	createReq := CreateWorkspaceCommandDefinitionParams{
		CommandID:   "cmd-def-1",
		WorkspaceID: "sess-1",
		Name:        "test",
		Command:     "go",
		Args:        `["test","./..."]`,
	}
	if err := store.CreateWorkspaceCommandDefinition(ctx, createReq); err != nil {
		t.Fatalf("CreateWorkspaceCommandDefinition returned error: %v", err)
	}

	gotByID, err := store.GetWorkspaceCommandDefinition(ctx, "sess-1", "cmd-def-1")
	if err != nil {
		t.Fatalf("GetWorkspaceCommandDefinition returned error: %v", err)
	}
	if gotByID.Name != "test" {
		t.Fatalf("GetWorkspaceCommandDefinition Name = %q, want %q", gotByID.Name, "test")
	}

	gotByName, err := store.GetWorkspaceCommandDefinitionByName(ctx, "sess-1", "test")
	if err != nil {
		t.Fatalf("GetWorkspaceCommandDefinitionByName returned error: %v", err)
	}
	if gotByName.CommandID != "cmd-def-1" {
		t.Fatalf("GetWorkspaceCommandDefinitionByName CommandID = %q, want %q", gotByName.CommandID, "cmd-def-1")
	}

	list, err := store.ListWorkspaceCommandDefinitions(ctx, "sess-1")
	if err != nil {
		t.Fatalf("ListWorkspaceCommandDefinitions returned error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ListWorkspaceCommandDefinitions len = %d, want 1", len(list))
	}
	if list[0].Command != "go" {
		t.Fatalf("ListWorkspaceCommandDefinitions[0].Command = %q, want %q", list[0].Command, "go")
	}

	if err := store.DeleteWorkspaceCommandDefinition(ctx, "sess-1", "cmd-def-1"); err != nil {
		t.Fatalf("DeleteWorkspaceCommandDefinition returned error: %v", err)
	}

	_, err = store.GetWorkspaceCommandDefinition(ctx, "sess-1", "cmd-def-1")
	if err == nil {
		t.Fatal("GetWorkspaceCommandDefinition after delete returned nil error")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetWorkspaceCommandDefinition after delete error = %v, want ErrNotFound", err)
	}
}

func TestWorkspaceCommandDefinitionRejectsNameThatCollidesWithExistingID(t *testing.T) {
	store := newCommandTestStore(t)
	ctx := context.Background()

	if err := store.CreateSession(ctx, "sess-1", "alpha", "/tmp/origin", "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}

	if err := store.CreateWorkspaceCommandDefinition(ctx, CreateWorkspaceCommandDefinitionParams{
		CommandID:   "cmd-def-1",
		WorkspaceID: "sess-1",
		Name:        "first",
		Command:     "go",
		Args:        `[]`,
	}); err != nil {
		t.Fatalf("CreateWorkspaceCommandDefinition first returned error: %v", err)
	}

	err := store.CreateWorkspaceCommandDefinition(ctx, CreateWorkspaceCommandDefinitionParams{
		CommandID:   "cmd-def-2",
		WorkspaceID: "sess-1",
		Name:        "cmd-def-1",
		Command:     "go",
		Args:        `[]`,
	})
	if err == nil {
		t.Fatal("CreateWorkspaceCommandDefinition returned nil error for colliding name")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("CreateWorkspaceCommandDefinition collision error = %v, want ErrInvalidInput", err)
	}
}

func TestWorkspaceCommandDefinitionRejectsIDThatCollidesWithExistingName(t *testing.T) {
	store := newCommandTestStore(t)
	ctx := context.Background()

	if err := store.CreateSession(ctx, "sess-1", "alpha", "/tmp/origin", "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}

	if err := store.CreateWorkspaceCommandDefinition(ctx, CreateWorkspaceCommandDefinitionParams{
		CommandID:   "cmd-def-1",
		WorkspaceID: "sess-1",
		Name:        "build",
		Command:     "go",
		Args:        `[]`,
	}); err != nil {
		t.Fatalf("CreateWorkspaceCommandDefinition first returned error: %v", err)
	}

	err := store.CreateWorkspaceCommandDefinition(ctx, CreateWorkspaceCommandDefinitionParams{
		CommandID:   "build",
		WorkspaceID: "sess-1",
		Name:        "test",
		Command:     "go",
		Args:        `[]`,
	})
	if err == nil {
		t.Fatal("CreateWorkspaceCommandDefinition returned nil error for colliding id")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("CreateWorkspaceCommandDefinition collision error = %v, want ErrInvalidInput", err)
	}
}

func TestWorkspaceCommandDefinitionRejectsNullArgs(t *testing.T) {
	store := newCommandTestStore(t)
	ctx := context.Background()

	if err := store.CreateSession(ctx, "sess-1", "alpha", "/tmp/origin", "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}

	err := store.CreateWorkspaceCommandDefinition(ctx, CreateWorkspaceCommandDefinitionParams{
		CommandID:   "cmd-def-1",
		WorkspaceID: "sess-1",
		Name:        "test",
		Command:     "go",
		Args:        `null`,
	})
	if err == nil {
		t.Fatal("CreateWorkspaceCommandDefinition returned nil error for null args")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("CreateWorkspaceCommandDefinition null args error = %v, want ErrInvalidInput", err)
	}
}

func TestWorkspaceCommandDefinitionRejectsDuplicateNameInSameWorkspace(t *testing.T) {
	store := newCommandTestStore(t)
	ctx := context.Background()

	if err := store.CreateSession(ctx, "sess-1", "alpha", "/tmp/origin", "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}

	if err := store.CreateWorkspaceCommandDefinition(ctx, CreateWorkspaceCommandDefinitionParams{
		CommandID:   "cmd-def-1",
		WorkspaceID: "sess-1",
		Name:        "test",
		Command:     "go",
		Args:        `[]`,
	}); err != nil {
		t.Fatalf("CreateWorkspaceCommandDefinition first returned error: %v", err)
	}

	err := store.CreateWorkspaceCommandDefinition(ctx, CreateWorkspaceCommandDefinitionParams{
		CommandID:   "cmd-def-2",
		WorkspaceID: "sess-1",
		Name:        "test",
		Command:     "go",
		Args:        `[]`,
	})
	if err == nil {
		t.Fatal("CreateWorkspaceCommandDefinition returned nil error for duplicate name")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("CreateWorkspaceCommandDefinition duplicate name error = %v, want ErrInvalidInput", err)
	}
}

func TestWorkspaceCommandDefinitionRejectsDuplicateCommandIDInSameWorkspace(t *testing.T) {
	store := newCommandTestStore(t)
	ctx := context.Background()

	if err := store.CreateSession(ctx, "sess-1", "alpha", "/tmp/origin", "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}

	if err := store.CreateWorkspaceCommandDefinition(ctx, CreateWorkspaceCommandDefinitionParams{
		CommandID:   "cmd-def-1",
		WorkspaceID: "sess-1",
		Name:        "test-1",
		Command:     "go",
		Args:        `[]`,
	}); err != nil {
		t.Fatalf("CreateWorkspaceCommandDefinition first returned error: %v", err)
	}

	err := store.CreateWorkspaceCommandDefinition(ctx, CreateWorkspaceCommandDefinitionParams{
		CommandID:   "cmd-def-1",
		WorkspaceID: "sess-1",
		Name:        "test-2",
		Command:     "go",
		Args:        `[]`,
	})
	if err == nil {
		t.Fatal("CreateWorkspaceCommandDefinition returned nil error for duplicate command id")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("CreateWorkspaceCommandDefinition duplicate id error = %v, want ErrInvalidInput", err)
	}
}

func intPtr(v int) *int { return &v }

func stringPtr(v string) *string { return &v }

func newCommandTestStore(t *testing.T) *Store {
	return newMigratedGlobalDBStore(t, "command-store")
}
