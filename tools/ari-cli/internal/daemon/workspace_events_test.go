package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

func TestWorkspaceEventRPCAppendSubscribeNextAndAck(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	if err := store.CreateWorkspace(context.Background(), "ws-1", "ws-1", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}

	started := appendWorkspaceEventForTest(t, store, globaldb.WorkspaceEvent{EventID: "we-started", WorkspaceID: "ws-1", EventType: "worker.started", SubjectType: "harness_session", SubjectID: "worker-1", ProducerType: "session", ProducerID: "orch-1", CorrelationID: "fanout-1", PayloadJSON: `{"status":"running"}`})
	completed := appendWorkspaceEventForTest(t, store, globaldb.WorkspaceEvent{EventID: "we-completed", WorkspaceID: "ws-1", EventType: "worker.completed", SubjectType: "harness_session", SubjectID: "worker-1", ProducerType: "session", ProducerID: "worker-1", CorrelationID: "fanout-1", CausationID: started.EventID, PayloadJSON: `{"status":"completed"}`, PayloadRefJSON: `{"kind":"final_response","id":"fr-1"}`, AttentionRequired: true})
	if started.Sequence != 1 || completed.Sequence != 2 {
		t.Fatalf("event sequences = %d, %d, want 1, 2", started.Sequence, completed.Sequence)
	}

	listed := callMethod[WorkspaceEventsResponse](t, registry, "workspace.events.after", WorkspaceEventsAfterRequest{WorkspaceID: "ws-1", AfterSequence: 0, Limit: 10})
	if len(listed.Events) != 2 || listed.Events[1].EventID != completed.EventID || !listed.Events[1].AttentionRequired {
		t.Fatalf("workspace.events.after = %#v, want two workspace events with completed attention", listed)
	}

	subscription := callMethod[WorkspaceEventSubscriptionResponse](t, registry, "workspace.events.subscribe", WorkspaceEventSubscribeRequest{SubscriptionID: "sub-1", WorkspaceID: "ws-1", OwnerSessionID: "orch-1", FilterJSON: `{"event_types":["worker.completed"],"correlation_ids":["fanout-1"]}`})
	if subscription.SubscriptionID != "sub-1" || subscription.CursorSequence != 0 {
		t.Fatalf("workspace.events.subscribe = %#v, want active sub-1 at cursor 0", subscription)
	}

	next := callMethod[WorkspaceEventsResponse](t, registry, "workspace.events.next", WorkspaceEventsNextRequest{SubscriptionID: "sub-1", Limit: 10})
	if len(next.Events) != 1 || next.Events[0].EventID != completed.EventID {
		t.Fatalf("workspace.events.next = %#v, want completed only", next)
	}

	ack := callMethod[WorkspaceEventAckResponse](t, registry, "workspace.events.ack", WorkspaceEventAckRequest{SubscriptionID: "sub-1", Sequence: completed.Sequence})
	if !ack.Acked || ack.Subscription.CursorSequence != completed.Sequence || ack.Subscription.AckSequence != completed.Sequence {
		t.Fatalf("workspace.events.ack = %#v, want acked at completed sequence", ack)
	}
	next = callMethod[WorkspaceEventsResponse](t, registry, "workspace.events.next", WorkspaceEventsNextRequest{SubscriptionID: "sub-1", Limit: 10})
	if len(next.Events) != 0 {
		t.Fatalf("workspace.events.next after ack = %#v, want empty", next)
	}
}

