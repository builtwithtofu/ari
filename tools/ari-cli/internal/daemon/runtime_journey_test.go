package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

func TestJourneyStickyFlowFanoutUsesFakeHarnessExecutableBoundary(t *testing.T) {
	j := newJourneyRuntime(t)
	primaryFolder := t.TempDir()
	j.seedWorkspace("ws-1", primaryFolder)
	j.createSessionConfig("planner", "ws-1", "planner", "claude")
	j.createHarnessSession("planner-run", "ws-1", "planner", "claude", "waiting", globaldb.HarnessSessionUsageSticky)
	j.createSessionConfig("researcher", "ws-1", "researcher", "claude")
	j.createSessionConfig("reviewer", "ws-1", "reviewer", "claude")
	fake := buildFakeHarnessExecutable(t)
	recordPath := filepath.Join(t.TempDir(), "fake-harness-record.jsonl")
	t.Setenv("ARI_FAKE_HARNESS", "claude")
	t.Setenv("ARI_FAKE_HARNESS_RECORD", recordPath)
	j.daemon.setHarnessFactoryForTest("claude", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (HarnessAdapter, error) {
		_ = req
		_ = sink
		return NewClaudeExecutorForTest(claudeExecutorOptions{Executable: fake, Cwd: primaryFolder, InvocationMode: HarnessInvocationModeHeadless}), nil
	})

	fanout := callMethod[AgentMessageSendResponse](t, j.registry, "session.fanout", AgentMessageSendRequest{FanoutGroupID: "fg-fake-exec", SourceSessionID: "planner-run", TargetProfileIDs: []string{"researcher", "reviewer"}, Body: "fan out through executable"})
	if fanout.FanoutGroupID != "fg-fake-exec" || len(fanout.FanoutMembers) != 2 {
		t.Fatalf("fanout = %#v, want durable executable-backed group and workers", fanout)
	}
	waitForFinalResponseContains(t, j.ctx, j.store, "fg-fake-exec-c"+stableRuntimeAgentIDSegment("researcher")+"-run", "fake claude response")
	waitForFinalResponseContains(t, j.ctx, j.store, "fg-fake-exec-c"+stableRuntimeAgentIDSegment("reviewer")+"-run", "fake claude response")
	status := waitForProjectedFanoutMemberStatuses(t, j.registry, "ws-1", map[string]string{"researcher": "completed", "reviewer": "completed"})
	assertInboxKinds(t, status.Inbox, map[string]string{"fg-fake-exec-mresearcher": "worker_completed", "fg-fake-exec-mreviewer": "worker_completed"})
	record, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("ReadFile fake-harness record returned error: %v", err)
	}
	if got := strings.Count(string(record), `"harness":"claude"`); got != 2 {
		t.Fatalf("fake-harness record = %s, want two claude process invocations", record)
	}
}

