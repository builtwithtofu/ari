package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

func TestJourneyInitToProjectStateIsDaemonBacked(t *testing.T) {
	stubBootstrap(t)
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	configPath := filepath.Join(t.TempDir(), "config.json")
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", configPath, "defaults", "test-version")
	harness := newJourneyHarnessFactory(t, "codex", []TimelineItem{{Kind: "agent_text", Status: "completed", Text: "helper ready"}})
	harness.register(d)
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	homeRoot := filepath.Join(t.TempDir(), "home-root")
	initResp := callMethod[InitApplyResponse](t, registry, "init.apply", InitApplyRequest{Harness: "codex", Model: "gpt-5.5", Root: homeRoot})
	if !initResp.HomeWorkspaceReady || !initResp.HomeHelperReady || initResp.DefaultRoot != homeRoot {
		t.Fatalf("init response = %#v, want home workspace/helper/defaults", initResp)
	}
	active := callMethod[ContextGetResponse](t, registry, "context.get", ContextGetRequest{})
	if active.Current.WorkspaceID == "" {
		t.Fatalf("active context after init = %#v, want home workspace", active.Current)
	}
	homeStatus := callMethod[WorkspaceStatusResponse](t, registry, "workspace.status", WorkspaceStatusRequest{WorkspaceID: active.Current.WorkspaceID})
	if homeStatus.WorkspaceName != "home" || len(homeStatus.Sessions) == 0 || len(homeStatus.RecentOperations) == 0 {
		t.Fatalf("home status = %#v, want home helper/session/audit projection", homeStatus)
	}
	homeTimeline := callMethod[WorkspaceTimelineResponse](t, registry, "workspace.timeline", WorkspaceTimelineRequest{WorkspaceID: active.Current.WorkspaceID})
	if len(homeTimeline.Items) == 0 || homeTimeline.Items[0].SourceKind != "operation" {
		t.Fatalf("home timeline = %#v, want operation-backed timeline", homeTimeline)
	}

	repoRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatalf("create git marker: %v", err)
	}
	setup := callMethod[WorkspaceSetupExistingResponse](t, registry, "workspace.setup_existing", WorkspaceSetupExistingRequest{Name: "project", Folder: repoRoot})
	active = callMethod[ContextGetResponse](t, registry, "context.get", ContextGetRequest{})
	if active.Current.WorkspaceID != setup.WorkspaceID {
		t.Fatalf("active context after setup = %#v, want project %q", active.Current, setup.WorkspaceID)
	}
	projectStatus := callMethod[WorkspaceStatusResponse](t, registry, "workspace.status", WorkspaceStatusRequest{WorkspaceID: setup.WorkspaceID})
	if projectStatus.WorkspaceName != "project" || len(projectStatus.WorkspaceRoots) != 1 || projectStatus.WorkspaceRoots[0] != repoRoot || len(projectStatus.RecentOperations) == 0 {
		t.Fatalf("project status = %#v, want project roots and audit projection", projectStatus)
	}
	projectTimeline := callMethod[WorkspaceTimelineResponse](t, registry, "workspace.timeline", WorkspaceTimelineRequest{WorkspaceID: setup.WorkspaceID})
	if len(projectTimeline.Items) == 0 || projectTimeline.Items[0].Kind != "workspace_project_setup" {
		t.Fatalf("project timeline = %#v, want setup operation", projectTimeline)
	}
	records, err := store.ListOperationRecords(context.Background(), "")
	if err != nil {
		t.Fatalf("ListOperationRecords returned error: %v", err)
	}
	if len(records) < 4 {
		t.Fatalf("operation records = %#v, want init and project checkpoints/operations", records)
	}
}