func TestWorkspaceEventSubscriptionReadsFanoutWorkerLifecycle(t *testing.T) {
	j := newJourneyRuntime(t)
	j.seedWorkspace("ws-1", t.TempDir())
	j.createSessionConfig("planner", "ws-1", "planner", "planner-harness")
	j.createHarnessSession("planner-run", "ws-1", "planner", "planner-harness", "waiting", globaldb.HarnessSessionUsageSticky)
	j.createSessionConfig("worker", "ws-1", "worker", "worker-harness")
	j.daemon.setHarnessFactoryForTest("worker-harness", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return newFakeHarness("worker-harness", []TimelineItem{{Kind: "agent_text", Text: "worker result"}}), nil
	})

	_ = callMethod[WorkspaceEventSubscriptionResponse](t, j.registry, "workspace.events.subscribe", WorkspaceEventSubscribeRequest{SubscriptionID: "sub-fanout", WorkspaceID: "ws-1", OwnerSessionID: "planner-run", FilterJSON: `{"event_types":["worker.started","worker.completed"],"correlation_ids":["fg-events"]}`})
	fanout := callMethod[AgentMessageSendResponse](t, j.registry, "session.fanout", AgentMessageSendRequest{FanoutGroupID: "fg-events", SourceSessionID: "planner-run", TargetProfileIDs: []string{"worker"}, Body: "fan out"})
	if len(fanout.FanoutMembers) != 1 {
		t.Fatalf("fanout members = %#v, want one worker", fanout.FanoutMembers)
	}
	workerSessionID := fanout.FanoutMembers[0].Session.SessionID
	waitForStoredHarnessSession(t, j.ctx, j.store, workerSessionID, func(run globaldb.HarnessSession) bool { return run.Status == "completed" })

	events := waitForSubscriptionEvents(t, j.registry, "sub-fanout", 2)
	if events[0].EventType != "worker.started" || events[1].EventType != "worker.completed" {
		t.Fatalf("workspace event types = %q, %q; want worker.started then worker.completed", events[0].EventType, events[1].EventType)
	}
	for _, event := range events {
		if event.WorkspaceID != "ws-1" || event.SubjectType != "harness_session" || event.SubjectID != workerSessionID || event.CorrelationID != "fg-events" {
			t.Fatalf("workspace event = %#v, want workspace-scoped fanout worker event", event)
		}
	}
	ref := payloadRefForTest(t, events[1].PayloadRefJSON)
	if ref["kind"] != "final_response" || ref["id"] == "" {
		t.Fatalf("completed payload_ref = %#v, want final_response link", ref)
	}
}

func TestWorkspaceEventSubscriptionReadsStickySessionCompletion(t *testing.T) {
	j := newJourneyRuntime(t)
	j.seedWorkspace("ws-1", t.TempDir())
	j.daemon.setHarnessFactoryForTest("planner-harness", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return newFinalResponseHarness("planner-harness", []TimelineItem{{Kind: "agent_text", Text: "sticky result"}}), nil
	})

	_ = callMethod[WorkspaceEventSubscriptionResponse](t, j.registry, "workspace.events.subscribe", WorkspaceEventSubscribeRequest{SubscriptionID: "sub-session-completed", WorkspaceID: "ws-1", OwnerSessionID: "observer-run", FilterJSON: `{"event_types":["session.completed"]}`})
	started := callMethod[HarnessSessionStartResponse](t, j.registry, "session.start", HarnessSessionStartRequest{Executor: "planner-harness", SessionID: "planner-run", Packet: ContextPacket{ID: "ctx-planner-run", WorkspaceID: "ws-1", TaskID: "planner-run", Sections: []ContextSection{{Name: "message", Content: "do work"}}}})
	if started.Run.SessionID != "planner-run" || started.Run.Status != "completed" {
		t.Fatalf("session.start = %#v, want completed planner-run", started.Run)
	}

	events := callMethod[WorkspaceEventsResponse](t, j.registry, "workspace.events.next", WorkspaceEventsNextRequest{SubscriptionID: "sub-session-completed", Limit: 10})
	if len(events.Events) != 1 {
		t.Fatalf("workspace.events.next = %#v, want one session.completed event", events)
	}
	event := events.Events[0]
	if event.EventType != "session.completed" || event.SubjectType != "harness_session" || event.SubjectID != "planner-run" || event.ProducerType != "daemon" {
		t.Fatalf("session completion event = %#v, want daemon-produced harness session completion", event)
	}
	payload := globaldb.WorkspaceEventStringPayload(event.PayloadJSON)
	if payload["status"] != "completed" || payload["session_id"] != "planner-run" || payload["harness"] != "planner-harness" {
		t.Fatalf("session completion payload = %#v, want completed planner-run payload", payload)
	}
	ref := payloadRefForTest(t, event.PayloadRefJSON)
	if ref["kind"] != "final_response" || ref["id"] == "" {
		t.Fatalf("session completion payload_ref = %#v, want final_response link", ref)
	}
}