func TestJourneyMixedHarnessFanoutAcrossAdapters(t *testing.T) {
	j := newJourneyRuntime(t)
	primaryFolder := t.TempDir()
	j.seedWorkspace("ws-1", primaryFolder)
	j.createSessionConfig("planner", "ws-1", "planner", "claude")
	j.createHarnessSession("planner-run", "ws-1", "planner", "claude", "waiting", globaldb.HarnessSessionUsageSticky)
	j.createSessionConfig("researcher", "ws-1", "researcher", "claude")
	j.createSessionConfig("analyst", "ws-1", "analyst", "pi")
	j.createSessionConfig("reviewer", "ws-1", "reviewer", "grok")

	// One fake binary, persona selected per symlink basename so a single
	// fanout group can launch three different harnesses side by side.
	fake := buildFakeHarnessExecutable(t)
	linkDir := t.TempDir()
	links := map[string]string{}
	for _, harness := range []string{"claude", "pi", "grok"} {
		link := filepath.Join(linkDir, "fake-"+harness)
		if err := os.Symlink(fake, link); err != nil {
			t.Fatalf("symlink fake-%s: %v", harness, err)
		}
		links[harness] = link
	}
	recordPath := filepath.Join(t.TempDir(), "fanout-record.jsonl")
	t.Setenv("ARI_FAKE_HARNESS", "")
	t.Setenv("ARI_FAKE_HARNESS_RECORD", recordPath)
	j.daemon.setHarnessFactoryForTest("claude", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (HarnessAdapter, error) {
		_ = req
		_ = sink
		return NewClaudeExecutorForTest(claudeExecutorOptions{Executable: links["claude"], Cwd: primaryFolder, InvocationMode: HarnessInvocationModeHeadless}), nil
	})
	j.daemon.setHarnessFactoryForTest("pi", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (HarnessAdapter, error) {
		_ = req
		_ = sink
		return NewPiExecutorForTest(piExecutorOptions{Executable: links["pi"], Cwd: primaryFolder}), nil
	})
	j.daemon.setHarnessFactoryForTest("grok", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (HarnessAdapter, error) {
		_ = req
		_ = sink
		return NewGrokExecutorForTest(grokExecutorOptions{Executable: links["grok"], Cwd: primaryFolder}), nil
	})

	fanout := callMethod[AgentMessageSendResponse](t, j.registry, "session.fanout", AgentMessageSendRequest{FanoutGroupID: "fg-mixed", SourceSessionID: "planner-run", TargetProfileIDs: []string{"researcher", "analyst", "reviewer"}, Body: "fan out across harnesses"})
	if fanout.FanoutGroupID != "fg-mixed" || len(fanout.FanoutMembers) != 3 {
		t.Fatalf("fanout = %#v, want three mixed-harness workers", fanout)
	}
	waitForFinalResponseContains(t, j.ctx, j.store, "fg-mixed-c"+stableRuntimeAgentIDSegment("researcher")+"-run", "fake claude response")
	waitForFinalResponseContains(t, j.ctx, j.store, "fg-mixed-c"+stableRuntimeAgentIDSegment("analyst")+"-run", "fake pi response")
	waitForFinalResponseContains(t, j.ctx, j.store, "fg-mixed-c"+stableRuntimeAgentIDSegment("reviewer")+"-run", "fake grok response")
	status := waitForProjectedFanoutMemberStatuses(t, j.registry, "ws-1", map[string]string{"researcher": "completed", "analyst": "completed", "reviewer": "completed"})
	assertInboxKinds(t, status.Inbox, map[string]string{"fg-mixed-mresearcher": "worker_completed", "fg-mixed-manalyst": "worker_completed", "fg-mixed-mreviewer": "worker_completed"})
	record, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("ReadFile fanout record returned error: %v", err)
	}
	for _, harness := range []string{"claude", "pi", "grok"} {
		if !strings.Contains(string(record), `"harness":"`+harness+`"`) {
			t.Fatalf("fanout record missing %s invocation: %s", harness, record)
		}
	}
}

func TestJourneyFanoutWorkerRateLimitFailureSurfacesInInbox(t *testing.T) {
	j := newJourneyRuntime(t)
	primaryFolder := t.TempDir()
	j.seedWorkspace("ws-1", primaryFolder)
	j.createSessionConfig("planner", "ws-1", "planner", "claude")
	j.createHarnessSession("planner-run", "ws-1", "planner", "claude", "waiting", globaldb.HarnessSessionUsageSticky)
	j.createSessionConfig("researcher", "ws-1", "researcher", "claude")
	fake := buildFakeHarnessExecutable(t)
	t.Setenv("ARI_FAKE_HARNESS", "claude")
	t.Setenv("ARI_FAKE_HARNESS_MODE", "exit-rate-limit")
	j.daemon.setHarnessFactoryForTest("claude", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (HarnessAdapter, error) {
		_ = req
		_ = sink
		return NewClaudeExecutorForTest(claudeExecutorOptions{Executable: fake, Cwd: primaryFolder, InvocationMode: HarnessInvocationModeHeadless}), nil
	})

	fanout := callMethod[AgentMessageSendResponse](t, j.registry, "session.fanout", AgentMessageSendRequest{FanoutGroupID: "fg-ratelimit", SourceSessionID: "planner-run", TargetProfileIDs: []string{"researcher"}, Body: "fan out into a rate limit"})
	if fanout.FanoutGroupID != "fg-ratelimit" || len(fanout.FanoutMembers) != 1 {
		t.Fatalf("fanout = %#v, want one worker", fanout)
	}
	status := waitForProjectedFanoutMemberStatuses(t, j.registry, "ws-1", map[string]string{"researcher": "failed"})
	assertInboxKinds(t, status.Inbox, map[string]string{"fg-ratelimit-mresearcher": "worker_failed"})
}

func buildFakeHarnessExecutable(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-harness")
	cmd := exec.Command("go", "build", "-o", path, "./cmd/fake-harness")
	cmd.Dir = repoRootForTest(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fake-harness executable: %v\n%s", err, out)
	}
	return path
}

