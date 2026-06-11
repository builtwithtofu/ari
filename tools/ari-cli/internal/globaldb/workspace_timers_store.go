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
	workspaceTimerStatusScheduled = "scheduled"
	workspaceTimerStatusFired     = "fired"
	workspaceTimerStatusCanceled  = "canceled"

	workspaceTimerFiredEventType = "timer.fired"
)

type WorkspaceTimer struct {
	TimerID        string
	WorkspaceID    string
	OwnerSessionID string
	SubscriptionID string
	SubjectType    string
	SubjectID      string
	Purpose        string
	Status         string
	FireAt         time.Time
	PayloadJSON    string
	FiredEventID   string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (s *Store) CreateWorkspaceTimer(ctx context.Context, timer WorkspaceTimer) (WorkspaceTimer, error) {
	timer = normalizeWorkspaceTimer(timer)
	if err := validateWorkspaceTimer(timer); err != nil {
		return WorkspaceTimer{}, err
	}
	if timer.TimerID == "" {
		timer.TimerID = randomID("wt")
	}
	now := time.Now().UTC()
	if timer.CreatedAt.IsZero() {
		timer.CreatedAt = now
	}
	if timer.UpdatedAt.IsZero() {
		timer.UpdatedAt = timer.CreatedAt
	}
	if err := s.sqlcQueries().CreateWorkspaceTimer(ctx, dbsqlc.CreateWorkspaceTimerParams{TimerID: timer.TimerID, WorkspaceID: timer.WorkspaceID, OwnerSessionID: timer.OwnerSessionID, SubscriptionID: optionalString(timer.SubscriptionID), SubjectType: timer.SubjectType, SubjectID: timer.SubjectID, Purpose: timer.Purpose, Status: timer.Status, FireAt: timer.FireAt.UTC().Format(time.RFC3339Nano), PayloadJson: timer.PayloadJSON, FiredEventID: timer.FiredEventID, CreatedAt: timer.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: timer.UpdatedAt.UTC().Format(time.RFC3339Nano)}); err != nil {
		return WorkspaceTimer{}, fmt.Errorf("create workspace timer %q: %w", timer.TimerID, err)
	}
	return timer, nil
}

func (s *Store) GetWorkspaceTimer(ctx context.Context, timerID string) (WorkspaceTimer, error) {
	timerID = strings.TrimSpace(timerID)
	if timerID == "" {
		return WorkspaceTimer{}, fmt.Errorf("%w: timer id is required", ErrInvalidInput)
	}
	row, err := s.sqlcQueries().GetWorkspaceTimer(ctx, dbsqlc.GetWorkspaceTimerParams{TimerID: timerID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return WorkspaceTimer{}, ErrNotFound
		}
		return WorkspaceTimer{}, fmt.Errorf("get workspace timer %q: %w", timerID, err)
	}
	return workspaceTimerFromSQLC(row), nil
}

func (s *Store) ListDueWorkspaceTimers(ctx context.Context, now time.Time, limit int) ([]WorkspaceTimer, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.sqlcQueries().ListDueWorkspaceTimers(ctx, dbsqlc.ListDueWorkspaceTimersParams{FireAt: now.UTC().Format(time.RFC3339Nano), Limit: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("list due workspace timers: %w", err)
	}
	timers := make([]WorkspaceTimer, 0, len(rows))
	for _, row := range rows {
		timers = append(timers, workspaceTimerFromSQLC(row))
	}
	return timers, nil
}

func (s *Store) FireDueWorkspaceTimers(ctx context.Context, now time.Time, limit int) ([]WorkspaceTimer, error) {
	due, err := s.ListDueWorkspaceTimers(ctx, now, limit)
	if err != nil {
		return nil, err
	}
	fired := make([]WorkspaceTimer, 0, len(due))
	var fireErrs []string
	for _, timer := range due {
		updated, err := s.FireWorkspaceTimer(ctx, timer.TimerID)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			fireErrs = append(fireErrs, err.Error())
			continue
		}
		fired = append(fired, updated)
	}
	if len(fireErrs) > 0 {
		return fired, fmt.Errorf("fire due workspace timers: %s", strings.Join(fireErrs, "; "))
	}
	return fired, nil
}

func (s *Store) FireWorkspaceTimer(ctx context.Context, timerID string) (WorkspaceTimer, error) {
	timerID = strings.TrimSpace(timerID)
	if timerID == "" {
		return WorkspaceTimer{}, fmt.Errorf("%w: timer id is required", ErrInvalidInput)
	}
	var fired WorkspaceTimer
	if err := s.withImmediateQueries(ctx, func(queries *dbsqlc.Queries) error {
		row, err := queries.GetWorkspaceTimer(ctx, dbsqlc.GetWorkspaceTimerParams{TimerID: timerID})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("get workspace timer %q: %w", timerID, err)
		}
		timer := workspaceTimerFromSQLC(row)
		if timer.Status != workspaceTimerStatusScheduled {
			return ErrNotFound
		}
		event := normalizeWorkspaceEvent(WorkspaceEvent{EventID: newWorkspaceEventID(), WorkspaceID: timer.WorkspaceID, EventType: workspaceTimerFiredEventType, SubjectType: "timer", SubjectID: timer.TimerID, ProducerType: "daemon", ProducerID: "timer", CorrelationID: defaultString(timer.Purpose, timer.TimerID), CausationID: timer.SubscriptionID, PayloadJSON: timer.PayloadJSON, PayloadRefJSON: workspaceTimerPayloadRef(timer.TimerID)})
		if event.CreatedAt.IsZero() {
			event.CreatedAt = time.Now().UTC()
		}
		if err := validateWorkspaceEvent(event); err != nil {
			return err
		}
		if err := createWorkspaceEventWithQueries(ctx, queries, &event); err != nil {
			return err
		}
		if err := createPendingDeliveriesForWorkspaceEvent(ctx, queries, event); err != nil {
			return err
		}
		now := time.Now().UTC()
		rows, err := queries.MarkWorkspaceTimerFired(ctx, dbsqlc.MarkWorkspaceTimerFiredParams{FiredEventID: event.EventID, UpdatedAt: now.Format(time.RFC3339Nano), TimerID: timer.TimerID})
		if err != nil {
			return fmt.Errorf("mark workspace timer %q fired: %w", timer.TimerID, err)
		}
		if rows == 0 {
			return ErrNotFound
		}
		timer.Status = workspaceTimerStatusFired
		timer.FiredEventID = event.EventID
		timer.UpdatedAt = now
		fired = timer
		return nil
	}); err != nil {
		return WorkspaceTimer{}, err
	}
	return fired, nil
}

