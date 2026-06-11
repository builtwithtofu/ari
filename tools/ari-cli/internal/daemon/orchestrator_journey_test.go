package daemon

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

// TestOrchestratorJourneyFanoutPartialResultsAndCoherentProjections is the
// executable journey proof for workspace-event orchestration: a sticky
// orchestrator fans out to three workers across gated harnesses, observes a
// bounded wait-all timeout while workers run, receives worker A's result
// through its durable event subscription while B and C continue, and finally
// sees fanout status, inbox, workspace status, and timeline agree because
// they all derive from the same workspace event history.
func TestOrchestratorJourneyFanoutPartialResultsAndCoherentProjections(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)

	releases := map[string]chan struct{}{}
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	for _, worker := range []struct {
		profileID string
		harness   string
		answer    string
	}{
		{profileID: "journey-worker-a", harness: "journey-harness-a", answer: "answer-a"},
		{profileID: "journey-worker-b", harness: "journey-harness-b", answer: "answer-b"},
		{profileID: "journey-worker-c", harness: "journey-harness-c", answer: "answer-c"},
	} {
		if err := store.CreateHarnessSessionConfig(ctx, globaldb.HarnessSessionConfig{AgentID: worker.profileID, WorkspaceID: "ws-1", Name: worker.profileID, Harness: worker.harness, Model: "model-1", Prompt: worker.profileID}); err != nil {
			t.Fatalf("CreateHarnessSessionConfig %s returned error: %v", worker.profileID, err)
		}
		release := make(chan struct{})
		releases[worker.profileID] = release
		harness := worker.harness
		answer := worker.answer
		d.setHarnessFactoryForTest(harness, func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
			_ = req
			_ = primaryFolder
			_ = sink
			return &blockingItemsHarness{name: harness, started: make(chan struct{}), release: release, items: []TimelineItem{{Kind: "agent_text", Text: answer}}}, nil
		})
	}
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	scope := AriToolScope{SourceRunID: "run-1", WorkspaceID: "ws-1", ProfileID: "agent-1", ProfileName: "executor", WithinDefaultScope: true}

	// The orchestrator subscribes to worker lifecycle facts before fanning out.
	_ = callMethod[WorkspaceEventSubscriptionResponse](t, registry, "workspace.events.subscribe", WorkspaceEventSubscribeRequest{SubscriptionID: "sub-journey", WorkspaceID: "ws-1", OwnerSessionID: "run-1", FilterJSON: `{"event_types":["worker.started","worker.completed","worker.failed","worker.stopped"]}`})

	// Fan out with a bounded wait-all: every worker is gated, so the wait
	// times out without cancelling or failing anyone.
	fanout := callMethod[AriToolCallResponse](t, registry, "ari.tool.call", AriToolCallRequest{Name: "ari.session.fanout", Scope: scope, Input: map[string]any{"target_profile_ids": []string{"journey-worker-a", "journey-worker-b", "journey-worker-c"}, "body": "gather evidence", "fanout_group_id": "fg-journey-proof", "wait": map[string]any{"mode": "all", "timeout_ms": 25}}})
	if fanout.Status != "ok" || fanout.Output["wait_timed_out"] != true || fanout.Output["wait_status"] != "partial" {
		t.Fatalf("fanout response = %#v, want timed-out partial wait-all", fanout.Output)
	}
	members := fanoutToolMembersForTest(t, fanout)
	if len(members) != 3 {
		t.Fatalf("fanout members = %#v, want three workers", members)
	}
	workerSessions := map[string]string{}
	for profileID, member := range members {
		if member["status"] != "running" {
			t.Fatalf("member %q = %#v, want running after wait timeout", profileID, member)
		}
		workerSessions[profileID], _ = member["worker_session_id"].(string)
	}

	// The subscription already carries the three started facts; ack them so
	// the next read isolates terminal results.
	started := waitForAriSubscriptionEvents(t, registry, scope, "sub-journey", "worker.started", 3)
	ackAriSubscriptionThrough(t, registry, scope, "sub-journey", started)

	// Release worker A only: the orchestrator receives one result while B and
	// C keep running (partial results story).
	close(releases["journey-worker-a"])
	completedA := waitForAriSubscriptionEvents(t, registry, scope, "sub-journey", "worker.completed", 1)
	eventA := completedA[0]
	if eventA["subject_id"] != workerSessions["journey-worker-a"] {
		t.Fatalf("first completed event = %#v, want worker A session %q", eventA, workerSessions["journey-worker-a"])
	}
	refJSON, _ := eventA["payload_ref_json"].(string)
	refA := payloadRefForTest(t, refJSON)
	finalResponseIDA := refA["id"]
	if refA["kind"] != "final_response" || finalResponseIDA == "" {
		t.Fatalf("completed event payload_ref = %#v, want final_response link", refA)
	}
	finalA := callMethod[FinalResponseResponse](t, registry, "final_response.get", FinalResponseGetRequest{SessionID: workerSessions["journey-worker-a"]})
	if finalA.FinalResponseID != finalResponseIDA || finalA.Text != "answer-a" || finalA.Status != "completed" {
		t.Fatalf("final response A = %#v, want event ref %q resolving to answer-a", finalA, finalResponseIDA)
	}
	count := callMethod[AriToolCallResponse](t, registry, "ari.tool.call", AriToolCallRequest{Name: "ari.inbox.count", Scope: scope, Input: map[string]any{"source_session_id": "run-1"}})
	if count.Output["unread_count"] != 1 {
		t.Fatalf("inbox count after first result = %#v, want one unread worker result", count.Output)
	}
	status := callMethod[AriToolCallResponse](t, registry, "ari.tool.call", AriToolCallRequest{Name: "ari.fanout.status", Scope: scope, Input: map[string]any{"fanout_group_id": "fg-journey-proof", "source_session_id": "run-1"}})
	if status.Output["status"] != "partial" {
		t.Fatalf("fanout status after first result = %#v, want partial", status.Output)
	}
	statusMembers := fanoutToolMembersForTest(t, status)
	if statusMembers["journey-worker-a"]["status"] != "completed" || statusMembers["journey-worker-b"]["status"] != "running" || statusMembers["journey-worker-c"]["status"] != "running" {
		t.Fatalf("fanout members after first result = %#v, want only worker A completed", statusMembers)
	}

	// Acknowledge worker A's result and mark its inbox item read.
	ackAriSubscriptionThrough(t, registry, scope, "sub-journey", completedA)
	inbox := callMethod[AriToolCallResponse](t, registry, "ari.tool.call", AriToolCallRequest{Name: "ari.inbox.list", Scope: scope, Input: map[string]any{"source_session_id": "run-1", "unread_only": true}})
	inboxItems, _ := inbox.Output["items"].([]map[string]any)
	if len(inboxItems) != 1 {
		t.Fatalf("unread inbox = %#v, want worker A item", inbox.Output["items"])
	}
	itemID, _ := inboxItems[0]["inbox_item_id"].(string)
	marked := callMethod[AriToolCallResponse](t, registry, "ari.tool.call", AriToolCallRequest{Name: "ari.inbox.mark_read", Scope: scope, Input: map[string]any{"source_session_id": "run-1", "inbox_item_ids": []string{itemID}}})
	if marked.Output["marked_count"] != 1 {
		t.Fatalf("mark read = %#v, want one marked item", marked.Output)
	}

	// Release the rest and observe the remaining terminal facts.
	close(releases["journey-worker-b"])
	close(releases["journey-worker-c"])
	remaining := waitForAriSubscriptionEvents(t, registry, scope, "sub-journey", "worker.completed", 2)
	ackAriSubscriptionThrough(t, registry, scope, "sub-journey", remaining)

	// Coherence: fanout status, inbox, workspace status, and timeline agree
	// on member status and final-response evidence from one event history.
	status = callMethod[AriToolCallResponse](t, registry, "ari.tool.call", AriToolCallRequest{Name: "ari.fanout.status", Scope: scope, Input: map[string]any{"fanout_group_id": "fg-journey-proof", "source_session_id": "run-1"}})
	if status.Output["status"] != "completed" {
		t.Fatalf("final fanout status = %#v, want completed", status.Output)
	}
	statusMembers = fanoutToolMembersForTest(t, status)
	finalResponseIDs := map[string]string{}
	for profileID, member := range statusMembers {
		if member["status"] != "completed" || member["final_response_id"] == "" {
			t.Fatalf("final member %q = %#v, want completed with final response evidence", profileID, member)
		}
		finalResponseIDs[profileID], _ = member["final_response_id"].(string)
	}
	workspaceStatus := waitForProjectedFanoutMemberStatuses(t, registry, "ws-1", map[string]string{"journey-worker-a": "completed", "journey-worker-b": "completed", "journey-worker-c": "completed"})
	for _, member := range workspaceStatus.FanoutMembers {
		if member.FinalResponseID != finalResponseIDs[member.TargetProfileID] {
			t.Fatalf("workspace status member = %#v, want final response %q from fanout status", member, finalResponseIDs[member.TargetProfileID])
		}
	}
	timeline := callMethod[WorkspaceTimelineResponse](t, registry, "workspace.timeline", WorkspaceTimelineRequest{WorkspaceID: "ws-1"})
	timelineMembers := 0
	for _, item := range timeline.Items {
		if item.Kind != "fanout_member" {
			continue
		}
		timelineMembers++
		profileID, _ := item.Metadata["target_profile_id"].(string)
		if item.Status != "completed" || item.Metadata["final_response_id"] != finalResponseIDs[profileID] {
			t.Fatalf("timeline fanout item = %#v, want completed with final response %q", item, finalResponseIDs[profileID])
		}
	}
	if timelineMembers != 3 {
		t.Fatalf("timeline fanout member items = %d, want 3", timelineMembers)
	}

	// Every inbox item must resolve to a real workspace event of its type, and
	// the read state from the orchestrator's actions must be preserved.
	allInbox := callMethod[AriToolCallResponse](t, registry, "ari.tool.call", AriToolCallRequest{Name: "ari.inbox.list", Scope: scope, Input: map[string]any{"source_session_id": "run-1"}})
	allItems, _ := allInbox.Output["items"].([]map[string]any)
	if len(allItems) != 3 {
		t.Fatalf("inbox items = %#v, want one per worker", allInbox.Output["items"])
	}
	events, err := store.ListWorkspaceEventsAfterSequence(ctx, "ws-1", 0, 500)
	if err != nil {
		t.Fatalf("ListWorkspaceEventsAfterSequence returned error: %v", err)
	}
	eventsByID := map[string]globaldb.WorkspaceEvent{}
	for _, event := range events {
		eventsByID[event.EventID] = event
	}
	readCount := 0
	for _, item := range allItems {
		eventID, _ := item["workspace_event_id"].(string)
		event, ok := eventsByID[eventID]
		if !ok || event.EventType != item["event_type"] {
			t.Fatalf("inbox item %#v does not resolve to a workspace event of its type", item)
		}
		if item["status"] == "read" {
			readCount++
		}
	}
	if readCount != 1 {
		t.Fatalf("read inbox items = %d, want worker A's read state preserved", readCount)
	}

	// Rebuild proof: replaying event history reproduces the materialized
	// fanout member projection.
	materialized, err := store.ListFanoutMembers(ctx, "fg-journey-proof")
	if err != nil {
		t.Fatalf("ListFanoutMembers returned error: %v", err)
	}
	rebuilt, err := fanoutMembersFromWorkspaceEvents(ctx, store, "ws-1", "fg-journey-proof")
	if err != nil {
		t.Fatalf("fanoutMembersFromWorkspaceEvents returned error: %v", err)
	}
	if len(rebuilt) != len(materialized) || len(rebuilt) != 3 {
		t.Fatalf("rebuilt members = %#v, want replay to reproduce %#v", rebuilt, materialized)
	}
	rebuiltByID := map[string]globaldb.FanoutMember{}
	for _, member := range rebuilt {
		rebuiltByID[member.FanoutMemberID] = member
	}
	for _, member := range materialized {
		replayed, ok := rebuiltByID[member.FanoutMemberID]
		if !ok || replayed.Status != member.Status || replayed.WorkerSessionID != member.WorkerSessionID || replayed.FinalResponseID != member.FinalResponseID {
			t.Fatalf("replayed member %#v, want to match materialized %#v", replayed, member)
		}
	}
}