func repoRootForTest(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	for dir := cwd; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
	}
	t.Fatalf("could not find go.mod from %s", cwd)
	return ""
}

func TestJourneyStickyFlowFanoutSuspendResumeAndInbox(t *testing.T) {
	j := newJourneyRuntime(t)
	j.seedWorkspace("ws-1", t.TempDir())
	j.seedWorkspace("ws-2", t.TempDir())
	j.createSessionConfig("planner", "ws-1", "planner", "planner-harness")
	j.createHarnessSession("planner-run", "ws-1", "planner", "planner-harness", "waiting", globaldb.HarnessSessionUsageSticky)
	j.createSessionConfig("good-worker", "ws-1", "good", "good-harness")
	j.createSessionConfig("bad-worker", "ws-1", "bad", "bad-harness")
	j.createSessionConfig("slow-worker", "ws-1", "slow", "slow-harness")

	slowStarted := make(chan struct{})
	slowRelease := make(chan struct{})
	stopped := make(chan string, 1)
	j.daemon.setHarnessFactoryForTest("good-harness", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (HarnessAdapter, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return newFakeHarness("good-harness", []TimelineItem{{Kind: "agent_text", Text: "good result"}}), nil
	})
	j.daemon.setHarnessFactoryForTest("bad-harness", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (HarnessAdapter, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return itemsFailHarness{}, nil
	})
	j.daemon.setHarnessFactoryForTest("slow-harness", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (HarnessAdapter, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return &blockingItemsHarness{name: "slow-harness", providerSessionID: "provider-slow", started: slowStarted, release: slowRelease, stopped: stopped, items: []TimelineItem{{Kind: "agent_text", Text: "slow result"}}}, nil
	})

	fanout := callMethod[AgentMessageSendResponse](t, j.registry, "session.fanout", AgentMessageSendRequest{FanoutGroupID: "fg-journey", SourceSessionID: "planner-run", TargetProfileIDs: []string{"good-worker", "bad-worker", "slow-worker"}, Body: "fan out"})
	if fanout.FanoutGroupID != "fg-journey" || len(fanout.FanoutMembers) != 3 {
		t.Fatalf("fanout = %#v, want durable group and three workers", fanout)
	}
	select {
	case <-slowStarted:
	case <-time.After(time.Second):
		t.Fatal("slow worker did not start")
	}
	plannerMessages, err := j.store.ListRunLogMessages(j.ctx, "planner-run", 0, 100)
	if err != nil {
		t.Fatalf("ListRunLogMessages planner returned error: %v", err)
	}
	if len(plannerMessages) != 0 {
		t.Fatalf("planner run log = %#v, want no active-turn worker injection", plannerMessages)
	}
	waitForFinalResponseText(t, j.ctx, j.store, "fg-journey-c"+stableRuntimeAgentIDSegment("good-worker")+"-run", "good result")
	waitForFinalResponseContains(t, j.ctx, j.store, "fg-journey-c"+stableRuntimeAgentIDSegment("bad-worker")+"-run", "items failed")
	status := waitForProjectedFanoutMemberStatuses(t, j.registry, "ws-1", map[string]string{"good-worker": "completed", "bad-worker": "failed", "slow-worker": "running"})
	assertInboxKinds(t, status.Inbox, map[string]string{"fg-journey-mgood-worker": "worker_completed", "fg-journey-mbad-worker": "worker_failed"})

	_ = callMethod[WorkspaceStatusResponse](t, j.registry, "workspace.status", WorkspaceStatusRequest{WorkspaceID: "ws-2"})
	assertHarnessSessionStatusRemains(t, j.ctx, j.store, "fg-journey-c"+stableRuntimeAgentIDSegment("slow-worker")+"-run", "running", 75*time.Millisecond)
	suspended := callMethod[WorkspaceSuspendResponse](t, j.registry, "workspace.suspend", WorkspaceSuspendRequest{WorkspaceID: "ws-1"})
	if suspended.Status != "suspended" {
		t.Fatalf("suspend = %#v, want suspended", suspended)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("slow worker was not stopped on workspace suspend")
	}
	events, err := j.store.ListWorkspaceEventsAfterSequence(j.ctx, "ws-1", 0, 200)
	if err != nil {
		t.Fatalf("ListWorkspaceEventsAfterSequence returned error: %v", err)
	}
	assertSessionWorkspaceEvent(t, events, "fg-journey-c"+stableRuntimeAgentIDSegment("slow-worker")+"-run", workspaceEventSessionStopped, false)
	status = callMethod[WorkspaceStatusResponse](t, j.registry, "workspace.status", WorkspaceStatusRequest{WorkspaceID: "ws-1"})
	assertProjectedFanoutMemberStatuses(t, status.FanoutMembers, map[string]string{"slow-worker": "stopped"})
	assertInboxKinds(t, status.Inbox, map[string]string{"fg-journey-mslow-worker": "worker_stopped"})
	resumed := callMethod[WorkspaceResumeResponse](t, j.registry, "workspace.resume", WorkspaceResumeRequest{WorkspaceID: "ws-1"})
	if resumed.Status != "active" {
		t.Fatalf("resume = %#v, want active", resumed)
	}
	assertHarnessSessionStatusRemains(t, j.ctx, j.store, "fg-journey-c"+stableRuntimeAgentIDSegment("slow-worker")+"-run", "stopped", 75*time.Millisecond)
	close(slowRelease)
}

