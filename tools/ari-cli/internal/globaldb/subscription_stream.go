package globaldb

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb/dbsqlc"
)

// SubscriptionStream is a compiled subscription over workspace event history.
// It owns matching, scanning, delivery creation, and cursor/ack advancement so
// callers do not each recreate stream semantics around workspace_events.
type SubscriptionStream struct {
	compiled compiledEventSubscription
}

func NewSubscriptionStream(subscription EventSubscription) (SubscriptionStream, error) {
	compiled, err := compileEventSubscription(subscription)
	if err != nil {
		return SubscriptionStream{}, err
	}
	return SubscriptionStream{compiled: compiled}, nil
}

func (s SubscriptionStream) Subscription() EventSubscription {
	return s.compiled.EventSubscription
}

func (s SubscriptionStream) HasDeliveryTarget() bool {
	return s.compiled.hasDeliveryTarget()
}

func (s SubscriptionStream) MatchEvent(event WorkspaceEvent, options eventSubscriptionMatchOptions) bool {
	return s.compiled.matchesEvent(event, options)
}

func CreatePendingDeliveriesForEvent(ctx context.Context, queries *dbsqlc.Queries, event WorkspaceEvent) error {
	if workspaceEventSkipsDeliveryFanout(event) {
		return nil
	}
	rows, err := queries.ListActiveEventSubscriptionsByWorkspace(ctx, dbsqlc.ListActiveEventSubscriptionsByWorkspaceParams{WorkspaceID: event.WorkspaceID})
	if err != nil {
		return fmt.Errorf("list active event subscriptions for %q: %w", event.WorkspaceID, err)
	}
	for _, row := range rows {
		subscription, err := eventSubscriptionFromSQLC(row)
		if err != nil {
			return err
		}
		stream, err := NewSubscriptionStream(subscription)
		if err != nil {
			return err
		}
		if !stream.HasDeliveryTarget() {
			continue
		}
		if !stream.MatchEvent(event, eventSubscriptionMatchOptions{skipDeliveryEvents: true, applyCursor: true, applyTimeout: true}) {
			continue
		}
		if err := stream.CreatePendingDeliveryForEvent(ctx, queries, event); err != nil {
			return err
		}
	}
	return nil
}

func (s SubscriptionStream) BackfillDeliveries(ctx context.Context, queries *dbsqlc.Queries) error {
	if s.compiled.Status != eventSubscriptionStatusActive || !s.HasDeliveryTarget() {
		return nil
	}
	sequence := s.compiled.CursorSequence
	for {
		events, err := listWorkspaceEventsAfterSequenceWithQueries(ctx, queries, s.compiled.WorkspaceID, sequence, defaultSubscriptionEventScanPageSize)
		if err != nil {
			return err
		}
		if len(events) == 0 {
			return nil
		}
		for _, event := range events {
			sequence = event.Sequence
			if !s.MatchEvent(event, eventSubscriptionMatchOptions{skipDeliveryEvents: true, applyTimeout: true}) {
				continue
			}
			if err := s.CreatePendingDeliveryForEvent(ctx, queries, event); err != nil {
				return err
			}
		}
		if len(events) < defaultSubscriptionEventScanPageSize {
			return nil
		}
	}
}

func (s SubscriptionStream) Read(ctx context.Context, queries *dbsqlc.Queries, options eventSubscriptionReadOptions) (EventSubscriptionReadResult, error) {
	completion := s.compiled.completion
	if options.minEvents > 0 {
		completion = EventSubscriptionCompletionCondition{Mode: "count", MinEvents: options.minEvents}
	}
	limit := eventSubscriptionReadLimit(options.limit, options.minEvents, completion)
	events, err := s.ScanUnreadEvents(ctx, queries, limit, completion)
	if err != nil {
		return EventSubscriptionReadResult{}, err
	}
	timedOut := s.hasSubscriptionDeadlineEvent(events)
	completionEvents := s.completionEvents(events)
	return EventSubscriptionReadResult{Subscription: s.compiled.EventSubscription, Events: events, Completion: completion.evaluate(completionEvents, timedOut)}, nil
}

func (s SubscriptionStream) ScanUnreadEvents(ctx context.Context, queries *dbsqlc.Queries, limit int, completion EventSubscriptionCompletionCondition) ([]WorkspaceEvent, error) {
	if s.compiled.Status == eventSubscriptionStatusCanceled {
		return []WorkspaceEvent{}, nil
	}
	if limit <= 0 {
		limit = 100
	}
	pageSize := limit * 4
	if pageSize < defaultSubscriptionEventScanPageSize {
		pageSize = defaultSubscriptionEventScanPageSize
	}
	completionAware := completion.requiresSubjectScan()
	sequence := s.compiled.CursorSequence
	matched := make([]WorkspaceEvent, 0, limit)
	for {
		events, err := listWorkspaceEventsAfterSequenceWithQueries(ctx, queries, s.compiled.WorkspaceID, sequence, pageSize)
		if err != nil {
			return nil, err
		}
		if len(events) == 0 {
			break
		}
		stop := false
		for _, event := range events {
			sequence = event.Sequence
			if !s.MatchEvent(event, eventSubscriptionMatchOptions{applyTimeout: true}) {
				continue
			}
			matched = append(matched, event)
			if completionAware {
				if completion.evaluate(matched, false).Satisfied {
					stop = true
					break
				}
			} else if len(matched) == limit {
				stop = true
				break
			}
		}
		if stop || len(events) < pageSize {
			break
		}
	}
	return matched, nil
}

