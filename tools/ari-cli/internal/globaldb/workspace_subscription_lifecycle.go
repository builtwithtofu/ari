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
	defaultSubscriptionEventScanPageSize = 100
	defaultEventSubscriptionPollInterval = 10 * time.Millisecond

	EventSubscriptionWaitStatusReady   = "ready"
	EventSubscriptionWaitStatusPartial = "partial"
	EventSubscriptionWaitStatusTimeout = "timeout"
)

type EventSubscriptionReadRequest struct {
	SubscriptionID string
	Limit          int
	MinEvents      int
}

type EventSubscriptionWaitRequest struct {
	SubscriptionID string
	Limit          int
	MinEvents      int
	Timeout        time.Duration
	PollInterval   time.Duration
}

type EventSubscriptionWaitOptions struct {
	Limit        int
	MinEvents    int
	Timeout      time.Duration
	PollInterval time.Duration
}

type EventSubscriptionReadResult struct {
	Subscription EventSubscription
	Events       []WorkspaceEvent
	Completion   EventSubscriptionCompletion
}

type EventSubscriptionCompletion struct {
	Configured   bool
	Satisfied    bool
	TimedOut     bool
	Status       string
	MatchedCount int
	Required     int
}

type EventSubscriptionCompletionCondition struct {
	Mode               string   `json:"mode,omitempty"`
	MinEvents          int      `json:"min_events,omitempty"`
	SubjectIDs         []string `json:"subject_ids,omitempty"`
	TerminalEventTypes []string `json:"terminal_event_types,omitempty"`
}

type eventSubscriptionLifecycle struct {
	queries *dbsqlc.Queries
}

type compiledEventSubscription struct {
	EventSubscription
	filter     EventSubscriptionFilter
	completion EventSubscriptionCompletionCondition
}

type eventSubscriptionMatchOptions struct {
	skipDeliveryEvents bool
	applyCursor        bool
	applyTimeout       bool
}

func newEventSubscriptionLifecycle(queries *dbsqlc.Queries) eventSubscriptionLifecycle {
	return eventSubscriptionLifecycle{queries: queries}
}

func (s *Store) ReadEventSubscription(ctx context.Context, req EventSubscriptionReadRequest) (EventSubscriptionReadResult, error) {
	if req.MinEvents < 0 {
		return EventSubscriptionReadResult{}, fmt.Errorf("%w: min events must not be negative", ErrInvalidInput)
	}
	subscription, err := s.GetEventSubscription(ctx, req.SubscriptionID)
	if err != nil {
		return EventSubscriptionReadResult{}, err
	}
	return newEventSubscriptionLifecycle(s.sqlcQueries()).read(ctx, subscription, eventSubscriptionReadOptions{limit: req.Limit, minEvents: req.MinEvents})
}

func (s *Store) WaitEventSubscription(ctx context.Context, req EventSubscriptionWaitRequest) (EventSubscriptionReadResult, error) {
	if req.MinEvents < 0 || req.Timeout < 0 || req.PollInterval < 0 {
		return EventSubscriptionReadResult{}, fmt.Errorf("%w: event subscription wait values must not be negative", ErrInvalidInput)
	}
	subscription, err := s.GetEventSubscription(ctx, req.SubscriptionID)
	if err != nil {
		return EventSubscriptionReadResult{}, err
	}
	return s.waitEventSubscription(ctx, subscription, EventSubscriptionWaitOptions{Limit: req.Limit, MinEvents: req.MinEvents, Timeout: req.Timeout, PollInterval: req.PollInterval})
}

func (s *Store) WaitEventSubscriptionCondition(ctx context.Context, subscription EventSubscription, options EventSubscriptionWaitOptions) (EventSubscriptionReadResult, error) {
	if options.MinEvents < 0 || options.Timeout < 0 || options.PollInterval < 0 {
		return EventSubscriptionReadResult{}, fmt.Errorf("%w: event subscription wait values must not be negative", ErrInvalidInput)
	}
	subscription = normalizeEventSubscription(subscription)
	if err := validateEventSubscription(subscription); err != nil {
		return EventSubscriptionReadResult{}, err
	}
	return s.waitEventSubscription(ctx, subscription, options)
}