func TestEphemeralCallEmitsSessionLifecycleWorkspaceEvents(t *testing.T) {
	t.Run("completed", func(t *testing.T) {
		j := newJourneyRuntime(t)
		j.seedWorkspace("ws-eph-events", t.TempDir())
		j.createSessionConfig("planner", "ws-eph-events", "planner", "planner-harness")
		j.createHarnessSession("planner-run", "ws-eph-events", "planner", "planner-harness", "waiting", globaldb.HarnessSessionUsageSticky)
		j.createSessionConfig("worker", "ws-eph-events", "worker", "eph-events-harness")
		j.daemon.setHarnessFactoryForTest("eph-events-harness", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
			_ = req
			_ = primaryFolder
			_ = sink
			return newFakeHarness("eph-events-harness", []TimelineItem{{Kind: "agent_text", Text: "ephemeral result"}}), nil
		})

		_ = callMethod[WorkspaceEventSubscriptionResponse](t, j.registry, "workspace.events.subscribe", WorkspaceEventSubscribeRequest{SubscriptionID: "sub-eph-completed", WorkspaceID: "ws-eph-events", OwnerSessionID: "planner-run", FilterJSON: `{"event_types":["session.completed","session.failed","session.stopped"]}`})
		got := callMethod[EphemeralCallResponse](t, j.registry, "session.call.ephemeral", EphemeralCallRequest{CallID: "call-we", SourceSessionID: "planner-run", TargetAgentID: "worker", Body: "do work"})
		final := waitForFinalResponseText(t, j.ctx, j.store, got.Run.SessionID, "ephemeral result")

		events := waitForSubscriptionEvents(t, j.registry, "sub-eph-completed", 1)
		event := events[0]
		if event.EventType != "session.completed" || event.SubjectType != "harness_session" || event.SubjectID != got.Run.SessionID {
			t.Fatalf("ephemeral completion event = %#v, want session.completed for %q", event, got.Run.SessionID)
		}
		payload := globaldb.WorkspaceEventStringPayload(event.PayloadJSON)
		if payload["status"] != "completed" || payload["session_id"] != got.Run.SessionID {
			t.Fatalf("ephemeral completion payload = %#v, want completed worker session payload", payload)
		}
		ref := payloadRefForTest(t, event.PayloadRefJSON)
		if ref["kind"] != "final_response" || ref["id"] != final.FinalResponseID {
			t.Fatalf("ephemeral completion payload_ref = %#v, want final_response link to %q", ref, final.FinalResponseID)
		}
		assertSingleTerminalSessionWorkspaceEvent(t, j.ctx, j.store, "ws-eph-events", got.Run.SessionID, "session.completed")
	})

	t.Run("failed", func(t *testing.T) {
		j := newJourneyRuntime(t)
		j.seedWorkspace("ws-eph-failed", t.TempDir())
		j.createSessionConfig("planner", "ws-eph-failed", "planner", "planner-harness")
		j.createHarnessSession("planner-run", "ws-eph-failed", "planner", "planner-harness", "waiting", globaldb.HarnessSessionUsageSticky)
		j.createSessionConfig("worker", "ws-eph-failed", "worker", "eph-fail-harness")
		j.daemon.setHarnessFactoryForTest("eph-fail-harness", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
			_ = req
			_ = primaryFolder
			_ = sink
			return itemsFailHarness{}, nil
		})

		_ = callMethod[WorkspaceEventSubscriptionResponse](t, j.registry, "workspace.events.subscribe", WorkspaceEventSubscribeRequest{SubscriptionID: "sub-eph-failed", WorkspaceID: "ws-eph-failed", OwnerSessionID: "planner-run", FilterJSON: `{"event_types":["session.completed","session.failed","session.stopped"]}`})
		got := callMethod[EphemeralCallResponse](t, j.registry, "session.call.ephemeral", EphemeralCallRequest{CallID: "call-we-fail", SourceSessionID: "planner-run", TargetAgentID: "worker", Body: "do work"})
		final := waitForFinalResponseContains(t, j.ctx, j.store, got.Run.SessionID, "items failed")
		if final.Status != "failed" {
			t.Fatalf("final response status = %q, want failed", final.Status)
		}

		events := waitForSubscriptionEvents(t, j.registry, "sub-eph-failed", 1)
		event := events[0]
		if event.EventType != "session.failed" || event.SubjectID != got.Run.SessionID || !event.AttentionRequired {
			t.Fatalf("ephemeral failure event = %#v, want attention-required session.failed for %q", event, got.Run.SessionID)
		}
		ref := payloadRefForTest(t, event.PayloadRefJSON)
		if ref["kind"] != "final_response" || ref["id"] != final.FinalResponseID {
			t.Fatalf("ephemeral failure payload_ref = %#v, want final_response link to %q", ref, final.FinalResponseID)
		}
		assertSingleTerminalSessionWorkspaceEvent(t, j.ctx, j.store, "ws-eph-failed", got.Run.SessionID, "session.failed")
	})
}

