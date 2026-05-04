package daemon

import (
	"context"
	"strings"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

func TestPublicOrchestrationMethodInventoryUsesSessionProfileContextNames(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	expectedPublic := []string{
		"profile.create",
		"profile.get",
		"profile.list",
		"session.start",
		"session.list",
		"session.get",
		"session.message.send",
		"session.call.ephemeral",
		"session.fanout",
		"context.excerpt.create_from_tail",
		"context.excerpt.create_from_range",
		"context.excerpt.create_from_explicit_ids",
		"context.excerpt.get",
		"run.messages.tail",
		"run.messages.list",
		"workspace.status",
		"workspace.timeline",
	}
	for _, name := range expectedPublic {
		if _, ok := registry.Get(name); !ok {
			t.Fatalf("public orchestration method %q is not registered", name)
		}
	}

	legacyPublic := []string{
		"workspace.agent.create",
		"workspace.agent.list",
		"workspace.agent.update",
		"workspace.agent.delete",
		"workspace.agent.run",
		"agent.run",
		"agent.message.send",
		"agent.call.ephemeral",
		"agent.profile.run",
		"agent.profile.create",
		"agent.profile.get",
		"agent.profile.list",
		"agent.profile.helper.ensure",
		"agent.profile.helper.get",
		"profile.run",
		"profile.helper.ensure",
		"profile.helper.get",
		"message.excerpt.create_from_tail",
		"message.excerpt.create_from_range",
		"message.excerpt.create_from_explicit_ids",
		"message.excerpt.get",
	}
	for _, name := range legacyPublic {
		if _, ok := registry.Get(name); ok {
			t.Fatalf("legacy public orchestration method %q is still registered", name)
		}
	}
}

func TestSessionStartUsesStoredProfileAndSessionTerminology(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	var captured ExecutorStartRequest
	d.setHarnessFactoryForTest("test-harness", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return &capturingHarness{name: "test-harness", captured: &captured}, nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())

	created := callMethod[AgentProfileResponse](t, registry, "profile.create", AgentProfileCreateRequest{WorkspaceID: "ws-1", Name: "executor", Harness: "test-harness", Model: "model-1", Prompt: "profile behavior", InvocationClass: HarnessInvocationAgent})
	run := callMethod[AgentSessionStartResponse](t, registry, "session.start", AgentSessionStartRequest{WorkspaceID: "ws-1", Profile: created.Name, SessionID: "executor-main", Packet: ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}, Message: "Start phase 1"})

	if run.Run.AgentSessionID != "executor-main" || run.Run.SessionID != "executor-main" || run.Run.WorkspaceID != "ws-1" || run.Run.Executor != "test-harness" {
		t.Fatalf("run = %#v, want stable Ari session identity from profile", run.Run)
	}
	if captured.WorkspaceID != "ws-1" || captured.RunID != "executor-main" || captured.SessionID != "executor-main" || captured.Model != "model-1" || captured.Prompt != "profile behavior" || !strings.Contains(captured.ContextPacket, "Start phase 1") {
		t.Fatalf("captured start = %#v, want profile prompt as behavior and message as visible task payload", captured)
	}
	stored, err := store.GetAgentSession(context.Background(), "executor-main")
	if err != nil {
		t.Fatalf("GetAgentSession returned error: %v", err)
	}
	if stored.AgentID != created.ProfileID || stored.Usage != "sticky" {
		t.Fatalf("stored session = %#v, want sticky Ari session linked to profile", stored)
	}
	got := callMethod[SessionGetResponse](t, registry, "session.get", SessionGetRequest{SessionID: "executor-main"})
	if got.Session.SessionID != "executor-main" || got.Session.WorkspaceID != "ws-1" || got.Session.Executor != "test-harness" {
		t.Fatalf("session.get = %#v, want stored session-shaped response", got)
	}
	listed := callMethod[SessionListResponse](t, registry, "session.list", SessionListRequest{WorkspaceID: "ws-1"})
	if len(listed.Sessions) != 1 || listed.Sessions[0].SessionID != "executor-main" || listed.Sessions[0].Executor != "test-harness" {
		t.Fatalf("session.list = %#v, want started workspace session", listed)
	}
}

