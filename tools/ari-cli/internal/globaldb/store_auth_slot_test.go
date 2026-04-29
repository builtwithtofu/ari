package globaldb

import (
	"context"
	"errors"
	"testing"
)

func TestAuthSlotPersistsMetadataWithoutCredentialSources(t *testing.T) {
	store := newMigratedGlobalDBStore(t, "auth-slot")
	ctx := context.Background()

	if err := store.UpsertAuthSlot(ctx, AuthSlot{AuthSlotID: "codex-personal", Harness: "codex", Label: "Personal", ProviderLabel: "ChatGPT Plus", Status: "authenticated"}); err != nil {
		t.Fatalf("UpsertAuthSlot returned error: %v", err)
	}

	slot, err := store.GetAuthSlot(ctx, "codex-personal")
	if err != nil {
		t.Fatalf("GetAuthSlot returned error: %v", err)
	}
	if slot.AuthSlotID != "codex-personal" || slot.Harness != "codex" || slot.Label != "Personal" || slot.ProviderLabel != "ChatGPT Plus" || slot.CredentialOwner != "provider" || slot.Status != "authenticated" {
		t.Fatalf("slot = %#v, want provider-owned metadata only", slot)
	}
}

func TestAuthSlotListFiltersByHarness(t *testing.T) {
	store := newMigratedGlobalDBStore(t, "auth-slot-list")
	ctx := context.Background()
	for _, slot := range []AuthSlot{
		{AuthSlotID: "codex-work", Harness: "codex", Label: "Work", Status: "authenticated"},
		{AuthSlotID: "claude-work", Harness: "claude", Label: "Work", Status: "auth_required"},
	} {
		if err := store.UpsertAuthSlot(ctx, slot); err != nil {
			t.Fatalf("UpsertAuthSlot(%s) returned error: %v", slot.AuthSlotID, err)
		}
	}

	slots, err := store.ListAuthSlots(ctx, "codex")
	if err != nil {
		t.Fatalf("ListAuthSlots returned error: %v", err)
	}
	if len(slots) != 1 || slots[0].AuthSlotID != "codex-work" {
		t.Fatalf("slots = %#v, want only codex slot", slots)
	}
}

func TestAuthSlotRejectsSourceFieldsInMetadata(t *testing.T) {
	store := newMigratedGlobalDBStore(t, "auth-slot-invalid")
	err := store.UpsertAuthSlot(context.Background(), AuthSlot{AuthSlotID: "codex-work", Harness: "codex", Label: "Work", Status: "authenticated", MetadataJSON: `{"provider":{"source_ref":"/secret"}}`})
	if err == nil {
		t.Fatal("UpsertAuthSlot returned nil error for source_ref metadata")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("UpsertAuthSlot error = %v, want ErrInvalidInput", err)
	}
}