func assertSingleTerminalSessionWorkspaceEvent(t *testing.T, ctx context.Context, store *globaldb.Store, workspaceID, sessionID, wantType string) {
	t.Helper()
	all, err := store.ListWorkspaceEventsAfterSequence(ctx, workspaceID, 0, 500)
	if err != nil {
		t.Fatalf("ListWorkspaceEventsAfterSequence returned error: %v", err)
	}
	var terminal []globaldb.WorkspaceEvent
	for _, event := range all {
		if event.SubjectID != sessionID {
			continue
		}
		switch event.EventType {
		case "session.completed", "session.failed", "session.stopped":
			terminal = append(terminal, event)
		}
	}
	if len(terminal) != 1 || terminal[0].EventType != wantType {
		t.Fatalf("terminal session events for %q = %#v, want exactly one %s", sessionID, terminal, wantType)
	}
}

// Solo-style wake composition needs normalized idle/needs-input facts: a
// sticky session finishing a turn is idle (available for the next input), and
// an approval runtime event means the session needs human/orchestrator input.
func TestStickySessionEmitsIdleAndNeedsInputWorkspaceEvents(t *testing.T) {
	j := newJourneyRuntime(t)
	j.seedWorkspace("ws-idle", t.TempDir())
	j.daemon.setHarnessFactoryForTest("idle-harness", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return newFinalResponseHarness("idle-harness", []TimelineItem{{Kind: string(HarnessEventApproval), Text: "allow tool?", Metadata: map[string]any{"tool": "bash"}}, {Kind: "agent_text", Text: "turn done"}}), nil
	})
	_ = callMethod[WorkspaceEventSubscriptionResponse](t, j.registry, "workspace.events.subscribe", WorkspaceEventSubscribeRequest{SubscriptionID: "sub-idle", WorkspaceID: "ws-idle", OwnerSessionID: "observer-run", FilterJSON: `{"event_types":["session.idle","session.needs_input"]}`})

	started := callMethod[HarnessSessionStartResponse](t, j.registry, "session.start", HarnessSessionStartRequest{Executor: "idle-harness", SessionID: "sticky-idle-run", Packet: ContextPacket{ID: "ctx-sticky-idle", WorkspaceID: "ws-idle", TaskID: "sticky-idle-run", Sections: []ContextSection{{Name: "message", Content: "do work"}}}})
	if started.Run.Status != "completed" {
		t.Fatalf("session.start = %#v, want completed sticky turn", started.Run)
	}

	events := waitForSubscriptionEvents(t, j.registry, "sub-idle", 2)
	byType := map[string]WorkspaceEventResponse{}
	for _, event := range events {
		byType[event.EventType] = event
	}
	needsInput, ok := byType["session.needs_input"]
	if !ok || needsInput.SubjectID != "sticky-idle-run" || !needsInput.AttentionRequired || needsInput.CausationID == "" {
		t.Fatalf("needs_input event = %#v, want attention-required fact caused by the approval runtime event", needsInput)
	}
	ref := payloadRefForTest(t, needsInput.PayloadRefJSON)
	if ref["kind"] != "harness_runtime_event" || ref["id"] != needsInput.CausationID {
		t.Fatalf("needs_input payload_ref = %#v, want link to runtime event %q", ref, needsInput.CausationID)
	}
	idle, ok := byType["session.idle"]
	if !ok || idle.SubjectID != "sticky-idle-run" || idle.AttentionRequired {
		t.Fatalf("idle event = %#v, want non-attention idle fact for the sticky session", idle)
	}
}

