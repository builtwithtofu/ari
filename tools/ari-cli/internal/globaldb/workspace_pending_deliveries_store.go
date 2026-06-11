package globaldb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb/dbsqlc"
)

const (
	pendingDeliveryStatusPending   = "pending"
	pendingDeliveryStatusAttempted = "attempted"
	pendingDeliveryStatusCompleted = "completed"
	pendingDeliveryStatusFailed    = "failed"

	pendingDeliveryEventAttempted      = "delivery.attempted"
	pendingDeliveryEventCompleted      = "delivery.completed"
	pendingDeliveryEventFailed         = "delivery.failed"
	pendingDeliveryEventRetryScheduled = "delivery.retry_scheduled"

	pendingDeliverySubjectType  = "pending_delivery"
	pendingDeliveryProducerType = "daemon"
	pendingDeliveryProducerID   = "workspace_delivery_worker"
)

type PendingDelivery struct {
	DeliveryID         string
	WorkspaceID        string
	SubscriptionID     string
	TargetType         string
	TargetID           string
	DeliveryPolicyJSON string
	EventIDs           []string
	Status             string
	Attempts           int64
	NextAttemptAt      *time.Time
	DeadlineAt         *time.Time
	LastError          string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	TerminalAt         *time.Time
}

func (s *Store) CreatePendingDelivery(ctx context.Context, delivery PendingDelivery) (PendingDelivery, error) {
	return createPendingDeliveryWithQueries(ctx, s.sqlcQueries(), delivery)
}

func createPendingDeliveryWithQueries(ctx context.Context, queries *dbsqlc.Queries, delivery PendingDelivery) (PendingDelivery, error) {
	delivery = normalizePendingDelivery(delivery)
	if err := validatePendingDelivery(delivery); err != nil {
		return PendingDelivery{}, err
	}
	if delivery.DeliveryID == "" {
		delivery.DeliveryID = randomID("pd")
	}
	now := time.Now().UTC()
	if delivery.CreatedAt.IsZero() {
		delivery.CreatedAt = now
	}
	if delivery.UpdatedAt.IsZero() {
		delivery.UpdatedAt = delivery.CreatedAt
	}
	eventIDsJSON, err := json.Marshal(delivery.EventIDs)
	if err != nil {
		return PendingDelivery{}, fmt.Errorf("%w: pending delivery event ids are invalid", ErrInvalidInput)
	}
	if err := queries.CreatePendingDelivery(ctx, dbsqlc.CreatePendingDeliveryParams{DeliveryID: delivery.DeliveryID, WorkspaceID: delivery.WorkspaceID, SubscriptionID: delivery.SubscriptionID, TargetType: delivery.TargetType, TargetID: delivery.TargetID, DeliveryPolicyJson: delivery.DeliveryPolicyJSON, EventIdsJson: string(eventIDsJSON), Status: delivery.Status, Attempts: delivery.Attempts, NextAttemptAt: formatOptionalTime(delivery.NextAttemptAt), DeadlineAt: formatOptionalTime(delivery.DeadlineAt), LastError: delivery.LastError, CreatedAt: delivery.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: delivery.UpdatedAt.UTC().Format(time.RFC3339Nano), TerminalAt: formatOptionalTime(delivery.TerminalAt)}); err != nil {
		return PendingDelivery{}, fmt.Errorf("create pending delivery %q: %w", delivery.DeliveryID, err)
	}
	return delivery, nil
}

func (s *Store) GetPendingDelivery(ctx context.Context, deliveryID string) (PendingDelivery, error) {
	deliveryID = strings.TrimSpace(deliveryID)
	if deliveryID == "" {
		return PendingDelivery{}, fmt.Errorf("%w: delivery id is required", ErrInvalidInput)
	}
	row, err := s.sqlcQueries().GetPendingDelivery(ctx, dbsqlc.GetPendingDeliveryParams{DeliveryID: deliveryID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PendingDelivery{}, ErrNotFound
		}
		return PendingDelivery{}, fmt.Errorf("get pending delivery %q: %w", deliveryID, err)
	}
	return pendingDeliveryFromSQLC(row), nil
}

func (s *Store) ListDuePendingDeliveries(ctx context.Context, now time.Time, limit int) ([]PendingDelivery, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if err := s.failExpiredPendingDeliveries(ctx, now); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}
	formatted := now.UTC().Format(time.RFC3339Nano)
	rows, err := s.sqlcQueries().ListDuePendingDeliveries(ctx, dbsqlc.ListDuePendingDeliveriesParams{NextAttemptAt: &formatted, DeadlineAt: &formatted, Limit: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("list due pending deliveries: %w", err)
	}
	deliveries := make([]PendingDelivery, 0, len(rows))
	for _, row := range rows {
		deliveries = append(deliveries, pendingDeliveryFromSQLC(row))
	}
	return deliveries, nil
}

