package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
)

type WorkspaceDeliveryAttemptStatus string

const (
	WorkspaceDeliveryAttemptCompleted WorkspaceDeliveryAttemptStatus = "completed"
	WorkspaceDeliveryAttemptFailed    WorkspaceDeliveryAttemptStatus = "failed"
	WorkspaceDeliveryAttemptRetry     WorkspaceDeliveryAttemptStatus = "retry"
)

type WorkspaceDeliveryWorkerOutcomeStatus string

const (
	WorkspaceDeliveryWorkerOutcomeCompleted WorkspaceDeliveryWorkerOutcomeStatus = "completed"
	WorkspaceDeliveryWorkerOutcomeFailed    WorkspaceDeliveryWorkerOutcomeStatus = "failed"
	WorkspaceDeliveryWorkerOutcomeRetry     WorkspaceDeliveryWorkerOutcomeStatus = "retry"
	WorkspaceDeliveryWorkerOutcomeSkipped   WorkspaceDeliveryWorkerOutcomeStatus = "skipped"
)

type WorkspaceDeliveryAttempt struct {
	Delivery globaldb.PendingDelivery
	Now      time.Time
}

type WorkspaceDeliveryAttemptResult struct {
	Status        WorkspaceDeliveryAttemptStatus
	LastError     string
	NextAttemptAt *time.Time
}

type WorkspaceDeliveryWorkerOutcome struct {
	DeliveryID string
	Status     WorkspaceDeliveryWorkerOutcomeStatus
	LastError  string
}

type WorkspaceDeliveryDispatcher interface {
	AttemptWorkspaceDelivery(context.Context, WorkspaceDeliveryAttempt) (WorkspaceDeliveryAttemptResult, error)
}

const (
	defaultWorkspaceDeliveryWorkerInterval = time.Second
	defaultWorkspaceDeliveryWorkerLimit    = 100
)

func (d *Daemon) startWorkspaceDeliveryWorker(store *globaldb.Store) {
	if d == nil || store == nil {
		return
	}
	dispatcher := newHarnessWorkspaceDeliveryDispatcher(d)
	d.startHarnessLifecycleWork(func(ctx context.Context) {
		ticker := time.NewTicker(defaultWorkspaceDeliveryWorkerInterval)
		defer ticker.Stop()
		_ = runWorkspaceDeliveryWorkerLoop(ctx, store, dispatcher, ticker.C, defaultWorkspaceDeliveryWorkerLimit)
	})
}

func runWorkspaceDeliveryWorkerLoop(ctx context.Context, store *globaldb.Store, dispatcher WorkspaceDeliveryDispatcher, ticks <-chan time.Time, limit int) error {
	if ctx == nil {
		return fmt.Errorf("context is required")
	}
	if ticks == nil {
		return fmt.Errorf("workspace delivery worker ticks are required")
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case now, ok := <-ticks:
			if !ok {
				return nil
			}
			// Pending deliveries are durable rows; a transient store error
			// leaves them due, so the next tick retries instead of killing
			// the worker until daemon restart.
			_, _ = runWorkspaceDeliveryWorkerOnce(ctx, store, dispatcher, now, limit)
		}
	}
}

func runWorkspaceDeliveryWorkerOnce(ctx context.Context, store *globaldb.Store, dispatcher WorkspaceDeliveryDispatcher, now time.Time, limit int) ([]WorkspaceDeliveryWorkerOutcome, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}
	if store == nil {
		return nil, fmt.Errorf("globaldb store is required")
	}
	if dispatcher == nil {
		return nil, fmt.Errorf("workspace delivery dispatcher is required")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	due, err := store.ListDuePendingDeliveries(ctx, now, limit)
	if err != nil {
		return nil, err
	}
	outcomes := make([]WorkspaceDeliveryWorkerOutcome, 0, len(due))
	for _, pending := range due {
		claimed, err := store.ClaimDuePendingDeliveryAttempt(ctx, pending.DeliveryID, now)
		if err != nil {
			if errors.Is(err, globaldb.ErrNotFound) {
				outcomes = append(outcomes, WorkspaceDeliveryWorkerOutcome{DeliveryID: pending.DeliveryID, Status: WorkspaceDeliveryWorkerOutcomeSkipped})
				continue
			}
			return outcomes, err
		}

		result, dispatchErr := dispatcher.AttemptWorkspaceDelivery(ctx, WorkspaceDeliveryAttempt{Delivery: claimed, Now: now})
		if dispatchErr != nil {
			result = WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptRetry, LastError: dispatchErr.Error()}
		}
		outcome, err := finishWorkspaceDeliveryAttempt(ctx, store, claimed, result, now)
		outcomes = append(outcomes, outcome)
		if err != nil {
			return outcomes, err
		}
	}
	return outcomes, nil
}