func TestEphemeralCallDoesNotEmitSessionIdle(t *testing.T) {
	j := newJourneyRuntime(t)
	j.seedWorkspace("ws-eph-no-idle", t.TempDir())
	j.createSessionConfig("planner", "ws-eph-no-idle", "planner", "planner-harness")
	j.createHarnessSession("planner-run", "ws-eph-no-idle", "planner", "planner-harness", "waiting", globaldb.HarnessSessionUsageSticky)
	j.createSessionConfig("worker", "ws-eph-no-idle", "worker", "eph-no-idle-harness")
	j.daemon.setHarnessFactoryForTest("eph-no-idle-harness", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return newFakeHarness("eph-no-idle-harness", []TimelineItem{{Kind: "agent_text", Text: "done"}}), nil
	})

	got := callMethod[EphemeralCallResponse](t, j.registry, "session.call.ephemeral", EphemeralCallRequest{CallID: "call-no-idle", SourceSessionID: "planner-run", TargetAgentID: "worker", Body: "do work"})
	waitForFinalResponseText(t, j.ctx, j.store, got.Run.SessionID, "done")

	events, err := j.store.ListWorkspaceEventsAfterSequence(j.ctx, "ws-eph-no-idle", 0, 200)
	if err != nil {
		t.Fatalf("ListWorkspaceEventsAfterSequence returned error: %v", err)
	}
	for _, event := range events {
		if event.EventType == "session.idle" {
			t.Fatalf("event %#v, want no session.idle for terminating ephemeral workers", event)
		}
	}
}

func TestWorkspaceEventSubscriptionReadsHarnessRuntimeEvents(t *testing.T) {
	items := []TimelineItem{
		{Kind: string(HarnessEventLifecycle), Status: HarnessLifecycleTurnStarted, Metadata: map[string]any{"reason": "prompt"}},
		{Kind: string(HarnessEventUsage), Metadata: map[string]any{"input_tokens": int64(3), "output_tokens": int64(5)}},
		{Kind: string(HarnessEventAgentText), Text: "runtime result", Metadata: map[string]any{"final": true}},
	}
	wantTypes := []string{"harness.event.lifecycle", "harness.event.usage", "harness.event.agent_text"}

	t.Run("sticky session", func(t *testing.T) {
		j := newJourneyRuntime(t)
		j.seedWorkspace("ws-runtime-sticky", t.TempDir())
		j.daemon.setHarnessFactoryForTest("runtime-harness", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
			_ = req
			_ = primaryFolder
			_ = sink
			return newFinalResponseHarness("runtime-harness", items), nil
		})

		_ = callMethod[WorkspaceEventSubscriptionResponse](t, j.registry, "workspace.events.subscribe", WorkspaceEventSubscribeRequest{SubscriptionID: "sub-runtime-sticky", WorkspaceID: "ws-runtime-sticky", OwnerSessionID: "observer-run", FilterJSON: `{"event_types":["harness.event.lifecycle","harness.event.usage","harness.event.agent_text"],"subject_ids":["sticky-runtime-run"]}`})
		started := callMethod[HarnessSessionStartResponse](t, j.registry, "session.start", HarnessSessionStartRequest{Executor: "runtime-harness", SessionID: "sticky-runtime-run", Packet: ContextPacket{ID: "ctx-sticky-runtime-run", WorkspaceID: "ws-runtime-sticky", TaskID: "sticky-runtime-run", Sections: []ContextSection{{Name: "message", Content: "do runtime work"}}}})
		if started.Run.SessionID != "sticky-runtime-run" || started.Run.Status != "completed" {
			t.Fatalf("session.start = %#v, want completed sticky-runtime-run", started.Run)
		}

		events := callMethod[WorkspaceEventsResponse](t, j.registry, "workspace.events.next", WorkspaceEventsNextRequest{SubscriptionID: "sub-runtime-sticky", Limit: 10})
		assertHarnessRuntimeWorkspaceEvents(t, events.Events, "ws-runtime-sticky", "sticky-runtime-run", wantTypes)
	})

	t.Run("ephemeral fanout worker", func(t *testing.T) {
		j := newJourneyRuntime(t)
		j.seedWorkspace("ws-runtime-ephemeral", t.TempDir())
		j.createSessionConfig("planner", "ws-runtime-ephemeral", "planner", "planner-harness")
		j.createHarnessSession("planner-run", "ws-runtime-ephemeral", "planner", "planner-harness", "waiting", globaldb.HarnessSessionUsageSticky)
		j.createSessionConfig("worker", "ws-runtime-ephemeral", "worker", "runtime-harness")
		j.daemon.setHarnessFactoryForTest("runtime-harness", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
			_ = req
			_ = primaryFolder
			_ = sink
			return newFinalResponseHarness("runtime-harness", items), nil
		})

		_ = callMethod[WorkspaceEventSubscriptionResponse](t, j.registry, "workspace.events.subscribe", WorkspaceEventSubscribeRequest{SubscriptionID: "sub-runtime-ephemeral", WorkspaceID: "ws-runtime-ephemeral", OwnerSessionID: "planner-run", FilterJSON: `{"event_types":["harness.event.lifecycle","harness.event.usage","harness.event.agent_text"]}`})
		fanout := callMethod[AgentMessageSendResponse](t, j.registry, "session.fanout", AgentMessageSendRequest{FanoutGroupID: "fg-runtime-events", SourceSessionID: "planner-run", TargetProfileIDs: []string{"worker"}, Body: "fan out runtime work"})
		if len(fanout.FanoutMembers) != 1 {
			t.Fatalf("fanout members = %#v, want one worker", fanout.FanoutMembers)
		}
		workerSessionID := fanout.FanoutMembers[0].Session.SessionID
		waitForStoredHarnessSession(t, j.ctx, j.store, workerSessionID, func(run globaldb.HarnessSession) bool { return run.Status == "completed" })

		events := waitForSubscriptionEvents(t, j.registry, "sub-runtime-ephemeral", len(wantTypes))
		assertHarnessRuntimeWorkspaceEvents(t, events, "ws-runtime-ephemeral", workerSessionID, wantTypes)
		waitForProjectedFanoutMemberStatuses(t, j.registry, "ws-runtime-ephemeral", map[string]string{"worker": "completed"})
	})
}

