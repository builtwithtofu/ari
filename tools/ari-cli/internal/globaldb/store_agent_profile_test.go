package globaldb

import (
	"context"
	"errors"
	"testing"
)

func TestAgentProfilePersistsAndFallsBackToGlobalScope(t *testing.T) {
	store := newMigratedGlobalDBStore(t, "agent-profile")
	ctx := context.Background()
	if err := store.UpsertAgentProfile(ctx, AgentProfile{ProfileID: "ap_global", Name: "executor", Harness: "codex", Model: "gpt-5.1-codex", Prompt: "Do work", InvocationClass: "agent"}); err != nil {
		t.Fatalf("UpsertAgentProfile global returned error: %v", err)
	}
	if err := store.UpsertAgentProfile(ctx, AgentProfile{ProfileID: "ap_workspace", WorkspaceID: "ws-1", Name: "executor", Harness: "claude", Model: "opus"}); err != nil {
		t.Fatalf("UpsertAgentProfile workspace returned error: %v", err)
	}

	workspaceProfile, err := store.GetAgentProfile(ctx, "ws-1", "executor")
	if err != nil {
		t.Fatalf("GetAgentProfile workspace returned error: %v", err)
	}
	if workspaceProfile.ProfileID != "ap_workspace" || workspaceProfile.Harness != "claude" || workspaceProfile.Model != "opus" {
		t.Fatalf("workspace profile = %#v, want workspace override", workspaceProfile)
	}
	fallbackProfile, err := store.GetAgentProfile(ctx, "ws-2", "executor")
	if err != nil {
		t.Fatalf("GetAgentProfile fallback returned error: %v", err)
	}
	if fallbackProfile.ProfileID != "ap_global" || fallbackProfile.Harness != "codex" || fallbackProfile.Prompt != "Do work" {
		t.Fatalf("fallback profile = %#v, want global profile", fallbackProfile)
	}
}

func TestAgentProfileAllowsNullableOverrides(t *testing.T) {
	store := newMigratedGlobalDBStore(t, "agent-profile-nullable")
	ctx := context.Background()
	if err := store.UpsertAgentProfile(ctx, AgentProfile{ProfileID: "ap_partial", Name: "partial"}); err != nil {
		t.Fatalf("UpsertAgentProfile partial returned error: %v", err)
	}
	profile, err := store.GetAgentProfile(ctx, "", "partial")
	if err != nil {
		t.Fatalf("GetAgentProfile partial returned error: %v", err)
	}
	if profile.Harness != "" || profile.Model != "" || profile.InvocationClass != "" {
		t.Fatalf("partial profile = %#v, want empty explicit overrides", profile)
	}
}

func TestAgentProfileUpsertUpdatesExistingScopeAndName(t *testing.T) {
	store := newMigratedGlobalDBStore(t, "agent-profile-upsert")
	ctx := context.Background()
	if err := store.UpsertAgentProfile(ctx, AgentProfile{ProfileID: "ap_first", WorkspaceID: "ws-1", Name: "executor", Harness: "codex", Model: "gpt-5.1-codex"}); err != nil {
		t.Fatalf("UpsertAgentProfile first returned error: %v", err)
	}
	if err := store.UpsertAgentProfile(ctx, AgentProfile{ProfileID: "ap_second", WorkspaceID: "ws-1", Name: "executor", Harness: "claude", Model: "opus"}); err != nil {
		t.Fatalf("UpsertAgentProfile update returned error: %v", err)
	}

	profile, err := store.GetAgentProfile(ctx, "ws-1", "executor")
	if err != nil {
		t.Fatalf("GetAgentProfile returned error: %v", err)
	}
	if profile.ProfileID != "ap_first" || profile.Harness != "claude" || profile.Model != "opus" {
		t.Fatalf("profile = %#v, want existing scoped name updated in place", profile)
	}
}

