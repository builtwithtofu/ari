package globaldb

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb/dbsqlc"
)

// FanoutProjection materializes fanout member state from worker workspace
// events. The workspace event payload carries the fanout identity; the row is a
// rebuildable cache over event history.
type FanoutProjection struct{}

func (FanoutProjection) Name() string { return "fanout_members" }

func (FanoutProjection) EventTypes() []string {
	return []string{WorkspaceEventWorkerStarted, WorkspaceEventWorkerCompleted, WorkspaceEventWorkerFailed, WorkspaceEventWorkerStopped}
}

func (FanoutProjection) ProjectWorkspaceEvent(ctx context.Context, queries *dbsqlc.Queries, event WorkspaceEvent) error {
	member, ok, err := fanoutMemberFromWorkspaceEvent(event)
	if err != nil || !ok {
		return err
	}
	return upsertFanoutMemberWithQueries(ctx, queries, member)
}

func (p FanoutProjection) MembersFromWorkspaceEvents(ctx context.Context, store *Store, workspaceID, groupID string) ([]FanoutMember, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	groupID = strings.TrimSpace(groupID)
	if workspaceID == "" || store == nil {
		return nil, nil
	}
	return fanoutMembersFromWorkspaceEventsWithQueries(ctx, store.sqlcQueries(), workspaceID, groupID)
}

func (p FanoutProjection) Rebuild(ctx context.Context, store *Store, workspaceID string) error {
	if store == nil {
		return fmt.Errorf("%w: globaldb store is required", ErrInvalidInput)
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return ErrInvalidInput
	}
	return store.withImmediateQueries(ctx, func(ctx context.Context, queries *dbsqlc.Queries) error {
		members, err := fanoutMembersFromWorkspaceEventsWithQueries(ctx, queries, workspaceID, "")
		if err != nil {
			return err
		}
		if _, err := queries.DeleteFanoutMembersByWorkspace(ctx, dbsqlc.DeleteFanoutMembersByWorkspaceParams{WorkspaceID: workspaceID}); err != nil {
			return fmt.Errorf("delete fanout members for workspace %q: %w", workspaceID, err)
		}
		for _, member := range members {
			if err := upsertFanoutMemberWithQueries(ctx, queries, member); err != nil {
				return err
			}
		}
		return nil
	})
}

func fanoutMembersFromWorkspaceEventsWithQueries(ctx context.Context, queries *dbsqlc.Queries, workspaceID, groupID string) ([]FanoutMember, error) {
	const pageSize = 500
	sequence := int64(0)
	projected := map[string]FanoutMember{}
	order := make([]string, 0)
	for {
		events, err := listWorkspaceEventsAfterSequenceWithQueries(ctx, queries, workspaceID, sequence, pageSize)
		if err != nil {
			return nil, err
		}
		if len(events) == 0 {
			break
		}
		for _, event := range events {
			sequence = event.Sequence
			if !IsFanoutWorkerWorkspaceEvent(event.EventType) || strings.TrimSpace(event.CorrelationID) == "" || (groupID != "" && strings.TrimSpace(event.CorrelationID) != groupID) {
				continue
			}
			member, ok, err := fanoutMemberFromWorkspaceEvent(event)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			key := strings.TrimSpace(member.FanoutMemberID)
			if key == "" {
				key = strings.TrimSpace(member.WorkerSessionID)
			}
			if key == "" {
				continue
			}
			if existing, ok := projected[key]; ok {
				projected[key] = mergeFanoutMember(existing, member)
			} else {
				order = append(order, key)
				projected[key] = member
			}
		}
		if len(events) < pageSize {
			break
		}
	}
	members := make([]FanoutMember, 0, len(order))
	for _, key := range order {
		members = append(members, projected[key])
	}
	return members, nil
}

func mergeFanoutMember(existing, next FanoutMember) FanoutMember {
	out := existing
	if out.FanoutMemberID == "" {
		out.FanoutMemberID = next.FanoutMemberID
	}
	if out.FanoutGroupID == "" {
		out.FanoutGroupID = next.FanoutGroupID
	}
	if out.WorkspaceID == "" {
		out.WorkspaceID = next.WorkspaceID
	}
	if out.WorkerSessionID == "" {
		out.WorkerSessionID = next.WorkerSessionID
	}
	if next.TargetProfileID != "" {
		out.TargetProfileID = next.TargetProfileID
	}
	if next.RequestAgentMessageID != "" {
		out.RequestAgentMessageID = next.RequestAgentMessageID
	}
	if next.ReplyAgentMessageID != "" {
		out.ReplyAgentMessageID = next.ReplyAgentMessageID
	}
	if next.FinalResponseID != "" {
		out.FinalResponseID = next.FinalResponseID
	}
	if next.Status != "" {
		out.Status = next.Status
	}
	if out.CreatedAt == "" {
		out.CreatedAt = next.CreatedAt
	}
	if next.UpdatedAt != "" {
		out.UpdatedAt = next.UpdatedAt
	}
	if out.UpdatedAt == "" {
		out.UpdatedAt = out.CreatedAt
	}
	return out
}

// InboxProjection materializes sticky-session inbox rows from terminal worker
// workspace events. Read/unread state is consumer state and is preserved by the
// inbox upsert while event evidence is refreshed.
type InboxProjection struct{}

func (InboxProjection) Name() string { return "inbox_items" }

func (InboxProjection) EventTypes() []string {
	return []string{WorkspaceEventWorkerCompleted, WorkspaceEventWorkerFailed, WorkspaceEventWorkerStopped}
}

func (InboxProjection) ProjectWorkspaceEvent(ctx context.Context, queries *dbsqlc.Queries, event WorkspaceEvent) error {
	item, ok, err := inboxItemFromWorkspaceEvent(ctx, queries, event)
	if err != nil || !ok {
		return err
	}
	return createInboxItemWithQueries(ctx, queries, item)
}

func fanoutMemberFromWorkspaceEvent(event WorkspaceEvent) (FanoutMember, bool, error) {
	if !IsFanoutWorkerWorkspaceEvent(event.EventType) {
		return FanoutMember{}, false, nil
	}
	payload := WorkspaceEventStringPayload(event.PayloadJSON)
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
		Status:                WorkerEventStatus(event.EventType),
		CreatedAt:             event.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:             event.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	switch event.EventType {
	case WorkspaceEventWorkerStarted:
		member.RequestAgentMessageID = strings.TrimSpace(event.CausationID)
	case WorkspaceEventWorkerCompleted:
		member.ReplyAgentMessageID = strings.TrimSpace(event.CausationID)
	}
	if finalResponseID := FinalResponseIDFromWorkspaceEventRef(event.PayloadRefJSON); finalResponseID != "" {
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
		Summary:           "worker " + WorkerEventStatus(event.EventType),
		CreatedAt:         event.CreatedAt,
		UpdatedAt:         event.CreatedAt,
	}
	item = normalizeInboxItem(item)
	if err := validateInboxItem(item); err != nil {
		return InboxItem{}, false, err
	}
	return item, true, nil
}

func inboxKindForFanoutWorkerEvent(eventType string) string {
	switch strings.TrimSpace(eventType) {
	case WorkspaceEventWorkerCompleted:
		return "worker_completed"
	case WorkspaceEventWorkerFailed:
		return "worker_failed"
	case WorkspaceEventWorkerStopped:
		return "worker_stopped"
	default:
		return ""
	}
}
