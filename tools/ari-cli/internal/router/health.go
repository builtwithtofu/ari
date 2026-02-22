package router

import (
	"context"

	"github.com/aledsdavies/ari/tools/ari-cli/internal/state"
)

// HealthTracker monitors model health and performance
type HealthTracker struct {
	store *state.Store
}

// NewHealthTracker creates a new health tracker
func NewHealthTracker(store *state.Store) *HealthTracker {
	return &HealthTracker{
		store: store,
	}
}

// RecordSuccess records a successful model invocation
func (h *HealthTracker) RecordSuccess(ctx context.Context, modelID string) error {
	_ = ctx
	_ = modelID
	return nil
}

// RecordFailure records a failed model invocation
func (h *HealthTracker) RecordFailure(ctx context.Context, modelID string) error {
	_ = ctx
	_ = modelID
	return nil
}

// IsHealthy checks if a model is healthy
func (h *HealthTracker) IsHealthy(ctx context.Context, modelID string) (bool, error) {
	// For v0, we always return true (health tracking is basic)
	// This will be expanded in later phases
	return true, nil
}
