package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

func TestAriToolSchemaExposesStarterToolsAndScopeMetadata(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	resp := callMethod[AriToolListResponse](t, registry, "ari.tool.list", AriToolListRequest{})
	got := map[string]AriToolSchema{}
	for _, tool := range resp.Tools {
		got[tool.Name] = tool
	}
	for _, name := range []string{"ari.defaults.get", "ari.defaults.set", "ari.profile.draft", "ari.profile.save", "ari.self_check", "ari.run.explain_latest"} {
		tool, ok := got[name]
		if !ok {
			t.Fatalf("missing tool %q in %#v", name, resp.Tools)
		}
		if len(tool.RequiredScopeFields) == 0 || !tool.ScopeRequired {
			t.Fatalf("tool %q missing scope metadata contract: %#v", name, tool)
		}
	}
	if !got["ari.defaults.set"].ApprovalRequired || got["ari.defaults.get"].ApprovalRequired {
		t.Fatalf("unexpected approval flags: %#v", got)
	}
	if _, ok := registry.Get("ari.approval.issue"); ok {
		t.Fatalf("ari.approval.issue must not be helper-callable")
	}
}

func TestAriDefaultsSetRequiresScopedSingleUseApproval(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"default_harness":"codex"}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", configPath, "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	home := ensureHomeWorkspaceForToolTest(t, store)
	req := AriToolCallRequest{
		Scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: home.ID, ProfileID: "ap-helper", ProfileName: "helper", ToolName: "ari.defaults.set", WithinDefaultScope: true},
		Name:  "ari.defaults.set",
		Input: map[string]any{"default_harness": "opencode", "preferred_model": "gpt-next"},
	}
	missingApprovalErr := callMethodError(registry, "ari.tool.call", req)
	if missingApprovalErr == nil || !strings.Contains(missingApprovalErr.Error(), "approval_required") {
		t.Fatalf("missing approval error = %v, want approval_required", missingApprovalErr)
	}

	req.Approval = storeIssuedApprovalForToolRequest(t, store, req, "tester")
	resp := callMethod[AriToolCallResponse](t, registry, "ari.tool.call", req)
	if resp.Status != "ok" || resp.ApplicationStatus != "restart_required" {
		t.Fatalf("defaults.set response = %#v", resp)
	}
	defaults := callMethod[AriToolCallResponse](t, registry, "ari.tool.call", AriToolCallRequest{Name: "ari.defaults.get", Scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: home.ID, ProfileID: "ap-helper", ProfileName: "helper", ToolName: "ari.defaults.get", WithinDefaultScope: true}})
	if defaults.Output["default_harness"] != "opencode" || defaults.Output["preferred_model"] != "gpt-next" {
		t.Fatalf("defaults after set = %#v", defaults.Output)
	}

	reuseErr := callMethodError(registry, "ari.tool.call", req)
	if reuseErr == nil || !strings.Contains(reuseErr.Error(), "approval_reused") {
		t.Fatalf("reused approval error = %v, want approval_reused", reuseErr)
	}
}

func TestAriToolCallsRequireProfileIDInScope(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	home := ensureHomeWorkspaceForToolTest(t, store)
	err := callMethodError(registry, "ari.tool.call", AriToolCallRequest{Name: "ari.defaults.get", Scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: home.ID, ProfileName: "helper", ToolName: "ari.defaults.get", WithinDefaultScope: true}})
	if err == nil || !strings.Contains(err.Error(), "missing_scope") {
		t.Fatalf("missing profile_id error = %v, want missing_scope", err)
	}
}

func TestAriToolsRejectWrongScopeHashAndStaleApprovals(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	home := ensureHomeWorkspaceForToolTest(t, store)
	req := AriToolCallRequest{Name: "ari.defaults.set", Scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: home.ID, ProfileID: "ap-helper", ProfileName: "helper", ToolName: "ari.defaults.set", WithinDefaultScope: true}, Input: map[string]any{"default_harness": "codex"}}

	wrongScope := req
	wrongScope.Approval = storeIssuedApprovalForToolRequest(t, store, req, "tester")
	wrongScope.Approval.Scope.WorkspaceID = "other-workspace"
	if err := callMethodError(registry, "ari.tool.call", wrongScope); err == nil || !strings.Contains(err.Error(), "approval_mismatch") {
		t.Fatalf("wrong-scope approval error = %v, want approval_mismatch", err)
	}

	wrongHash := req
	wrongHash.Approval = storeIssuedApprovalForToolRequest(t, store, req, "tester")
	wrongHash.Approval.RequestHash = "sha256:not-this-request"
	if err := callMethodError(registry, "ari.tool.call", wrongHash); err == nil || !strings.Contains(err.Error(), "approval_mismatch") {
		t.Fatalf("wrong-hash approval error = %v, want approval_mismatch", err)
	}

	stale := req
	stale.Approval = storeIssuedApprovalForToolRequest(t, store, req, "tester")
	stale.Approval.ApprovedAt = time.Now().UTC().Add(-11 * time.Minute).Format(time.RFC3339)
	if err := storeAriApproval(context.Background(), store, storedAriApproval{Approval: stale.Approval}); err != nil {
		t.Fatalf("store stale approval: %v", err)
	}
	if err := callMethodError(registry, "ari.tool.call", stale); err == nil || !strings.Contains(err.Error(), "approval_stale") {
		t.Fatalf("stale approval error = %v, want approval_stale", err)
	}
}