func TestJourneyStickyOrchestratorFanoutLoopThroughAriTools(t *testing.T) {
	j := newJourneyRuntime(t)
	j.seedWorkspace("ws-1", t.TempDir())
	j.createSessionConfig("planner", "ws-1", "planner", "planner-harness")
	j.createHarnessSession("planner-run", "ws-1", "planner", "planner-harness", "waiting", globaldb.HarnessSessionUsageSticky)
	j.createSessionConfig("good-worker", "ws-1", "good", "good-harness")
	j.createSessionConfig("bad-worker", "ws-1", "bad", "bad-harness")
	j.createSessionConfig("slow-worker", "ws-1", "slow", "slow-harness")

	slowStarted := make(chan struct{})
	slowRelease := make(chan struct{})
	stopped := make(chan string, 1)
	j.daemon.setHarnessFactoryForTest("good-harness", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (HarnessAdapter, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return newFakeHarness("good-harness", []TimelineItem{{Kind: "agent_text", Text: "good result"}}), nil
	})
	j.daemon.setHarnessFactoryForTest("bad-harness", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (HarnessAdapter, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return itemsFailHarness{}, nil
	})
	j.daemon.setHarnessFactoryForTest("slow-harness", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (HarnessAdapter, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return &blockingItemsHarness{name: "slow-harness", providerSessionID: "provider-slow", started: slowStarted, release: slowRelease, stopped: stopped, items: []TimelineItem{{Kind: "agent_text", Text: "slow result"}}}, nil
	})
	scope := AriToolScope{SourceRunID: "planner-run", WorkspaceID: "ws-1", ProfileID: "planner", ProfileName: "planner", WithinDefaultScope: true}

	fanout := callMethod[AriToolCallResponse](t, j.registry, "ari.tool.call", AriToolCallRequest{Name: "ari.session.fanout", Scope: scope, Input: map[string]any{"fanout_group_id": "fg-tool-journey", "target_profile_ids": []string{"good-worker", "bad-worker", "slow-worker"}, "body": "fan out", "wait": map[string]any{"mode": "all", "timeout_ms": 25}}})
	if fanout.Status != "ok" || fanout.Output["fanout_group_id"] != "fg-tool-journey" || fanout.Output["wait_status"] != "partial" || fanout.Output["wait_timed_out"] != true {
		t.Fatalf("tool fanout = %#v, want partial wait timeout without cancellation", fanout)
	}
	select {
	case <-slowStarted:
	case <-time.After(time.Second):
		t.Fatal("slow worker did not start")
	}
	assertFanoutToolMemberStatuses(t, fanout, map[string]string{"good-worker": "completed", "bad-worker": "failed", "slow-worker": "running"})
	plannerMessages, err := j.store.ListRunLogMessages(j.ctx, "planner-run", 0, 100)
	if err != nil {
		t.Fatalf("ListRunLogMessages planner returned error: %v", err)
	}
	if len(plannerMessages) != 0 {
		t.Fatalf("planner run log = %#v, want no active-turn worker injection", plannerMessages)
	}

	statusTool := callMethod[AriToolCallResponse](t, j.registry, "ari.tool.call", AriToolCallRequest{Name: "ari.fanout.status", Scope: scope, Input: map[string]any{"fanout_group_id": "fg-tool-journey"}})
	if statusTool.Status != "ok" || statusTool.Output["fanout_group_id"] != "fg-tool-journey" || statusTool.Output["source_session_id"] != "planner-run" || statusTool.Output["status"] != "partial" {
		t.Fatalf("status tool response = %#v, want stable partial fanout metadata", statusTool)
	}
	assertFanoutToolMemberStatuses(t, statusTool, map[string]string{"good-worker": "completed", "bad-worker": "failed", "slow-worker": "running"})
	inboxTool := callMethod[AriToolCallResponse](t, j.registry, "ari.tool.call", AriToolCallRequest{Name: "ari.inbox.list", Scope: scope, Input: map[string]any{"unread_only": true}})
	assertAriToolInboxKinds(t, inboxTool, map[string]string{"fg-tool-journey-mgood-worker": "worker_completed", "fg-tool-journey-mbad-worker": "worker_failed"})
	status := waitForProjectedFanoutMemberStatuses(t, j.registry, "ws-1", map[string]string{"good-worker": "completed", "bad-worker": "failed", "slow-worker": "running"})
	assertInboxKinds(t, status.Inbox, map[string]string{"fg-tool-journey-mgood-worker": "worker_completed", "fg-tool-journey-mbad-worker": "worker_failed"})

	suspended := callMethod[WorkspaceSuspendResponse](t, j.registry, "workspace.suspend", WorkspaceSuspendRequest{WorkspaceID: "ws-1"})
	if suspended.Status != "suspended" {
		t.Fatalf("suspend = %#v, want suspended", suspended)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("slow worker was not stopped on workspace suspend")
	}
	statusTool = callMethod[AriToolCallResponse](t, j.registry, "ari.tool.call", AriToolCallRequest{Name: "ari.fanout.status", Scope: scope, Input: map[string]any{"fanout_group_id": "fg-tool-journey"}})
	if statusTool.Output["status"] != "failed" {
		t.Fatalf("status tool response after suspend = %#v, want failed aggregate because one worker failed", statusTool)
	}
	assertFanoutToolMemberStatuses(t, statusTool, map[string]string{"good-worker": "completed", "bad-worker": "failed", "slow-worker": "stopped"})
	inboxTool = callMethod[AriToolCallResponse](t, j.registry, "ari.tool.call", AriToolCallRequest{Name: "ari.inbox.list", Scope: scope, Input: map[string]any{"unread_only": true}})
	assertAriToolInboxKinds(t, inboxTool, map[string]string{"fg-tool-journey-mgood-worker": "worker_completed", "fg-tool-journey-mbad-worker": "worker_failed", "fg-tool-journey-mslow-worker": "worker_stopped"})
	close(slowRelease)
}

