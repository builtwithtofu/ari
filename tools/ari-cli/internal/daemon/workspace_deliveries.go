package daemon

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

type WorkspaceDeliveryGetRequest struct {
	DeliveryID string `json:"delivery_id"`
}

type WorkspaceDeliveryResponse struct {
	Delivery WorkspaceDeliveryState `json:"delivery"`
}

type WorkspaceDeliveryState struct {
	DeliveryID         string   `json:"delivery_id"`
	WorkspaceID        string   `json:"workspace_id"`
	SubscriptionID     string   `json:"subscription_id"`
	TargetType         string   `json:"target_type"`
	TargetID           string   `json:"target_id"`
	DeliveryPolicyJSON string   `json:"delivery_policy_json"`
	EventIDs           []string `json:"event_ids"`
	Status             string   `json:"status"`
	Attempts           int64    `json:"attempts"`
	NextAttemptAt      string   `json:"next_attempt_at,omitempty"`
	DeadlineAt         string   `json:"deadline_at,omitempty"`
	LastError          string   `json:"last_error,omitempty"`
	CreatedAt          string   `json:"created_at"`
	UpdatedAt          string   `json:"updated_at"`
	TerminalAt         string   `json:"terminal_at,omitempty"`
}

func (d *Daemon) registerWorkspaceDeliveryMethods(registry *rpc.MethodRegistry, store *globaldb.Store) error {
	if err := rpc.RegisterMethod(registry, rpc.Method[WorkspaceDeliveryGetRequest, WorkspaceDeliveryResponse]{
		Name:        "workspace.deliveries.get",
		Description: "Get a pending workspace event delivery",
		Handler: func(ctx context.Context, req WorkspaceDeliveryGetRequest) (WorkspaceDeliveryResponse, error) {
			delivery, err := store.GetPendingDelivery(ctx, req.DeliveryID)
			if err != nil {
				return WorkspaceDeliveryResponse{}, workspaceDeliveryRPCError(err)
			}
			return WorkspaceDeliveryResponse{Delivery: workspaceDeliveryState(delivery)}, nil
		},
	}); err != nil {
		return fmt.Errorf("register workspace.deliveries.get: %w", err)
	}

	return nil
}

func workspaceDeliveryRPCError(err error) error {
	if errors.Is(err, globaldb.ErrInvalidInput) {
		return rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "invalid_workspace_delivery_request"})
	}
	if errors.Is(err, globaldb.ErrNotFound) {
		return rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "workspace_delivery_not_found"})
	}
	return err
}

func workspaceDeliveryState(delivery globaldb.PendingDelivery) WorkspaceDeliveryState {
	state := WorkspaceDeliveryState{DeliveryID: delivery.DeliveryID, WorkspaceID: delivery.WorkspaceID, SubscriptionID: delivery.SubscriptionID, TargetType: delivery.TargetType, TargetID: delivery.TargetID, DeliveryPolicyJSON: delivery.DeliveryPolicyJSON, EventIDs: append([]string(nil), delivery.EventIDs...), Status: delivery.Status, Attempts: delivery.Attempts, LastError: delivery.LastError, CreatedAt: delivery.CreatedAt.Format(time.RFC3339Nano), UpdatedAt: delivery.UpdatedAt.Format(time.RFC3339Nano)}
	if delivery.NextAttemptAt != nil {
		state.NextAttemptAt = delivery.NextAttemptAt.Format(time.RFC3339Nano)
	}
	if delivery.DeadlineAt != nil {
		state.DeadlineAt = delivery.DeadlineAt.Format(time.RFC3339Nano)
	}
	if delivery.TerminalAt != nil {
		state.TerminalAt = delivery.TerminalAt.Format(time.RFC3339Nano)
	}
	return state
}