func (s *Store) CancelWorkspaceTimer(ctx context.Context, timerID string) (WorkspaceTimer, error) {
	timerID = strings.TrimSpace(timerID)
	if timerID == "" {
		return WorkspaceTimer{}, fmt.Errorf("%w: timer id is required", ErrInvalidInput)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	rows, err := s.sqlcQueries().CancelWorkspaceTimer(ctx, dbsqlc.CancelWorkspaceTimerParams{UpdatedAt: now, TimerID: timerID})
	if err != nil {
		return WorkspaceTimer{}, fmt.Errorf("cancel workspace timer %q: %w", timerID, err)
	}
	if rows == 0 {
		timer, getErr := s.GetWorkspaceTimer(ctx, timerID)
		if getErr != nil {
			return WorkspaceTimer{}, getErr
		}
		if timer.Status == workspaceTimerStatusCanceled {
			return timer, nil
		}
		return WorkspaceTimer{}, ErrNotFound
	}
	return s.GetWorkspaceTimer(ctx, timerID)
}

func normalizeWorkspaceTimer(timer WorkspaceTimer) WorkspaceTimer {
	timer.TimerID = strings.TrimSpace(timer.TimerID)
	timer.WorkspaceID = strings.TrimSpace(timer.WorkspaceID)
	timer.OwnerSessionID = strings.TrimSpace(timer.OwnerSessionID)
	timer.SubscriptionID = strings.TrimSpace(timer.SubscriptionID)
	timer.SubjectType = strings.TrimSpace(timer.SubjectType)
	timer.SubjectID = strings.TrimSpace(timer.SubjectID)
	timer.Purpose = strings.TrimSpace(timer.Purpose)
	timer.Status = strings.TrimSpace(timer.Status)
	if timer.Status == "" {
		timer.Status = workspaceTimerStatusScheduled
	}
	timer.PayloadJSON = defaultJSON(timer.PayloadJSON, "{}")
	timer.FiredEventID = strings.TrimSpace(timer.FiredEventID)
	return timer
}

func validateWorkspaceTimer(timer WorkspaceTimer) error {
	if timer.WorkspaceID == "" {
		return fmt.Errorf("%w: workspace timer workspace id is required", ErrInvalidInput)
	}
	if timer.FireAt.IsZero() {
		return fmt.Errorf("%w: workspace timer fire_at is required", ErrInvalidInput)
	}
	if !json.Valid([]byte(timer.PayloadJSON)) {
		return fmt.Errorf("%w: workspace timer payload json is invalid", ErrInvalidInput)
	}
	return nil
}

func workspaceTimerFromSQLC(row dbsqlc.WorkspaceTimer) WorkspaceTimer {
	fireAt, _ := time.Parse(time.RFC3339Nano, row.FireAt)
	createdAt, _ := time.Parse(time.RFC3339Nano, row.CreatedAt)
	updatedAt, _ := time.Parse(time.RFC3339Nano, row.UpdatedAt)
	subscriptionID := ""
	if row.SubscriptionID != nil {
		subscriptionID = *row.SubscriptionID
	}
	return WorkspaceTimer{TimerID: row.TimerID, WorkspaceID: row.WorkspaceID, OwnerSessionID: row.OwnerSessionID, SubscriptionID: subscriptionID, SubjectType: row.SubjectType, SubjectID: row.SubjectID, Purpose: row.Purpose, Status: row.Status, FireAt: fireAt, PayloadJSON: row.PayloadJson, FiredEventID: row.FiredEventID, CreatedAt: createdAt, UpdatedAt: updatedAt}
}

func workspaceTimerPayloadRef(timerID string) string {
	encoded, err := json.Marshal(map[string]string{"kind": "timer", "id": strings.TrimSpace(timerID)})
	if err != nil {
		return "{}"
	}
	return string(encoded)
}
