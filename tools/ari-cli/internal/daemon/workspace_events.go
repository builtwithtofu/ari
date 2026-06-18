package daemon

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

type WorkspaceEventAppendRequest struct {
	EventID           string `json:"event_id,omitempty"`
	WorkspaceID       string `json:"workspace_id"`
	EventType         string `json:"event_type"`
	SubjectType       string `json:"subject_type"`
	SubjectID         string `json:"subject_id"`
	ProducerType      string `json:"producer_type,omitempty"`
	ProducerID        string `json:"producer_id,omitempty"`
	CorrelationID     string `json:"correlation_id,omitempty"`
	CausationID       string `json:"causation_id,omitempty"`
	PayloadJSON       string `json:"payload_json,omitempty"`
	PayloadRefJSON    string `json:"payload_ref_json,omitempty"`
	AttentionRequired bool   `json:"attention_required,omitempty"`
}

type WorkspaceEventsAfterRequest struct {
	WorkspaceID   string `json:"workspace_id"`
	AfterSequence int64  `json:"after_sequence,omitempty"`
	Limit         int    `json:"limit,omitempty"`
}

type WorkspaceEventSubscribeRequest struct {
	SubscriptionID          string `json:"subscription_id,omitempty"`
	WorkspaceID             string `json:"workspace_id"`
	OwnerSessionID          string `json:"owner_session_id,omitempty"`
	Name                    string `json:"name,omitempty"`
	FilterJSON              string `json:"filter_json,omitempty"`
	DeliveryTargetType      string `json:"delivery_target_type,omitempty"`
	DeliveryTargetID        string `json:"delivery_target_id,omitempty"`
	DeliveryPolicyJSON      string `json:"delivery_policy_json,omitempty"`
	CursorSequence          int64  `json:"cursor_sequence,omitempty"`
	AckSequence             int64  `json:"ack_sequence,omitempty"`
	CompletionConditionJSON string `json:"completion_condition_json,omitempty"`
	TimeoutAt               string `json:"timeout_at,omitempty"`
}

type WorkspaceEventsNextRequest struct {
	SubscriptionID string `json:"subscription_id"`
	Limit          int    `json:"limit,omitempty"`
	MinEvents      int    `json:"min_events,omitempty"`
	TimeoutMS      int    `json:"timeout_ms,omitempty"`
}

type WorkspaceEventAckRequest struct {
	SubscriptionID string `json:"subscription_id"`
	Sequence       int64  `json:"sequence"`
}

type WorkspaceEventSubscriptionCancelRequest struct {
	SubscriptionID string `json:"subscription_id"`
}

type WorkspaceEventsResponse struct {
	Events       []WorkspaceEventResponse `json:"events"`
	WaitStatus   string                   `json:"wait_status,omitempty"`
	WaitTimedOut bool                     `json:"wait_timed_out,omitempty"`
}

type WorkspaceEventAckResponse struct {
	Acked        bool                               `json:"acked"`
	Subscription WorkspaceEventSubscriptionResponse `json:"subscription"`
}

type WorkspaceEventResponse struct {
	EventID           string `json:"event_id"`
	WorkspaceID       string `json:"workspace_id"`
	Sequence          int64  `json:"sequence"`
	EventType         string `json:"event_type"`
	SubjectType       string `json:"subject_type"`
	SubjectID         string `json:"subject_id"`
	ProducerType      string `json:"producer_type,omitempty"`
	ProducerID        string `json:"producer_id,omitempty"`
	CorrelationID     string `json:"correlation_id,omitempty"`
	CausationID       string `json:"causation_id,omitempty"`
	PayloadJSON       string `json:"payload_json"`
	PayloadRefJSON    string `json:"payload_ref_json"`
	AttentionRequired bool   `json:"attention_required"`
	CreatedAt         string `json:"created_at"`
}

type WorkspaceEventSubscriptionResponse struct {
	SubscriptionID          string `json:"subscription_id"`
	WorkspaceID             string `json:"workspace_id"`
	OwnerSessionID          string `json:"owner_session_id,omitempty"`
	Name                    string `json:"name,omitempty"`
	FilterJSON              string `json:"filter_json"`
	DeliveryTargetType      string `json:"delivery_target_type,omitempty"`
	DeliveryTargetID        string `json:"delivery_target_id,omitempty"`
	DeliveryPolicyJSON      string `json:"delivery_policy_json"`
	CursorSequence          int64  `json:"cursor_sequence"`
	AckSequence             int64  `json:"ack_sequence"`
	Status                  string `json:"status"`
	CompletionConditionJSON string `json:"completion_condition_json"`
	TimeoutAt               string `json:"timeout_at,omitempty"`
	CreatedAt               string `json:"created_at"`
	UpdatedAt               string `json:"updated_at"`
}

