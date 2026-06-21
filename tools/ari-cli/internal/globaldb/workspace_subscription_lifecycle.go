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
}

type EventSubscriptionWaitOptions struct {
	Limit     int
	MinEvents int
	Timeout   time.Duration
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

func (s *Store) ReadEventSubscription(ctx context.Context, req EventSubscriptionReadRequest) (EventSubscriptionReadResult, error) {
	if req.MinEvents < 0 {
		return EventSubscriptionReadResult{}, fmt.Errorf("%w: min events must not be negative", ErrInvalidInput)
	}
	subscription, err := s.GetEventSubscription(ctx, req.SubscriptionID)
	if err != nil {
		return EventSubscriptionReadResult{}, err
	}
	stream, err := NewSubscriptionStream(subscription)
	if err != nil {
		return EventSubscriptionReadResult{}, err
	}
	return stream.Read(ctx, s.sqlcQueries(), eventSubscriptionReadOptions{limit: req.Limit, minEvents: req.MinEvents})
}

func (s *Store) WaitEventSubscription(ctx context.Context, req EventSubscriptionWaitRequest) (EventSubscriptionReadResult, error) {
	if req.MinEvents < 0 || req.Timeout < 0 {
		return EventSubscriptionReadResult{}, fmt.Errorf("%w: event subscription wait values must not be negative", ErrInvalidInput)
	}
	subscription, err := s.GetEventSubscription(ctx, req.SubscriptionID)
	if err != nil {
		return EventSubscriptionReadResult{}, err
	}
	return s.waitEventSubscription(ctx, subscription, EventSubscriptionWaitOptions{Limit: req.Limit, MinEvents: req.MinEvents, Timeout: req.Timeout})
}

func (s *Store) WaitEventSubscriptionCondition(ctx context.Context, subscription EventSubscription, options EventSubscriptionWaitOptions) (EventSubscriptionReadResult, error) {
	if options.MinEvents < 0 || options.Timeout < 0 {
		return EventSubscriptionReadResult{}, fmt.Errorf("%w: event subscription wait values must not be negative", ErrInvalidInput)
	}
	subscription = normalizeEventSubscription(subscription)
	if err := validateEventSubscription(subscription); err != nil {
		return EventSubscriptionReadResult{}, err
	}
	return s.waitEventSubscription(ctx, subscription, options)
}

func (s *Store) waitEventSubscription(ctx context.Context, subscription EventSubscription, options EventSubscriptionWaitOptions) (EventSubscriptionReadResult, error) {
	stream, err := NewSubscriptionStream(subscription)
	if err != nil {
		return EventSubscriptionReadResult{}, err
	}
	readOptions := eventSubscriptionReadOptions{limit: options.Limit, minEvents: options.MinEvents}
	read := func(timedOut bool) (EventSubscriptionReadResult, error) {
		result, err := stream.Read(ctx, s.sqlcQueries(), readOptions)
		if err != nil {
			return EventSubscriptionReadResult{}, err
		}
		if timedOut && result.Completion.Configured && !result.Completion.Satisfied {
			result.Completion = result.Completion.withTimeout()
		}
		return result, nil
	}
	if options.Timeout <= 0 {
		return read(false)
	}
	wake, unsubscribe := s.subscribeWorkspaceEventWake(subscription.WorkspaceID)
	defer unsubscribe()
	result, err := read(false)
	if err != nil {
		return EventSubscriptionReadResult{}, err
	}
	if !result.Completion.Configured || result.Completion.Satisfied {
		return result, nil
	}
	timer := time.NewTimer(options.Timeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return EventSubscriptionReadResult{}, ctx.Err()
		case <-timer.C:
			return read(true)
		case <-wake:
			result, err = read(false)
			if err != nil {
				return EventSubscriptionReadResult{}, err
			}
			if result.Completion.Satisfied {
				return result, nil
			}
		}
	}
}

type eventSubscriptionReadOptions struct {
	limit     int
	minEvents int
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
