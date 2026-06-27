package globaldb

import (
	"context"
	"database/sql"
	"errors"
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

// InboxProjection materializes addressed attention rows from workspace events.
// The rows are rebuildable event evidence; read/unread state lives in separate
// per-consumer read state and is joined at read time.
type InboxProjection struct{}

func (InboxProjection) Name() string { return "inbox_items" }

func (InboxProjection) EventTypes() []string {
	return []string{WorkspaceEventWorkerCompleted, WorkspaceEventWorkerFailed, WorkspaceEventWorkerStopped, WorkspaceEventSessionNeedsInput, WorkspaceEventSignalSent, WorkspaceEventTimerFired, WorkspaceEventDeliveryFailed}
}

func (p InboxProjection) ProjectWorkspaceEvent(ctx context.Context, queries *dbsqlc.Queries, event WorkspaceEvent) error {
	item, ok, err := inboxItemFromWorkspaceEvent(ctx, queries, event)
	if err != nil || !ok {
		return err
	}
	return createInboxItemWithQueries(ctx, queries, item)
}

func (p InboxProjection) Rebuild(ctx context.Context, store *Store, workspaceID string) error {
	if store == nil {
		return fmt.Errorf("%w: globaldb store is required", ErrInvalidInput)
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return ErrInvalidInput
	}
	return store.withImmediateQueries(ctx, func(ctx context.Context, queries *dbsqlc.Queries) error {
		items, err := inboxItemsFromWorkspaceEventsWithQueries(ctx, queries, workspaceID)
		if err != nil {
			return err
		}
		if _, err := queries.DeleteInboxItemsByWorkspace(ctx, dbsqlc.DeleteInboxItemsByWorkspaceParams{WorkspaceID: workspaceID}); err != nil {
			return fmt.Errorf("delete inbox items for workspace %q: %w", workspaceID, err)
		}
		for _, item := range items {
			if err := createInboxItemWithQueries(ctx, queries, item); err != nil {
				return err
			}
		}
		return nil
	})
}

func inboxItemsFromWorkspaceEventsWithQueries(ctx context.Context, queries *dbsqlc.Queries, workspaceID string) ([]InboxItem, error) {
	const pageSize = 500
	sequence := int64(0)
	projected := map[string]InboxItem{}
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
			item, ok, err := inboxItemFromWorkspaceEvent(ctx, queries, event)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			key := strings.TrimSpace(item.InboxItemID)
			if key == "" {
				continue
			}
			if _, ok := projected[key]; !ok {
				order = append(order, key)
			}
			projected[key] = item
		}
		if len(events) < pageSize {
			break
		}
	}
	items := make([]InboxItem, 0, len(order))
	for _, key := range order {
		items = append(items, projected[key])
	}
	return items, nil
}