func (s SubscriptionStream) AdvanceAckForCompletedDelivery(ctx context.Context, queries *dbsqlc.Queries, delivery PendingDelivery, updatedAt time.Time) error {
	completedByCurrentDelivery := make(map[string]struct{}, len(delivery.EventIDs))
	for _, eventID := range delivery.EventIDs {
		completedByCurrentDelivery[strings.TrimSpace(eventID)] = struct{}{}
	}
	sequence := s.compiled.CursorSequence
	ackSequence := s.compiled.CursorSequence
	for {
		events, err := listWorkspaceEventsAfterSequenceWithQueries(ctx, queries, s.compiled.WorkspaceID, sequence, defaultSubscriptionEventScanPageSize)
		if err != nil {
			return err
		}
		if len(events) == 0 {
			break
		}
		for _, event := range events {
			sequence = event.Sequence
			if !s.MatchEvent(event, eventSubscriptionMatchOptions{skipDeliveryEvents: true}) {
				continue
			}
			_, completed := completedByCurrentDelivery[event.EventID]
			if !completed {
				completed, err = pendingDeliveryForSubscriptionEventIsCompleted(ctx, queries, s.compiled.SubscriptionID, event.EventID)
				if err != nil {
					return err
				}
			}
			if !completed {
				goto updateCursor
			}
			ackSequence = event.Sequence
		}
		if len(events) < defaultSubscriptionEventScanPageSize {
			break
		}
	}

updateCursor:
	if ackSequence == s.compiled.CursorSequence {
		return nil
	}
	if err := s.AckCursor(ctx, queries, ackSequence, updatedAt); err != nil {
		return fmt.Errorf("ack event subscription %q for delivery %q: %w", s.compiled.SubscriptionID, delivery.DeliveryID, err)
	}
	return nil
}

func (s SubscriptionStream) AckCursor(ctx context.Context, queries *dbsqlc.Queries, sequence int64, updatedAt time.Time) error {
	return ackEventSubscriptionCursor(ctx, queries, s.compiled.SubscriptionID, sequence, updatedAt)
}

func (s SubscriptionStream) hasSubscriptionDeadlineEvent(events []WorkspaceEvent) bool {
	for _, event := range events {
		if isSubscriptionDeadlineTimerEvent(event) && WorkspaceTimerTargetSubscriptionIDFromEvent(event) == s.compiled.SubscriptionID {
			return true
		}
	}
	return false
}

func (s SubscriptionStream) completionEvents(events []WorkspaceEvent) []WorkspaceEvent {
	out := make([]WorkspaceEvent, 0, len(events))
	for _, event := range events {
		if isSubscriptionDeadlineTimerEvent(event) && WorkspaceTimerTargetSubscriptionIDFromEvent(event) == s.compiled.SubscriptionID {
			continue
		}
		out = append(out, event)
	}
	return out
}

func (s SubscriptionStream) CreatePendingDeliveryForEvent(ctx context.Context, queries *dbsqlc.Queries, event WorkspaceEvent) error {
	nextAttemptAt := event.CreatedAt
	_, err := createPendingDeliveryWithQueries(ctx, queries, PendingDelivery{DeliveryID: pendingDeliveryIDForSubscriptionEvent(s.compiled.SubscriptionID, event.EventID), WorkspaceID: event.WorkspaceID, SubscriptionID: s.compiled.SubscriptionID, TargetType: s.compiled.DeliveryTargetType, TargetID: s.compiled.DeliveryTargetID, DeliveryPolicyJSON: s.compiled.DeliveryPolicyJSON, EventIDs: []string{event.EventID}, NextAttemptAt: &nextAttemptAt})
	return err
}

func ackEventSubscriptionCursor(ctx context.Context, queries *dbsqlc.Queries, subscriptionID string, sequence int64, updatedAt time.Time) error {
	subscriptionID = strings.TrimSpace(subscriptionID)
	if subscriptionID == "" || sequence < 0 {
		return fmt.Errorf("%w: subscription id and non-negative sequence are required", ErrInvalidInput)
	}
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	formatted := updatedAt.UTC().Format(time.RFC3339Nano)
	rows, err := queries.UpdateEventSubscriptionCursor(ctx, dbsqlc.UpdateEventSubscriptionCursorParams{CursorSequence: sequence, CursorSequence_2: sequence, AckSequence: sequence, MIN: sequence, CursorSequence_3: sequence, Column6: sequence, UpdatedAt: formatted, SubscriptionID: subscriptionID})
	if err != nil {
		return fmt.Errorf("ack event subscription %q: %w", subscriptionID, err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}
