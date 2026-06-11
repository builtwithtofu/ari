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
	event = normalizeWorkspaceEvent(event)
	if err := validateWorkspaceEvent(event); err != nil {
		return WorkspaceEvent{}, err
	}
	if event.EventID == "" {
		event.EventID = newWorkspaceEventID()
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	if err := s.withImmediateQueries(ctx, func(queries *dbsqlc.Queries) error {
		if err := createWorkspaceEventWithQueries(ctx, queries, &event); err != nil {
			return err
		}
		return createPendingDeliveriesForWorkspaceEvent(ctx, queries, event)
	}); err != nil {
		return WorkspaceEvent{}, err
	}
	return event, nil
}

func createPendingDeliveriesForWorkspaceEvent(ctx context.Context, queries *dbsqlc.Queries, event WorkspaceEvent) error {
	if strings.HasPrefix(event.EventType, "delivery.") {
		return nil
	}
	rows, err := queries.ListActiveEventSubscriptionsByWorkspace(ctx, dbsqlc.ListActiveEventSubscriptionsByWorkspaceParams{WorkspaceID: event.WorkspaceID})
	if err != nil {
		return fmt.Errorf("list active event subscriptions for %q: %w", event.WorkspaceID, err)
	}
	for _, row := range rows {
		subscription := eventSubscriptionFromSQLC(row)
		if strings.TrimSpace(subscription.DeliveryTargetType) == "" || strings.TrimSpace(subscription.DeliveryTargetID) == "" {
			continue
		}
		if subscription.TimeoutAt != nil && !subscription.TimeoutAt.After(event.CreatedAt) {
			continue
		}
		if event.Sequence <= subscription.CursorSequence {
			continue
		}
		filter, err := parseEventSubscriptionFilter(subscription.FilterJSON)
		if err != nil {
			return fmt.Errorf("parse event subscription filter %q: %w", subscription.SubscriptionID, err)
		}
		if !filter.matches(event) {
			continue
		}
		nextAttemptAt := event.CreatedAt
		if _, err := createPendingDeliveryWithQueries(ctx, queries, PendingDelivery{DeliveryID: pendingDeliveryIDForSubscriptionEvent(subscription.SubscriptionID, event.EventID), WorkspaceID: event.WorkspaceID, SubscriptionID: subscription.SubscriptionID, TargetType: subscription.DeliveryTargetType, TargetID: subscription.DeliveryTargetID, DeliveryPolicyJSON: subscription.DeliveryPolicyJSON, EventIDs: []string{event.EventID}, NextAttemptAt: &nextAttemptAt}); err != nil {
			return err
		}
	}
	return nil
}

func pendingDeliveryIDForSubscriptionEvent(subscriptionID, eventID string) string {
	return "pd-" + strings.TrimSpace(subscriptionID) + "-" + strings.TrimSpace(eventID)
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
	return workspaceEventsFromSQLC(rows), nil
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
	if err := s.sqlcQueries().CreateEventSubscription(ctx, dbsqlc.CreateEventSubscriptionParams{SubscriptionID: subscription.SubscriptionID, WorkspaceID: subscription.WorkspaceID, OwnerSessionID: subscription.OwnerSessionID, Name: subscription.Name, FilterJson: subscription.FilterJSON, DeliveryTargetType: subscription.DeliveryTargetType, DeliveryTargetID: subscription.DeliveryTargetID, DeliveryPolicyJson: subscription.DeliveryPolicyJSON, CursorSequence: subscription.CursorSequence, AckSequence: subscription.AckSequence, Status: subscription.Status, CompletionConditionJson: subscription.CompletionConditionJSON, TimeoutAt: timeoutAt, CreatedAt: subscription.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: subscription.UpdatedAt.UTC().Format(time.RFC3339Nano)}); err != nil {
		return EventSubscription{}, fmt.Errorf("create event subscription %q: %w", subscription.SubscriptionID, err)
	}
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
	return eventSubscriptionFromSQLC(row), nil
}