func fanoutMemberFromWorkspaceEvent(event WorkspaceEvent) (FanoutMember, bool, error) {
	decoded, ok, err := DecodeFanoutWorkerWorkspaceEvent(event)
	if err != nil || !ok {
		return FanoutMember{}, false, err
	}
	if decoded.FanoutMemberID == "" {
		return FanoutMember{}, false, nil
	}
	member := FanoutMember{
		FanoutMemberID:        decoded.FanoutMemberID,
		FanoutGroupID:         decoded.FanoutGroupID,
		WorkspaceID:           decoded.WorkspaceID,
		WorkerSessionID:       decoded.WorkerSessionID,
		TargetProfileID:       decoded.TargetProfileID,
		RequestAgentMessageID: decoded.RequestAgentMessageID,
		ReplyAgentMessageID:   decoded.ReplyAgentMessageID,
		FinalResponseID:       decoded.FinalResponseID,
		Status:                decoded.Status,
		CreatedAt:             decoded.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:             decoded.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	if err := validateFanoutMemberProjection(member); err != nil {
		return FanoutMember{}, false, err
	}
	return member, true, nil
}

func inboxItemFromWorkspaceEvent(ctx context.Context, queries *dbsqlc.Queries, event WorkspaceEvent) (InboxItem, bool, error) {
	switch strings.TrimSpace(event.EventType) {
	case WorkspaceEventWorkerCompleted, WorkspaceEventWorkerFailed, WorkspaceEventWorkerStopped:
		return fanoutWorkerInboxItemFromWorkspaceEvent(event)
	case WorkspaceEventSessionNeedsInput:
		return sessionNeedsInputInboxItemFromWorkspaceEvent(event)
	case WorkspaceEventSignalSent:
		return signalSentInboxItemFromWorkspaceEvent(ctx, queries, event)
	case WorkspaceEventTimerFired:
		return timerInboxItemFromWorkspaceEvent(ctx, queries, event)
	case WorkspaceEventDeliveryFailed:
		return deliveryFailedInboxItemFromWorkspaceEvent(ctx, queries, event)
	default:
		return InboxItem{}, false, nil
	}
}

func fanoutWorkerInboxItemFromWorkspaceEvent(event WorkspaceEvent) (InboxItem, bool, error) {
	kind := inboxKindForFanoutWorkerEvent(event.EventType)
	if kind == "" {
		return InboxItem{}, false, nil
	}
	member, ok, err := fanoutMemberFromWorkspaceEvent(event)
	if err != nil || !ok {
		return InboxItem{}, false, err
	}
	decoded, _, _ := DecodeFanoutWorkerWorkspaceEvent(event)
	sourceSessionID := decoded.SourceSessionID
	if sourceSessionID == "" {
		return InboxItem{}, false, fmt.Errorf("%w: fanout worker event is missing source_session_id", ErrInvalidInput)
	}
	return validateProjectedInboxItem(InboxItem{InboxItemID: "inbox-" + member.FanoutMemberID, WorkspaceID: event.WorkspaceID, SourceSessionID: sourceSessionID, WorkspaceEventID: event.EventID, EventType: event.EventType, FanoutGroupID: member.FanoutGroupID, FanoutMemberID: member.FanoutMemberID, WorkerSessionID: member.WorkerSessionID, FinalResponseID: member.FinalResponseID, Kind: kind, Status: inboxItemStatusUnread, AttentionRequired: event.AttentionRequired, Summary: "worker " + WorkerEventStatus(event.EventType), CreatedAt: event.CreatedAt, UpdatedAt: event.CreatedAt})
}

func sessionNeedsInputInboxItemFromWorkspaceEvent(event WorkspaceEvent) (InboxItem, bool, error) {
	decoded, _ := DecodeHarnessSessionWorkspaceEvent(event)
	sessionID := strings.TrimSpace(event.SubjectID)
	if sessionID == "" {
		sessionID = decoded.SessionID
	}
	if sessionID == "" {
		return InboxItem{}, false, nil
	}
	summary := "session needs input"
	if harness := decoded.Harness; harness != "" {
		summary = harness + " session needs input"
	}
	return validateProjectedInboxItem(InboxItem{InboxItemID: "inbox-event-" + event.EventID, WorkspaceID: event.WorkspaceID, SourceSessionID: sessionID, WorkspaceEventID: event.EventID, EventType: event.EventType, WorkerSessionID: sessionID, Kind: "session_needs_input", Status: inboxItemStatusUnread, AttentionRequired: true, Summary: summary, CreatedAt: event.CreatedAt, UpdatedAt: event.CreatedAt})
}

func signalSentInboxItemFromWorkspaceEvent(ctx context.Context, queries *dbsqlc.Queries, event WorkspaceEvent) (InboxItem, bool, error) {
	decoded, _ := DecodeSignalWorkspaceEvent(event)
	sourceSessionID := decoded.SourceSessionID
	if sourceSessionID == "" {
		sourceSessionID = decoded.TargetSessionID
	}
	if sourceSessionID == "" {
		switch strings.TrimSpace(event.SubjectType) {
		case WorkspaceEventSubjectHarnessSession:
			sourceSessionID = strings.TrimSpace(event.SubjectID)
		case WorkspaceEventSubjectEventSubscription, WorkspaceEventSubjectSubscription:
			subscription, err := subscriptionByIDWithQueries(ctx, queries, event.SubjectID)
			if err != nil {
				if errors.Is(err, ErrNotFound) {
					return InboxItem{}, false, nil
				}
				return InboxItem{}, false, err
			}
			if strings.TrimSpace(subscription.WorkspaceID) != strings.TrimSpace(event.WorkspaceID) {
				return InboxItem{}, false, nil
			}
			sourceSessionID = strings.TrimSpace(subscription.OwnerSessionID)
		case WorkspaceEventSubjectFanoutGroup:
			group, err := queries.GetFanoutGroup(ctx, dbsqlc.GetFanoutGroupParams{FanoutGroupID: strings.TrimSpace(event.SubjectID)})
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return InboxItem{}, false, nil
				}
				return InboxItem{}, false, fmt.Errorf("project signal inbox item for fanout group %q: %w", event.SubjectID, err)
			}
			if strings.TrimSpace(group.WorkspaceID) != strings.TrimSpace(event.WorkspaceID) {
				return InboxItem{}, false, nil
			}
			sourceSessionID = strings.TrimSpace(group.SourceSessionID)
		}
	}
	if sourceSessionID == "" {
		return InboxItem{}, false, nil
	}
	summary := "signal sent"
	if action := decoded.Action; action != "" {
		summary = "signal sent: " + action
	}
	return validateProjectedInboxItem(InboxItem{InboxItemID: "inbox-signal-" + event.EventID, WorkspaceID: event.WorkspaceID, SourceSessionID: sourceSessionID, WorkspaceEventID: event.EventID, EventType: event.EventType, WorkerSessionID: sourceSessionID, Kind: "signal_sent", Status: inboxItemStatusUnread, AttentionRequired: true, Summary: summary, CreatedAt: event.CreatedAt, UpdatedAt: event.CreatedAt})
}