func TestWorkspaceEventSubscriptionCancelStopsNextReads(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	if err := store.CreateWorkspace(context.Background(), "ws-cancel", "ws-cancel", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	created := appendWorkspaceEventForTest(t, store, globaldb.WorkspaceEvent{EventID: "we-cancel", WorkspaceID: "ws-cancel", EventType: "worker.completed", SubjectType: "harness_session", SubjectID: "worker-cancel"})
	_ = callMethod[WorkspaceEventSubscriptionResponse](t, registry, "workspace.events.subscribe", WorkspaceEventSubscribeRequest{SubscriptionID: "sub-cancel", WorkspaceID: "ws-cancel", OwnerSessionID: "owner-cancel", FilterJSON: `{"event_types":["worker.completed"]}`})
	beforeCancel := callMethod[WorkspaceEventsResponse](t, registry, "workspace.events.next", WorkspaceEventsNextRequest{SubscriptionID: "sub-cancel", Limit: 10})
	if len(beforeCancel.Events) != 1 || beforeCancel.Events[0].EventID != created.EventID {
		t.Fatalf("workspace.events.next before cancel = %#v, want created event", beforeCancel)
	}

	canceled := callMethod[WorkspaceEventSubscriptionResponse](t, registry, "workspace.events.cancel", WorkspaceEventSubscriptionCancelRequest{SubscriptionID: "sub-cancel"})
	if canceled.Status != "canceled" {
		t.Fatalf("workspace.events.cancel = %#v, want canceled status", canceled)
	}
	afterCancel := callMethod[WorkspaceEventsResponse](t, registry, "workspace.events.next", WorkspaceEventsNextRequest{SubscriptionID: "sub-cancel", Limit: 10})
	if len(afterCancel.Events) != 0 {
		t.Fatalf("workspace.events.next after cancel = %#v, want no delivery from canceled subscription", afterCancel)
	}
}

func TestWorkspaceEventSubscriptionNextWaitsForMatchingEvent(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	if err := store.CreateWorkspace(context.Background(), "ws-wait", "ws-wait", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	_ = callMethod[WorkspaceEventSubscriptionResponse](t, registry, "workspace.events.subscribe", WorkspaceEventSubscribeRequest{SubscriptionID: "sub-wait", WorkspaceID: "ws-wait", OwnerSessionID: "owner-wait", FilterJSON: `{"event_types":["worker.completed"]}`})

	type callResult struct {
		response WorkspaceEventsResponse
		err      error
	}
	resultC := make(chan callResult, 1)
	readyC := make(chan struct{})
	go func() {
		close(readyC)
		response, err := callMethodResult[WorkspaceEventsResponse](registry, "workspace.events.next", WorkspaceEventsNextRequest{SubscriptionID: "sub-wait", Limit: 10, MinEvents: 1, TimeoutMS: 1000})
		resultC <- callResult{response: response, err: err}
	}()
	<-readyC
	created := appendWorkspaceEventForTest(t, store, globaldb.WorkspaceEvent{EventID: "we-wait", WorkspaceID: "ws-wait", EventType: "worker.completed", SubjectType: "harness_session", SubjectID: "worker-wait"})

	select {
	case result := <-resultC:
		if result.err != nil {
			t.Fatalf("workspace.events.next wait returned error: %v", result.err)
		}
		if len(result.response.Events) != 1 || result.response.Events[0].EventID != created.EventID || result.response.WaitStatus != "ready" || result.response.WaitTimedOut {
			t.Fatalf("workspace.events.next wait = %#v, want ready created event", result.response)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("workspace.events.next wait did not return after matching event")
	}
}

func TestWorkspaceEventSubscriptionNextWaitsOnStoredCompletionCondition(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	if err := store.CreateWorkspace(context.Background(), "ws-condition-wait", "ws-condition-wait", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	_ = callMethod[WorkspaceEventSubscriptionResponse](t, registry, "workspace.events.subscribe", WorkspaceEventSubscribeRequest{SubscriptionID: "sub-condition-wait", WorkspaceID: "ws-condition-wait", OwnerSessionID: "owner-condition", FilterJSON: `{"event_types":["worker.completed","worker.failed","worker.stopped"],"correlation_ids":["fg-condition"]}`, CompletionConditionJSON: `{"mode":"all","subject_ids":["worker-a","worker-b"],"terminal_event_types":["worker.completed","worker.failed","worker.stopped"]}`})
	first := appendWorkspaceEventForTest(t, store, globaldb.WorkspaceEvent{EventID: "we-condition-a", WorkspaceID: "ws-condition-wait", EventType: "worker.completed", SubjectType: "harness_session", SubjectID: "worker-a", CorrelationID: "fg-condition"})

	partial := callMethod[WorkspaceEventsResponse](t, registry, "workspace.events.next", WorkspaceEventsNextRequest{SubscriptionID: "sub-condition-wait", Limit: 10})
	if len(partial.Events) != 1 || partial.Events[0].EventID != first.EventID || partial.WaitStatus != "partial" || partial.WaitTimedOut {
		t.Fatalf("workspace.events.next partial condition = %#v, want one event and partial completion", partial)
	}

	type callResult struct {
		response WorkspaceEventsResponse
		err      error
	}
	resultC := make(chan callResult, 1)
	readyC := make(chan struct{})
	go func() {
		close(readyC)
		response, err := callMethodResult[WorkspaceEventsResponse](registry, "workspace.events.next", WorkspaceEventsNextRequest{SubscriptionID: "sub-condition-wait", Limit: 10, TimeoutMS: 1000})
		resultC <- callResult{response: response, err: err}
	}()
	<-readyC
	second := appendWorkspaceEventForTest(t, store, globaldb.WorkspaceEvent{EventID: "we-condition-b", WorkspaceID: "ws-condition-wait", EventType: "worker.failed", SubjectType: "harness_session", SubjectID: "worker-b", CorrelationID: "fg-condition"})

	select {
	case result := <-resultC:
		if result.err != nil {
			t.Fatalf("workspace.events.next condition wait returned error: %v", result.err)
		}
		if len(result.response.Events) != 2 || result.response.Events[1].EventID != second.EventID || result.response.WaitStatus != "ready" || result.response.WaitTimedOut {
			t.Fatalf("workspace.events.next condition wait = %#v, want ready with both terminal events", result.response)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("workspace.events.next condition wait did not return after completion condition")
	}
}

func waitForSubscriptionEvents(t *testing.T, registry *rpc.MethodRegistry, subscriptionID string, want int) []WorkspaceEventResponse {
	t.Helper()
	resp := callMethod[WorkspaceEventsResponse](t, registry, "workspace.events.next", WorkspaceEventsNextRequest{SubscriptionID: subscriptionID, Limit: want, MinEvents: want, TimeoutMS: 5000})
	if resp.WaitTimedOut || len(resp.Events) < want {
		t.Fatalf("workspace.events.next(%s) = %#v, want at least %d events before timeout", subscriptionID, resp.Events, want)
	}
	return resp.Events
}

func appendWorkspaceEventForTest(t *testing.T, store *globaldb.Store, event globaldb.WorkspaceEvent) WorkspaceEventResponse {
	t.Helper()
	stored, err := store.AppendWorkspaceEvent(context.Background(), event)
	if err != nil {
		t.Fatalf("AppendWorkspaceEvent returned error: %v", err)
	}
	return workspaceEventResponse(stored)
}

func callMethodResult[T any](registry *rpc.MethodRegistry, methodName string, params any) (T, error) {
	spec, ok := registry.Get(methodName)
	if !ok {
		return *new(T), fmt.Errorf("method %s not registered", methodName)
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return *new(T), fmt.Errorf("marshal params for %s: %w", methodName, err)
	}
	resultAny, err := spec.Call(context.Background(), raw)
	if err != nil {
		return *new(T), err
	}
	result, ok := resultAny.(T)
	if !ok {
		return *new(T), fmt.Errorf("call %s result type = %T, want %T", methodName, resultAny, *new(T))
	}
	return result, nil
}

func payloadRefForTest(t *testing.T, raw string) map[string]string {
	t.Helper()
	var ref map[string]string
	if err := json.Unmarshal([]byte(raw), &ref); err != nil {
		t.Fatalf("payload_ref_json %q did not decode: %v", raw, err)
	}
	return ref
}

func assertHarnessRuntimeWorkspaceEvents(t *testing.T, events []WorkspaceEventResponse, workspaceID, sessionID string, wantTypes []string) {
	t.Helper()
	if len(events) != len(wantTypes) {
		t.Fatalf("workspace runtime events = %#v, want %d events", events, len(wantTypes))
	}
	for i, event := range events {
		if event.EventType != wantTypes[i] || event.WorkspaceID != workspaceID || event.SubjectType != "harness_session" || event.SubjectID != sessionID || event.ProducerType != "session" || event.ProducerID != sessionID || event.CorrelationID != sessionID {
			t.Fatalf("workspace runtime event[%d] = %#v, want %s for session %s", i, event, wantTypes[i], sessionID)
		}
		payload := workspaceEventAnyPayload(t, event.PayloadJSON)
		if payload["kind"] != strings.TrimPrefix(wantTypes[i], "harness.event.") {
			t.Fatalf("workspace runtime event[%d] payload = %#v, want matching kind", i, payload)
		}
		if payload["harness_event_id"] == "" || payload["sequence"] == nil {
			t.Fatalf("workspace runtime event[%d] payload = %#v, want harness_event_id and sequence", i, payload)
		}
		ref := payloadRefForTest(t, event.PayloadRefJSON)
		if ref["kind"] != "harness_runtime_event" || ref["id"] == "" {
			t.Fatalf("workspace runtime event[%d] payload_ref = %#v, want harness runtime event link", i, ref)
		}
		if event.EventType == "harness.event.agent_text" {
			inner, ok := payload["payload"].(map[string]any)
			if !ok || inner["text"] != "runtime result" || inner["final"] != true {
				t.Fatalf("agent text payload = %#v, want runtime result final payload", payload)
			}
		}
	}
}

func workspaceEventAnyPayload(t *testing.T, raw string) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("payload_json %q did not decode: %v", raw, err)
	}
	return payload
}
