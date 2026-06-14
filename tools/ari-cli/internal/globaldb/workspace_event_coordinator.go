package globaldb

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb/dbsqlc"
)

// EventCoordinator owns the durable side effects of appending workspace
// events: sequence allocation, event storage, subscription delivery creation,
// and any event-derived projections that must commit atomically with the fact.
//
// Store methods remain the public persistence surface for now; they delegate
// here so every event append crosses one seam instead of hand-assembling the
// choreography at each call site.
type EventCoordinator struct {
	store *Store
}

// EventCoordinator returns the workspace event coordinator backed by this
// store. The returned value is cheap and carries no state beyond the store.
func (s *Store) EventCoordinator() EventCoordinator {
	return EventCoordinator{store: s}
}

// AppendWorkspaceEvent records a workspace event and creates pending deliveries
// for matching subscriptions unless the event type is guarded from recursive
// delivery.
func (c EventCoordinator) AppendWorkspaceEvent(ctx context.Context, event WorkspaceEvent) (WorkspaceEvent, error) {
	if c.store == nil {
		return WorkspaceEvent{}, fmt.Errorf("%w: globaldb store is required", ErrInvalidInput)
	}
	prepared, err := prepareCoordinatedWorkspaceEvent(event)
	if err != nil {
		return WorkspaceEvent{}, err
	}
	if err := c.store.withImmediateQueries(ctx, func(queries *dbsqlc.Queries) error {
		return appendCoordinatedWorkspaceEventWithQueries(ctx, queries, &prepared)
	}); err != nil {
		return WorkspaceEvent{}, err
	}
	return prepared, nil
}

type workspaceEventProjection func(context.Context, *dbsqlc.Queries, WorkspaceEvent) error

func prepareCoordinatedWorkspaceEvent(event WorkspaceEvent) (WorkspaceEvent, error) {
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
	return event, nil
}

func appendCoordinatedWorkspaceEventWithQueries(ctx context.Context, queries *dbsqlc.Queries, event *WorkspaceEvent, projections ...workspaceEventProjection) error {
	if event == nil {
		return fmt.Errorf("%w: workspace event is required", ErrInvalidInput)
	}
	if err := createWorkspaceEventWithQueries(ctx, queries, event); err != nil {
		return err
	}
	if err := createPendingDeliveriesForCoordinatedEvent(ctx, queries, *event); err != nil {
		return err
	}
	for _, project := range projections {
		if project == nil {
			continue
		}
		if err := project(ctx, queries, *event); err != nil {
			return err
		}
	}
	return nil
}

func createPendingDeliveriesForCoordinatedEvent(ctx context.Context, queries *dbsqlc.Queries, event WorkspaceEvent) error {
	if workspaceEventSkipsDeliveryFanout(event) {
		return nil
	}
	return createPendingDeliveriesForWorkspaceEvent(ctx, queries, event)
}

func workspaceEventSkipsDeliveryFanout(event WorkspaceEvent) bool {
	return strings.HasPrefix(strings.TrimSpace(event.EventType), "delivery.")
}
