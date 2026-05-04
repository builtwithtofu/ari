package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

func TestActiveContextPersistsInDaemonStore(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())

	set := callMethod[ContextSetResponse](t, registry, "context.set", ContextSetRequest{WorkspaceID: "ws-1"})
	if set.Current.WorkspaceID != "ws-1" {
		t.Fatalf("context.set current workspace_id = %q, want ws-1", set.Current.WorkspaceID)
	}
	if set.Current.Version == "" {
		t.Fatal("context.set current version is empty")
	}

	get := callMethod[ContextGetResponse](t, registry, "context.get", ContextGetRequest{})
	if get.Current.WorkspaceID != "ws-1" {
		t.Fatalf("context.get current workspace_id = %q, want ws-1", get.Current.WorkspaceID)
	}
	if get.Current.Version != set.Current.Version {
		t.Fatalf("context.get version = %q, want %q", get.Current.Version, set.Current.Version)
	}

	newDaemonRegistry := rpc.NewMethodRegistry()
	newDaemon := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := newDaemon.registerMethods(newDaemonRegistry, store); err != nil {
		t.Fatalf("registerMethods on new daemon returned error: %v", err)
	}
	restarted := callMethod[ContextGetResponse](t, newDaemonRegistry, "context.get", ContextGetRequest{})
	if restarted.Current.WorkspaceID != "ws-1" || restarted.Current.Version != set.Current.Version {
		t.Fatalf("restarted context = %#v, want persisted ws-1 version %q", restarted.Current, set.Current.Version)
	}
}

func TestWorkspaceMembershipsForPathListsContainingWorkspacesAndMarksActive(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	root := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "ws-active", root)
	nestedRoot := filepath.Join(root, "nested")
	seedSessionWithPrimaryFolder(t, store, "ws-other", nestedRoot)
	_ = callMethod[ContextSetResponse](t, registry, "context.set", ContextSetRequest{WorkspaceID: "ws-active"})

	resp := callMethod[WorkspaceMembershipsForPathResponse](t, registry, "workspace.memberships_for_path", WorkspaceMembershipsForPathRequest{Path: filepath.Join(nestedRoot, "deeper")})
	if resp.Path == "" {
		t.Fatal("normalized path is empty")
	}
	if len(resp.Memberships) != 2 {
		t.Fatalf("memberships len = %d, want 2", len(resp.Memberships))
	}
	if resp.Memberships[0].WorkspaceID != "ws-active" || !resp.Memberships[0].Active {
		t.Fatalf("first membership = %#v, want active workspace first", resp.Memberships[0])
	}
	if resp.Memberships[1].WorkspaceID != "ws-other" || resp.Memberships[1].Active {
		t.Fatalf("second membership = %#v, want other non-active workspace", resp.Memberships[1])
	}
}

func TestWorkspaceMembershipsForPathDeduplicatesWorkspaceByClosestFolder(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	root := t.TempDir()
	nestedRoot := filepath.Join(root, "project")
	seedSessionWithPrimaryFolder(t, store, "ws-1", root)
	if err := store.AddFolder(context.Background(), "ws-1", nestedRoot, "jj", false); err != nil {
		t.Fatalf("AddFolder returned error: %v", err)
	}

	resp := callMethod[WorkspaceMembershipsForPathResponse](t, registry, "workspace.memberships_for_path", WorkspaceMembershipsForPathRequest{Path: filepath.Join(nestedRoot, "deeper")})
	if len(resp.Memberships) != 1 {
		t.Fatalf("memberships len = %d, want one membership per workspace: %#v", len(resp.Memberships), resp.Memberships)
	}
	if resp.Memberships[0].FolderPath != nestedRoot {
		t.Fatalf("membership folder = %q, want closest folder %q", resp.Memberships[0].FolderPath, nestedRoot)
	}
}

func TestDashboardGetUsesActiveContextAndIncludesCwdMemberships(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	activeRoot := t.TempDir()
	cwdRoot := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "ws-active", activeRoot)
	seedSessionWithPrimaryFolder(t, store, "ws-cwd", cwdRoot)
	_ = callMethod[ContextSetResponse](t, registry, "context.set", ContextSetRequest{WorkspaceID: "ws-active"})

	resp := callMethod[DashboardGetResponse](t, registry, "dashboard.get", DashboardGetRequest{CWD: cwdRoot})
	if resp.ActiveContext.WorkspaceID != "ws-active" {
		t.Fatalf("active context = %#v, want ws-active", resp.ActiveContext)
	}
	if resp.EffectiveWorkspaceID != "ws-active" {
		t.Fatalf("effective workspace = %q, want ws-active", resp.EffectiveWorkspaceID)
	}
	if resp.Activity.WorkspaceID != "ws-active" {
		t.Fatalf("activity workspace = %q, want ws-active", resp.Activity.WorkspaceID)
	}
	if len(resp.CWDMemberships) != 1 || resp.CWDMemberships[0].WorkspaceID != "ws-cwd" || resp.CWDMemberships[0].Active {
		t.Fatalf("cwd memberships = %#v, want non-active cwd workspace", resp.CWDMemberships)
	}
}

