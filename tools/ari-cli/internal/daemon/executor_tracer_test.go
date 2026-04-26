package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

func TestStartExecutorRunProjectsPacketIntoAgentRunAndTimeline(t *testing.T) {
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	executor := NewFakeExecutor("fake", []TimelineItem{{Kind: "agent_text", Text: "done"}})

	run, items, err := StartExecutorRun(context.Background(), executor, packet)
	if err != nil {
		t.Fatalf("StartExecutorRun returned error: %v", err)
	}
	if run.WorkspaceID != "ws-1" || run.TaskID != "task-1" || run.ContextPacketID != "ctx_123" {
		t.Fatalf("agent run ids = %#v, want workspace/task/context packet ids", run)
	}
	if run.Executor != "fake" || run.Status != "completed" {
		t.Fatalf("agent run executor/status = %q/%q, want fake/completed", run.Executor, run.Status)
	}
	if run.ProviderRunID == "" {
		t.Fatal("provider run id is empty")
	}
	if len(items) != 1 {
		t.Fatalf("items len = %d, want 1", len(items))
	}
	if executor.lastContextPacket == "" || !strings.Contains(executor.lastContextPacket, "ctx_123") {
		t.Fatalf("executor context packet = %q, want serialized packet", executor.lastContextPacket)
	}
	if items[0].RunID != run.AgentRunID || items[0].Kind != "agent_text" || items[0].SourceKind != "executor" {
		t.Fatalf("timeline item = %#v, want executor agent_text linked to run", items[0])
	}
}

func TestStartExecutorRunRejectsMissingPacketIdentity(t *testing.T) {
	executor := NewFakeExecutor("fake", nil)
	_, _, err := StartExecutorRun(context.Background(), executor, ContextPacket{WorkspaceID: "ws-1", TaskID: "task-1"})
	if err == nil {
		t.Fatal("StartExecutorRun returned nil error for missing context packet id")
	}
}

func TestAgentRunMethodStartsFakeExecutorFromContextPacket(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())

	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	resp := callMethod[AgentRunStartResponse](t, registry, "agent.run", AgentRunStartRequest{
		Executor:  "fake",
		Packet:    packet,
		FakeItems: []TimelineItem{{Kind: "agent_text", Text: "done"}},
	})
	if resp.Run.Executor != "fake" || resp.Run.ContextPacketID != "ctx_123" {
		t.Fatalf("agent run = %#v, want fake run linked to context packet", resp.Run)
	}
	if len(resp.Items) != 1 || resp.Items[0].Kind != "agent_text" || resp.Items[0].RunID != resp.Run.AgentRunID {
		t.Fatalf("items = %#v, want one agent_text linked to run", resp.Items)
	}
	timeline := callMethod[WorkspaceTimelineResponse](t, registry, "workspace.timeline", WorkspaceTimelineRequest{WorkspaceID: "ws-1"})
	if len(timeline.Items) != 1 || timeline.Items[0].RunID != resp.Run.AgentRunID {
		t.Fatalf("timeline items = %#v, want persisted executor item", timeline.Items)
	}
	activity := callMethod[WorkspaceActivityResponse](t, registry, "workspace.activity", WorkspaceActivityRequest{WorkspaceID: "ws-1"})
	if len(activity.Agents) != 1 || activity.Agents[0].ID != resp.Run.AgentRunID {
		t.Fatalf("activity agents = %#v, want executor run agent activity", activity.Agents)
	}
}

