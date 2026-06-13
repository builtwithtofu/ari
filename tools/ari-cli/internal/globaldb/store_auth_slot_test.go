package globaldb

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestAuthSlotPersistsMetadataWithoutCredentialSources(t *testing.T) {
	store := newGlobalDBTestStore(t, "auth-slot")
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

func TestAuthSlotTimestampParseFailuresSurface(t *testing.T) {
	store := newGlobalDBTestStore(t, "auth-slot-invalid-timestamp")
	ctx := context.Background()
	if err := store.UpsertAuthSlot(ctx, AuthSlot{AuthSlotID: "codex-work", Harness: "codex", Label: "Work", Status: "authenticated"}); err != nil {
		t.Fatalf("UpsertAuthSlot returned error: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE auth_slots SET updated_at = ? WHERE auth_slot_id = ?`, "not-a-time", "codex-work"); err != nil {
		t.Fatalf("corrupt auth slot timestamp: %v", err)
	}

	_, err := store.GetAuthSlot(ctx, "codex-work")
	if err == nil {
		t.Fatal("GetAuthSlot returned nil error for malformed updated_at")
	}
	if !strings.Contains(err.Error(), `auth slot "codex-work" updated_at "not-a-time"`) {
		t.Fatalf("GetAuthSlot error = %v, want auth slot id and raw timestamp", err)
	}
}

func TestAuthSlotListFiltersByHarness(t *testing.T) {
	store := newGlobalDBTestStore(t, "auth-slot-list")
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
	store := newGlobalDBTestStore(t, "auth-slot-invalid")
	ctx := context.Background()

	for _, metadata := range []string{
		`{"provider":{"credential_source_ref":"/secret"}}`,
		`{"api_key":"sk-test"}`,
		`{"client_secret":"shh"}`,
		`{"nested":{"bearer_token":"tok"}}`,
		`{"items":[{"refresh_token":"secret"}]}`,
	} {
		err := store.UpsertAuthSlot(ctx, AuthSlot{AuthSlotID: "codex-work", Harness: "codex", Label: "Work", Status: "authenticated", MetadataJSON: metadata})
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("UpsertAuthSlot metadata %s error = %v, want ErrInvalidInput", metadata, err)
		}
	}
}

func TestAuthSlotAllowsNonSecretSourceMetadata(t *testing.T) {
	store := newGlobalDBTestStore(t, "auth-slot-source-metadata")
	err := store.UpsertAuthSlot(context.Background(), AuthSlot{AuthSlotID: "codex-work", Harness: "codex", Label: "Work", Status: "authenticated", MetadataJSON: `{"source":"harness","source_ref":"provider-documentation"}`})
	if err != nil {
		t.Fatalf("UpsertAuthSlot returned error for non-secret source metadata: %v", err)
	}
}

func TestAuthSlotDefaultsIncludeProviderOwnedSlots(t *testing.T) {
	store := newGlobalDBTestStore(t, "auth-slot-defaults")
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

func TestProfilePersistsAuthBindings(t *testing.T) {
	store := newGlobalDBTestStore(t, "profile-auth-bindings")
	ctx := context.Background()

	profile := Profile{ProfileID: "ap_auth", Name: "codex-work", Harness: "codex", AuthSlotID: "codex-work", AuthPoolJSON: `{"slot_ids":["codex-work","codex-personal"],"strategy":"failover"}`, DefaultsJSON: `{}`}
	if err := store.UpsertProfile(ctx, profile); err != nil {
		t.Fatalf("UpsertProfile returned error: %v", err)
	}

	stored, err := store.GetProfile(ctx, "", "codex-work")
	if err != nil {
		t.Fatalf("GetProfile returned error: %v", err)
	}
	if stored.AuthSlotID != "codex-work" || stored.AuthPoolJSON != profile.AuthPoolJSON {
		t.Fatalf("stored profile = %#v, want auth slot and ordered auth pool", stored)
	}
}