func TestDashboardGetIncludesResumeAffordanceForPersistedRunningAgentSession(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
	if err := store.CreateAgentSessionConfig(context.Background(), globaldb.AgentSessionConfig{AgentID: "agent-1", WorkspaceID: "ws-1", Name: "executor", Harness: "codex"}); err != nil {
		t.Fatalf("CreateAgent returned error: %v", err)
	}
	if err := store.CreateAgentSession(context.Background(), globaldb.AgentSession{SessionID: "run-1", WorkspaceID: "ws-1", AgentID: "agent-1", Harness: "codex", Status: "running", Usage: "durable"}); err != nil {
		t.Fatalf("CreateAgentSession returned error: %v", err)
	}
	if err := store.CreateAgentSessionConfig(context.Background(), globaldb.AgentSessionConfig{AgentID: "agent-2", WorkspaceID: "ws-1", Name: "reviewer", Harness: "opencode"}); err != nil {
		t.Fatalf("CreateAgent ephemeral returned error: %v", err)
	}
	if err := store.CreateAgentSession(context.Background(), globaldb.AgentSession{SessionID: "run-2", WorkspaceID: "ws-1", AgentID: "agent-2", Harness: "opencode", Status: "running", Usage: "ephemeral"}); err != nil {
		t.Fatalf("CreateAgentSession ephemeral returned error: %v", err)
	}
	_ = callMethod[ContextSetResponse](t, registry, "context.set", ContextSetRequest{WorkspaceID: "ws-1"})

	resp := callMethod[DashboardGetResponse](t, registry, "dashboard.get", DashboardGetRequest{})
	if len(resp.ResumeActions) != 1 {
		t.Fatalf("resume actions len = %d, want 1", len(resp.ResumeActions))
	}
	action := resp.ResumeActions[0]
	if action.ID != "resume:session:run-1" || action.Kind != "resume_session" || action.SourceID != "run-1" || action.WorkspaceID != "ws-1" || action.Label != "codex" {
		t.Fatalf("resume action = %#v, want session resume affordance for persisted running run", action)
	}
}

func TestResumeActionResolvesDashboardAffordance(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
	if err := store.CreateAgentSessionConfig(context.Background(), globaldb.AgentSessionConfig{AgentID: "agent-1", WorkspaceID: "ws-1", Name: "executor", Harness: "codex"}); err != nil {
		t.Fatalf("CreateAgent returned error: %v", err)
	}
	if err := store.CreateAgentSession(context.Background(), globaldb.AgentSession{SessionID: "run-1", WorkspaceID: "ws-1", AgentID: "agent-1", Harness: "codex", Status: "running", Usage: "durable"}); err != nil {
		t.Fatalf("CreateAgentSession returned error: %v", err)
	}
	contextSet := callMethod[ContextSetResponse](t, registry, "context.set", ContextSetRequest{WorkspaceID: "ws-1"})

	resp := callMethod[ResumeActionResponse](t, registry, "resume.action", ResumeActionRequest{ActionID: "resume:session:run-1", ObservedContextVersion: contextSet.Current.Version})
	if resp.Action.Kind != "resume_session" || resp.Action.SourceID != "run-1" || resp.Action.WorkspaceID != "ws-1" {
		t.Fatalf("resume action response = %#v, want session resume action", resp)
	}
}

func TestDashboardGetRejectsStaleObservedActiveContextVersion(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-one", t.TempDir())
	seedSessionWithPrimaryFolder(t, store, "ws-two", t.TempDir())
	first := callMethod[ContextSetResponse](t, registry, "context.set", ContextSetRequest{WorkspaceID: "ws-one"})
	_ = callMethod[ContextSetResponse](t, registry, "context.set", ContextSetRequest{WorkspaceID: "ws-two"})

	spec, ok := registry.Get("dashboard.get")
	if !ok {
		t.Fatal("dashboard.get method not registered")
	}
	_, err := spec.Call(context.Background(), []byte(`{"observed_context_version":"`+first.Current.Version+`"}`))
	if err == nil {
		t.Fatal("dashboard.get returned nil error for stale observed context version")
	}
	var rpcErr *rpc.HandlerError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("dashboard.get error = %T, want *rpc.HandlerError", err)
	}
	data, ok := rpcErr.Data.(map[string]any)
	if !ok || data["reason"] != "context_changed" || data["current_workspace_id"] != "ws-two" || data["observed_version"] != first.Current.Version {
		t.Fatalf("dashboard.get error data = %#v, want context_changed with current workspace", rpcErr.Data)
	}
}

func TestDashboardGetExplicitWorkspaceIgnoresStaleActiveContextVersion(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-one", t.TempDir())
	seedSessionWithPrimaryFolder(t, store, "ws-two", t.TempDir())
	first := callMethod[ContextSetResponse](t, registry, "context.set", ContextSetRequest{WorkspaceID: "ws-one"})
	_ = callMethod[ContextSetResponse](t, registry, "context.set", ContextSetRequest{WorkspaceID: "ws-two"})

	resp := callMethod[DashboardGetResponse](t, registry, "dashboard.get", DashboardGetRequest{WorkspaceID: "ws-one", ObservedContextVersion: first.Current.Version})
	if resp.EffectiveWorkspaceID != "ws-one" || resp.Activity.WorkspaceID != "ws-one" {
		t.Fatalf("dashboard response = %#v, want explicit ws-one despite stale active context version", resp)
	}
}

func TestActiveContextRejectsUnknownWorkspace(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	spec, ok := registry.Get("context.set")
	if !ok {
		t.Fatal("context.set method not registered")
	}
	if _, err := spec.Call(t.Context(), []byte(`{"workspace_id":"missing"}`)); err == nil {
		t.Fatal("context.set returned nil error for unknown workspace")
	}
}