func (s *Store) ListEventSubscriptionEvents(ctx context.Context, subscriptionID string, limit int) ([]WorkspaceEvent, error) {
	subscription, err := s.GetEventSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	if subscription.Status != eventSubscriptionStatusActive {
		return []WorkspaceEvent{}, nil
	}
	filter, err := parseEventSubscriptionFilter(subscription.FilterJSON)
	if err != nil {
		return nil, fmt.Errorf("parse event subscription filter %q: %w", subscription.SubscriptionID, err)
	}
	if limit <= 0 {
		limit = 100
	}
	pageSize := limit * 4
	if pageSize < 100 {
		pageSize = 100
	}
	sequence := subscription.CursorSequence
	matched := make([]WorkspaceEvent, 0, limit)
	for len(matched) < limit {
		events, err := s.ListWorkspaceEventsAfterSequence(ctx, subscription.WorkspaceID, sequence, pageSize)
		if err != nil {
			return nil, err
		}
		if len(events) == 0 {
			break
		}
		for _, event := range events {
			sequence = event.Sequence
			if filter.matches(event) {
				matched = append(matched, event)
				if len(matched) == limit {
					break
				}
			}
		}
		if len(events) < pageSize {
			break
		}
	}
	return matched, nil
}

func (s *Store) AckEventSubscription(ctx context.Context, subscriptionID string, sequence int64) error {
	subscriptionID = strings.TrimSpace(subscriptionID)
	if subscriptionID == "" || sequence < 0 {
		return fmt.Errorf("%w: subscription id and non-negative sequence are required", ErrInvalidInput)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	rows, err := s.sqlcQueries().UpdateEventSubscriptionCursor(ctx, dbsqlc.UpdateEventSubscriptionCursorParams{CursorSequence: sequence, CursorSequence_2: sequence, AckSequence: sequence, MIN: sequence, CursorSequence_3: sequence, Column6: sequence, UpdatedAt: now, SubscriptionID: subscriptionID})
	if err != nil {
		return fmt.Errorf("ack event subscription %q: %w", subscriptionID, err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CancelEventSubscription(ctx context.Context, subscriptionID string) (EventSubscription, error) {
	subscriptionID = strings.TrimSpace(subscriptionID)
	if subscriptionID == "" {
		return EventSubscription{}, fmt.Errorf("%w: subscription id is required", ErrInvalidInput)
	}
	now := time.Now().UTC()
	formatted := now.Format(time.RFC3339Nano)
	if err := s.withImmediateQueries(ctx, func(queries *dbsqlc.Queries) error {
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
	return eventSubscriptionFromSQLC(row), nil
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
	return nil
}

func workspaceEventsFromSQLC(rows []dbsqlc.WorkspaceEvent) []WorkspaceEvent {
	events := make([]WorkspaceEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, workspaceEventFromSQLC(row))
	}
	return events
}

func workspaceEventFromSQLC(row dbsqlc.WorkspaceEvent) WorkspaceEvent {
	createdAt, _ := time.Parse(time.RFC3339Nano, row.CreatedAt)
	return WorkspaceEvent{EventID: row.EventID, WorkspaceID: row.WorkspaceID, Sequence: row.Sequence, EventType: row.EventType, SubjectType: row.SubjectType, SubjectID: row.SubjectID, ProducerType: row.ProducerType, ProducerID: row.ProducerID, CorrelationID: row.CorrelationID, CausationID: row.CausationID, PayloadJSON: row.PayloadJson, PayloadRefJSON: row.PayloadRefJson, AttentionRequired: row.AttentionRequired != 0, CreatedAt: createdAt}
}

func eventSubscriptionFromSQLC(row dbsqlc.EventSubscription) EventSubscription {
	createdAt, _ := time.Parse(time.RFC3339Nano, row.CreatedAt)
	updatedAt, _ := time.Parse(time.RFC3339Nano, row.UpdatedAt)
	var timeoutAt *time.Time
	if row.TimeoutAt != nil {
		parsed, _ := time.Parse(time.RFC3339Nano, *row.TimeoutAt)
		timeoutAt = &parsed
	}
	return EventSubscription{SubscriptionID: row.SubscriptionID, WorkspaceID: row.WorkspaceID, OwnerSessionID: row.OwnerSessionID, Name: row.Name, FilterJSON: row.FilterJson, DeliveryTargetType: row.DeliveryTargetType, DeliveryTargetID: row.DeliveryTargetID, DeliveryPolicyJSON: row.DeliveryPolicyJson, CursorSequence: row.CursorSequence, AckSequence: row.AckSequence, Status: row.Status, CompletionConditionJSON: row.CompletionConditionJson, TimeoutAt: timeoutAt, CreatedAt: createdAt, UpdatedAt: updatedAt}
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