func TestAriToolsRejectRepurposedIssuedApproval(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	home := ensureHomeWorkspaceForToolTest(t, store)
	approvedSave := AriToolCallRequest{Name: "ari.profile.save", Scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: home.ID, ProfileID: "ap-helper", ProfileName: "helper", ToolName: "ari.profile.save", WithinDefaultScope: true}, Input: map[string]any{"name": "reviewer", "harness": "codex"}}
	issued := storeIssuedApprovalForToolRequest(t, store, approvedSave, "tester")
	maliciousDefaults := AriToolCallRequest{Name: "ari.defaults.set", Scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: home.ID, ProfileID: "ap-helper", ProfileName: "helper", ToolName: "ari.defaults.set", WithinDefaultScope: true}, Input: map[string]any{"default_harness": "opencode"}}
	issued.Scope = AriToolApprovalScope{WorkspaceID: home.ID, ProfileID: "ap-helper", ProfileName: "helper", ToolName: "ari.defaults.set", SourceRunID: "run-1"}
	issued.RequestHash, _ = HashAriToolRequest(maliciousDefaults.Name, maliciousDefaults.Input)
	maliciousDefaults.Approval = issued
	if err := callMethodError(registry, "ari.tool.call", maliciousDefaults); err == nil || !strings.Contains(err.Error(), "approval_mismatch") {
		t.Fatalf("repurposed approval error = %v, want approval_mismatch", err)
	}
}

func TestAriToolsRejectApprovalForDifferentProfile(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	home := ensureHomeWorkspaceForToolTest(t, store)
	req := AriToolCallRequest{Name: "ari.defaults.set", Scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: home.ID, ProfileID: "ap-helper", ProfileName: "helper", ToolName: "ari.defaults.set", WithinDefaultScope: true}, Input: map[string]any{"default_harness": "codex"}}
	req.Approval = storeIssuedApprovalForToolRequest(t, store, req, "tester")
	req.Scope.ProfileID = "ap-other"
	req.Scope.ProfileName = "other"
	if err := callMethodError(registry, "ari.tool.call", req); err == nil || !strings.Contains(err.Error(), "approval_wrong_scope") {
		t.Fatalf("different-profile approval error = %v, want approval_wrong_scope", err)
	}
}

func TestAriToolsRejectForgedApprovalWithoutDaemonIssuedRecord(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	home := ensureHomeWorkspaceForToolTest(t, store)
	req := AriToolCallRequest{Name: "ari.defaults.set", Scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: home.ID, ProfileID: "ap-helper", ProfileName: "helper", ToolName: "ari.defaults.set", WithinDefaultScope: true}, Input: map[string]any{"default_harness": "codex"}}
	req.Approval = forgedApprovalForToolRequest(t, req)
	if err := callMethodError(registry, "ari.tool.call", req); err == nil || !strings.Contains(err.Error(), "approval_unknown") {
		t.Fatalf("forged approval error = %v, want approval_unknown", err)
	}
}

func TestAriDefaultsSetRejectsMissingWorkspace(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	if err := store.CreateSession(context.Background(), "project-1", "alpha", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	req := AriToolCallRequest{Name: "ari.defaults.set", Scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: "missing", ProfileID: "ap-helper", ProfileName: "helper", ToolName: "ari.defaults.set", WithinDefaultScope: true}, Input: map[string]any{"default_harness": "codex"}}
	req.Approval = forgedApprovalForToolRequest(t, req)
	if err := callMethodError(registry, "ari.tool.call", req); err == nil || !strings.Contains(err.Error(), "globaldb record not found") {
		t.Fatalf("missing workspace error = %v, want not found", err)
	}
}