func finishWorkspaceDeliveryAttempt(ctx context.Context, store *globaldb.Store, delivery globaldb.PendingDelivery, result WorkspaceDeliveryAttemptResult, now time.Time) (WorkspaceDeliveryWorkerOutcome, error) {
	// Each store transition below emits its delivery.* workspace event in the
	// same transaction as the row change; the worker only owns delivery
	// semantics (retry policy, max attempts, outcome mapping).
	switch result.Status {
	case WorkspaceDeliveryAttemptCompleted:
		completed, err := store.CompletePendingDelivery(ctx, delivery.DeliveryID)
		return WorkspaceDeliveryWorkerOutcome{DeliveryID: completed.DeliveryID, Status: WorkspaceDeliveryWorkerOutcomeCompleted}, err
	case WorkspaceDeliveryAttemptFailed:
		failed, err := store.FailPendingDelivery(ctx, delivery.DeliveryID, result.LastError)
		return WorkspaceDeliveryWorkerOutcome{DeliveryID: failed.DeliveryID, Status: WorkspaceDeliveryWorkerOutcomeFailed, LastError: failed.LastError}, err
	case WorkspaceDeliveryAttemptRetry:
		policy, err := parseWorkspaceDeliveryPolicy(delivery.DeliveryPolicyJSON)
		if err != nil {
			failed, err := store.FailPendingDelivery(ctx, delivery.DeliveryID, err.Error())
			return WorkspaceDeliveryWorkerOutcome{DeliveryID: failed.DeliveryID, Status: WorkspaceDeliveryWorkerOutcomeFailed, LastError: failed.LastError}, err
		}
		if policy.MaxAttempts > 0 && delivery.Attempts >= policy.MaxAttempts {
			lastError := strings.TrimSpace(result.LastError)
			if lastError == "" {
				lastError = fmt.Sprintf("delivery retry limit reached after %d attempts", delivery.Attempts)
			}
			failed, err := store.FailPendingDelivery(ctx, delivery.DeliveryID, lastError)
			return WorkspaceDeliveryWorkerOutcome{DeliveryID: failed.DeliveryID, Status: WorkspaceDeliveryWorkerOutcomeFailed, LastError: failed.LastError}, err
		}
		nextAttemptAt := result.NextAttemptAt
		if nextAttemptAt == nil || nextAttemptAt.IsZero() {
			fallback := now.Add(defaultWorkspaceDeliveryRetryDelay(delivery))
			nextAttemptAt = &fallback
		}
		retry, err := store.SchedulePendingDeliveryRetry(ctx, delivery.DeliveryID, *nextAttemptAt, result.LastError)
		return WorkspaceDeliveryWorkerOutcome{DeliveryID: retry.DeliveryID, Status: WorkspaceDeliveryWorkerOutcomeRetry, LastError: retry.LastError}, err
	default:
		lastError := strings.TrimSpace(result.LastError)
		if lastError == "" {
			lastError = fmt.Sprintf("unsupported delivery attempt result %q", result.Status)
		}
		failed, err := store.FailPendingDelivery(ctx, delivery.DeliveryID, lastError)
		return WorkspaceDeliveryWorkerOutcome{DeliveryID: failed.DeliveryID, Status: WorkspaceDeliveryWorkerOutcomeFailed, LastError: failed.LastError}, err
	}
}

func defaultWorkspaceDeliveryRetryDelay(delivery globaldb.PendingDelivery) time.Duration {
	attempt := delivery.Attempts
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 6 {
		attempt = 6
	}
	return time.Duration(attempt) * time.Second
}
