package daemon

import (
	"context"
	"errors"
	"fmt"
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
	engine := NewDeliveryPolicyEngine()
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
		outcome, err := engine.FinishAttempt(ctx, store, claimed, result, now)
		outcomes = append(outcomes, outcome)
		if err != nil {
			return outcomes, err
		}
	}
	return outcomes, nil
}
