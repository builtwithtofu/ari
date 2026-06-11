package daemon

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

type WorkspaceDeliveryDispatchRequest struct {
	DeliveryID         string   `json:"delivery_id,omitempty"`
	WorkspaceID        string   `json:"workspace_id"`
	SubscriptionID     string   `json:"subscription_id"`
	TargetType         string   `json:"target_type"`
	TargetID           string   `json:"target_id"`
	DeliveryPolicyJSON string   `json:"delivery_policy_json,omitempty"`
	EventIDs           []string `json:"event_ids"`
	NextAttemptAt      string   `json:"next_attempt_at,omitempty"`
	DeadlineAt         string   `json:"deadline_at,omitempty"`
}

type WorkspaceDeliveryGetRequest struct {
	DeliveryID string `json:"delivery_id"`
}

type WorkspaceDeliveriesRetryDueRequest struct {
	Now   string `json:"now,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type WorkspaceDeliveryRecordAttemptRequest struct {
	DeliveryID    string `json:"delivery_id"`
	LastError     string `json:"last_error,omitempty"`
	NextAttemptAt string `json:"next_attempt_at,omitempty"`
}

type WorkspaceDeliveryCompleteRequest struct {
	DeliveryID string `json:"delivery_id"`
}

type WorkspaceDeliveryFailRequest struct {
	DeliveryID string `json:"delivery_id"`
	LastError  string `json:"last_error,omitempty"`
}

type WorkspaceDeliveryResponse struct {
	Delivery WorkspaceDeliveryState `json:"delivery"`
}

type WorkspaceDeliveriesResponse struct {
	Deliveries []WorkspaceDeliveryState `json:"deliveries"`
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
	if err := rpc.RegisterMethod(registry, rpc.Method[WorkspaceDeliveryDispatchRequest, WorkspaceDeliveryResponse]{
		Name:        "workspace.deliveries.dispatch",
		Description: "Create a pending workspace event delivery",
		Handler: func(ctx context.Context, req WorkspaceDeliveryDispatchRequest) (WorkspaceDeliveryResponse, error) {
			nextAttemptAt, err := parseOptionalDeliveryTime(req.NextAttemptAt, "invalid_next_attempt_at")
			if err != nil {
				return WorkspaceDeliveryResponse{}, err
			}
			deadlineAt, err := parseOptionalDeliveryTime(req.DeadlineAt, "invalid_deadline_at")
			if err != nil {
				return WorkspaceDeliveryResponse{}, err
			}
			delivery, err := store.CreatePendingDelivery(ctx, globaldb.PendingDelivery{DeliveryID: req.DeliveryID, WorkspaceID: req.WorkspaceID, SubscriptionID: req.SubscriptionID, TargetType: req.TargetType, TargetID: req.TargetID, DeliveryPolicyJSON: req.DeliveryPolicyJSON, EventIDs: req.EventIDs, NextAttemptAt: nextAttemptAt, DeadlineAt: deadlineAt})
			if err != nil {
				return WorkspaceDeliveryResponse{}, workspaceDeliveryRPCError(err)
			}
			return WorkspaceDeliveryResponse{Delivery: workspaceDeliveryState(delivery)}, nil
		},
	}); err != nil {
		return fmt.Errorf("register workspace.deliveries.dispatch: %w", err)
	}

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

	if err := rpc.RegisterMethod(registry, rpc.Method[WorkspaceDeliveriesRetryDueRequest, WorkspaceDeliveriesResponse]{
		Name:        "workspace.deliveries.retry_due",
		Description: "List pending deliveries due for retry",
		Handler: func(ctx context.Context, req WorkspaceDeliveriesRetryDueRequest) (WorkspaceDeliveriesResponse, error) {
			now := time.Now().UTC()
			if req.Now != "" {
				parsed, err := parseWorkspaceTimerTime(req.Now, "invalid_now")
				if err != nil {
					return WorkspaceDeliveriesResponse{}, err
				}
				now = parsed
			}
			deliveries, err := store.ListDuePendingDeliveries(ctx, now, req.Limit)
			if err != nil {
				return WorkspaceDeliveriesResponse{}, workspaceDeliveryRPCError(err)
			}
			return WorkspaceDeliveriesResponse{Deliveries: workspaceDeliveryStates(deliveries)}, nil
		},
	}); err != nil {
		return fmt.Errorf("register workspace.deliveries.retry_due: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[WorkspaceDeliveryRecordAttemptRequest, WorkspaceDeliveryResponse]{
		Name:        "workspace.deliveries.record_attempt",
		Description: "Record a failed pending delivery attempt and next retry time",
		Handler: func(ctx context.Context, req WorkspaceDeliveryRecordAttemptRequest) (WorkspaceDeliveryResponse, error) {
			if req.NextAttemptAt == "" {
				return WorkspaceDeliveryResponse{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "missing_required_fields", "missing_field": "next_attempt_at"})
			}
			nextAttemptAt, err := parseOptionalDeliveryTime(req.NextAttemptAt, "invalid_next_attempt_at")
			if err != nil {
				return WorkspaceDeliveryResponse{}, err
			}
			delivery, err := store.RecordPendingDeliveryAttempt(ctx, req.DeliveryID, nextAttemptAt, req.LastError)
			if err != nil {
				return WorkspaceDeliveryResponse{}, workspaceDeliveryRPCError(err)
			}
			return WorkspaceDeliveryResponse{Delivery: workspaceDeliveryState(delivery)}, nil
		},
	}); err != nil {
		return fmt.Errorf("register workspace.deliveries.record_attempt: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[WorkspaceDeliveryCompleteRequest, WorkspaceDeliveryResponse]{
		Name:        "workspace.deliveries.complete",
		Description: "Mark a pending delivery completed",
		Handler: func(ctx context.Context, req WorkspaceDeliveryCompleteRequest) (WorkspaceDeliveryResponse, error) {
			delivery, err := store.CompletePendingDelivery(ctx, req.DeliveryID)
			if err != nil {
				return WorkspaceDeliveryResponse{}, workspaceDeliveryRPCError(err)
			}
			return WorkspaceDeliveryResponse{Delivery: workspaceDeliveryState(delivery)}, nil
		},
	}); err != nil {
		return fmt.Errorf("register workspace.deliveries.complete: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[WorkspaceDeliveryFailRequest, WorkspaceDeliveryResponse]{
		Name:        "workspace.deliveries.fail",
		Description: "Mark a pending delivery permanently failed",
		Handler: func(ctx context.Context, req WorkspaceDeliveryFailRequest) (WorkspaceDeliveryResponse, error) {
			delivery, err := store.FailPendingDelivery(ctx, req.DeliveryID, req.LastError)
			if err != nil {
				return WorkspaceDeliveryResponse{}, workspaceDeliveryRPCError(err)
			}
			return WorkspaceDeliveryResponse{Delivery: workspaceDeliveryState(delivery)}, nil
		},
	}); err != nil {
		return fmt.Errorf("register workspace.deliveries.fail: %w", err)
	}

	return nil
}

func parseOptionalDeliveryTime(raw, reason string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return nil, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": reason})
	}
	return &parsed, nil
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

func workspaceDeliveryStates(deliveries []globaldb.PendingDelivery) []WorkspaceDeliveryState {
	states := make([]WorkspaceDeliveryState, 0, len(deliveries))
	for _, delivery := range deliveries {
		states = append(states, workspaceDeliveryState(delivery))
	}
	return states
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