func (d *Daemon) registerWorkspaceEventMethods(registry *rpc.MethodRegistry, store *globaldb.Store) error {
	if err := rpc.RegisterMethod(registry, rpc.Method[WorkspaceEventAppendRequest, WorkspaceEventResponse]{
		Name:        "workspace.events.append",
		Description: "Append a durable workspace event",
		Handler: func(ctx context.Context, req WorkspaceEventAppendRequest) (WorkspaceEventResponse, error) {
			event, err := store.AppendWorkspaceEvent(ctx, globaldb.AppendWorkspaceEventParams{
				EventID:           req.EventID,
				WorkspaceID:       req.WorkspaceID,
				EventType:         req.EventType,
				SubjectType:       req.SubjectType,
				SubjectID:         req.SubjectID,
				ProducerType:      req.ProducerType,
				ProducerID:        req.ProducerID,
				CorrelationID:     req.CorrelationID,
				CausationID:       req.CausationID,
				PayloadJSON:       req.PayloadJSON,
				PayloadRefJSON:    req.PayloadRefJSON,
				AttentionRequired: req.AttentionRequired,
			})
			if err != nil {
				return WorkspaceEventResponse{}, workspaceEventRPCError(err)
			}
			return workspaceEventResponse(event), nil
		},
	}); err != nil {
		return fmt.Errorf("register workspace.events.append: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[WorkspaceEventsAfterRequest, WorkspaceEventsResponse]{
		Name:        "workspace.events.after",
		Description: "List workspace events after a workspace sequence cursor",
		Handler: func(ctx context.Context, req WorkspaceEventsAfterRequest) (WorkspaceEventsResponse, error) {
			events, err := store.ListWorkspaceEventsAfterSequence(ctx, req.WorkspaceID, req.AfterSequence, req.Limit)
			if err != nil {
				return WorkspaceEventsResponse{}, workspaceEventRPCError(err)
			}
			return WorkspaceEventsResponse{Events: workspaceEventResponses(events)}, nil
		},
	}); err != nil {
		return fmt.Errorf("register workspace.events.after: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[WorkspaceEventSubscribeRequest, WorkspaceEventSubscriptionResponse]{
		Name:        "workspace.events.subscribe",
		Description: "Create a durable workspace event subscription",
		Handler: func(ctx context.Context, req WorkspaceEventSubscribeRequest) (WorkspaceEventSubscriptionResponse, error) {
			timeoutAt, err := parseWorkspaceEventTimeout(req.TimeoutAt)
			if err != nil {
				return WorkspaceEventSubscriptionResponse{}, err
			}
			subscription, err := store.CreateEventSubscription(ctx, globaldb.EventSubscription{
				SubscriptionID:          req.SubscriptionID,
				WorkspaceID:             req.WorkspaceID,
				OwnerSessionID:          req.OwnerSessionID,
				Name:                    req.Name,
				FilterJSON:              req.FilterJSON,
				DeliveryTargetType:      req.DeliveryTargetType,
				DeliveryTargetID:        req.DeliveryTargetID,
				DeliveryPolicyJSON:      req.DeliveryPolicyJSON,
				CursorSequence:          req.CursorSequence,
				AckSequence:             req.AckSequence,
				CompletionConditionJSON: req.CompletionConditionJSON,
				TimeoutAt:               timeoutAt,
			})
			if err != nil {
				return WorkspaceEventSubscriptionResponse{}, workspaceEventRPCError(err)
			}
			return workspaceEventSubscriptionResponse(subscription), nil
		},
	}); err != nil {
		return fmt.Errorf("register workspace.events.subscribe: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[WorkspaceEventsNextRequest, WorkspaceEventsResponse]{
		Name:        "workspace.events.next",
		Description: "List unread events for a durable workspace event subscription",
		Handler: func(ctx context.Context, req WorkspaceEventsNextRequest) (WorkspaceEventsResponse, error) {
			return workspaceEventsNext(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register workspace.events.next: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[WorkspaceEventSubscriptionCancelRequest, WorkspaceEventSubscriptionResponse]{
		Name:        "workspace.events.cancel",
		Description: "Cancel a durable workspace event subscription",
		Handler: func(ctx context.Context, req WorkspaceEventSubscriptionCancelRequest) (WorkspaceEventSubscriptionResponse, error) {
			subscription, err := store.CancelEventSubscription(ctx, req.SubscriptionID)
			if err != nil {
				return WorkspaceEventSubscriptionResponse{}, workspaceEventRPCError(err)
			}
			return workspaceEventSubscriptionResponse(subscription), nil
		},
	}); err != nil {
		return fmt.Errorf("register workspace.events.cancel: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[WorkspaceEventAckRequest, WorkspaceEventAckResponse]{
		Name:        "workspace.events.ack",
		Description: "Advance a workspace event subscription cursor and acknowledgement sequence",
		Handler: func(ctx context.Context, req WorkspaceEventAckRequest) (WorkspaceEventAckResponse, error) {
			if err := store.AckEventSubscription(ctx, req.SubscriptionID, req.Sequence); err != nil {
				return WorkspaceEventAckResponse{}, workspaceEventRPCError(err)
			}
			subscription, err := store.GetEventSubscription(ctx, req.SubscriptionID)
			if err != nil {
				return WorkspaceEventAckResponse{}, workspaceEventRPCError(err)
			}
			return WorkspaceEventAckResponse{Acked: true, Subscription: workspaceEventSubscriptionResponse(subscription)}, nil
		},
	}); err != nil {
		return fmt.Errorf("register workspace.events.ack: %w", err)
	}

	return nil
}

func workspaceEventsNext(ctx context.Context, store *globaldb.Store, req WorkspaceEventsNextRequest) (WorkspaceEventsResponse, error) {
	if req.MinEvents < 0 || req.TimeoutMS < 0 {
		return WorkspaceEventsResponse{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "invalid_wait_request"})
	}
	result, err := store.WaitEventSubscription(ctx, globaldb.EventSubscriptionWaitRequest{SubscriptionID: req.SubscriptionID, Limit: req.Limit, MinEvents: req.MinEvents, Timeout: time.Duration(req.TimeoutMS) * time.Millisecond})
	if err != nil {
		return WorkspaceEventsResponse{}, workspaceEventRPCError(err)
	}
	response := WorkspaceEventsResponse{Events: workspaceEventResponses(result.Events)}
	if result.Completion.Configured {
		response.WaitStatus = result.Completion.Status
		response.WaitTimedOut = result.Completion.TimedOut
	}
	return response, nil
}

func workspaceEventRPCError(err error) error {
	if errors.Is(err, globaldb.ErrInvalidInput) {
		return rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "invalid_workspace_event_request"})
	}
	if errors.Is(err, globaldb.ErrNotFound) {
		return rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "workspace_event_resource_not_found"})
	}
	return err
}