func timerInboxItemFromWorkspaceEvent(ctx context.Context, queries *dbsqlc.Queries, event WorkspaceEvent) (InboxItem, bool, error) {
	row, err := queries.GetWorkspaceTimer(ctx, dbsqlc.GetWorkspaceTimerParams{TimerID: strings.TrimSpace(event.SubjectID)})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return InboxItem{}, false, nil
		}
		return InboxItem{}, false, fmt.Errorf("project timer inbox item for %q: %w", event.SubjectID, err)
	}
	timer := workspaceTimerFromSQLC(row)
	ownerSessionID := strings.TrimSpace(timer.OwnerSessionID)
	if ownerSessionID == "" && strings.TrimSpace(timer.TargetSubscriptionID) != "" {
		subscription, err := subscriptionByIDWithQueries(ctx, queries, timer.TargetSubscriptionID)
		if err != nil {
			return InboxItem{}, false, err
		}
		ownerSessionID = strings.TrimSpace(subscription.OwnerSessionID)
	}
	if ownerSessionID == "" {
		return InboxItem{}, false, nil
	}
	summary := "timer fired"
	if timer.Purpose != "" {
		summary = "timer fired: " + timer.Purpose
	}
	return validateProjectedInboxItem(InboxItem{InboxItemID: "inbox-timer-" + timer.TimerID, WorkspaceID: event.WorkspaceID, SourceSessionID: ownerSessionID, WorkspaceEventID: event.EventID, EventType: event.EventType, Kind: "timer_fired", Status: inboxItemStatusUnread, AttentionRequired: true, Summary: summary, CreatedAt: event.CreatedAt, UpdatedAt: event.CreatedAt})
}

func deliveryFailedInboxItemFromWorkspaceEvent(ctx context.Context, queries *dbsqlc.Queries, event WorkspaceEvent) (InboxItem, bool, error) {
	decoded, _ := DecodeDeliveryWorkspaceEvent(event)
	deliveryID := decoded.DeliveryID
	if deliveryID == "" {
		deliveryID = strings.TrimSpace(event.SubjectID)
	}
	if deliveryID == "" {
		return InboxItem{}, false, nil
	}
	sourceSessionID := ""
	if subscriptionID := decoded.SubscriptionID; subscriptionID != "" {
		if subscription, err := subscriptionByIDWithQueries(ctx, queries, subscriptionID); err == nil {
			if strings.TrimSpace(subscription.WorkspaceID) == strings.TrimSpace(event.WorkspaceID) {
				sourceSessionID = strings.TrimSpace(subscription.OwnerSessionID)
			}
		} else if !errors.Is(err, ErrNotFound) {
			return InboxItem{}, false, err
		}
	}
	if sourceSessionID == "" && decoded.TargetType == WorkspaceEventSubjectHarnessSession {
		sourceSessionID = decoded.TargetID
	}
	if sourceSessionID == "" {
		return InboxItem{}, false, nil
	}
	summary := "delivery failed"
	if lastError := decoded.LastError; lastError != "" {
		summary = summary + ": " + lastError
	}
	return validateProjectedInboxItem(InboxItem{InboxItemID: "inbox-delivery-" + deliveryID, WorkspaceID: event.WorkspaceID, SourceSessionID: sourceSessionID, WorkspaceEventID: event.EventID, EventType: event.EventType, Kind: "delivery_failed", Status: inboxItemStatusUnread, AttentionRequired: true, Summary: summary, CreatedAt: event.CreatedAt, UpdatedAt: event.CreatedAt})
}

func validateProjectedInboxItem(item InboxItem) (InboxItem, bool, error) {
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
