package daemon

import (
	"context"
	"fmt"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

type journeyHarnessFactory struct {
	t       *testing.T
	starts  int
	items   []TimelineItem
	harness string
}

func newJourneyHarnessFactory(t *testing.T, harness string, items []TimelineItem) *journeyHarnessFactory {
	t.Helper()
	if harness == "" {
		harness = "journey-harness"
	}
	return &journeyHarnessFactory{t: t, harness: harness, items: append([]TimelineItem(nil), items...)}
}

func (f *journeyHarnessFactory) register(d *Daemon) {
	f.t.Helper()
	d.setHarnessFactoryForTest(f.harness, func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		f.starts++
		return newFakeHarness(f.harness, f.items), nil
	})
}

func (f *journeyHarnessFactory) requireStarts(want int) {
	f.t.Helper()
	if f.starts != want {
		f.t.Fatalf("harness starts = %d, want %d", f.starts, want)
	}
}

type journeyRuntime struct {
	t        *testing.T
	ctx      context.Context
	store    *globaldb.Store
	registry *rpc.MethodRegistry
	daemon   *Daemon
}

func newJourneyRuntime(t *testing.T) *journeyRuntime {
	t.Helper()
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	return &journeyRuntime{t: t, ctx: context.Background(), store: store, registry: registry, daemon: d}
}

func (j *journeyRuntime) seedWorkspace(workspaceID string, folders ...string) {
	j.t.Helper()
	if len(folders) == 0 {
		folders = []string{j.t.TempDir()}
	}
	seedSessionWithPrimaryFolder(j.t, j.store, workspaceID, folders[0])
	for _, folder := range folders[1:] {
		if err := j.store.AddFolder(j.ctx, workspaceID, folder, "git", false); err != nil {
			j.t.Fatalf("AddFolder(%s, %s) returned error: %v", workspaceID, folder, err)
		}
	}
}

func (j *journeyRuntime) createProfile(workspaceID, name, harness string) ProfileResponse {
	j.t.Helper()
	return callMethod[ProfileResponse](j.t, j.registry, "profile.create", ProfileCreateRequest{WorkspaceID: workspaceID, Name: name, Harness: harness, Model: "model-1", Prompt: fmt.Sprintf("%s behavior", name), InvocationClass: HarnessInvocationSticky})
}

func (j *journeyRuntime) createSessionConfig(agentID, workspaceID, name, harness string) {
	j.t.Helper()
	if err := j.store.CreateHarnessSessionConfig(j.ctx, globaldb.HarnessSessionConfig{AgentID: agentID, WorkspaceID: workspaceID, Name: name, Harness: harness, Model: "model-1", Prompt: name + " behavior"}); err != nil {
		j.t.Fatalf("CreateHarnessSessionConfig(%s) returned error: %v", agentID, err)
	}
}

func (j *journeyRuntime) createHarnessSession(sessionID, workspaceID, agentID, harness, status, usage string) {
	j.t.Helper()
	if usage == "" {
		usage = globaldb.HarnessSessionUsageSticky
	}
	if err := j.store.CreateHarnessSession(j.ctx, globaldb.HarnessSession{SessionID: sessionID, WorkspaceID: workspaceID, AgentID: agentID, Harness: harness, Model: "model-1", Status: status, Usage: usage, CWD: j.t.TempDir()}); err != nil {
		j.t.Fatalf("CreateHarnessSession(%s) returned error: %v", sessionID, err)
	}
}

func (j *journeyRuntime) recordHarnessTimeline(run HarnessSession, items []TimelineItem) {
	j.t.Helper()
	j.daemon.recordExecutorRun(run, items)
	if err := appendHarnessRuntimeWorkspaceEvents(j.ctx, j.store, run, harnessRuntimeEventsFromItems(run, items)); err != nil {
		j.t.Fatalf("appendHarnessRuntimeWorkspaceEvents(%s) returned error: %v", run.HarnessSessionID, err)
	}
}

func (j *journeyRuntime) appendTextMessage(sessionID, messageID string, sequence int, role, text string) {
	j.t.Helper()
	if err := j.store.AppendRunLogMessage(j.ctx, globaldb.RunLogMessage{MessageID: messageID, SessionID: sessionID, Sequence: sequence, Role: role, Status: "completed", Parts: []globaldb.RunLogMessagePart{{PartID: messageID + "-part-1", Sequence: 1, Kind: "text", Text: text}}}); err != nil {
		j.t.Fatalf("AppendRunLogMessage(%s) returned error: %v", messageID, err)
	}
}

func requireStatusSession(t *testing.T, status WorkspaceStatusResponse, sessionID, wantStatus, wantUsage string) SessionActivity {
	t.Helper()
	for _, session := range status.Sessions {
		if session.ID == sessionID {
			if wantStatus != "" && session.Status != wantStatus {
				t.Fatalf("status session %s status = %q, want %q", sessionID, session.Status, wantStatus)
			}
			if wantUsage != "" && session.Usage != wantUsage {
				t.Fatalf("status session %s usage = %q, want %q", sessionID, session.Usage, wantUsage)
			}
			return session
		}
	}
	t.Fatalf("status sessions = %#v, missing %s", status.Sessions, sessionID)
	return SessionActivity{}
}

func requireTimelineSession(t *testing.T, timeline WorkspaceTimelineResponse, sessionID string) TimelineItem {
	t.Helper()
	for _, item := range timeline.Items {
		if item.RunID == sessionID || item.SourceID == sessionID {
			return item
		}
	}
	t.Fatalf("timeline items = %#v, missing session %s", timeline.Items, sessionID)
	return TimelineItem{}
}