func parseWorkspaceEventTimeout(raw string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return nil, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "invalid_timeout_at"})
	}
	return &parsed, nil
}

func workspaceEventResponses(events []globaldb.WorkspaceEvent) []WorkspaceEventResponse {
	responses := make([]WorkspaceEventResponse, 0, len(events))
	for _, event := range events {
		responses = append(responses, workspaceEventResponse(event))
	}
	return responses
}

func workspaceEventResponse(event globaldb.WorkspaceEvent) WorkspaceEventResponse {
	return WorkspaceEventResponse{
		EventID:           event.EventID,
		WorkspaceID:       event.WorkspaceID,
		Sequence:          event.Sequence,
		EventType:         event.EventType,
		SubjectType:       event.SubjectType,
		SubjectID:         event.SubjectID,
		ProducerType:      event.ProducerType,
		ProducerID:        event.ProducerID,
		CorrelationID:     event.CorrelationID,
		CausationID:       event.CausationID,
		PayloadJSON:       event.PayloadJSON,
		PayloadRefJSON:    event.PayloadRefJSON,
		AttentionRequired: event.AttentionRequired,
		CreatedAt:         event.CreatedAt.Format(time.RFC3339Nano),
	}
}

func workspaceEventSubscriptionResponse(subscription globaldb.EventSubscription) WorkspaceEventSubscriptionResponse {
	response := WorkspaceEventSubscriptionResponse{
		SubscriptionID:          subscription.SubscriptionID,
		WorkspaceID:             subscription.WorkspaceID,
		OwnerSessionID:          subscription.OwnerSessionID,
		Name:                    subscription.Name,
		FilterJSON:              subscription.FilterJSON,
		DeliveryTargetType:      subscription.DeliveryTargetType,
		DeliveryTargetID:        subscription.DeliveryTargetID,
		DeliveryPolicyJSON:      subscription.DeliveryPolicyJSON,
		CursorSequence:          subscription.CursorSequence,
		AckSequence:             subscription.AckSequence,
		Status:                  subscription.Status,
		CompletionConditionJSON: subscription.CompletionConditionJSON,
		CreatedAt:               subscription.CreatedAt.Format(time.RFC3339Nano),
		UpdatedAt:               subscription.UpdatedAt.Format(time.RFC3339Nano),
	}
	if subscription.TimeoutAt != nil {
		response.TimeoutAt = subscription.TimeoutAt.Format(time.RFC3339Nano)
	}
	return response
}
