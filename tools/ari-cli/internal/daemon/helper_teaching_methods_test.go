package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

func TestHelperContextHomeUsesConfiguredState(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"default_harness":"codex","preferred_model":"gpt-5.1"}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", configPath, "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	home := t.TempDir()
	if err := store.CreateSession(context.Background(), "home-id", "home", home, "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	if err := store.AddFolder(context.Background(), "home-id", home, "unknown", true); err != nil {
		t.Fatalf("AddFolder returned error: %v", err)
	}
	if err := store.UpsertAgentProfile(context.Background(), globaldb.AgentProfile{ProfileID: "ap-helper", WorkspaceID: "home-id", Name: "helper", Harness: "codex", Prompt: helperPrompt()}); err != nil {
		t.Fatalf("UpsertAgentProfile helper returned error: %v", err)
	}
	if err := store.UpsertAgentProfile(context.Background(), globaldb.AgentProfile{ProfileID: "ap-reviewer", WorkspaceID: "home-id", Name: "frontend-reviewer", Harness: "opencode", Prompt: "Review UI regressions"}); err != nil {
		t.Fatalf("UpsertAgentProfile reviewer returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "project-1", t.TempDir())

	resp := callMethod[HelperContextResponse](t, registry, "helper.context", HelperContextRequest{WorkspaceID: "home-id", Question: "what do I have?"})
	if resp.Workspace.Name != "home" || resp.Workspace.OriginRoot != home {
		t.Fatalf("workspace context = %#v", resp.Workspace)
	}
	if resp.Defaults["default_harness"] != "codex" || resp.Defaults["preferred_model"] != "gpt-5.1" {
		t.Fatalf("defaults = %#v", resp.Defaults)
	}
	if resp.Health.DaemonVersion != "test-version" || !resp.Health.ConfigReadable || !resp.Health.WorkspaceAvailable {
		t.Fatalf("health = %#v", resp.Health)
	}
	if !containsProfileSummary(resp.Profiles, "frontend-reviewer", "opencode") {
		t.Fatalf("profiles = %#v, want frontend-reviewer opencode", resp.Profiles)
	}
	if len(resp.Workspaces) < 2 {
		t.Fatalf("workspaces = %#v, want home and project summaries", resp.Workspaces)
	}
	if !containsString(resp.Docs, "ari init") || !containsString(resp.Explanations, "profile") {
		t.Fatalf("docs/explanations = %#v %#v", resp.Docs, resp.Explanations)
	}
}

func TestHelperContextProjectIncludesWorkflowLearningsFromAriStateAndArtifacts(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	projectRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectRoot, ".ari", "active", "alpha"), 0o755); err != nil {
		t.Fatalf("create .ari active dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, ".ari", "active", "alpha", "STATE.json"), []byte(`{"current_phase":"Phase A","next":"run verify"}`), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	if err := store.CreateSession(context.Background(), "project-1", "project-1", projectRoot, "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	if err := store.AddFolder(context.Background(), "project-1", projectRoot, "git", true); err != nil {
		t.Fatalf("AddFolder returned error: %v", err)
	}
	if err := store.UpsertAgentProfile(context.Background(), globaldb.AgentProfile{ProfileID: "ap-helper", WorkspaceID: "project-1", Name: "helper", Harness: "codex", Prompt: helperPrompt()}); err != nil {
		t.Fatalf("UpsertAgentProfile returned error: %v", err)
	}
	finishedAt := time.Now().UTC()
	if err := store.UpsertFinalResponse(context.Background(), globaldb.FinalResponse{FinalResponseID: "fr-1", RunID: "run-1", WorkspaceID: "project-1", TaskID: "task-1", ContextPacketID: "cp-1", Status: "failed", Text: "Build failed because gofmt found files", CreatedAt: finishedAt}); err != nil {
		t.Fatalf("UpsertFinalResponse returned error: %v", err)
	}
	if err := store.UpsertAgentSessionTelemetry(context.Background(), globaldb.AgentSessionTelemetry{RunID: "run-1", WorkspaceID: "project-1", TaskID: "task-1", ProfileID: "ap-helper", ProfileName: "helper", Harness: "codex", Model: "gpt", InvocationClass: "agent", Status: "failed", ExitCodeKnown: true, ExitCode: int64Ptr(1), CreatedAt: finishedAt, UpdatedAt: finishedAt}); err != nil {
		t.Fatalf("UpsertAgentSessionTelemetry returned error: %v", err)
	}
	if err := store.CreateCommand(context.Background(), globaldb.CreateCommandParams{CommandID: "cmd-verify", WorkspaceID: "project-1", Command: "just", Args: `["verify"]`, Status: "exited", ExitCode: intPtr(1), StartedAt: "2026-04-28T00:00:00Z"}); err != nil {
		t.Fatalf("CreateCommand returned error: %v", err)
	}
	d.setCommandOutput("cmd-verify", "verify failed")

	resp := callMethod[HelperContextResponse](t, registry, "helper.context", HelperContextRequest{WorkspaceID: "project-1", Question: "tell me about this project"})
	if resp.Workspace.OriginRoot != projectRoot {
		t.Fatalf("workspace = %#v", resp.Workspace)
	}
	if len(resp.FinalResponses) != 1 || resp.FinalResponses[0].Summary != "Build failed because gofmt found files" {
		t.Fatalf("final responses = %#v", resp.FinalResponses)
	}
	if len(resp.Telemetry) != 1 || resp.Telemetry[0].Failed != 1 || resp.Telemetry[0].ProfileName != "helper" {
		t.Fatalf("telemetry = %#v", resp.Telemetry)
	}
	if len(resp.Proofs) != 1 || resp.Proofs[0].Status != "failed" || resp.Proofs[0].Command != "just verify" {
		t.Fatalf("proofs = %#v", resp.Proofs)
	}
	if !containsLearning(resp.WorkflowLearnings, "run-1") || !containsLearning(resp.WorkflowLearnings, "Phase A") || !containsLearning(resp.WorkflowLearnings, "run verify") || !containsLearning(resp.WorkflowLearnings, "cmd-verify") {
		t.Fatalf("workflow learnings = %#v", resp.WorkflowLearnings)
	}
}

func TestHelperExplainUsesStructuredTopicsAndLatestFailure(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "project-1", t.TempDir())
	createdAt := time.Now().UTC()
	if err := store.UpsertFinalResponse(context.Background(), globaldb.FinalResponse{FinalResponseID: "fr-1", RunID: "run-1", WorkspaceID: "project-1", TaskID: "task-1", ContextPacketID: "cp-1", Status: "failed", Text: "Tests failed in package ./internal/daemon", CreatedAt: createdAt}); err != nil {
		t.Fatalf("UpsertFinalResponse returned error: %v", err)
	}

	profile := callMethod[HelperExplainResponse](t, registry, "helper.explain", HelperExplainRequest{WorkspaceID: "project-1", Topic: "profile"})
	if profile.Topic != "profile" || !strings.Contains(profile.Explanation, "Profiles are workspace-scoped") || len(profile.Anchors) == 0 {
		t.Fatalf("profile explanation = %#v", profile)
	}
	failure := callMethod[HelperExplainResponse](t, registry, "helper.explain", HelperExplainRequest{WorkspaceID: "project-1", Topic: "latest failed run"})
	if failure.Topic != "latest failed run" || !strings.Contains(failure.Explanation, "Tests failed") || failure.RunID != "run-1" {
		t.Fatalf("failure explanation = %#v", failure)
	}
}

func containsProfileSummary(profiles []HelperProfileSummary, name, harness string) bool {
	for _, profile := range profiles {
		if profile.Name == name && profile.Harness == harness {
			return true
		}
	}
	return false
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func containsLearning(values []WorkflowLearning, needle string) bool {
	for _, value := range values {
		if strings.Contains(value.Summary, needle) || strings.Contains(value.SourceID, needle) {
			return true
		}
	}
	return false
}

func int64Ptr(value int64) *int64 {
	return &value
}