func TestJourneyWorkspaceConnectResumeAndStickyReattach(t *testing.T) {
	j := newJourneyRuntime(t)
	harness := newJourneyHarnessFactory(t, "test-harness", []TimelineItem{{Kind: "agent_text", Status: "completed", Text: "started"}})
	harness.register(j.daemon)
	primaryFolder := t.TempDir()
	j.seedWorkspace("ws-1", primaryFolder, t.TempDir())
	profile := j.createProfile("ws-1", "executor", "test-harness")

	started := callMethod[HarnessSessionStartResponse](t, j.registry, "session.start", HarnessSessionStartRequest{WorkspaceID: "ws-1", Profile: profile.Name, SessionID: "executor-main", Message: "implement feature"})
	if started.Run.SessionID != "executor-main" || started.Run.WorkspaceID != "ws-1" {
		t.Fatalf("started run = %#v, want sticky executor-main in ws-1", started.Run)
	}
	harness.requireStarts(1)

	reattached := callMethod[HarnessSessionStartResponse](t, j.registry, "session.start", HarnessSessionStartRequest{WorkspaceID: "ws-1", Profile: profile.Name, SessionID: "executor-main", Message: "reattach"})
	if reattached.Run.SessionID != started.Run.SessionID || reattached.Run.ProviderSessionID != started.Run.ProviderSessionID {
		t.Fatalf("reattached run = %#v, want existing run %#v", reattached.Run, started.Run)
	}
	harness.requireStarts(1)

	status := callMethod[WorkspaceStatusResponse](t, j.registry, "workspace.status", WorkspaceStatusRequest{WorkspaceID: "ws-1"})
	if len(status.WorkspaceRoots) != 2 {
		t.Fatalf("workspace roots = %#v, want two connected folders", status.WorkspaceRoots)
	}
	if status.WorkspaceRoots[0] != primaryFolder {
		t.Fatalf("primary workspace root = %q, want original primary %q", status.WorkspaceRoots[0], primaryFolder)
	}
	requireStatusSession(t, status, "executor-main", "completed", "")
}

func TestJourneyConcurrentPlannerExecutorRemainVisible(t *testing.T) {
	j := newJourneyRuntime(t)
	j.seedWorkspace("ws-1", t.TempDir())
	j.createSessionConfig("planner", "ws-1", "planner", "test-harness")
	j.createSessionConfig("executor", "ws-1", "executor", "test-harness")
	j.createHarnessSession("planner-run", "ws-1", "planner", "test-harness", "waiting", globaldb.HarnessSessionUsageSticky)
	j.createHarnessSession("executor-run", "ws-1", "executor", "test-harness", "running", globaldb.HarnessSessionUsageSticky)
	j.daemon.recordExecutorRun(HarnessSession{HarnessSessionID: "planner-run", SessionID: "planner-run", WorkspaceID: "ws-1", Executor: "test-harness", Status: "waiting", Usage: globaldb.HarnessSessionUsageSticky, StartedAt: "2026-05-13T00:00:00Z"}, []TimelineItem{{ID: "planner-run:item-1", WorkspaceID: "ws-1", RunID: "planner-run", SourceKind: "harness_session", SourceID: "planner-run", Kind: "run_log_message", Status: "completed", Text: "planner message"}})
	j.daemon.recordExecutorRun(HarnessSession{HarnessSessionID: "executor-run", SessionID: "executor-run", WorkspaceID: "ws-1", Executor: "test-harness", Status: "running", Usage: globaldb.HarnessSessionUsageSticky, StartedAt: "2026-05-13T00:00:01Z"}, []TimelineItem{{ID: "executor-run:item-1", WorkspaceID: "ws-1", RunID: "executor-run", SourceKind: "harness_session", SourceID: "executor-run", Kind: "run_log_message", Status: "running", Text: "executor work"}})
	j.appendTextMessage("executor-run", "executor-msg-1", 1, "assistant", "implementation in progress")
	j.appendTextMessage("planner-run", "planner-msg-1", 1, "user", "add a user story for audit logging")

	status := callMethod[WorkspaceStatusResponse](t, j.registry, "workspace.status", WorkspaceStatusRequest{WorkspaceID: "ws-1"})
	requireStatusSession(t, status, "planner-run", "", "")
	requireStatusSession(t, status, "executor-run", "running", "")

	timeline := callMethod[WorkspaceTimelineResponse](t, j.registry, "workspace.timeline", WorkspaceTimelineRequest{WorkspaceID: "ws-1"})
	requireTimelineSession(t, timeline, "planner-run")
	requireTimelineSession(t, timeline, "executor-run")
}