func TestAgentProfileListUsesRequestedScope(t *testing.T) {
	store := newMigratedGlobalDBStore(t, "agent-profile-list")
	ctx := context.Background()
	if err := store.UpsertAgentProfile(ctx, AgentProfile{ProfileID: "ap_global", Name: "global", Harness: "codex"}); err != nil {
		t.Fatalf("UpsertAgentProfile global returned error: %v", err)
	}
	if err := store.UpsertAgentProfile(ctx, AgentProfile{ProfileID: "ap_workspace", WorkspaceID: "ws-1", Name: "workspace", Harness: "claude"}); err != nil {
		t.Fatalf("UpsertAgentProfile workspace returned error: %v", err)
	}

	globalProfiles, err := store.ListAgentProfiles(ctx, "")
	if err != nil {
		t.Fatalf("ListAgentProfiles global returned error: %v", err)
	}
	if len(globalProfiles) != 1 || globalProfiles[0].ProfileID != "ap_global" {
		t.Fatalf("global profiles = %#v, want only global", globalProfiles)
	}
	workspaceProfiles, err := store.ListAgentProfiles(ctx, "ws-1")
	if err != nil {
		t.Fatalf("ListAgentProfiles workspace returned error: %v", err)
	}
	if len(workspaceProfiles) != 1 || workspaceProfiles[0].ProfileID != "ap_workspace" {
		t.Fatalf("workspace profiles = %#v, want only workspace", workspaceProfiles)
	}
}

func TestEnsureDefaultHelperProfileCreatesWorkspaceScopedHelper(t *testing.T) {
	store := newMigratedGlobalDBStore(t, "helper-profile")
	ctx := context.Background()
	if err := store.CreateSession(ctx, "ws-1", "alpha", "/tmp/origin", "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}

	help, err := store.EnsureDefaultHelperProfile(ctx, "ws-1", "codex", "Explain this workspace")
	if err != nil {
		t.Fatalf("EnsureDefaultHelperProfile returned error: %v", err)
	}
	if help.WorkspaceID != "ws-1" || help.Name != "helper" || help.Harness != "codex" || help.Prompt != "Explain this workspace" {
		t.Fatalf("helper profile = %#v", help)
	}

	got, err := store.GetAgentProfile(ctx, "ws-1", "helper")
	if err != nil {
		t.Fatalf("GetAgentProfile returned error: %v", err)
	}
	if got.ProfileID != help.ProfileID {
		t.Fatalf("persisted helper id = %q, want %q", got.ProfileID, help.ProfileID)
	}
}

func TestEnsureDefaultHelperProfileDoesNotOverwriteExistingHelper(t *testing.T) {
	store := newMigratedGlobalDBStore(t, "helper-profile-existing")
	ctx := context.Background()
	if err := store.CreateSession(ctx, "ws-1", "alpha", "/tmp/origin", "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	if err := store.UpsertAgentProfile(ctx, AgentProfile{ProfileID: "ap_existing", WorkspaceID: "ws-1", Name: "helper", Harness: "opencode", Prompt: "Keep me"}); err != nil {
		t.Fatalf("UpsertAgentProfile returned error: %v", err)
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
	store := newMigratedGlobalDBStore(t, "helper-profile-unique-ids")
	ctx := context.Background()
	if err := store.CreateSession(ctx, "a-b_c", "one", "/tmp/one", "manual", "auto"); err != nil {
		t.Fatalf("CreateSession one returned error: %v", err)
	}
	if err := store.CreateSession(ctx, "a_b-c", "two", "/tmp/two", "manual", "auto"); err != nil {
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
	store := newMigratedGlobalDBStore(t, "helper-profile-scope")
	ctx := context.Background()
	if err := store.CreateSession(ctx, "system-id", "system", "/tmp/system-origin", "manual", "auto"); err != nil {
		t.Fatalf("CreateSession system returned error: %v", err)
	}
	if err := store.CreateSession(ctx, "project-id", "project", "/tmp/project-origin", "manual", "auto"); err != nil {
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
	store := newMigratedGlobalDBStore(t, "helper-profile-missing-workspace")
	_, err := store.EnsureDefaultHelperProfile(context.Background(), "missing", "codex", "Help")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("EnsureDefaultHelperProfile error = %v, want ErrNotFound", err)
	}
}

func TestAgentProfileRejectsInvalidInput(t *testing.T) {
	store := newMigratedGlobalDBStore(t, "agent-profile-invalid")
	err := store.UpsertAgentProfile(context.Background(), AgentProfile{Name: "missing-id"})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("UpsertAgentProfile error = %v, want ErrInvalidInput", err)
	}
}
