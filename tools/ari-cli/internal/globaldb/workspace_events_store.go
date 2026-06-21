package globaldb

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb/dbsqlc"
)

const (
	eventSubscriptionStatusActive   = "active"
	eventSubscriptionStatusCanceled = "canceled"
)

type WorkspaceEvent struct {
	EventID           string
	WorkspaceID       string
	Sequence          int64
	EventType         string
	SubjectType       string
	SubjectID         string
	ProducerType      string
	ProducerID        string
	CorrelationID     string
	CausationID       string
	PayloadJSON       string
	PayloadRefJSON    string
	AttentionRequired bool
	CreatedAt         time.Time
}

type AppendWorkspaceEventParams = WorkspaceEvent

type EventSubscription struct {
	SubscriptionID          string
	WorkspaceID             string
	OwnerSessionID          string
	Name                    string
	FilterJSON              string
	DeliveryTargetType      string
	DeliveryTargetID        string
	DeliveryPolicyJSON      string
	CursorSequence          int64
	AckSequence             int64
	Status                  string
	CompletionConditionJSON string
	TimeoutAt               *time.Time
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

type EventSubscriptionFilter struct {
	EventTypes     []string `json:"event_types,omitempty"`
	SubjectTypes   []string `json:"subject_types,omitempty"`
	SubjectIDs     []string `json:"subject_ids,omitempty"`
	ProducerTypes  []string `json:"producer_types,omitempty"`
	ProducerIDs    []string `json:"producer_ids,omitempty"`
	CorrelationIDs []string `json:"correlation_ids,omitempty"`
	CausationIDs   []string `json:"causation_ids,omitempty"`
}

func (s *Store) AppendWorkspaceEvent(ctx context.Context, event AppendWorkspaceEventParams) (WorkspaceEvent, error) {
	return s.EventCoordinator().AppendWorkspaceEvent(ctx, event)
}

func createWorkspaceEventWithQueries(ctx context.Context, queries *dbsqlc.Queries, event *WorkspaceEvent) error {
	sequence, err := queries.NextWorkspaceEventSequence(ctx, dbsqlc.NextWorkspaceEventSequenceParams{WorkspaceID: event.WorkspaceID})
	if err != nil {
		return fmt.Errorf("next workspace event sequence for %q: %w", event.WorkspaceID, err)
	}
	event.Sequence = sequence
	if err := queries.CreateWorkspaceEvent(ctx, dbsqlc.CreateWorkspaceEventParams{EventID: event.EventID, WorkspaceID: event.WorkspaceID, Sequence: event.Sequence, EventType: event.EventType, SubjectType: event.SubjectType, SubjectID: event.SubjectID, ProducerType: event.ProducerType, ProducerID: event.ProducerID, CorrelationID: event.CorrelationID, CausationID: event.CausationID, PayloadJson: event.PayloadJSON, PayloadRefJson: event.PayloadRefJSON, AttentionRequired: boolInt64(event.AttentionRequired), CreatedAt: event.CreatedAt.UTC().Format(time.RFC3339Nano)}); err != nil {
		return fmt.Errorf("append workspace event %q: %w", event.EventID, err)
	}
	recordWorkspaceEventAfterCommit(ctx, *event)
	return nil
}

func (s *Store) ListWorkspaceEventsAfterSequence(ctx context.Context, workspaceID string, afterSequence int64, limit int) ([]WorkspaceEvent, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf("%w: workspace id is required", ErrInvalidInput)
	}
	if afterSequence < 0 {
		return nil, fmt.Errorf("%w: event sequence must not be negative", ErrInvalidInput)
	}
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.sqlcQueries().ListWorkspaceEventsAfterSequence(ctx, dbsqlc.ListWorkspaceEventsAfterSequenceParams{WorkspaceID: workspaceID, Sequence: afterSequence, Limit: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("list workspace events for %q after %d: %w", workspaceID, afterSequence, err)
	}
	return workspaceEventsFromSQLC(rows)
}

func (s *Store) CreateEventSubscription(ctx context.Context, subscription EventSubscription) (EventSubscription, error) {
	subscription = normalizeEventSubscription(subscription)
	if err := validateEventSubscription(subscription); err != nil {
		return EventSubscription{}, err
	}
	if subscription.SubscriptionID == "" {
		subscription.SubscriptionID = newEventSubscriptionID()
	}
	if subscription.CreatedAt.IsZero() {
		subscription.CreatedAt = time.Now().UTC()
	}
	if subscription.UpdatedAt.IsZero() {
		subscription.UpdatedAt = subscription.CreatedAt
	}
	var timeoutAt *string
	if subscription.TimeoutAt != nil {
		formatted := subscription.TimeoutAt.UTC().Format(time.RFC3339Nano)
		timeoutAt = &formatted
	}
	if err := s.withImmediateQueries(ctx, func(ctx context.Context, queries *dbsqlc.Queries) error {
		if err := queries.CreateEventSubscription(ctx, dbsqlc.CreateEventSubscriptionParams{SubscriptionID: subscription.SubscriptionID, WorkspaceID: subscription.WorkspaceID, OwnerSessionID: subscription.OwnerSessionID, Name: subscription.Name, FilterJson: subscription.FilterJSON, DeliveryTargetType: subscription.DeliveryTargetType, DeliveryTargetID: subscription.DeliveryTargetID, DeliveryPolicyJson: subscription.DeliveryPolicyJSON, CursorSequence: subscription.CursorSequence, AckSequence: subscription.AckSequence, Status: subscription.Status, CompletionConditionJson: subscription.CompletionConditionJSON, TimeoutAt: timeoutAt, CreatedAt: subscription.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: subscription.UpdatedAt.UTC().Format(time.RFC3339Nano)}); err != nil {
			return fmt.Errorf("create event subscription %q: %w", subscription.SubscriptionID, err)
		}
		if err := createSubscriptionDeadlineTimerWithQueries(ctx, queries, subscription); err != nil {
			return fmt.Errorf("create event subscription %q deadline timer: %w", subscription.SubscriptionID, err)
		}
		stream, err := NewSubscriptionStream(subscription)
		if err != nil {
			return err
		}
		return stream.BackfillDeliveries(ctx, queries)
	}); err != nil {
		return EventSubscription{}, err
	}
	s.notifyOrchestrationWake()
	return subscription, nil
}

func (s *Store) GetEventSubscription(ctx context.Context, subscriptionID string) (EventSubscription, error) {
	subscriptionID = strings.TrimSpace(subscriptionID)
	if subscriptionID == "" {
		return EventSubscription{}, fmt.Errorf("%w: subscription id is required", ErrInvalidInput)
	}
	row, err := s.sqlcQueries().GetEventSubscription(ctx, dbsqlc.GetEventSubscriptionParams{SubscriptionID: subscriptionID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return EventSubscription{}, ErrNotFound
		}
		return EventSubscription{}, fmt.Errorf("get event subscription %q: %w", subscriptionID, err)
	}
	return eventSubscriptionFromSQLC(row)
}

func (s *Store) AckEventSubscription(ctx context.Context, subscriptionID string, sequence int64) error {
	subscription, err := s.GetEventSubscription(ctx, subscriptionID)
	if err != nil {
		return err
	}
	stream, err := NewSubscriptionStream(subscription)
	if err != nil {
		return err
	}
	return stream.AckCursor(ctx, s.sqlcQueries(), sequence, time.Now().UTC())
}

func (s *Store) CancelEventSubscription(ctx context.Context, subscriptionID string) (EventSubscription, error) {
	subscriptionID = strings.TrimSpace(subscriptionID)
	if subscriptionID == "" {
		return EventSubscription{}, fmt.Errorf("%w: subscription id is required", ErrInvalidInput)
	}
	now := time.Now().UTC()
	formatted := now.Format(time.RFC3339Nano)
	if err := s.withImmediateQueries(ctx, func(ctx context.Context, queries *dbsqlc.Queries) error {
		rows, err := queries.CancelEventSubscription(ctx, dbsqlc.CancelEventSubscriptionParams{UpdatedAt: formatted, SubscriptionID: subscriptionID})
		if err != nil {
			return fmt.Errorf("cancel event subscription %q: %w", subscriptionID, err)
		}
		if rows == 0 {
			subscription, getErr := subscriptionByIDWithQueries(ctx, queries, subscriptionID)
			if getErr != nil {
				return getErr
			}
			if subscription.Status == eventSubscriptionStatusCanceled {
				return nil
			}
			return ErrNotFound
		}
		if err := cancelSubscriptionDeadlineTimersWithQueries(ctx, queries, subscriptionID, now); err != nil {
			return fmt.Errorf("cancel event subscription %q deadline timers: %w", subscriptionID, err)
		}
		return failPendingDeliveriesForSubscriptionWithQueries(ctx, queries, subscriptionID, "event subscription canceled", now)
	}); err != nil {
		return EventSubscription{}, err
	}
	return s.GetEventSubscription(ctx, subscriptionID)
}

func subscriptionByIDWithQueries(ctx context.Context, queries *dbsqlc.Queries, subscriptionID string) (EventSubscription, error) {
	row, err := queries.GetEventSubscription(ctx, dbsqlc.GetEventSubscriptionParams{SubscriptionID: subscriptionID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return EventSubscription{}, ErrNotFound
		}
		return EventSubscription{}, fmt.Errorf("get event subscription %q: %w", subscriptionID, err)
	}
	return eventSubscriptionFromSQLC(row)
}

func normalizeWorkspaceEvent(event WorkspaceEvent) WorkspaceEvent {
	event.EventID = strings.TrimSpace(event.EventID)
	event.WorkspaceID = strings.TrimSpace(event.WorkspaceID)
	event.EventType = strings.TrimSpace(event.EventType)
	event.SubjectType = strings.TrimSpace(event.SubjectType)
	event.SubjectID = strings.TrimSpace(event.SubjectID)
	event.ProducerType = strings.TrimSpace(event.ProducerType)
	event.ProducerID = strings.TrimSpace(event.ProducerID)
	event.CorrelationID = strings.TrimSpace(event.CorrelationID)
	event.CausationID = strings.TrimSpace(event.CausationID)
	event.PayloadJSON = defaultJSON(event.PayloadJSON, "{}")
	event.PayloadRefJSON = defaultJSON(event.PayloadRefJSON, "{}")
	return event
}

func validateWorkspaceEvent(event WorkspaceEvent) error {
	if event.WorkspaceID == "" || event.EventType == "" || event.SubjectType == "" || event.SubjectID == "" {
		return fmt.Errorf("%w: workspace event required field is missing", ErrInvalidInput)
	}
	if !json.Valid([]byte(event.PayloadJSON)) {
		return fmt.Errorf("%w: workspace event payload json is invalid", ErrInvalidInput)
	}
	if !json.Valid([]byte(event.PayloadRefJSON)) {
		return fmt.Errorf("%w: workspace event payload ref json is invalid", ErrInvalidInput)
	}
	return nil
}

func normalizeEventSubscription(subscription EventSubscription) EventSubscription {
	subscription.SubscriptionID = strings.TrimSpace(subscription.SubscriptionID)
	subscription.WorkspaceID = strings.TrimSpace(subscription.WorkspaceID)
	subscription.OwnerSessionID = strings.TrimSpace(subscription.OwnerSessionID)
	subscription.Name = strings.TrimSpace(subscription.Name)
	subscription.FilterJSON = defaultJSON(subscription.FilterJSON, "{}")
	subscription.DeliveryTargetType = strings.TrimSpace(subscription.DeliveryTargetType)
	subscription.DeliveryTargetID = strings.TrimSpace(subscription.DeliveryTargetID)
	subscription.DeliveryPolicyJSON = defaultJSON(subscription.DeliveryPolicyJSON, "{}")
	subscription.CompletionConditionJSON = defaultJSON(subscription.CompletionConditionJSON, "{}")
	subscription.Status = strings.TrimSpace(subscription.Status)
	if subscription.Status == "" {
		subscription.Status = eventSubscriptionStatusActive
	}
	return subscription
}

func validateEventSubscription(subscription EventSubscription) error {
	if subscription.WorkspaceID == "" {
		return fmt.Errorf("%w: event subscription workspace id is required", ErrInvalidInput)
	}
	if subscription.CursorSequence < 0 || subscription.AckSequence < 0 {
		return fmt.Errorf("%w: event subscription cursor values must not be negative", ErrInvalidInput)
	}
	if !json.Valid([]byte(subscription.FilterJSON)) {
		return fmt.Errorf("%w: event subscription filter json is invalid", ErrInvalidInput)
	}
	if (subscription.DeliveryTargetType == "") != (subscription.DeliveryTargetID == "") {
		return fmt.Errorf("%w: event subscription delivery target type and id must be set together", ErrInvalidInput)
	}
	if !json.Valid([]byte(subscription.DeliveryPolicyJSON)) {
		return fmt.Errorf("%w: event subscription delivery policy json is invalid", ErrInvalidInput)
	}
	if !json.Valid([]byte(subscription.CompletionConditionJSON)) {
		return fmt.Errorf("%w: event subscription completion condition json is invalid", ErrInvalidInput)
	}
	if _, err := ParseEventSubscriptionCompletionCondition(subscription.CompletionConditionJSON); err != nil {
		return err
	}
	return nil
}

func workspaceEventsFromSQLC(rows []dbsqlc.WorkspaceEvent) ([]WorkspaceEvent, error) {
	events := make([]WorkspaceEvent, 0, len(rows))
	for _, row := range rows {
		event, err := workspaceEventFromSQLC(row)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}

func workspaceEventFromSQLC(row dbsqlc.WorkspaceEvent) (WorkspaceEvent, error) {
	createdAt, err := time.Parse(time.RFC3339Nano, row.CreatedAt)
	if err != nil {
		return WorkspaceEvent{}, fmt.Errorf("parse workspace event %q created_at %q: %w", row.EventID, row.CreatedAt, err)
	}
	return WorkspaceEvent{EventID: row.EventID, WorkspaceID: row.WorkspaceID, Sequence: row.Sequence, EventType: row.EventType, SubjectType: row.SubjectType, SubjectID: row.SubjectID, ProducerType: row.ProducerType, ProducerID: row.ProducerID, CorrelationID: row.CorrelationID, CausationID: row.CausationID, PayloadJSON: row.PayloadJson, PayloadRefJSON: row.PayloadRefJson, AttentionRequired: row.AttentionRequired != 0, CreatedAt: createdAt}, nil
}

func eventSubscriptionFromSQLC(row dbsqlc.EventSubscription) (EventSubscription, error) {
	createdAt, err := time.Parse(time.RFC3339Nano, row.CreatedAt)
	if err != nil {
		return EventSubscription{}, fmt.Errorf("parse event subscription %q created_at %q: %w", row.SubscriptionID, row.CreatedAt, err)
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, row.UpdatedAt)
	if err != nil {
		return EventSubscription{}, fmt.Errorf("parse event subscription %q updated_at %q: %w", row.SubscriptionID, row.UpdatedAt, err)
	}
	var timeoutAt *time.Time
	if row.TimeoutAt != nil {
		parsed, err := time.Parse(time.RFC3339Nano, *row.TimeoutAt)
		if err != nil {
			return EventSubscription{}, fmt.Errorf("parse event subscription %q timeout_at %q: %w", row.SubscriptionID, *row.TimeoutAt, err)
		}
		timeoutAt = &parsed
	}
	return EventSubscription{SubscriptionID: row.SubscriptionID, WorkspaceID: row.WorkspaceID, OwnerSessionID: row.OwnerSessionID, Name: row.Name, FilterJSON: row.FilterJson, DeliveryTargetType: row.DeliveryTargetType, DeliveryTargetID: row.DeliveryTargetID, DeliveryPolicyJSON: row.DeliveryPolicyJson, CursorSequence: row.CursorSequence, AckSequence: row.AckSequence, Status: row.Status, CompletionConditionJSON: row.CompletionConditionJson, TimeoutAt: timeoutAt, CreatedAt: createdAt, UpdatedAt: updatedAt}, nil
}

func parseEventSubscriptionFilter(raw string) (EventSubscriptionFilter, error) {
	var filter EventSubscriptionFilter
	if strings.TrimSpace(raw) == "" {
		return filter, nil
	}
	if err := json.Unmarshal([]byte(raw), &filter); err != nil {
		return EventSubscriptionFilter{}, err
	}
	filter.EventTypes = normalizeStringSet(filter.EventTypes)
	filter.SubjectTypes = normalizeStringSet(filter.SubjectTypes)
	filter.SubjectIDs = normalizeStringSet(filter.SubjectIDs)
	filter.ProducerTypes = normalizeStringSet(filter.ProducerTypes)
	filter.ProducerIDs = normalizeStringSet(filter.ProducerIDs)
	filter.CorrelationIDs = normalizeStringSet(filter.CorrelationIDs)
	filter.CausationIDs = normalizeStringSet(filter.CausationIDs)
	return filter, nil
}

func (filter EventSubscriptionFilter) matches(event WorkspaceEvent) bool {
	return matchesAny(filter.EventTypes, event.EventType) && matchesAny(filter.SubjectTypes, event.SubjectType) && matchesAny(filter.SubjectIDs, event.SubjectID) && matchesAny(filter.ProducerTypes, event.ProducerType) && matchesAny(filter.ProducerIDs, event.ProducerID) && matchesAny(filter.CorrelationIDs, event.CorrelationID) && matchesAny(filter.CausationIDs, event.CausationID)
}

func normalizeStringSet(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func matchesAny(accepted []string, actual string) bool {
	if len(accepted) == 0 {
		return true
	}
	for _, candidate := range accepted {
		if candidate == actual {
			return true
		}
	}
	return false
}

func newWorkspaceEventID() string {
	return randomID("we")
}

func newEventSubscriptionID() string {
	return randomID("es")
}

func randomID(prefix string) string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		panic(fmt.Sprintf("crypto/rand.Read failed: %v", err))
	}
	return prefix + "_" + hex.EncodeToString(bytes[:])
}