func TestAgentRunMethodStartsPTYExecutorFromContextPacket(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())

	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	start := time.Now()
	resp := callMethod[AgentRunStartResponse](t, registry, "agent.run", AgentRunStartRequest{
		Executor: "pty",
		Packet:   packet,
		Command:  "/bin/sh",
		Args:     []string{"-c", "sleep 0.2; printf done"},
	})
	if time.Since(start) > 500*time.Millisecond {
		t.Fatalf("agent.run pty took %s, want prompt return", time.Since(start))
	}
	if resp.Run.Executor != "pty" || resp.Run.ContextPacketID != "ctx_123" {
		t.Fatalf("agent run = %#v, want pty run linked to context packet", resp.Run)
	}
	if len(resp.Items) != 1 || resp.Items[0].Kind != "lifecycle" || resp.Items[0].Status != "running" {
		t.Fatalf("items = %#v, want one running lifecycle item", resp.Items)
	}
	deadline := time.Now().Add(boundedTestTimeout(t, 5*time.Second))
	for time.Now().Before(deadline) {
		timeline := callMethod[WorkspaceTimelineResponse](t, registry, "workspace.timeline", WorkspaceTimelineRequest{WorkspaceID: "ws-1"})
		for _, item := range timeline.Items {
			if item.RunID == resp.Run.AgentRunID && item.Kind == "terminal_output" && item.Text == "done" {
				activity := callMethod[WorkspaceActivityResponse](t, registry, "workspace.activity", WorkspaceActivityRequest{WorkspaceID: "ws-1"})
				if len(activity.Agents) != 1 || activity.Agents[0].Status != "completed" {
					t.Fatalf("activity agents = %#v, want completed pty run after output", activity.Agents)
				}
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("workspace.timeline did not persist pty output for run %s", resp.Run.AgentRunID)
}

func TestRecordExecutorRunPreservesBufferedSinkItems(t *testing.T) {
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.appendExecutorItems("run-1", []TimelineItem{{ID: "run-1:output", WorkspaceID: "ws-1", RunID: "run-1", SourceKind: "executor", SourceID: "run-1", Kind: "terminal_output", Status: "completed", Text: "done"}})

	d.recordExecutorRun(AgentRun{AgentRunID: "run-1", WorkspaceID: "ws-1", Status: "running", Executor: "pty"}, []TimelineItem{{ID: "run-1:lifecycle", WorkspaceID: "ws-1", RunID: "run-1", SourceKind: "executor", SourceID: "run-1", Kind: "lifecycle", Status: "running", Text: "pty"}})

	items := d.executorTimelineItems("ws-1")
	if len(items) != 2 {
		t.Fatalf("executor items len = %d, want buffered output plus initial lifecycle: %#v", len(items), items)
	}
	if items[0].ID != "run-1:lifecycle" || items[1].ID != "run-1:output" {
		t.Fatalf("executor items = %#v, want lifecycle then buffered output", items)
	}
	activity := AgentActivity{Status: d.executorRuns["run-1"].Status}
	if activity.Status != "completed" {
		t.Fatalf("executor run status = %q, want completed from buffered sink item", activity.Status)
	}
}

func TestExecutorRunStatusFailureTakesPrecedence(t *testing.T) {
	status := executorRunStatusFromItems([]TimelineItem{
		{ID: "run-1:output", Status: "completed"},
		{ID: "run-1:failure", Status: "failed"},
	})
	if status != "failed" {
		t.Fatalf("executor status = %q, want failed", status)
	}
}

func TestAgentRunMethodMarksPTYFailureFromExitCode(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())

	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	resp := callMethod[AgentRunStartResponse](t, registry, "agent.run", AgentRunStartRequest{
		Executor: "pty",
		Packet:   packet,
		Command:  "/bin/sh",
		Args:     []string{"-c", "printf failed; exit 7"},
	})
	deadline := time.Now().Add(boundedTestTimeout(t, 5*time.Second))
	for time.Now().Before(deadline) {
		activity := callMethod[WorkspaceActivityResponse](t, registry, "workspace.activity", WorkspaceActivityRequest{WorkspaceID: "ws-1"})
		if len(activity.Agents) == 1 && activity.Agents[0].ID == resp.Run.AgentRunID && activity.Agents[0].Status == "failed" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("workspace.activity did not mark failed pty run %s as failed", resp.Run.AgentRunID)
}
