package oplog

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestStoreCreateLoadUpdateAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oplog.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("open returned error: %v", err)
	}

	node := OperationNode{
		OperationID: "op-2",
		State:       OperationStatePending,
		Goal:        "Implement operation DAG persistence",
		CreatedAt:   "2026-02-22T10:00:00Z",
		UpdatedAt:   "2026-02-22T10:00:00Z",
	}

	if err := store.CreateOperation(node); err != nil {
		t.Fatalf("create operation returned error: %v", err)
	}

	loaded, err := store.LoadOperation("op-2")
	if err != nil {
		t.Fatalf("load operation returned error: %v", err)
	}
	if loaded.OperationID != node.OperationID || loaded.State != node.State {
		t.Fatalf("loaded operation mismatch: got %+v want %+v", loaded, node)
	}

	updated, err := store.UpdateOperationState("op-2", OperationStateRunning, "2026-02-22T10:01:00Z")
	if err != nil {
		t.Fatalf("update operation returned error: %v", err)
	}
	if updated.State != OperationStateRunning {
		t.Fatalf("updated state = %q, want %q", updated.State, OperationStateRunning)
	}

	reloadedStore, err := Open(path)
	if err != nil {
		t.Fatalf("re-open returned error: %v", err)
	}

	reloaded, err := reloadedStore.LoadOperation("op-2")
	if err != nil {
		t.Fatalf("load operation from re-opened store returned error: %v", err)
	}
	if reloaded.State != OperationStateRunning {
		t.Fatalf("reloaded state = %q, want %q", reloaded.State, OperationStateRunning)
	}
}

func TestStoreListOperationsDeterministicOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oplog.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("open returned error: %v", err)
	}

	input := []OperationNode{
		{OperationID: "op-c", State: OperationStatePending, Goal: "C", CreatedAt: "2026-02-22T10:00:00Z", UpdatedAt: "2026-02-22T10:00:00Z"},
		{OperationID: "op-a", State: OperationStatePending, Goal: "A", CreatedAt: "2026-02-22T09:59:00Z", UpdatedAt: "2026-02-22T09:59:00Z"},
		{OperationID: "op-b", State: OperationStatePending, Goal: "B", CreatedAt: "2026-02-22T10:00:00Z", UpdatedAt: "2026-02-22T10:00:00Z"},
	}

	for _, node := range input {
		if err := store.CreateOperation(node); err != nil {
			t.Fatalf("create operation %q returned error: %v", node.OperationID, err)
		}
	}

	got := store.ListOperations()
	if len(got) != 3 {
		t.Fatalf("list operations length = %d, want 3", len(got))
	}

	if got[0].OperationID != "op-a" || got[1].OperationID != "op-b" || got[2].OperationID != "op-c" {
		t.Fatalf("unexpected operation order: got [%s %s %s]", got[0].OperationID, got[1].OperationID, got[2].OperationID)
	}
}

func TestStoreCreateEdgeAndListEdgesDeterministicOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oplog.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("open returned error: %v", err)
	}

	nodes := []OperationNode{
		{OperationID: "op-1", State: OperationStatePending, Goal: "1", CreatedAt: "2026-02-22T10:00:00Z", UpdatedAt: "2026-02-22T10:00:00Z"},
		{OperationID: "op-2", State: OperationStatePending, Goal: "2", CreatedAt: "2026-02-22T10:01:00Z", UpdatedAt: "2026-02-22T10:01:00Z"},
		{OperationID: "op-3", State: OperationStatePending, Goal: "3", CreatedAt: "2026-02-22T10:02:00Z", UpdatedAt: "2026-02-22T10:02:00Z"},
	}
	for _, node := range nodes {
		if err := store.CreateOperation(node); err != nil {
			t.Fatalf("create operation %q returned error: %v", node.OperationID, err)
		}
	}

	edges := []OperationEdge{
		{FromOperationID: "op-2", ToOperationID: "op-3"},
		{FromOperationID: "op-1", ToOperationID: "op-3"},
		{FromOperationID: "op-1", ToOperationID: "op-2"},
	}
	for _, edge := range edges {
		if err := store.CreateEdge(edge); err != nil {
			t.Fatalf("create edge %+v returned error: %v", edge, err)
		}
	}

	got := store.ListEdges()
	if len(got) != 3 {
		t.Fatalf("list edges length = %d, want 3", len(got))
	}

	if got[0].FromOperationID != "op-1" || got[0].ToOperationID != "op-2" {
		t.Fatalf("edge[0] = %+v, want op-1->op-2", got[0])
	}
	if got[1].FromOperationID != "op-1" || got[1].ToOperationID != "op-3" {
		t.Fatalf("edge[1] = %+v, want op-1->op-3", got[1])
	}
	if got[2].FromOperationID != "op-2" || got[2].ToOperationID != "op-3" {
		t.Fatalf("edge[2] = %+v, want op-2->op-3", got[2])
	}
}

func TestStoreExplicitErrorsForMissingAndInvalidUpdates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oplog.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("open returned error: %v", err)
	}

	_, err = store.LoadOperation("missing")
	if err == nil {
		t.Fatal("load missing operation returned nil error")
	}
	if !errors.Is(err, ErrOperationNotFound) {
		t.Fatalf("load missing operation error = %v, want operation not found", err)
	}

	_, err = store.UpdateOperationState("missing", OperationStateRunning, "2026-02-22T10:01:00Z")
	if err == nil {
		t.Fatal("update missing operation returned nil error")
	}
	if !errors.Is(err, ErrOperationNotFound) {
		t.Fatalf("update missing operation error = %v, want operation not found", err)
	}

	node := OperationNode{
		OperationID: "op-1",
		State:       OperationStatePending,
		Goal:        "Goal",
		CreatedAt:   "2026-02-22T10:00:00Z",
		UpdatedAt:   "2026-02-22T10:00:00Z",
	}
	if err := store.CreateOperation(node); err != nil {
		t.Fatalf("create operation returned error: %v", err)
	}

	_, err = store.UpdateOperationState("op-1", OperationState("bad_state"), "2026-02-22T10:01:00Z")
	if err == nil {
		t.Fatal("update with invalid state returned nil error")
	}
	if !errors.Is(err, ErrInvalidStateUpdate) {
		t.Fatalf("update invalid state error = %v, want invalid state update", err)
	}

	_, err = store.UpdateOperationState("op-1", OperationStateRunning, "")
	if err == nil {
		t.Fatal("update with empty updated_at returned nil error")
	}
	if !errors.Is(err, ErrInvalidStateUpdate) {
		t.Fatalf("update empty updated_at error = %v, want invalid state update", err)
	}
}
