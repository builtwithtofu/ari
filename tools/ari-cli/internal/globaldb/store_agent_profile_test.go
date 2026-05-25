package globaldb

import (
	"context"
	"errors"
	"testing"
)

func TestProfilePersistsAndFallsBackToGlobalScope(t *testing.T) {
	store := newGlobalDBTestStore(t, "agent-profile")
	ctx := context.Background()
	if err := store.UpsertProfile(ctx, Profile{ProfileID: "ap_global", Name: "executor", Harness: "codex", Model: "gpt-5.1-codex", Prompt: "Do work", InvocationClass: "sticky"}); err != nil {
		t.Fatalf("UpsertProfile global returned error: %v", err)
	}
	if err := store.UpsertProfile(ctx, Profile{ProfileID: "ap_workspace", WorkspaceID: "ws-1", Name: "executor", Harness: "claude", Model: "opus"}); err != nil {
		t.Fatalf("UpsertProfile workspace returned error: %v", err)
	}

	workspaceProfile, err := store.GetProfile(ctx, "ws-1", "executor")
	if err != nil {
		t.Fatalf("GetProfile workspace returned error: %v", err)
	}
	if workspaceProfile.ProfileID != "ap_workspace" || workspaceProfile.Harness != "claude" || workspaceProfile.Model != "opus" {
		t.Fatalf("workspace profile = %#v, want workspace override", workspaceProfile)
	}
	fallbackProfile, err := store.GetProfile(ctx, "ws-2", "executor")
	if err != nil {
		t.Fatalf("GetProfile fallback returned error: %v", err)
	}
	if fallbackProfile.ProfileID != "ap_global" || fallbackProfile.Harness != "codex" || fallbackProfile.Prompt != "Do work" {
		t.Fatalf("fallback profile = %#v, want global profile", fallbackProfile)
	}
}

func TestProfileAllowsNullableOverrides(t *testing.T) {
	store := newGlobalDBTestStore(t, "agent-profile-nullable")
	ctx := context.Background()
	if err := store.UpsertProfile(ctx, Profile{ProfileID: "ap_partial", Name: "partial"}); err != nil {
		t.Fatalf("UpsertProfile partial returned error: %v", err)
	}
	profile, err := store.GetProfile(ctx, "", "partial")
	if err != nil {
		t.Fatalf("GetProfile partial returned error: %v", err)
	}
	if profile.Harness != "" || profile.Model != "" || profile.InvocationClass != "" {
		t.Fatalf("partial profile = %#v, want empty explicit overrides", profile)
	}
}

func TestProfileUpsertUpdatesExistingScopeAndName(t *testing.T) {
	store := newGlobalDBTestStore(t, "agent-profile-upsert")
	ctx := context.Background()
	if err := store.UpsertProfile(ctx, Profile{ProfileID: "ap_first", WorkspaceID: "ws-1", Name: "executor", Harness: "codex", Model: "gpt-5.1-codex"}); err != nil {
		t.Fatalf("UpsertProfile first returned error: %v", err)
	}
	if err := store.UpsertProfile(ctx, Profile{ProfileID: "ap_second", WorkspaceID: "ws-1", Name: "executor", Harness: "claude", Model: "opus"}); err != nil {
		t.Fatalf("UpsertProfile update returned error: %v", err)
	}

	profile, err := store.GetProfile(ctx, "ws-1", "executor")
	if err != nil {
		t.Fatalf("GetProfile returned error: %v", err)
	}
	if profile.ProfileID != "ap_first" || profile.Harness != "claude" || profile.Model != "opus" {
		t.Fatalf("profile = %#v, want existing scoped name updated in place", profile)
	}
}

func TestProfileListUsesRequestedScope(t *testing.T) {
	store := newGlobalDBTestStore(t, "agent-profile-list")
	ctx := context.Background()
	if err := store.UpsertProfile(ctx, Profile{ProfileID: "ap_global", Name: "global", Harness: "codex"}); err != nil {
		t.Fatalf("UpsertProfile global returned error: %v", err)
	}
	if err := store.UpsertProfile(ctx, Profile{ProfileID: "ap_workspace", WorkspaceID: "ws-1", Name: "workspace", Harness: "claude"}); err != nil {
		t.Fatalf("UpsertProfile workspace returned error: %v", err)
	}

	globalProfiles, err := store.ListProfiles(ctx, "")
	if err != nil {
		t.Fatalf("ListProfiles global returned error: %v", err)
	}
	if len(globalProfiles) != 1 || globalProfiles[0].ProfileID != "ap_global" {
		t.Fatalf("global profiles = %#v, want only global", globalProfiles)
	}
	workspaceProfiles, err := store.ListProfiles(ctx, "ws-1")
	if err != nil {
		t.Fatalf("ListProfiles workspace returned error: %v", err)
	}
	if len(workspaceProfiles) != 1 || workspaceProfiles[0].ProfileID != "ap_workspace" {
		t.Fatalf("workspace profiles = %#v, want only workspace", workspaceProfiles)
	}
}