func TestSessionStartFromProfilePersistsDurableRunLogAndStatusProjection(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setHarnessFactoryForTest("test-harness", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return newFakeHarness("test-harness", []TimelineItem{{Kind: "agent_text", Status: "completed", Text: "profile session completed"}}), nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
	created := callMethod[AgentProfileResponse](t, registry, "profile.create", AgentProfileCreateRequest{WorkspaceID: "ws-1", Name: "executor", Harness: "test-harness", Model: "model-1", Prompt: "profile behavior", InvocationClass: HarnessInvocationAgent})

	run := callMethod[AgentSessionStartResponse](t, registry, "session.start", AgentSessionStartRequest{WorkspaceID: "ws-1", Profile: "executor", SessionID: "executor-main", Message: "Start phase 1"})
	if run.Run.AgentSessionID != "executor-main" || run.Run.Status != "completed" {
		t.Fatalf("run = %#v, want completed stable Ari session", run.Run)
	}
	tail := callMethod[RunLogMessagesTailResponse](t, registry, "run.messages.tail", RunLogMessagesTailRequest{SessionID: "executor-main", Count: 1})
	if len(tail.Messages) != 1 || tail.Messages[0].SessionID != "executor-main" || len(tail.Messages[0].Parts) != 1 || tail.Messages[0].Parts[0].Text != "profile session completed" {
		t.Fatalf("tail = %#v, want durable normalized run message for profile start", tail)
	}
	status := callMethod[WorkspaceStatusResponse](t, registry, "workspace.status", WorkspaceStatusRequest{WorkspaceID: "ws-1"})
	if len(status.Sessions) != 1 || status.Sessions[0].ID != "executor-main" || status.Sessions[0].Status != "completed" || status.Sessions[0].Executor != "test-harness" {
		t.Fatalf("status sessions = %#v, want durable profile session projection", status.Sessions)
	}
	stored, err := store.GetAgentSession(context.Background(), "executor-main")
	if err != nil {
		t.Fatalf("GetAgentSession returned error: %v", err)
	}
	if stored.AgentID != created.ProfileID || stored.Usage != "sticky" {
		t.Fatalf("stored session = %#v, want sticky profile-backed session", stored)
	}
}

func TestSessionStartAttachesExistingStoredProfileSessionBeforeHarnessStart(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	startInvoked := false
	d.setHarnessFactoryForTest("test-harness", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		startInvoked = true
		return newFakeHarness("test-harness", []TimelineItem{{Kind: "agent_text", Text: "new work"}}), nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
	created := callMethod[AgentProfileResponse](t, registry, "profile.create", AgentProfileCreateRequest{WorkspaceID: "ws-1", Name: "executor", Harness: "test-harness", Model: "model-1", Prompt: "profile behavior", InvocationClass: HarnessInvocationAgent})
	if err := store.EnsureAgentSessionConfig(context.Background(), globaldb.AgentSessionConfig{AgentID: created.ProfileID, WorkspaceID: "ws-1", Name: created.Name, Harness: created.Harness, Model: created.Model, Prompt: created.Prompt}); err != nil {
		t.Fatalf("EnsureAgentSessionConfig returned error: %v", err)
	}
	if err := store.CreateAgentSession(context.Background(), globaldb.AgentSession{SessionID: "executor-main", WorkspaceID: "ws-1", AgentID: created.ProfileID, Harness: "test-harness", Model: "model-1", ProviderSessionID: "provider-existing", Status: "waiting", Usage: "sticky"}); err != nil {
		t.Fatalf("CreateAgentSession returned error: %v", err)
	}

	run := callMethod[AgentSessionStartResponse](t, registry, "session.start", AgentSessionStartRequest{WorkspaceID: "ws-1", Profile: created.Name, SessionID: "executor-main", Message: "Do not launch a duplicate"})

	if startInvoked {
		t.Fatal("session.start invoked harness for existing sticky session; want attach before side effects")
	}
	if run.Run.SessionID != "executor-main" || run.Run.ProviderSessionID != "provider-existing" || run.Run.Status != "waiting" {
		t.Fatalf("attached run = %#v, want existing stored session-shaped response", run.Run)
	}
	listed := callMethod[SessionListResponse](t, registry, "session.list", SessionListRequest{WorkspaceID: "ws-1"})
	if len(listed.Sessions) != 1 || listed.Sessions[0].SessionID != "executor-main" {
		t.Fatalf("session.list = %#v, want one existing session without duplicate", listed)
	}
}

func TestSessionStartRejectsExistingSessionFromDifferentWorkspace(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
	seedSessionWithPrimaryFolder(t, store, "ws-2", t.TempDir())
	_ = callMethod[AgentProfileResponse](t, registry, "profile.create", AgentProfileCreateRequest{WorkspaceID: "ws-1", Name: "executor", Harness: "test-harness", Model: "model-1", Prompt: "profile behavior", InvocationClass: HarnessInvocationAgent})
	created := callMethod[AgentProfileResponse](t, registry, "profile.create", AgentProfileCreateRequest{WorkspaceID: "ws-2", Name: "executor", Harness: "test-harness", Model: "model-1", Prompt: "profile behavior", InvocationClass: HarnessInvocationAgent})
	if err := store.EnsureAgentSessionConfig(context.Background(), globaldb.AgentSessionConfig{AgentID: created.ProfileID, WorkspaceID: "ws-2", Name: created.Name, Harness: created.Harness, Model: created.Model, Prompt: created.Prompt}); err != nil {
		t.Fatalf("EnsureAgentSessionConfig returned error: %v", err)
	}
	if err := store.CreateAgentSession(context.Background(), globaldb.AgentSession{SessionID: "executor-main", WorkspaceID: "ws-2", AgentID: created.ProfileID, Harness: "test-harness", Model: "model-1", ProviderSessionID: "provider-existing", Status: "waiting", Usage: "sticky"}); err != nil {
		t.Fatalf("CreateAgentSession returned error: %v", err)
	}

	err := callMethodError(registry, "session.start", AgentSessionStartRequest{WorkspaceID: "ws-1", Profile: "executor", SessionID: "executor-main", Message: "Do not attach cross-workspace session"})
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "session_workspace_mismatch" || data["session_id"] != "executor-main" || data["workspace_id"] != "ws-1" || data["existing_workspace_id"] != "ws-2" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want cross-workspace rejection details", data)
	}
}

func TestSessionStartRejectsExistingSessionFromDifferentProfile(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
	planner := callMethod[AgentProfileResponse](t, registry, "profile.create", AgentProfileCreateRequest{WorkspaceID: "ws-1", Name: "planner", Harness: "test-harness", Model: "model-1", Prompt: "plan", InvocationClass: HarnessInvocationAgent})
	_ = callMethod[AgentProfileResponse](t, registry, "profile.create", AgentProfileCreateRequest{WorkspaceID: "ws-1", Name: "executor", Harness: "test-harness", Model: "model-1", Prompt: "execute", InvocationClass: HarnessInvocationAgent})
	if err := store.EnsureAgentSessionConfig(context.Background(), globaldb.AgentSessionConfig{AgentID: planner.ProfileID, WorkspaceID: "ws-1", Name: planner.Name, Harness: planner.Harness, Model: planner.Model, Prompt: planner.Prompt}); err != nil {
		t.Fatalf("EnsureAgentSessionConfig returned error: %v", err)
	}
	if err := store.CreateAgentSession(context.Background(), globaldb.AgentSession{SessionID: "shared-session", WorkspaceID: "ws-1", AgentID: planner.ProfileID, Harness: "test-harness", Model: "model-1", ProviderSessionID: "provider-existing", Status: "waiting", Usage: "sticky"}); err != nil {
		t.Fatalf("CreateAgentSession returned error: %v", err)
	}

	err := callMethodError(registry, "session.start", AgentSessionStartRequest{WorkspaceID: "ws-1", Profile: "executor", SessionID: "shared-session", Message: "Do not attach wrong profile"})
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "session_profile_mismatch" || data["session_id"] != "shared-session" || data["profile"] != "executor" || data["existing_profile"] != "planner" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want profile mismatch rejection details", data)
	}
}