func TestJourneyWorkspaceSuspendDoesNotFailContextCancelledFanoutWorker(t *testing.T) {
	j := newJourneyRuntime(t)
	j.seedWorkspace("ws-1", t.TempDir())
	j.createSessionConfig("planner", "ws-1", "planner", "planner-harness")
	j.createHarnessSession("planner-run", "ws-1", "planner", "planner-harness", "waiting", globaldb.HarnessSessionUsageSticky)
	j.createSessionConfig("slow-worker", "ws-1", "slow", "slow-harness")
	started := make(chan struct{})
	j.daemon.setHarnessFactoryForTest("slow-harness", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (HarnessAdapter, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return &contextCancelledItemsHarness{name: "slow-harness", providerSessionID: "provider-slow", started: started, store: j.store}, nil
	})

	_ = callMethod[AgentMessageSendResponse](t, j.registry, "session.fanout", AgentMessageSendRequest{FanoutGroupID: "fg-context-cancel", SourceSessionID: "planner-run", TargetProfileIDs: []string{"slow-worker"}, Body: "fan out"})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("context-cancelled worker did not start")
	}
	suspended := callMethod[WorkspaceSuspendResponse](t, j.registry, "workspace.suspend", WorkspaceSuspendRequest{WorkspaceID: "ws-1"})
	if suspended.Status != "suspended" {
		t.Fatalf("suspend = %#v, want suspended", suspended)
	}
	workerSessionID := "fg-context-cancel-c" + stableRuntimeAgentIDSegment("slow-worker") + "-run"
	assertHarnessSessionStatusEventually(t, j.ctx, j.store, workerSessionID, "stopped", time.Second)
	events, err := j.store.ListWorkspaceEventsAfterSequence(j.ctx, "ws-1", 0, 200)
	if err != nil {
		t.Fatalf("ListWorkspaceEventsAfterSequence returned error: %v", err)
	}
	if hasSessionWorkspaceEvent(events, workerSessionID, workspaceEventSessionFailed) {
		t.Fatalf("workspace events = %#v, want no failed event for intentionally stopped worker", events)
	}
	assertSessionWorkspaceEvent(t, events, workerSessionID, workspaceEventSessionStopped, false)
}