func TestJourneyContextMovementUsesImmutableVisibleMessages(t *testing.T) {
	j := newJourneyRuntime(t)
	j.seedWorkspace("ws-1", t.TempDir())
	j.createSessionConfig("planner", "ws-1", "planner", "test-harness")
	j.createSessionConfig("executor", "ws-1", "executor", "test-harness")
	j.createHarnessSession("planner-run", "ws-1", "planner", "test-harness", "waiting", globaldb.HarnessSessionUsageSticky)
	j.createHarnessSession("executor-run", "ws-1", "executor", "test-harness", "waiting", globaldb.HarnessSessionUsageSticky)
	j.appendTextMessage("planner-run", "planner-msg-1", 1, "assistant", "first plan")
	j.appendTextMessage("planner-run", "planner-msg-2", 2, "assistant", "second plan")

	excerpt, err := j.store.CreateContextExcerptFromTail(j.ctx, globaldb.CreateContextExcerptFromTailParams{ContextExcerptID: "excerpt-1", SourceSessionID: "planner-run", TargetAgentID: "executor", Count: 2, AppendedMessage: "use this plan"})
	if err != nil {
		t.Fatalf("CreateContextExcerptFromTail returned error: %v", err)
	}
	j.appendTextMessage("planner-run", "planner-msg-3", 3, "assistant", "late update")

	storedExcerpt, err := j.store.GetContextExcerpt(j.ctx, excerpt.ContextExcerptID)
	if err != nil {
		t.Fatalf("GetContextExcerpt returned error: %v", err)
	}
	if len(storedExcerpt.Items) != 2 || storedExcerpt.Items[0].CopiedText != "first plan" || storedExcerpt.Items[1].CopiedText != "second plan" || len(storedExcerpt.Items[1].CopiedParts) != 1 {
		t.Fatalf("stored excerpt = %#v, want immutable copied first two messages and parts", storedExcerpt)
	}

	msg := callMethod[AgentMessageSendResponse](t, j.registry, "session.message.send", AgentMessageSendRequest{AgentMessageID: "handoff-1", SourceSessionID: "planner-run", TargetSessionID: "executor-run", Body: "please implement", ContextExcerptIDs: []string{excerpt.ContextExcerptID}})
	if msg.AgentMessage.Status != "delivered" || msg.AgentMessage.TargetSessionID != "executor-run" {
		t.Fatalf("agent message = %#v, want delivered to executor-run", msg.AgentMessage)
	}
	tail, err := j.store.TailRunLogMessages(j.ctx, "executor-run", 4)
	if err != nil {
		t.Fatalf("TailRunLogMessages returned error: %v", err)
	}
	if len(tail) != 4 || tail[0].Parts[0].Text != "first plan" || tail[1].Parts[0].Text != "second plan" || tail[2].Parts[0].Text != "use this plan" || tail[3].Parts[0].Text != "please implement" {
		t.Fatalf("executor tail = %#v, want copied excerpt, appended message, visible handoff", tail)
	}
}

func TestJourneyBackgroundHelperWorkIsProjectedWithoutProviderHierarchy(t *testing.T) {
	j := newJourneyRuntime(t)
	j.seedWorkspace("ws-1", t.TempDir())
	j.createSessionConfig("planner", "ws-1", "planner", "test-harness")
	j.createHarnessSession("planner-run", "ws-1", "planner", "test-harness", "waiting", globaldb.HarnessSessionUsageSticky)
	j.appendTextMessage("planner-run", "planner-msg-1", 1, "assistant", "research auth options")
	j.createSessionConfig("reviewer", "ws-1", "reviewer", "test-harness")

	harness := newJourneyHarnessFactory(t, "test-harness", []TimelineItem{{Kind: "agent_text", Status: "completed", Text: "helper result"}})
	harness.register(j.daemon)
	excerpt, err := j.store.CreateContextExcerptFromTail(context.Background(), globaldb.CreateContextExcerptFromTailParams{ContextExcerptID: "excerpt-1", SourceSessionID: "planner-run", TargetAgentID: "reviewer", Count: 1})
	if err != nil {
		t.Fatalf("CreateContextExcerptFromTail returned error: %v", err)
	}
	call := callMethod[EphemeralCallResponse](t, j.registry, "session.call.ephemeral", EphemeralCallRequest{CallID: "call-1", SourceSessionID: "planner-run", TargetAgentID: "reviewer", Body: "research this", ContextExcerptIDs: []string{excerpt.ContextExcerptID}, ReplyAgentMessageID: "reply-1"})
	if call.Run.Usage != globaldb.HarnessSessionUsageEphemeral || call.Run.SourceSessionID != "planner-run" {
		t.Fatalf("ephemeral run = %#v, want helper linked to planner-run", call.Run)
	}
	waitForStoredHarnessSession(t, context.Background(), j.store, call.Run.SessionID, func(run globaldb.HarnessSession) bool { return run.Status == "completed" })
	harness.requireStarts(1)

	status := callMethod[WorkspaceStatusResponse](t, j.registry, "workspace.status", WorkspaceStatusRequest{WorkspaceID: "ws-1"})
	requireStatusSession(t, status, "planner-run", "waiting", globaldb.HarnessSessionUsageSticky)
	requireStatusSession(t, status, call.Run.SessionID, "completed", globaldb.HarnessSessionUsageEphemeral)

	timeline := callMethod[WorkspaceTimelineResponse](t, j.registry, "workspace.timeline", WorkspaceTimelineRequest{WorkspaceID: "ws-1"})
	requireTimelineSession(t, timeline, call.Run.SessionID)
	for _, item := range timeline.Items {
		if item.SourceKind == "subagent" || item.SourceKind == "opencode_subagent" {
			t.Fatalf("timeline item = %#v, want no provider-specific helper hierarchy", item)
		}
	}
}

