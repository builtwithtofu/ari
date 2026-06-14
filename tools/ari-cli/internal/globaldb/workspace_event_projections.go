package globaldb

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb/dbsqlc"
)

const (
	workspaceEventWorkerStarted   = "worker.started"
	workspaceEventWorkerCompleted = "worker.completed"
	workspaceEventWorkerFailed    = "worker.failed"
	workspaceEventWorkerStopped   = "worker.stopped"
)

// FanoutProjection materializes fanout member state from worker workspace
// events. The workspace event payload carries the fanout identity; the row is a
// rebuildable cache over event history.
type FanoutProjection struct{}

func (FanoutProjection) ProjectWorkspaceEvent(ctx context.Context, queries *dbsqlc.Queries, event WorkspaceEvent) error {
	member, ok, err := fanoutMemberFromWorkspaceEvent(event)
	if err != nil || !ok {
		return err
	}
	return upsertFanoutMemberWithQueries(ctx, queries, member)
}

// InboxProjection materializes sticky-session inbox rows from terminal worker
// workspace events. Read/unread state is consumer state and is preserved by the
// inbox upsert while event evidence is refreshed.
type InboxProjection struct{}

func (InboxProjection) ProjectWorkspaceEvent(ctx context.Context, queries *dbsqlc.Queries, event WorkspaceEvent) error {
	item, ok, err := inboxItemFromWorkspaceEvent(ctx, queries, event)
	if err != nil || !ok {
		return err
	}
	return createInboxItemWithQueries(ctx, queries, item)
}

func fanoutMemberFromWorkspaceEvent(event WorkspaceEvent) (FanoutMember, bool, error) {
	if !isFanoutWorkerWorkspaceEvent(event.EventType) {
		return FanoutMember{}, false, nil
	}
	payload := workspaceEventStringPayload(event.PayloadJSON)
	memberID := strings.TrimSpace(payload["fanout_member_id"])
	if memberID == "" {
		return FanoutMember{}, false, nil
	}
	groupID := strings.TrimSpace(payload["fanout_group_id"])
	if groupID == "" {
		groupID = strings.TrimSpace(event.CorrelationID)
	}
	member := FanoutMember{
		FanoutMemberID:        memberID,
		FanoutGroupID:         groupID,
		WorkspaceID:           strings.TrimSpace(event.WorkspaceID),
		WorkerSessionID:       strings.TrimSpace(event.SubjectID),
		TargetProfileID:       strings.TrimSpace(payload["target_profile_id"]),
		RequestAgentMessageID: strings.TrimSpace(payload["request_agent_message_id"]),
		ReplyAgentMessageID:   strings.TrimSpace(payload["reply_agent_message_id"]),
		Status:                fanoutWorkerEventStatus(event.EventType),
		CreatedAt:             event.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:             event.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	switch event.EventType {
	case workspaceEventWorkerStarted:
		member.RequestAgentMessageID = strings.TrimSpace(event.CausationID)
	case workspaceEventWorkerCompleted:
		member.ReplyAgentMessageID = strings.TrimSpace(event.CausationID)
	}
	if finalResponseID := finalResponseIDFromWorkspaceEventRef(event.PayloadRefJSON); finalResponseID != "" {
		member.FinalResponseID = finalResponseID
	}
	if err := validateFanoutMemberProjection(member); err != nil {
		return FanoutMember{}, false, err
	}
	return member, true, nil
}

func inboxItemFromWorkspaceEvent(ctx context.Context, queries *dbsqlc.Queries, event WorkspaceEvent) (InboxItem, bool, error) {
	kind := inboxKindForFanoutWorkerEvent(event.EventType)
	if kind == "" {
		return InboxItem{}, false, nil
	}
	member, ok, err := fanoutMemberFromWorkspaceEvent(event)
	if err != nil || !ok {
		return InboxItem{}, false, err
	}
	group, err := queries.GetFanoutGroup(ctx, dbsqlc.GetFanoutGroupParams{FanoutGroupID: member.FanoutGroupID})
	if err != nil {
		return InboxItem{}, false, fmt.Errorf("project inbox item for fanout group %q: %w", member.FanoutGroupID, err)
	}
	if strings.TrimSpace(group.WorkspaceID) != strings.TrimSpace(event.WorkspaceID) {
		return InboxItem{}, false, fmt.Errorf("%w: fanout group workspace does not match worker event", ErrInvalidInput)
	}
	item := InboxItem{
		InboxItemID:       "inbox-" + member.FanoutMemberID,
		WorkspaceID:       event.WorkspaceID,
		SourceSessionID:   group.SourceSessionID,
		WorkspaceEventID:  event.EventID,
		EventType:         event.EventType,
		FanoutGroupID:     member.FanoutGroupID,
		FanoutMemberID:    member.FanoutMemberID,
		WorkerSessionID:   member.WorkerSessionID,
		FinalResponseID:   member.FinalResponseID,
		Kind:              kind,
		Status:            inboxItemStatusUnread,
		AttentionRequired: event.AttentionRequired,
		Summary:           "worker " + fanoutWorkerEventStatus(event.EventType),
		CreatedAt:         event.CreatedAt,
		UpdatedAt:         event.CreatedAt,
	}
	item = normalizeInboxItem(item)
	if err := validateInboxItem(item); err != nil {
		return InboxItem{}, false, err
	}
	return item, true, nil
}

func isFanoutWorkerWorkspaceEvent(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case workspaceEventWorkerStarted, workspaceEventWorkerCompleted, workspaceEventWorkerFailed, workspaceEventWorkerStopped:
		return true
	default:
		return false
	}
}

func inboxKindForFanoutWorkerEvent(eventType string) string {
	switch strings.TrimSpace(eventType) {
	case workspaceEventWorkerCompleted:
		return "worker_completed"
	case workspaceEventWorkerFailed:
		return "worker_failed"
	case workspaceEventWorkerStopped:
		return "worker_stopped"
	default:
		return ""
	}
}

func fanoutWorkerEventStatus(eventType string) string {
	switch strings.TrimSpace(eventType) {
	case workspaceEventWorkerStarted:
		return "running"
	case workspaceEventWorkerCompleted:
		return "completed"
	case workspaceEventWorkerFailed:
		return "failed"
	case workspaceEventWorkerStopped:
		return "stopped"
	default:
		return strings.TrimSpace(eventType)
	}
}

func workspaceEventStringPayload(raw string) map[string]string {
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(payload))
	for key, value := range payload {
		switch typed := value.(type) {
		case string:
			out[key] = typed
		case fmt.Stringer:
			out[key] = typed.String()
		case nil:
			out[key] = ""
		default:
			out[key] = fmt.Sprint(typed)
		}
	}
	return out
}

func finalResponseIDFromWorkspaceEventRef(raw string) string {
	ref := workspaceEventStringPayload(raw)
	if strings.TrimSpace(ref["kind"]) != "final_response" {
		return ""
	}
	return strings.TrimSpace(ref["id"])
}
