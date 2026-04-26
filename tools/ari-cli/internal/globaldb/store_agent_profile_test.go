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

func TestAgentProfileRejectsInvalidInput(t *testing.T) {
	store := newMigratedGlobalDBStore(t, "agent-profile-invalid")
	err := store.UpsertAgentProfile(context.Background(), AgentProfile{Name: "missing-id"})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("UpsertAgentProfile error = %v, want ErrInvalidInput", err)
	}
}