func (s *Store) waitEventSubscription(ctx context.Context, subscription EventSubscription, options EventSubscriptionWaitOptions) (EventSubscriptionReadResult, error) {
	lifecycle := newEventSubscriptionLifecycle(s.sqlcQueries())
	readOptions := eventSubscriptionReadOptions{limit: options.Limit, minEvents: options.MinEvents}
	read := func(timedOut bool) (EventSubscriptionReadResult, error) {
		result, err := lifecycle.read(ctx, subscription, readOptions)
		if err != nil {
			return EventSubscriptionReadResult{}, err
		}
		if timedOut && result.Completion.Configured && !result.Completion.Satisfied {
			result.Completion = result.Completion.withTimeout()
		}
		return result, nil
	}
	result, err := read(false)
	if err != nil {
		return EventSubscriptionReadResult{}, err
	}
	if !result.Completion.Configured || result.Completion.Satisfied || options.Timeout <= 0 {
		return result, nil
	}
	pollInterval := options.PollInterval
	if pollInterval <= 0 {
		pollInterval = defaultEventSubscriptionPollInterval
	}
	deadline := time.Now().Add(options.Timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return read(true)
		}
		if remaining < pollInterval {
			pollInterval = remaining
		}
		select {
		case <-ctx.Done():
			return EventSubscriptionReadResult{}, ctx.Err()
		case <-time.After(pollInterval):
		}
		result, err = read(false)
		if err != nil {
			return EventSubscriptionReadResult{}, err
		}
		if result.Completion.Satisfied {
			return result, nil
		}
	}
}

func (l eventSubscriptionLifecycle) createPendingDeliveriesForEvent(ctx context.Context, event WorkspaceEvent) error {
	if workspaceEventSkipsDeliveryFanout(event) {
		return nil
	}
	rows, err := l.queries.ListActiveEventSubscriptionsByWorkspace(ctx, dbsqlc.ListActiveEventSubscriptionsByWorkspaceParams{WorkspaceID: event.WorkspaceID})
	if err != nil {
		return fmt.Errorf("list active event subscriptions for %q: %w", event.WorkspaceID, err)
	}
	for _, row := range rows {
		subscription, err := eventSubscriptionFromSQLC(row)
		if err != nil {
			return err
		}
		compiled, err := compileEventSubscription(subscription)
		if err != nil {
			return err
		}
		if !compiled.hasDeliveryTarget() {
			continue
		}
		if !compiled.matchesEvent(event, eventSubscriptionMatchOptions{skipDeliveryEvents: true, applyCursor: true, applyTimeout: true}) {
			continue
		}
		if err := l.createPendingDeliveryForEvent(ctx, compiled.EventSubscription, event); err != nil {
			return err
		}
	}
	return nil
}

func (l eventSubscriptionLifecycle) backfillPendingDeliveries(ctx context.Context, subscription EventSubscription) error {
	compiled, err := compileEventSubscription(subscription)
	if err != nil {
		return err
	}
	if compiled.Status != eventSubscriptionStatusActive || !compiled.hasDeliveryTarget() {
		return nil
	}
	sequence := compiled.CursorSequence
	for {
		events, err := listWorkspaceEventsAfterSequenceWithQueries(ctx, l.queries, compiled.WorkspaceID, sequence, defaultSubscriptionEventScanPageSize)
		if err != nil {
			return err
		}
		if len(events) == 0 {
			return nil
		}
		for _, event := range events {
			sequence = event.Sequence
			if !compiled.matchesEvent(event, eventSubscriptionMatchOptions{skipDeliveryEvents: true, applyTimeout: true}) {
				continue
			}
			if err := l.createPendingDeliveryForEvent(ctx, compiled.EventSubscription, event); err != nil {
				return err
			}
		}
		if len(events) < defaultSubscriptionEventScanPageSize {
			return nil
		}
	}
}

type eventSubscriptionReadOptions struct {
	limit     int
	minEvents int
}