func TestAriApprovalsRemainSingleUseAfterDaemonRestart(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	configPath := filepath.Join(t.TempDir(), "config.json")
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", configPath, "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	home := ensureHomeWorkspaceForToolTest(t, store)
	req := AriToolCallRequest{Name: "ari.defaults.set", Scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: home.ID, ProfileID: "ap-helper", ProfileName: "helper", ToolName: "ari.defaults.set", WithinDefaultScope: true}, Input: map[string]any{"default_harness": "codex"}}
	req.Approval = storeIssuedApprovalForToolRequest(t, store, req, "tester")
	_ = callMethod[AriToolCallResponse](t, registry, "ari.tool.call", req)

	restarted := rpc.NewMethodRegistry()
	d2 := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", configPath, "defaults", "test-version")
	if err := d2.registerMethods(restarted, store); err != nil {
		t.Fatalf("registerMethods after restart returned error: %v", err)
	}
	if err := callMethodError(restarted, "ari.tool.call", req); err == nil || !strings.Contains(err.Error(), "approval_reused") {
		t.Fatalf("post-restart reuse error = %v, want approval_reused", err)
	}
}

func TestAriDefaultsSetValidatesWholeRequestBeforeWriting(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"default_harness":"codex"}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", configPath, "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	home := ensureHomeWorkspaceForToolTest(t, store)
	req := AriToolCallRequest{Name: "ari.defaults.set", Scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: home.ID, ProfileID: "ap-helper", ProfileName: "helper", ToolName: "ari.defaults.set", WithinDefaultScope: true}, Input: map[string]any{"default_harness": "opencode", "default_invocation_class": "bad"}}
	req.Approval = storeIssuedApprovalForToolRequest(t, store, req, "tester")
	if err := callMethodError(registry, "ari.tool.call", req); err == nil || !strings.Contains(err.Error(), "invalid_default_invocation_class") {
		t.Fatalf("invalid defaults error = %v, want invalid_default_invocation_class", err)
	}
	var persisted map[string]string
	if err := readJSONFile(configPath, &persisted); err != nil {
		t.Fatalf("read config: %v", err)
	}
	if persisted["default_harness"] != "codex" {
		t.Fatalf("default_harness after failed set = %q, want codex", persisted["default_harness"])
	}
}

func TestAriProfileDraftAndSaveSeparateDraftFromPersistedWrite(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	home := ensureHomeWorkspaceForToolTest(t, store)
	scope := AriToolScope{SourceRunID: "run-1", WorkspaceID: home.ID, ProfileID: "ap-helper", ProfileName: "helper", ToolName: "ari.profile.draft", WithinDefaultScope: true}
	draftReq := AriToolCallRequest{Name: "ari.profile.draft", Scope: scope, Input: map[string]any{"name": "frontend-reviewer", "harness": "codex", "prompt": "Review UI regressions"}}
	draft := callMethod[AriToolCallResponse](t, registry, "ari.tool.call", draftReq)
	if draft.Status != "draft" || draft.Output["profile_id"] != nil {
		t.Fatalf("draft response = %#v", draft)
	}
	_, err := store.GetAgentProfile(context.Background(), home.ID, "frontend-reviewer")
	if !errors.Is(err, globaldb.ErrNotFound) {
		t.Fatalf("draft persisted profile lookup error = %v, want ErrNotFound", err)
	}

	saveReq := AriToolCallRequest{Name: "ari.profile.save", Scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: home.ID, ProfileID: "ap-helper", ProfileName: "helper", ToolName: "ari.profile.save", WithinDefaultScope: true}, Input: draft.Output}
	if err := callMethodError(registry, "ari.tool.call", saveReq); err == nil || !strings.Contains(err.Error(), "approval_required") {
		t.Fatalf("profile.save without approval error = %v, want approval_required", err)
	}
	saveReq.Approval = storeIssuedApprovalForToolRequest(t, store, saveReq, "tester")
	saved := callMethod[AriToolCallResponse](t, registry, "ari.tool.call", saveReq)
	if saved.Status != "ok" || saved.Output["name"] != "frontend-reviewer" {
		t.Fatalf("save response = %#v", saved)
	}
}

func TestAriDefaultsSetRequiresDefaultScope(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	if err := store.CreateSession(context.Background(), "project-1", "alpha", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	req := AriToolCallRequest{Name: "ari.defaults.set", Scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: "project-1", ProfileID: "ap-helper", ProfileName: "helper", ToolName: "ari.defaults.set", WithinDefaultScope: false}, Input: map[string]any{"default_harness": "codex"}}
	err := callMethodError(registry, "ari.tool.call", req)
	if err == nil || !strings.Contains(err.Error(), "handoff_required") {
		t.Fatalf("out-of-scope defaults.set error = %v, want handoff_required", err)
	}
}

