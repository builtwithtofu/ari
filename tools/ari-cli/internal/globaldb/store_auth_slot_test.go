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
	got := map[string]bool{}
	for _, slot := range slots {
		got[slot.AuthSlotID] = true
		if slot.Harness != "codex" {
			t.Fatalf("slots = %#v, want only codex harness", slots)
		}
	}
	if !got["codex-default"] || !got["codex-work"] || got["claude-work"] {
		t.Fatalf("slots = %#v, want codex default and work slots only", slots)
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

func TestAuthSlotMigrationSeedsDefaultProviderOwnedSlots(t *testing.T) {
	store := newMigratedGlobalDBStore(t, "auth-slot-defaults")
	slots, err := store.ListAuthSlots(context.Background(), "")
	if err != nil {
		t.Fatalf("ListAuthSlots returned error: %v", err)
	}
	got := map[string]AuthSlot{}
	for _, slot := range slots {
		got[slot.AuthSlotID] = slot
	}
	for _, want := range []struct {
		id      string
		harness string
	}{
		{id: "codex-default", harness: "codex"},
		{id: "claude-default", harness: "claude"},
		{id: "opencode-default", harness: "opencode"},
	} {
		slot, ok := got[want.id]
		if !ok {
			t.Fatalf("seeded slots = %#v, missing %s", got, want.id)
		}
		if slot.Harness != want.harness || slot.CredentialOwner != "provider" || slot.Status != "unknown" || slot.MetadataJSON != "{}" {
			t.Fatalf("slot %s = %#v, want provider-owned unknown default", want.id, slot)
		}
	}
}

func TestAgentProfilePersistsAuthBindings(t *testing.T) {
	store := newMigratedGlobalDBStore(t, "profile-auth-bindings")
	ctx := context.Background()

	profile := AgentProfile{ProfileID: "ap_auth", Name: "codex-work", Harness: "codex", AuthSlotID: "codex-work", AuthPoolJSON: `{"slot_ids":["codex-work","codex-personal"],"strategy":"failover"}`, DefaultsJSON: `{}`}
	if err := store.UpsertAgentProfile(ctx, profile); err != nil {
		t.Fatalf("UpsertAgentProfile returned error: %v", err)
	}

	stored, err := store.GetAgentProfile(ctx, "", "codex-work")
	if err != nil {
		t.Fatalf("GetAgentProfile returned error: %v", err)
	}
	if stored.AuthSlotID != "codex-work" || stored.AuthPoolJSON != profile.AuthPoolJSON {
		t.Fatalf("stored profile = %#v, want auth slot and ordered auth pool", stored)
	}
}