// assertSessionWorkspaceEvent asserts the session's terminal fact in
// workspace event history (subject_type harness_session).
func assertSessionWorkspaceEvent(t *testing.T, events []globaldb.WorkspaceEvent, sessionID, eventType string, attentionRequired bool) {
	t.Helper()
	for _, event := range events {
		if event.SubjectID != sessionID || event.EventType != eventType {
			continue
		}
		if event.SubjectType != workspaceEventSubjectHarnessSession || event.AttentionRequired != attentionRequired {
			t.Fatalf("workspace event for %s = %#v, want harness_session subject attention=%v", sessionID, event, attentionRequired)
		}
		return
	}
	t.Fatalf("workspace events = %#v, want %s event for session %s", events, eventType, sessionID)
}

func hasSessionWorkspaceEvent(events []globaldb.WorkspaceEvent, sessionID, eventType string) bool {
	for _, event := range events {
		if event.SubjectID == sessionID && event.EventType == eventType {
			return true
		}
	}
	return false
}

func assertHarnessSessionStatusEventually(t *testing.T, ctx context.Context, store *globaldb.Store, sessionID, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		run, err := store.GetHarnessSession(ctx, sessionID)
		if err != nil {
			t.Fatalf("GetHarnessSession(%q) returned error: %v", sessionID, err)
		}
		last = run.Status
		if last == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("session %q status = %q, want %q", sessionID, last, want)
}

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
	foundOperation := false
	for _, item := range homeTimeline.Items {
		if item.SourceKind == "operation" {
			foundOperation = true
			break
		}
	}
	if !foundOperation {
		t.Fatalf("home timeline = %#v, want operation event-backed timeline", homeTimeline)
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
	j.recordHarnessTimeline(HarnessSession{HarnessSessionID: "planner-run", SessionID: "planner-run", WorkspaceID: "ws-1", Executor: "test-harness", Status: "waiting", Usage: globaldb.HarnessSessionUsageSticky, StartedAt: "2026-05-13T00:00:00Z"}, []TimelineItem{{ID: "planner-run:item-1", WorkspaceID: "ws-1", RunID: "planner-run", SourceKind: "harness_session", SourceID: "planner-run", Kind: "run_log_message", Status: "completed", Text: "planner message"}})
	j.recordHarnessTimeline(HarnessSession{HarnessSessionID: "executor-run", SessionID: "executor-run", WorkspaceID: "ws-1", Executor: "test-harness", Status: "running", Usage: globaldb.HarnessSessionUsageSticky, StartedAt: "2026-05-13T00:00:01Z"}, []TimelineItem{{ID: "executor-run:item-1", WorkspaceID: "ws-1", RunID: "executor-run", SourceKind: "harness_session", SourceID: "executor-run", Kind: "run_log_message", Status: "running", Text: "executor work"}})
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
	j.recordHarnessTimeline(HarnessSession{HarnessSessionID: "planner-run", SessionID: "planner-run", WorkspaceID: "ws-1", Executor: "test-harness", Status: "waiting", Usage: globaldb.HarnessSessionUsageSticky, StartedAt: "2026-05-13T00:00:00Z"}, []TimelineItem{{ID: "planner-run:item-1", WorkspaceID: "ws-1", RunID: "planner-run", SourceKind: "harness_session", SourceID: "planner-run", Kind: "run_log_message", Status: "completed", Text: "planner visible"}})
	j.recordHarnessTimeline(HarnessSession{HarnessSessionID: "reviewer-call-run", SessionID: "reviewer-call-run", WorkspaceID: "ws-1", Executor: "test-harness", Status: "running", Usage: globaldb.HarnessSessionUsageEphemeral, StartedAt: "2026-05-13T00:00:01Z"}, []TimelineItem{{ID: "reviewer-call-run:item-1", WorkspaceID: "ws-1", RunID: "reviewer-call-run", SourceKind: "harness_session", SourceID: "reviewer-call-run", Kind: "run_log_message", Status: "running", Text: "reviewer visible"}})

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
