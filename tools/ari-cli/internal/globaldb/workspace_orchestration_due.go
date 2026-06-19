package globaldb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb/dbsqlc"
)

// NextWorkspaceOrchestrationDueAt returns the next durable wake time for any
// daemon-owned workspace orchestration work. It is advisory only: callers must
// still query durable state when they wake.
func (s *Store) NextWorkspaceOrchestrationDueAt(ctx context.Context, now time.Time) (time.Time, bool, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var next time.Time
	setNext := func(candidate time.Time) {
		if candidate.IsZero() {
			return
		}
		if next.IsZero() || candidate.Before(next) {
			next = candidate
		}
	}
	queries := s.sqlcQueries()
	if raw, err := queries.NextScheduledWorkspaceTimerFireAt(ctx); err == nil {
		fireAt, parseErr := time.Parse(time.RFC3339Nano, raw)
		if parseErr != nil {
			return time.Time{}, false, fmt.Errorf("parse next workspace timer fire_at %q: %w", raw, parseErr)
		}
		setNext(fireAt)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, fmt.Errorf("read next workspace timer fire_at: %w", err)
	}
	formatted := now.UTC().Format(time.RFC3339Nano)
	if raw, err := queries.NextPendingDeliveryAttemptAt(ctx, dbsqlc.NextPendingDeliveryAttemptAtParams{DeadlineAt: &formatted, TimeoutAt: &formatted}); err == nil && raw != nil {
		attemptAt, parseErr := time.Parse(time.RFC3339Nano, *raw)
		if parseErr != nil {
			return time.Time{}, false, fmt.Errorf("parse next pending delivery attempt_at %q: %w", *raw, parseErr)
		}
		setNext(attemptAt)
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, fmt.Errorf("read next pending delivery attempt_at: %w", err)
	}
	if raw, err := queries.NextPendingDeliveryDeadlineAt(ctx); err == nil && raw != nil {
		deadlineAt, parseErr := time.Parse(time.RFC3339Nano, *raw)
		if parseErr != nil {
			return time.Time{}, false, fmt.Errorf("parse next pending delivery deadline_at %q: %w", *raw, parseErr)
		}
		setNext(deadlineAt)
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, fmt.Errorf("read next pending delivery deadline_at: %w", err)
	}
	if raw, err := queries.OldestAttemptedPendingDeliveryUpdatedAt(ctx); err == nil {
		updatedAt, parseErr := time.Parse(time.RFC3339Nano, raw)
		if parseErr != nil {
			return time.Time{}, false, fmt.Errorf("parse oldest attempted pending delivery updated_at %q: %w", raw, parseErr)
		}
		setNext(updatedAt.Add(pendingDeliveryAttemptLease))
	} else if !errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, fmt.Errorf("read oldest attempted pending delivery updated_at: %w", err)
	}
	if next.IsZero() {
		return time.Time{}, false, nil
	}
	return next, true, nil
}