func TestSessionGetAndListPreservePersistedSessionLinkageFields(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
	created := callMethod[AgentProfileResponse](t, registry, "profile.create", AgentProfileCreateRequest{WorkspaceID: "ws-1", Name: "executor", Harness: "test-harness", Model: "model-1", Prompt: "profile behavior", InvocationClass: HarnessInvocationAgent})
	if err := store.EnsureAgentSessionConfig(context.Background(), globaldb.AgentSessionConfig{AgentID: created.ProfileID, WorkspaceID: "ws-1", Name: created.Name, Harness: created.Harness, Model: created.Model, Prompt: created.Prompt}); err != nil {
		t.Fatalf("EnsureAgentSessionConfig returned error: %v", err)
	}
	if err := store.CreateAgentSession(context.Background(), globaldb.AgentSession{SessionID: "worker-1", WorkspaceID: "ws-1", AgentID: created.ProfileID, Harness: "test-harness", Model: "model-1", Status: "running", Usage: globaldb.AgentSessionUsageEphemeral, SourceSessionID: "planner-1", SourceAgentID: created.ProfileID}); err != nil {
		t.Fatalf("CreateAgentSession returned error: %v", err)
	}

	got := callMethod[SessionGetResponse](t, registry, "session.get", SessionGetRequest{SessionID: "worker-1"})
	if got.Session.Usage != globaldb.AgentSessionUsageEphemeral || got.Session.SourceSessionID != "planner-1" || got.Session.SourceAgentID != created.ProfileID {
		t.Fatalf("session.get = %#v, want persisted usage/source linkage", got.Session)
	}
	listed := callMethod[SessionListResponse](t, registry, "session.list", SessionListRequest{WorkspaceID: "ws-1"})
	if len(listed.Sessions) != 1 || listed.Sessions[0].Usage != globaldb.AgentSessionUsageEphemeral || listed.Sessions[0].SourceSessionID != "planner-1" || listed.Sessions[0].SourceAgentID != created.ProfileID {
		t.Fatalf("session.list = %#v, want persisted usage/source linkage", listed)
	}
}

func TestSessionGetDescriptionMatchesIDOnlyLookupContract(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	method, ok := registry.Get("session.get")
	if !ok {
		t.Fatal("session.get is not registered")
	}
	description := strings.ToLower(method.Description)
	if strings.Contains(description, "name") {
		t.Fatalf("session.get description = %q, want id-only wording because request contract requires session_id", method.Description)
	}
}