func (s *Store) failExpiredPendingDeliveries(ctx context.Context, now time.Time) error {
	formatted := now.UTC().Format(time.RFC3339Nano)
	rows, err := s.sqlcQueries().ListExpiredPendingDeliveries(ctx, dbsqlc.ListExpiredPendingDeliveriesParams{DeadlineAt: &formatted})
	if err != nil {
		return fmt.Errorf("list expired pending deliveries: %w", err)
	}
	for _, row := range rows {
		delivery := pendingDeliveryFromSQLC(row)
		if _, err := s.FailPendingDelivery(ctx, delivery.DeliveryID, "delivery deadline exceeded"); err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
	}
	return nil
}

func (s *Store) RecordPendingDeliveryAttempt(ctx context.Context, deliveryID string, nextAttemptAt *time.Time, lastError string) (PendingDelivery, error) {
	deliveryID = strings.TrimSpace(deliveryID)
	if deliveryID == "" {
		return PendingDelivery{}, fmt.Errorf("%w: delivery id is required", ErrInvalidInput)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	rows, err := s.sqlcQueries().RecordPendingDeliveryAttempt(ctx, dbsqlc.RecordPendingDeliveryAttemptParams{NextAttemptAt: formatOptionalTime(nextAttemptAt), LastError: strings.TrimSpace(lastError), UpdatedAt: now, DeliveryID: deliveryID})
	if err != nil {
		return PendingDelivery{}, fmt.Errorf("record pending delivery attempt %q: %w", deliveryID, err)
	}
	if rows == 0 {
		return PendingDelivery{}, ErrNotFound
	}
	return s.GetPendingDelivery(ctx, deliveryID)
}

func (s *Store) ClaimDuePendingDeliveryAttempt(ctx context.Context, deliveryID string, now time.Time) (PendingDelivery, error) {
	deliveryID = strings.TrimSpace(deliveryID)
	if deliveryID == "" {
		return PendingDelivery{}, fmt.Errorf("%w: delivery id is required", ErrInvalidInput)
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	formatted := now.UTC().Format(time.RFC3339Nano)
	var delivery PendingDelivery
	if err := s.withImmediateQueries(ctx, func(queries *dbsqlc.Queries) error {
		rows, err := queries.ClaimDuePendingDeliveryAttempt(ctx, dbsqlc.ClaimDuePendingDeliveryAttemptParams{UpdatedAt: formatted, DeliveryID: deliveryID, NextAttemptAt: &formatted, DeadlineAt: &formatted})
		if err != nil {
			return fmt.Errorf("claim pending delivery attempt %q: %w", deliveryID, err)
		}
		if rows == 0 {
			return ErrNotFound
		}
		row, err := queries.GetPendingDelivery(ctx, dbsqlc.GetPendingDeliveryParams{DeliveryID: deliveryID})
		if err != nil {
			return fmt.Errorf("get claimed pending delivery %q: %w", deliveryID, err)
		}
		delivery = pendingDeliveryFromSQLC(row)
		return appendPendingDeliveryEventWithQueries(ctx, queries, delivery, pendingDeliveryEventAttempted, "attempted", "", nil)
	}); err != nil {
		return PendingDelivery{}, err
	}
	return delivery, nil
}

func (s *Store) SchedulePendingDeliveryRetry(ctx context.Context, deliveryID string, nextAttemptAt time.Time, lastError string) (PendingDelivery, error) {
	deliveryID = strings.TrimSpace(deliveryID)
	if deliveryID == "" || nextAttemptAt.IsZero() {
		return PendingDelivery{}, fmt.Errorf("%w: delivery id and next attempt time are required", ErrInvalidInput)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	formattedNextAttempt := nextAttemptAt.UTC().Format(time.RFC3339Nano)
	var delivery PendingDelivery
	if err := s.withImmediateQueries(ctx, func(queries *dbsqlc.Queries) error {
		rows, err := queries.SchedulePendingDeliveryRetry(ctx, dbsqlc.SchedulePendingDeliveryRetryParams{NextAttemptAt: &formattedNextAttempt, LastError: strings.TrimSpace(lastError), UpdatedAt: now, DeliveryID: deliveryID})
		if err != nil {
			return fmt.Errorf("schedule pending delivery retry %q: %w", deliveryID, err)
		}
		if rows == 0 {
			return ErrNotFound
		}
		row, err := queries.GetPendingDelivery(ctx, dbsqlc.GetPendingDeliveryParams{DeliveryID: deliveryID})
		if err != nil {
			return fmt.Errorf("get retried pending delivery %q: %w", deliveryID, err)
		}
		delivery = pendingDeliveryFromSQLC(row)
		return appendPendingDeliveryEventWithQueries(ctx, queries, delivery, pendingDeliveryEventRetryScheduled, "retry", delivery.LastError, delivery.NextAttemptAt)
	}); err != nil {
		return PendingDelivery{}, err
	}
	return delivery, nil
}

func (s *Store) CompletePendingDelivery(ctx context.Context, deliveryID string) (PendingDelivery, error) {
	deliveryID = strings.TrimSpace(deliveryID)
	if deliveryID == "" {
		return PendingDelivery{}, fmt.Errorf("%w: delivery id is required", ErrInvalidInput)
	}
	var delivery PendingDelivery
	if err := s.withImmediateQueries(ctx, func(queries *dbsqlc.Queries) error {
		now := time.Now().UTC()
		formatted := now.Format(time.RFC3339Nano)
		rows, err := queries.CompletePendingDelivery(ctx, dbsqlc.CompletePendingDeliveryParams{UpdatedAt: formatted, TerminalAt: &formatted, DeliveryID: deliveryID})
		if err != nil {
			return fmt.Errorf("complete pending delivery %q: %w", deliveryID, err)
		}
		if rows == 0 {
			return ErrNotFound
		}
		row, err := queries.GetPendingDelivery(ctx, dbsqlc.GetPendingDeliveryParams{DeliveryID: deliveryID})
		if err != nil {
			return fmt.Errorf("get completed pending delivery %q: %w", deliveryID, err)
		}
		delivery = pendingDeliveryFromSQLC(row)
		if err := ackSubscriptionForCompletedDelivery(ctx, queries, delivery, formatted); err != nil {
			return err
		}
		return appendPendingDeliveryEventWithQueries(ctx, queries, delivery, pendingDeliveryEventCompleted, "completed", "", nil)
	}); err != nil {
		return PendingDelivery{}, err
	}
	return delivery, nil
}

func ackSubscriptionForCompletedDelivery(ctx context.Context, queries *dbsqlc.Queries, delivery PendingDelivery, updatedAt string) error {
	subscription, err := subscriptionByIDWithQueries(ctx, queries, delivery.SubscriptionID)
	if err != nil {
		return err
	}
	filter, err := parseEventSubscriptionFilter(subscription.FilterJSON)
	if err != nil {
		return fmt.Errorf("parse event subscription filter %q: %w", subscription.SubscriptionID, err)
	}
	sequence := subscription.CursorSequence
	ackSequence := subscription.CursorSequence
	for {
		events, err := listWorkspaceEventsAfterSequenceWithQueries(ctx, queries, subscription.WorkspaceID, sequence, 100)
		if err != nil {
			return err
		}
		if len(events) == 0 {
			break
		}
		for _, event := range events {
			sequence = event.Sequence
			if !filter.matches(event) {
				continue
			}
			completed, err := pendingDeliveryForSubscriptionEventIsCompleted(ctx, queries, subscription.SubscriptionID, event.EventID)
			if err != nil {
				return err
			}
			if !completed {
				goto updateCursor
			}
			ackSequence = event.Sequence
		}
		if len(events) < 100 {
			break
		}
	}

updateCursor:
	if ackSequence == subscription.CursorSequence {
		return nil
	}
	rows, err := queries.UpdateEventSubscriptionCursor(ctx, dbsqlc.UpdateEventSubscriptionCursorParams{CursorSequence: ackSequence, CursorSequence_2: ackSequence, AckSequence: ackSequence, MIN: ackSequence, CursorSequence_3: ackSequence, Column6: ackSequence, UpdatedAt: updatedAt, SubscriptionID: delivery.SubscriptionID})
	if err != nil {
		return fmt.Errorf("ack event subscription %q for delivery %q: %w", delivery.SubscriptionID, delivery.DeliveryID, err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func pendingDeliveryForSubscriptionEventIsCompleted(ctx context.Context, queries *dbsqlc.Queries, subscriptionID, eventID string) (bool, error) {
	deliveryID := pendingDeliveryIDForSubscriptionEvent(subscriptionID, eventID)
	row, err := queries.GetPendingDelivery(ctx, dbsqlc.GetPendingDeliveryParams{DeliveryID: deliveryID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("get pending delivery %q: %w", deliveryID, err)
	}
	return row.Status == pendingDeliveryStatusCompleted, nil
}

func listWorkspaceEventsAfterSequenceWithQueries(ctx context.Context, queries *dbsqlc.Queries, workspaceID string, afterSequence int64, limit int) ([]WorkspaceEvent, error) {
	rows, err := queries.ListWorkspaceEventsAfterSequence(ctx, dbsqlc.ListWorkspaceEventsAfterSequenceParams{WorkspaceID: workspaceID, Sequence: afterSequence, Limit: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("list workspace events for %q after %d: %w", workspaceID, afterSequence, err)
	}
	return workspaceEventsFromSQLC(rows), nil
}

func failPendingDeliveriesForSubscriptionWithQueries(ctx context.Context, queries *dbsqlc.Queries, subscriptionID, lastError string, now time.Time) error {
	rows, err := queries.ListPendingDeliveriesForSubscription(ctx, dbsqlc.ListPendingDeliveriesForSubscriptionParams{SubscriptionID: subscriptionID})
	if err != nil {
		return fmt.Errorf("list pending deliveries for subscription %q: %w", subscriptionID, err)
	}
	formatted := now.UTC().Format(time.RFC3339Nano)
	for _, row := range rows {
		delivery := pendingDeliveryFromSQLC(row)
		if err := failPendingDeliveryWithQueries(ctx, queries, delivery.DeliveryID, lastError, formatted); err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
	}
	return nil
}

func failPendingDeliveryWithQueries(ctx context.Context, queries *dbsqlc.Queries, deliveryID, lastError, formattedNow string) error {
	rows, err := queries.FailPendingDelivery(ctx, dbsqlc.FailPendingDeliveryParams{LastError: strings.TrimSpace(lastError), UpdatedAt: formattedNow, TerminalAt: &formattedNow, DeliveryID: deliveryID})
	if err != nil {
		return fmt.Errorf("fail pending delivery %q: %w", deliveryID, err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	row, err := queries.GetPendingDelivery(ctx, dbsqlc.GetPendingDeliveryParams{DeliveryID: deliveryID})
	if err != nil {
		return fmt.Errorf("get failed pending delivery %q: %w", deliveryID, err)
	}
	delivery := pendingDeliveryFromSQLC(row)
	return appendPendingDeliveryEventWithQueries(ctx, queries, delivery, pendingDeliveryEventFailed, "failed", delivery.LastError, nil)
}

func (s *Store) FailPendingDelivery(ctx context.Context, deliveryID, lastError string) (PendingDelivery, error) {
	deliveryID = strings.TrimSpace(deliveryID)
	if deliveryID == "" {
		return PendingDelivery{}, fmt.Errorf("%w: delivery id is required", ErrInvalidInput)
	}
	now := time.Now().UTC()
	formatted := now.Format(time.RFC3339Nano)
	var delivery PendingDelivery
	if err := s.withImmediateQueries(ctx, func(queries *dbsqlc.Queries) error {
		rows, err := queries.FailPendingDelivery(ctx, dbsqlc.FailPendingDeliveryParams{LastError: strings.TrimSpace(lastError), UpdatedAt: formatted, TerminalAt: &formatted, DeliveryID: deliveryID})
		if err != nil {
			return fmt.Errorf("fail pending delivery %q: %w", deliveryID, err)
		}
		if rows == 0 {
			return ErrNotFound
		}
		row, err := queries.GetPendingDelivery(ctx, dbsqlc.GetPendingDeliveryParams{DeliveryID: deliveryID})
		if err != nil {
			return fmt.Errorf("get failed pending delivery %q: %w", deliveryID, err)
		}
		delivery = pendingDeliveryFromSQLC(row)
		return appendPendingDeliveryEventWithQueries(ctx, queries, delivery, pendingDeliveryEventFailed, "failed", delivery.LastError, nil)
	}); err != nil {
		return PendingDelivery{}, err
	}
	return delivery, nil
}

// appendPendingDeliveryEventWithQueries records a delivery lifecycle fact in
// workspace event history inside the same transaction as the delivery state
// change, so delivery rows and delivery.* events can never diverge.
func appendPendingDeliveryEventWithQueries(ctx context.Context, queries *dbsqlc.Queries, delivery PendingDelivery, eventType, status, lastError string, nextAttemptAt *time.Time) error {
	payload := map[string]string{
		"delivery_id":     delivery.DeliveryID,
		"subscription_id": delivery.SubscriptionID,
		"target_type":     delivery.TargetType,
		"target_id":       delivery.TargetID,
		"status":          status,
		"attempts":        fmt.Sprintf("%d", delivery.Attempts),
	}
	if len(delivery.EventIDs) > 0 {
		payload["event_ids"] = strings.Join(delivery.EventIDs, ",")
	}
	if lastError = strings.TrimSpace(lastError); lastError != "" {
		payload["last_error"] = lastError
	}
	if nextAttemptAt != nil && !nextAttemptAt.IsZero() {
		payload["next_attempt_at"] = nextAttemptAt.UTC().Format(time.RFC3339Nano)
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal delivery event payload for %q: %w", delivery.DeliveryID, err)
	}
	causationID := ""
	if len(delivery.EventIDs) > 0 {
		causationID = delivery.EventIDs[0]
	}
	event := normalizeWorkspaceEvent(WorkspaceEvent{
		EventID:           newWorkspaceEventID(),
		WorkspaceID:       delivery.WorkspaceID,
		EventType:         eventType,
		SubjectType:       pendingDeliverySubjectType,
		SubjectID:         delivery.DeliveryID,
		ProducerType:      pendingDeliveryProducerType,
		ProducerID:        pendingDeliveryProducerID,
		CorrelationID:     delivery.SubscriptionID,
		CausationID:       causationID,
		PayloadJSON:       string(payloadJSON),
		AttentionRequired: eventType == pendingDeliveryEventFailed,
	})
	if err := validateWorkspaceEvent(event); err != nil {
		return err
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	if err := createWorkspaceEventWithQueries(ctx, queries, &event); err != nil {
		return err
	}
	return createPendingDeliveriesForWorkspaceEvent(ctx, queries, event)
}

func normalizePendingDelivery(delivery PendingDelivery) PendingDelivery {
	delivery.DeliveryID = strings.TrimSpace(delivery.DeliveryID)
	delivery.WorkspaceID = strings.TrimSpace(delivery.WorkspaceID)
	delivery.SubscriptionID = strings.TrimSpace(delivery.SubscriptionID)
	delivery.TargetType = strings.TrimSpace(delivery.TargetType)
	delivery.TargetID = strings.TrimSpace(delivery.TargetID)
	delivery.DeliveryPolicyJSON = defaultJSON(delivery.DeliveryPolicyJSON, "{}")
	delivery.Status = strings.TrimSpace(delivery.Status)
	if delivery.Status == "" {
		delivery.Status = pendingDeliveryStatusPending
	}
	delivery.LastError = strings.TrimSpace(delivery.LastError)
	out := make([]string, 0, len(delivery.EventIDs))
	for _, eventID := range delivery.EventIDs {
		if eventID = strings.TrimSpace(eventID); eventID != "" {
			out = append(out, eventID)
		}
	}
	delivery.EventIDs = out
	return delivery
}

func validatePendingDelivery(delivery PendingDelivery) error {
	if delivery.WorkspaceID == "" || delivery.SubscriptionID == "" || delivery.TargetType == "" || delivery.TargetID == "" {
		return fmt.Errorf("%w: pending delivery required field is missing", ErrInvalidInput)
	}
	if len(delivery.EventIDs) == 0 {
		return fmt.Errorf("%w: pending delivery event ids are required", ErrInvalidInput)
	}
	if delivery.Attempts < 0 {
		return fmt.Errorf("%w: pending delivery attempts must not be negative", ErrInvalidInput)
	}
	if !json.Valid([]byte(delivery.DeliveryPolicyJSON)) {
		return fmt.Errorf("%w: pending delivery policy json is invalid", ErrInvalidInput)
	}
	return nil
}

func pendingDeliveryFromSQLC(row dbsqlc.PendingDelivery) PendingDelivery {
	createdAt, _ := time.Parse(time.RFC3339Nano, row.CreatedAt)
	updatedAt, _ := time.Parse(time.RFC3339Nano, row.UpdatedAt)
	var eventIDs []string
	_ = json.Unmarshal([]byte(row.EventIdsJson), &eventIDs)
	return PendingDelivery{DeliveryID: row.DeliveryID, WorkspaceID: row.WorkspaceID, SubscriptionID: row.SubscriptionID, TargetType: row.TargetType, TargetID: row.TargetID, DeliveryPolicyJSON: row.DeliveryPolicyJson, EventIDs: eventIDs, Status: row.Status, Attempts: row.Attempts, NextAttemptAt: parseOptionalTime(row.NextAttemptAt), DeadlineAt: parseOptionalTime(row.DeadlineAt), LastError: row.LastError, CreatedAt: createdAt, UpdatedAt: updatedAt, TerminalAt: parseOptionalTime(row.TerminalAt)}
}

func formatOptionalTime(value *time.Time) *string {
	if value == nil || value.IsZero() {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339Nano)
	return &formatted
}

func parseOptionalTime(value *string) *time.Time {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, *value)
	if err != nil {
		return nil
	}
	return &parsed
}
