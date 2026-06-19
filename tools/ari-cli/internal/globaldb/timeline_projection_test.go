package globaldb

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb/dbsqlc"
)

func TestTimelineProjectionMaterializesCommandEvents(t *testing.T) {
	store := newGlobalDBTestStore(t, "timeline-command")
	ctx := context.Background()
	if err := store.CreateWorkspace(ctx, "ws-1", "ws-1", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	if err := store.CreateCommand(ctx, CreateCommandParams{CommandID: "cmd-1", WorkspaceID: "ws-1", Command: "just", Args: `["verify"]`, Status: "running", StartedAt: "2026-06-18T12:00:00Z"}); err != nil {
		t.Fatalf("CreateCommand returned error: %v", err)
	}

	items, err := store.ListTimelineItems(ctx, "ws-1")
	if err != nil {
		t.Fatalf("ListTimelineItems returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("timeline items = %#v, want one command item", items)
	}
	got := items[0]
	if got.SourceKind != "command" || got.SourceID != "cmd-1" || got.Kind != "lifecycle" || got.Status != "running" || got.Sequence != 1 || got.Text != "just verify" || got.WorkspaceEventID == "" {
		t.Fatalf("timeline item = %#v, want materialized command lifecycle", got)
	}
}

func TestTimelineProjectionUpdatesFanoutMemberInPlace(t *testing.T) {
	store := newGlobalDBTestStore(t, "timeline-fanout")
	ctx := context.Background()
	base := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	if err := store.CreateWorkspace(ctx, "ws-1", "ws-1", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	if err := store.CreateFanoutGroup(ctx, FanoutGroup{FanoutGroupID: "fg-1", WorkspaceID: "ws-1", SourceSessionID: "run-1", SourceAgentID: "agent-1", RequestAgentMessageID: "request-1", Body: "compare"}); err != nil {
		t.Fatalf("CreateFanoutGroup returned error: %v", err)
	}
	if _, err := store.AppendWorkspaceEvent(ctx, WorkspaceEvent{EventID: "we-started", WorkspaceID: "ws-1", EventType: WorkspaceEventWorkerStarted, SubjectType: "harness_session", SubjectID: "worker-1", ProducerType: "session", ProducerID: "worker-1", CorrelationID: "fg-1", CausationID: "request-1", PayloadJSON: `{"fanout_member_id":"fm-1","fanout_group_id":"fg-1","target_profile_id":"agent-2"}`, CreatedAt: base}); err != nil {
		t.Fatalf("AppendWorkspaceEvent started returned error: %v", err)
	}
	completed, err := store.AppendWorkspaceEvent(ctx, WorkspaceEvent{EventID: "we-completed", WorkspaceID: "ws-1", EventType: WorkspaceEventWorkerCompleted, SubjectType: "harness_session", SubjectID: "worker-1", ProducerType: "session", ProducerID: "worker-1", CorrelationID: "fg-1", CausationID: "reply-1", PayloadJSON: `{"fanout_member_id":"fm-1","fanout_group_id":"fg-1","target_profile_id":"agent-2"}`, PayloadRefJSON: `{"kind":"final_response","id":"fr-1"}`, CreatedAt: base.Add(time.Second)})
	if err != nil {
		t.Fatalf("AppendWorkspaceEvent completed returned error: %v", err)
	}

	items, err := store.ListTimelineItems(ctx, "ws-1")
	if err != nil {
		t.Fatalf("ListTimelineItems returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("timeline items = %#v, want one fanout member item", items)
	}
	got := items[0]
	if got.ID != "fm-1" || got.SourceKind != "fanout_member" || got.SourceID != "fm-1" || got.Status != "completed" || got.Sequence != 1 || got.WorkspaceEventID != completed.EventID || got.Metadata["request_agent_message_id"] != "request-1" || got.Metadata["reply_agent_message_id"] != "reply-1" || got.Metadata["final_response_id"] != "fr-1" {
		t.Fatalf("fanout timeline item = %#v, want completed member update preserving sequence and linkage metadata", got)
	}
}

func TestTimelineProjectionIgnoresUnknownEventsByDesign(t *testing.T) {
	store := newGlobalDBTestStore(t, "timeline-unknown")
	ctx := context.Background()
	if err := store.CreateWorkspace(ctx, "ws-1", "ws-1", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	if _, err := store.AppendWorkspaceEvent(ctx, WorkspaceEvent{EventID: "we-signal", WorkspaceID: "ws-1", EventType: "signal.sent", SubjectType: "signal", SubjectID: "sig-1", ProducerType: "daemon", ProducerID: "test", PayloadJSON: `{"status":"sent"}`}); err != nil {
		t.Fatalf("AppendWorkspaceEvent returned error: %v", err)
	}
	items, err := store.ListTimelineItems(ctx, "ws-1")
	if err != nil {
		t.Fatalf("ListTimelineItems returned error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("timeline items = %#v, want unknown event type omitted", items)
	}
}

func TestTimelineProjectionRebuildMatchesMaterializedRows(t *testing.T) {
	store := newGlobalDBTestStore(t, "timeline-rebuild")
	ctx := context.Background()
	if err := store.CreateWorkspace(ctx, "ws-1", "ws-1", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	if err := store.CreateCommand(ctx, CreateCommandParams{CommandID: "cmd-1", WorkspaceID: "ws-1", Command: "just verify", Args: `[]`, Status: "running", StartedAt: "2026-06-18T12:00:00Z"}); err != nil {
		t.Fatalf("CreateCommand returned error: %v", err)
	}
	if err := store.UpdateCommandStatus(ctx, UpdateCommandStatusParams{WorkspaceID: "ws-1", CommandID: "cmd-1", Status: "exited", ExitCode: intPtr(0), FinishedAt: stringPtr("2026-06-18T12:01:00Z")}); err != nil {
		t.Fatalf("UpdateCommandStatus returned error: %v", err)
	}
	before, err := store.ListTimelineItems(ctx, "ws-1")
	if err != nil {
		t.Fatalf("ListTimelineItems before returned error: %v", err)
	}
	if err := (TimelineProjection{}).Rebuild(ctx, store, "ws-1"); err != nil {
		t.Fatalf("TimelineProjection.Rebuild returned error: %v", err)
	}
	After, err := store.ListTimelineItems(ctx, "ws-1")
	if err != nil {
		t.Fatalf("ListTimelineItems after returned error: %v", err)
	}
	if !reflect.DeepEqual(before, After) {
		t.Fatalf("rebuilt timeline items = %#v, want %#v", After, before)
	}
}

func TestDefaultProjectionRegistryDispatchesTimelinePrefixes(t *testing.T) {
	registry := DefaultProjectionRegistry()
	projections := registry.ProjectionsForEvent(WorkspaceEvent{EventType: "command.started"})
	for _, projection := range projections {
		if projection.Name() == "timeline_items" {
			return
		}
	}
	t.Fatalf("command.started projections = %#v, want timeline_items", projections)
}

type nonComparableTestProjection struct {
	ids []string
}

func (p nonComparableTestProjection) Name() string { return "non_comparable" }

func (p nonComparableTestProjection) EventTypes() []string { return []string{"custom.event"} }

func (p nonComparableTestProjection) EventTypePrefixes() []string { return []string{"custom."} }

func (p nonComparableTestProjection) ProjectWorkspaceEvent(context.Context, *dbsqlc.Queries, WorkspaceEvent) error {
	return nil
}

func TestProjectionRegistryDedupesNonComparableProjectionByName(t *testing.T) {
	registry := NewProjectionRegistry()
	if err := registry.Register(nonComparableTestProjection{ids: []string{"not-comparable"}}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	projections := registry.ProjectionsForEvent(WorkspaceEvent{EventType: "custom.event"})
	if len(projections) != 1 || projections[0].Name() != "non_comparable" {
		t.Fatalf("projections = %#v, want one non-comparable projection", projections)
	}
}