func (l eventSubscriptionLifecycle) read(ctx context.Context, subscription EventSubscription, options eventSubscriptionReadOptions) (EventSubscriptionReadResult, error) {
	compiled, err := compileEventSubscription(subscription)
	if err != nil {
		return EventSubscriptionReadResult{}, err
	}
	completion := compiled.completion
	if options.minEvents > 0 {
		completion = EventSubscriptionCompletionCondition{Mode: "count", MinEvents: options.minEvents}
	}
	limit := eventSubscriptionReadLimit(options.limit, options.minEvents, completion)
	events, err := l.listUnreadEvents(ctx, compiled, limit, completion)
	if err != nil {
		return EventSubscriptionReadResult{}, err
	}
	return EventSubscriptionReadResult{Subscription: compiled.EventSubscription, Events: events, Completion: completion.evaluate(events, false)}, nil
}

func eventSubscriptionReadLimit(limit, minEvents int, completion EventSubscriptionCompletionCondition) int {
	if minEvents > limit {
		limit = minEvents
	}
	if required := completion.requiredEventLimit(); required > limit {
		limit = required
	}
	if limit <= 0 {
		limit = 100
	}
	return limit
}

func (l eventSubscriptionLifecycle) listUnreadEvents(ctx context.Context, compiled compiledEventSubscription, limit int, completion EventSubscriptionCompletionCondition) ([]WorkspaceEvent, error) {
	if compiled.Status == eventSubscriptionStatusCanceled {
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
	sequence := compiled.CursorSequence
	matched := make([]WorkspaceEvent, 0, limit)
	for {
		events, err := listWorkspaceEventsAfterSequenceWithQueries(ctx, l.queries, compiled.WorkspaceID, sequence, pageSize)
		if err != nil {
			return nil, err
		}
		if len(events) == 0 {
			break
		}
		stop := false
		for _, event := range events {
			sequence = event.Sequence
			if !compiled.matchesEvent(event, eventSubscriptionMatchOptions{applyTimeout: true}) {
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

func (l eventSubscriptionLifecycle) ackCompletedDelivery(ctx context.Context, delivery PendingDelivery, updatedAt time.Time) error {
	subscription, err := subscriptionByIDWithQueries(ctx, l.queries, delivery.SubscriptionID)
	if err != nil {
		return err
	}
	compiled, err := compileEventSubscription(subscription)
	if err != nil {
		return err
	}
	completedByCurrentDelivery := make(map[string]struct{}, len(delivery.EventIDs))
	for _, eventID := range delivery.EventIDs {
		completedByCurrentDelivery[strings.TrimSpace(eventID)] = struct{}{}
	}
	sequence := compiled.CursorSequence
	ackSequence := compiled.CursorSequence
	for {
		events, err := listWorkspaceEventsAfterSequenceWithQueries(ctx, l.queries, compiled.WorkspaceID, sequence, defaultSubscriptionEventScanPageSize)
		if err != nil {
			return err
		}
		if len(events) == 0 {
			break
		}
		for _, event := range events {
			sequence = event.Sequence
			if !compiled.matchesEvent(event, eventSubscriptionMatchOptions{skipDeliveryEvents: true}) {
				continue
			}
			_, completed := completedByCurrentDelivery[event.EventID]
			if !completed {
				completed, err = pendingDeliveryForSubscriptionEventIsCompleted(ctx, l.queries, compiled.SubscriptionID, event.EventID)
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
	if ackSequence == compiled.CursorSequence {
		return nil
	}
	if err := l.ackReadCursor(ctx, delivery.SubscriptionID, ackSequence, updatedAt); err != nil {
		return fmt.Errorf("ack event subscription %q for delivery %q: %w", delivery.SubscriptionID, delivery.DeliveryID, err)
	}
	return nil
}

func (l eventSubscriptionLifecycle) ackReadCursor(ctx context.Context, subscriptionID string, sequence int64, updatedAt time.Time) error {
	subscriptionID = strings.TrimSpace(subscriptionID)
	if subscriptionID == "" || sequence < 0 {
		return fmt.Errorf("%w: subscription id and non-negative sequence are required", ErrInvalidInput)
	}
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	formatted := updatedAt.UTC().Format(time.RFC3339Nano)
	rows, err := l.queries.UpdateEventSubscriptionCursor(ctx, dbsqlc.UpdateEventSubscriptionCursorParams{CursorSequence: sequence, CursorSequence_2: sequence, AckSequence: sequence, MIN: sequence, CursorSequence_3: sequence, Column6: sequence, UpdatedAt: formatted, SubscriptionID: subscriptionID})
	if err != nil {
		return fmt.Errorf("ack event subscription %q: %w", subscriptionID, err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func compileEventSubscription(subscription EventSubscription) (compiledEventSubscription, error) {
	filter, err := parseEventSubscriptionFilter(subscription.FilterJSON)
	if err != nil {
		return compiledEventSubscription{}, fmt.Errorf("parse event subscription filter %q: %w", subscription.SubscriptionID, err)
	}
	completion, err := ParseEventSubscriptionCompletionCondition(subscription.CompletionConditionJSON)
	if err != nil {
		return compiledEventSubscription{}, fmt.Errorf("parse event subscription completion condition %q: %w", subscription.SubscriptionID, err)
	}
	return compiledEventSubscription{EventSubscription: subscription, filter: filter, completion: completion}, nil
}

func (subscription compiledEventSubscription) hasDeliveryTarget() bool {
	return strings.TrimSpace(subscription.DeliveryTargetType) != "" && strings.TrimSpace(subscription.DeliveryTargetID) != ""
}

func (subscription compiledEventSubscription) matchesEvent(event WorkspaceEvent, options eventSubscriptionMatchOptions) bool {
	if options.skipDeliveryEvents && workspaceEventSkipsDeliveryFanout(event) {
		return false
	}
	if options.applyCursor && event.Sequence <= subscription.CursorSequence {
		return false
	}
	if options.applyTimeout && subscription.TimeoutAt != nil && !subscription.TimeoutAt.After(event.CreatedAt) {
		return false
	}
	return subscription.filter.matches(event)
}

func (l eventSubscriptionLifecycle) createPendingDeliveryForEvent(ctx context.Context, subscription EventSubscription, event WorkspaceEvent) error {
	nextAttemptAt := event.CreatedAt
	_, err := createPendingDeliveryWithQueries(ctx, l.queries, PendingDelivery{DeliveryID: pendingDeliveryIDForSubscriptionEvent(subscription.SubscriptionID, event.EventID), WorkspaceID: event.WorkspaceID, SubscriptionID: subscription.SubscriptionID, TargetType: subscription.DeliveryTargetType, TargetID: subscription.DeliveryTargetID, DeliveryPolicyJSON: subscription.DeliveryPolicyJSON, EventIDs: []string{event.EventID}, NextAttemptAt: &nextAttemptAt})
	return err
}

func pendingDeliveryIDForSubscriptionEvent(subscriptionID, eventID string) string {
	return "pd-" + strings.TrimSpace(subscriptionID) + "-" + strings.TrimSpace(eventID)
}

func workspaceEventSkipsDeliveryFanout(event WorkspaceEvent) bool {
	return strings.HasPrefix(strings.TrimSpace(event.EventType), "delivery.")
}

func pendingDeliveryForSubscriptionEventIsCompleted(ctx context.Context, queries *dbsqlc.Queries, subscriptionID, eventID string) (bool, error) {
	deliveryID := pendingDeliveryIDForSubscriptionEvent(subscriptionID, eventID)
	row, err := queries.GetPendingDelivery(ctx, dbsqlc.GetPendingDeliveryParams{DeliveryID: deliveryID})
	if err == nil && row.Status == pendingDeliveryStatusCompleted {
		return true, nil
	}
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return false, fmt.Errorf("get pending delivery %q: %w", deliveryID, err)
		}
	}
	rows, listErr := queries.ListCompletedPendingDeliveriesForSubscription(ctx, dbsqlc.ListCompletedPendingDeliveriesForSubscriptionParams{SubscriptionID: subscriptionID})
	if listErr != nil {
		return false, fmt.Errorf("list completed pending deliveries for subscription %q: %w", subscriptionID, listErr)
	}
	for _, candidate := range rows {
		delivery := pendingDeliveryFromSQLC(candidate)
		for _, candidateEventID := range delivery.EventIDs {
			if strings.TrimSpace(candidateEventID) == strings.TrimSpace(eventID) {
				return true, nil
			}
		}
	}
	return false, nil
}

func ParseEventSubscriptionCompletionCondition(raw string) (EventSubscriptionCompletionCondition, error) {
	var condition EventSubscriptionCompletionCondition
	if strings.TrimSpace(raw) == "" {
		return condition, nil
	}
	if err := json.Unmarshal([]byte(raw), &condition); err != nil {
		return EventSubscriptionCompletionCondition{}, err
	}
	condition.Mode = strings.TrimSpace(condition.Mode)
	condition.SubjectIDs = normalizeStringSet(condition.SubjectIDs)
	condition.TerminalEventTypes = normalizeStringSet(condition.TerminalEventTypes)
	if condition.MinEvents < 0 {
		return EventSubscriptionCompletionCondition{}, fmt.Errorf("%w: completion min_events must not be negative", ErrInvalidInput)
	}
	switch condition.Mode {
	case "", "each", "stream":
		condition.Mode = ""
		condition.MinEvents = 0
		condition.SubjectIDs = nil
		condition.TerminalEventTypes = nil
		return condition, nil
	case "count":
		if condition.MinEvents <= 0 {
			return EventSubscriptionCompletionCondition{}, fmt.Errorf("%w: count completion requires min_events", ErrInvalidInput)
		}
		condition.SubjectIDs = nil
		return condition, nil
	case "any", "all":
		if len(condition.SubjectIDs) == 0 {
			return EventSubscriptionCompletionCondition{}, fmt.Errorf("%w: %s completion requires subject_ids", ErrInvalidInput, condition.Mode)
		}
		condition.MinEvents = 0
		return condition, nil
	default:
		return EventSubscriptionCompletionCondition{}, fmt.Errorf("%w: unsupported completion mode %q", ErrInvalidInput, condition.Mode)
	}
}

func (condition EventSubscriptionCompletionCondition) Configured() bool {
	return condition.Mode != ""
}

func (condition EventSubscriptionCompletionCondition) requiredEventLimit() int {
	switch condition.Mode {
	case "count":
		return condition.MinEvents
	case "any":
		return 1
	case "all":
		return len(condition.SubjectIDs)
	default:
		return 0
	}
}

func (condition EventSubscriptionCompletionCondition) requiresSubjectScan() bool {
	return condition.Mode == "any" || condition.Mode == "all"
}

func (condition EventSubscriptionCompletionCondition) evaluate(events []WorkspaceEvent, timedOut bool) EventSubscriptionCompletion {
	if !condition.Configured() {
		return EventSubscriptionCompletion{}
	}
	matched, required := condition.progress(events)
	satisfied := required > 0 && matched >= required
	status := EventSubscriptionWaitStatusPartial
	if satisfied {
		status = EventSubscriptionWaitStatusReady
	} else if timedOut && matched == 0 {
		status = EventSubscriptionWaitStatusTimeout
	}
	return EventSubscriptionCompletion{Configured: true, Satisfied: satisfied, TimedOut: timedOut && !satisfied, Status: status, MatchedCount: matched, Required: required}
}

func (condition EventSubscriptionCompletionCondition) progress(events []WorkspaceEvent) (int, int) {
	switch condition.Mode {
	case "count":
		return len(events), condition.MinEvents
	case "any":
		return condition.matchedSubjectCount(events), 1
	case "all":
		return condition.matchedSubjectCount(events), len(condition.SubjectIDs)
	default:
		return 0, 0
	}
}

func (condition EventSubscriptionCompletionCondition) matchedSubjectCount(events []WorkspaceEvent) int {
	wanted := make(map[string]struct{}, len(condition.SubjectIDs))
	for _, subjectID := range condition.SubjectIDs {
		wanted[subjectID] = struct{}{}
	}
	matched := make(map[string]struct{}, len(wanted))
	for _, event := range events {
		subjectID := strings.TrimSpace(event.SubjectID)
		if _, ok := wanted[subjectID]; !ok {
			continue
		}
		if !matchesAny(condition.TerminalEventTypes, event.EventType) {
			continue
		}
		matched[subjectID] = struct{}{}
	}
	return len(matched)
}

func (completion EventSubscriptionCompletion) withTimeout() EventSubscriptionCompletion {
	completion.TimedOut = true
	if completion.Status == "" || completion.Status == EventSubscriptionWaitStatusPartial {
		if completion.MatchedCount == 0 {
			completion.Status = EventSubscriptionWaitStatusTimeout
		} else {
			completion.Status = EventSubscriptionWaitStatusPartial
		}
	}
	return completion
}