func TestEnsureDefaultHelperProfileCreatesWorkspaceScopedHelper(t *testing.T) {
	store := newGlobalDBTestStore(t, "helper-profile")
	ctx := context.Background()
	if err := store.CreateWorkspace(ctx, "ws-1", "alpha", "/tmp/origin", "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}

	help, err := store.EnsureDefaultHelperProfile(ctx, "ws-1", "codex", "Explain this workspace")
	if err != nil {
		t.Fatalf("EnsureDefaultHelperProfile returned error: %v", err)
	}
	if help.WorkspaceID != "ws-1" || help.Name != "helper" || help.Harness != "codex" || help.Prompt != "Explain this workspace" {
		t.Fatalf("helper profile = %#v", help)
	}

	got, err := store.GetProfile(ctx, "ws-1", "helper")
	if err != nil {
		t.Fatalf("GetProfile returned error: %v", err)
	}
	if got.ProfileID != help.ProfileID {
		t.Fatalf("persisted helper id = %q, want %q", got.ProfileID, help.ProfileID)
	}
}

func TestEnsureDefaultHelperProfileDoesNotOverwriteExistingHelper(t *testing.T) {
	store := newGlobalDBTestStore(t, "helper-profile-existing")
	ctx := context.Background()
	if err := store.CreateWorkspace(ctx, "ws-1", "alpha", "/tmp/origin", "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	if err := store.UpsertProfile(ctx, Profile{ProfileID: "ap_existing", WorkspaceID: "ws-1", Name: "helper", Harness: "opencode", Prompt: "Keep me"}); err != nil {
		t.Fatalf("UpsertProfile returned error: %v", err)
	}

	help, err := store.EnsureDefaultHelperProfile(ctx, "ws-1", "codex", "Replace me")
	if err != nil {
		t.Fatalf("EnsureDefaultHelperProfile returned error: %v", err)
	}
	if help.ProfileID != "ap_existing" || help.Harness != "opencode" || help.Prompt != "Keep me" {
		t.Fatalf("helper profile was overwritten: %#v", help)
	}
}

func TestEnsureDefaultHelperProfileUsesUniqueProfileIDs(t *testing.T) {
	store := newGlobalDBTestStore(t, "helper-profile-unique-ids")
	ctx := context.Background()
	if err := store.CreateWorkspace(ctx, "a-b_c", "one", "/tmp/one", "manual", "auto"); err != nil {
		t.Fatalf("CreateSession one returned error: %v", err)
	}
	if err := store.CreateWorkspace(ctx, "a_b-c", "two", "/tmp/two", "manual", "auto"); err != nil {
		t.Fatalf("CreateSession two returned error: %v", err)
	}

	first, err := store.EnsureDefaultHelperProfile(ctx, "a-b_c", "codex", "one")
	if err != nil {
		t.Fatalf("EnsureDefaultHelperProfile first returned error: %v", err)
	}
	second, err := store.EnsureDefaultHelperProfile(ctx, "a_b-c", "codex", "two")
	if err != nil {
		t.Fatalf("EnsureDefaultHelperProfile second returned error: %v", err)
	}
	if first.ProfileID == second.ProfileID {
		t.Fatalf("helper profile IDs collided: %q", first.ProfileID)
	}
	if first.WorkspaceID != "a-b_c" || second.WorkspaceID != "a_b-c" {
		t.Fatalf("helpers crossed workspace scopes: %#v %#v", first, second)
	}
}

func TestGetDefaultHelperProfileDoesNotFallbackAcrossScopes(t *testing.T) {
	store := newGlobalDBTestStore(t, "helper-profile-scope")
	ctx := context.Background()
	if err := store.CreateWorkspace(ctx, "system-id", "system", "/tmp/system-origin", "manual", "auto"); err != nil {
		t.Fatalf("CreateSession system returned error: %v", err)
	}
	if err := store.CreateWorkspace(ctx, "project-id", "project", "/tmp/project-origin", "manual", "auto"); err != nil {
		t.Fatalf("CreateSession project returned error: %v", err)
	}
	if _, err := store.EnsureDefaultHelperProfile(ctx, "system-id", "codex", "Home helper"); err != nil {
		t.Fatalf("EnsureDefaultHelperProfile system returned error: %v", err)
	}

	_, err := store.GetDefaultHelperProfile(ctx, "project-id")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetDefaultHelperProfile project error = %v, want ErrNotFound", err)
	}
}

func TestEnsureDefaultHelperProfileRejectsUnknownWorkspace(t *testing.T) {
	store := newGlobalDBTestStore(t, "helper-profile-missing-workspace")
	_, err := store.EnsureDefaultHelperProfile(context.Background(), "missing", "codex", "Help")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("EnsureDefaultHelperProfile error = %v, want ErrNotFound", err)
	}
}

func TestProfileRejectsInvalidInput(t *testing.T) {
	store := newGlobalDBTestStore(t, "agent-profile-invalid")
	err := store.UpsertProfile(context.Background(), Profile{Name: "missing-id"})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("UpsertProfile error = %v, want ErrInvalidInput", err)
	}
}

func TestProfileRejectsSecretLikeDefaults(t *testing.T) {
	store := newGlobalDBTestStore(t, "agent-profile-secret-defaults")
	ctx := context.Background()

	for _, defaults := range []string{
		`{"api_key":"sk-test"}`,
		`{"nested":{"access_token":"secret"}}`,
		`{"items":[{"refresh_token":"secret"}]}`,
		`{"credential_source_ref":"secret://profile"}`,
	} {
		err := store.UpsertProfile(ctx, Profile{ProfileID: "ap_secret", Name: "secret-default", DefaultsJSON: defaults})
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("UpsertProfile defaults %s error = %v, want ErrInvalidInput", defaults, err)
		}
	}
}

func TestProfileAllowsNonSecretSourceDefaults(t *testing.T) {
	store := newGlobalDBTestStore(t, "agent-profile-source-defaults")
	err := store.UpsertProfile(context.Background(), Profile{ProfileID: "ap_source", Name: "source-default", DefaultsJSON: `{"source":"harness","source_ref":"provider-documentation"}`})
	if err != nil {
		t.Fatalf("UpsertProfile returned error for non-secret source defaults: %v", err)
	}
}
