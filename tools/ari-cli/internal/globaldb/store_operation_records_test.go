package globaldb

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestOperationRecordAppendGetList(t *testing.T) {
	store := newGlobalDBTestStore(t, "operation-records")
	ctx := context.Background()
	if err := store.CreateSession(ctx, "ws-1", "alpha", "/tmp/alpha", "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	baseTime := time.Date(2026, 5, 16, 10, 0, 0, 0, time.UTC)

	parent, err := store.AppendOperationRecord(ctx, operationRecordFixture("op-1", "", OperationScopeGlobal, baseTime))
	if err != nil {
		t.Fatalf("AppendOperationRecord parent returned error: %v", err)
	}
	child := operationRecordFixture("op-2", "ws-1", OperationScopeWorkspace, baseTime.Add(time.Minute))
	child.ParentOperationID = parent.OperationID
	child.CheckpointOperationID = parent.OperationID
	child.RollbackPointID = parent.OperationID
	child.TrustDecision = "trusted_once"
	gotChild, err := store.AppendOperationRecord(ctx, child)
	if err != nil {
		t.Fatalf("AppendOperationRecord child returned error: %v", err)
	}

	got, err := store.GetOperationRecord(ctx, gotChild.OperationID)
	if err != nil {
		t.Fatalf("GetOperationRecord returned error: %v", err)
	}
	if got.WorkspaceID != "ws-1" || got.ParentOperationID != "op-1" || got.CheckpointOperationID != "op-1" || got.RollbackPointID != "op-1" || got.TrustDecision != "trusted_once" {
		t.Fatalf("GetOperationRecord links/scope = %#v, want child links", got)
	}
	if got.RollbackDataJSON != `{"undo":"workspace"}` || got.PayloadSnapshotJSON != `{"after":"workspace"}` || got.PayloadHash != "sha256:op-2" {
		t.Fatalf("GetOperationRecord payload fields = %#v", got)
	}

	all, err := store.ListOperationRecords(ctx, "")
	if err != nil {
		t.Fatalf("ListOperationRecords global returned error: %v", err)
	}
	if len(all) != 2 || all[0].OperationID != "op-2" || all[1].OperationID != "op-1" {
		t.Fatalf("ListOperationRecords all ids = %#v, want op-2 then op-1", operationRecordIDs(all))
	}
	workspace, err := store.ListOperationRecords(ctx, "ws-1")
	if err != nil {
		t.Fatalf("ListOperationRecords workspace returned error: %v", err)
	}
	if len(workspace) != 1 || workspace[0].OperationID != "op-2" {
		t.Fatalf("ListOperationRecords workspace ids = %#v, want op-2", operationRecordIDs(workspace))
	}
}

func TestOperationRecordValidationAndMissingReferences(t *testing.T) {
	store := newGlobalDBTestStore(t, "operation-records-validation")
	ctx := context.Background()

	invalid := operationRecordFixture("op-invalid", "", OperationScopeGlobal, time.Time{})
	invalid.PayloadSnapshotJSON = "{"
	if _, err := store.AppendOperationRecord(ctx, invalid); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("AppendOperationRecord invalid JSON error = %v, want ErrInvalidInput", err)
	}
	invalid = operationRecordFixture("op-invalid", "", OperationScopeWorkspace, time.Time{})
	if _, err := store.AppendOperationRecord(ctx, invalid); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("AppendOperationRecord missing workspace error = %v, want ErrInvalidInput", err)
	}
	missingParent := operationRecordFixture("op-missing-parent", "", OperationScopeGlobal, time.Time{})
	missingParent.ParentOperationID = "missing"
	if _, err := store.AppendOperationRecord(ctx, missingParent); err == nil {
		t.Fatal("AppendOperationRecord missing parent returned nil error")
	}
	if records, err := store.ListOperationRecords(ctx, ""); err != nil || len(records) != 0 {
		t.Fatalf("ListOperationRecords after failed appends = (%#v, %v), want empty nil", records, err)
	}
	if _, err := store.GetOperationRecord(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetOperationRecord missing error = %v, want ErrNotFound", err)
	}
}

func operationRecordFixture(id, workspaceID, scope string, createdAt time.Time) OperationRecord {
	return OperationRecord{OperationID: id, WorkspaceID: workspaceID, OperationType: "workspace.init", Actor: "user", Source: "cli", Scope: scope, RequestSummary: "initialize workspace", Result: "applied", RollbackDataJSON: `{"undo":"workspace"}`, PayloadHash: "sha256:" + id, PayloadSnapshotJSON: `{"after":"workspace"}`, CreatedAt: createdAt}
}

func operationRecordIDs(records []OperationRecord) []string {
	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.OperationID)
	}
	return ids
}