// TestOrchestratorWakesOnWatchedSessionIdle proves the wake-when-idle
// composition end to end on durable machinery: an orchestrator subscribes to
// a watched session's idle/needs-input facts and blocks on the tool's
// server-side bounded wait; when the gated session finishes its turn, the
// session.idle event releases the orchestrator. No client-side polling.
func TestOrchestratorWakesOnWatchedSessionIdle(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.AddFolder(ctx, "ws-1", t.TempDir(), "git", true); err != nil {
		t.Fatalf("AddFolder returned error: %v", err)
	}
	release := make(chan struct{})
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	d.setHarnessFactoryForTest("wake-idle-harness", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return &blockingItemsHarness{name: "wake-idle-harness", started: make(chan struct{}), release: release, items: []TimelineItem{{Kind: "agent_text", Text: "watched result"}}}, nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	scope := AriToolScope{SourceRunID: "run-1", WorkspaceID: "ws-1", ProfileID: "agent-1", ProfileName: "executor", WithinDefaultScope: true}
	_ = callMethod[WorkspaceEventSubscriptionResponse](t, registry, "workspace.events.subscribe", WorkspaceEventSubscribeRequest{SubscriptionID: "sub-wake-idle", WorkspaceID: "ws-1", OwnerSessionID: "run-1", FilterJSON: `{"event_types":["session.idle","session.needs_input"],"subject_ids":["watched-run"]}`})

	sessionDone := make(chan error, 1)
	go func() {
		_, err := callMethodResult[HarnessSessionStartResponse](registry, "session.start", HarnessSessionStartRequest{Executor: "wake-idle-harness", SessionID: "watched-run", Packet: ContextPacket{ID: "ctx-watched-run", WorkspaceID: "ws-1", TaskID: "watched-run", Sections: []ContextSection{{Name: "message", Content: "long task"}}}})
		sessionDone <- err
	}()

	type wakeResult struct {
		response AriToolCallResponse
		err      error
	}
	wokeC := make(chan wakeResult, 1)
	waitStarted := make(chan struct{})
	go func() {
		close(waitStarted)
		response, err := callMethodResult[AriToolCallResponse](registry, "ari.tool.call", AriToolCallRequest{Name: "ari.workspace.events.next", Scope: scope, Input: map[string]any{"subscription_id": "sub-wake-idle", "min_events": 1, "timeout_ms": 5000}})
		wokeC <- wakeResult{response: response, err: err}
	}()
	<-waitStarted
	close(release)

	select {
	case woke := <-wokeC:
		if woke.err != nil {
			t.Fatalf("wake wait returned error: %v", woke.err)
		}
		output := woke.response.Output
		events, _ := output["events"].([]map[string]any)
		if output["wait_status"] != "ready" || output["wait_timed_out"] != false || len(events) == 0 {
			t.Fatalf("wake output = %#v, want ready with the idle event", output)
		}
		if events[0]["event_type"] != "session.idle" || events[0]["subject_id"] != "watched-run" {
			t.Fatalf("wake event = %#v, want session.idle for watched-run", events[0])
		}
	case <-time.After(6 * time.Second):
		t.Fatal("orchestrator was not woken by the watched session going idle")
	}
	if err := <-sessionDone; err != nil {
		t.Fatalf("watched session.start returned error: %v", err)
	}
}

// waitForAriSubscriptionEvents blocks on the tool's server-side bounded wait
// until the unacked window holds want events of eventType. The server cannot
// filter by type inside one subscription, so the required total (min_events)
// grows by one whenever a full window still lacks enough matches; all blocking
// happens daemon-side, never as client sleeps. Reads do not advance ack state,
// so repeated calls are deterministic against release-gated workers.
func waitForAriSubscriptionEvents(t *testing.T, registry *rpc.MethodRegistry, scope AriToolScope, subscriptionID, eventType string, want int) []map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	minEvents := want
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			t.Fatalf("subscription %q did not observe %d %s events", subscriptionID, want, eventType)
		}
		next := callMethod[AriToolCallResponse](t, registry, "ari.tool.call", AriToolCallRequest{Name: "ari.workspace.events.next", Scope: scope, Input: map[string]any{"subscription_id": subscriptionID, "limit": 50, "min_events": minEvents, "timeout_ms": int(remaining.Milliseconds())}})
		events, _ := next.Output["events"].([]map[string]any)
		matched := make([]map[string]any, 0, want)
		for _, event := range events {
			if event["event_type"] == eventType {
				matched = append(matched, event)
			}
		}
		if len(matched) >= want {
			return matched
		}
		if next.Output["wait_timed_out"] == true {
			t.Fatalf("subscription %q timed out with %d/%d %s events", subscriptionID, len(matched), want, eventType)
		}
		minEvents = len(events) + 1
	}
}

func ackAriSubscriptionThrough(t *testing.T, registry *rpc.MethodRegistry, scope AriToolScope, subscriptionID string, events []map[string]any) {
	t.Helper()
	var maxSequence int64
	for _, event := range events {
		if sequence, ok := event["sequence"].(int64); ok && sequence > maxSequence {
			maxSequence = sequence
		}
	}
	if maxSequence == 0 {
		t.Fatalf("events %#v carry no sequence to ack", events)
	}
	ack := callMethod[AriToolCallResponse](t, registry, "ari.tool.call", AriToolCallRequest{Name: "ari.workspace.events.ack", Scope: scope, Input: map[string]any{"subscription_id": subscriptionID, "sequence": maxSequence}})
	if ack.Output["acked"] != true {
		t.Fatalf("ack response = %#v, want acked", ack.Output)
	}
}