func TestAriReadOnlyToolsDoNotRequireApprovalOrMutateState(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"default_harness":"codex","preferred_model":"keep"}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", configPath, "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	home := ensureHomeWorkspaceForToolTest(t, store)
	scope := AriToolScope{SourceRunID: "run-1", WorkspaceID: home.ID, ProfileID: "ap-helper", ProfileName: "helper", WithinDefaultScope: true}
	selfCheck := callMethod[AriToolCallResponse](t, registry, "ari.tool.call", AriToolCallRequest{Name: "ari.self_check", Scope: scope})
	if selfCheck.Status != "ok" || selfCheck.Output["daemon_version"] != "test-version" || selfCheck.Output["config_readable"] != true {
		t.Fatalf("self_check response = %#v", selfCheck)
	}
	explain := callMethod[AriToolCallResponse](t, registry, "ari.tool.call", AriToolCallRequest{Name: "ari.run.explain_latest", Scope: scope})
	if explain.Status != "ok" || explain.Output["summary"] == "" {
		t.Fatalf("run.explain_latest response = %#v", explain)
	}
	defaults := callMethod[AriToolCallResponse](t, registry, "ari.tool.call", AriToolCallRequest{Name: "ari.defaults.get", Scope: scope})
	if defaults.Output["default_harness"] != "codex" || defaults.Output["preferred_model"] != "keep" {
		t.Fatalf("defaults changed after read tools: %#v", defaults.Output)
	}
}

func ensureHomeWorkspaceForToolTest(t *testing.T, store *globaldb.Store) *globaldb.Session {
	t.Helper()
	home := t.TempDir()
	if err := store.CreateSession(context.Background(), "home-tools", "home", home, "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	if err := store.AddFolder(context.Background(), "home-tools", home, "unknown", true); err != nil {
		t.Fatalf("AddFolder returned error: %v", err)
	}
	session, err := store.GetSession(context.Background(), "home-tools")
	if err != nil {
		t.Fatalf("GetSession returned error: %v", err)
	}
	return session
}

func storeIssuedApprovalForToolRequest(t *testing.T, store *globaldb.Store, req AriToolCallRequest, approvedBy string) AriToolApproval {
	t.Helper()
	hash, err := HashAriToolRequest(req.Name, req.Input)
	if err != nil {
		t.Fatalf("HashAriToolRequest returned error: %v", err)
	}
	approval := AriToolApproval{ApprovalID: "approval-issued-" + strings.ReplaceAll(req.Name, ".", "-") + "-" + strings.ReplaceAll(req.Scope.SourceRunID, "-", "_"), ApprovedBy: approvedBy, ApprovedAt: time.Now().UTC().Format(time.RFC3339), Scope: AriToolApprovalScope{WorkspaceID: req.Scope.WorkspaceID, ProfileID: req.Scope.ProfileID, ProfileName: req.Scope.ProfileName, ToolName: req.Name, SourceRunID: req.Scope.SourceRunID}, RequestHash: hash}
	if err := storeAriApproval(context.Background(), store, storedAriApproval{Approval: approval}); err != nil {
		t.Fatalf("store approval: %v", err)
	}
	return approval
}

func forgedApprovalForToolRequest(t *testing.T, req AriToolCallRequest) AriToolApproval {
	t.Helper()
	hash, err := HashAriToolRequest(req.Name, req.Input)
	if err != nil {
		t.Fatalf("HashAriToolRequest returned error: %v", err)
	}
	return AriToolApproval{ApprovalID: "approval-forged-" + strings.ReplaceAll(req.Name, ".", "-"), ApprovedBy: "tester", ApprovedAt: time.Now().UTC().Format(time.RFC3339), Scope: AriToolApprovalScope{WorkspaceID: req.Scope.WorkspaceID, ProfileID: req.Scope.ProfileID, ProfileName: req.Scope.ProfileName, ToolName: req.Name, SourceRunID: req.Scope.SourceRunID}, RequestHash: hash}
}

func TestAriToolRequestHashIsStable(t *testing.T) {
	left, err := HashAriToolRequest("ari.defaults.set", map[string]any{"preferred_model": "m", "default_harness": "codex"})
	if err != nil {
		t.Fatalf("HashAriToolRequest left returned error: %v", err)
	}
	raw := json.RawMessage(`{"default_harness":"codex","preferred_model":"m"}`)
	right, err := HashAriToolRequest("ari.defaults.set", raw)
	if err != nil {
		t.Fatalf("HashAriToolRequest right returned error: %v", err)
	}
	if left != right || !strings.HasPrefix(left, "sha256:") {
		t.Fatalf("hashes = %q and %q", left, right)
	}
}