func TestJourneyWorkspaceStatusAndTimelineAgreeOnSessionFacts(t *testing.T) {
	j := newJourneyRuntime(t)
	j.seedWorkspace("ws-1", t.TempDir())
	j.createSessionConfig("planner", "ws-1", "planner", "test-harness")
	j.createSessionConfig("reviewer", "ws-1", "reviewer", "test-harness")
	j.createHarnessSession("planner-run", "ws-1", "planner", "test-harness", "waiting", globaldb.HarnessSessionUsageSticky)
	j.createHarnessSession("reviewer-call-run", "ws-1", "reviewer", "test-harness", "running", globaldb.HarnessSessionUsageEphemeral)
	j.appendTextMessage("planner-run", "planner-msg-1", 1, "assistant", "please review")
	j.daemon.recordExecutorRun(HarnessSession{HarnessSessionID: "planner-run", SessionID: "planner-run", WorkspaceID: "ws-1", Executor: "test-harness", Status: "waiting", Usage: globaldb.HarnessSessionUsageSticky, StartedAt: "2026-05-13T00:00:00Z"}, []TimelineItem{{ID: "planner-run:item-1", WorkspaceID: "ws-1", RunID: "planner-run", SourceKind: "harness_session", SourceID: "planner-run", Kind: "run_log_message", Status: "completed", Text: "planner visible"}})
	j.daemon.recordExecutorRun(HarnessSession{HarnessSessionID: "reviewer-call-run", SessionID: "reviewer-call-run", WorkspaceID: "ws-1", Executor: "test-harness", Status: "running", Usage: globaldb.HarnessSessionUsageEphemeral, StartedAt: "2026-05-13T00:00:01Z"}, []TimelineItem{{ID: "reviewer-call-run:item-1", WorkspaceID: "ws-1", RunID: "reviewer-call-run", SourceKind: "harness_session", SourceID: "reviewer-call-run", Kind: "run_log_message", Status: "running", Text: "reviewer visible"}})

	status := callMethod[WorkspaceStatusResponse](t, j.registry, "workspace.status", WorkspaceStatusRequest{WorkspaceID: "ws-1"})
	timeline := callMethod[WorkspaceTimelineResponse](t, j.registry, "workspace.timeline", WorkspaceTimelineRequest{WorkspaceID: "ws-1"})

	statusSessions := map[string]SessionActivity{}
	for _, session := range status.Sessions {
		statusSessions[session.ID] = session
	}
	for _, sessionID := range []string{"planner-run", "reviewer-call-run"} {
		statusSession, ok := statusSessions[sessionID]
		if !ok {
			t.Fatalf("status sessions = %#v, missing %s", status.Sessions, sessionID)
		}
		item := requireTimelineSession(t, timeline, sessionID)
		if item.WorkspaceID != statusSession.WorkspaceID || item.Status == "" || statusSession.Status == "" {
			t.Fatalf("status session = %#v timeline item = %#v, want shared workspace and non-empty status facts", statusSession, item)
		}
	}
}
