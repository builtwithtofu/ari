package daemon

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

type WorkspaceTimerCreateRequest struct {
	TimerID        string `json:"timer_id,omitempty"`
	WorkspaceID    string `json:"workspace_id"`
	OwnerSessionID string `json:"owner_session_id,omitempty"`
	SubscriptionID string `json:"subscription_id,omitempty"`
	SubjectType    string `json:"subject_type,omitempty"`
	SubjectID      string `json:"subject_id,omitempty"`
	Purpose        string `json:"purpose,omitempty"`
	FireAt         string `json:"fire_at"`
	PayloadJSON    string `json:"payload_json,omitempty"`
}

type WorkspaceTimerGetRequest struct {
	TimerID string `json:"timer_id"`
}

type WorkspaceTimerCancelRequest struct {
	TimerID string `json:"timer_id"`
}

type WorkspaceTimersFireDueRequest struct {
	Now   string `json:"now,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type WorkspaceTimersResponse struct {
	Timers []WorkspaceTimerResponse `json:"timers"`
}

type WorkspaceTimerResponse struct {
	TimerID        string `json:"timer_id"`
	WorkspaceID    string `json:"workspace_id"`
	OwnerSessionID string `json:"owner_session_id,omitempty"`
	SubscriptionID string `json:"subscription_id,omitempty"`
	SubjectType    string `json:"subject_type,omitempty"`
	SubjectID      string `json:"subject_id,omitempty"`
	Purpose        string `json:"purpose,omitempty"`
	Status         string `json:"status"`
	FireAt         string `json:"fire_at"`
	PayloadJSON    string `json:"payload_json"`
	FiredEventID   string `json:"fired_event_id,omitempty"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

func (d *Daemon) registerWorkspaceTimerMethods(registry *rpc.MethodRegistry, store *globaldb.Store) error {
	if err := rpc.RegisterMethod(registry, rpc.Method[WorkspaceTimerCreateRequest, WorkspaceTimerResponse]{
		Name:        "workspace.timers.create",
		Description: "Create a durable workspace timer",
		Handler: func(ctx context.Context, req WorkspaceTimerCreateRequest) (WorkspaceTimerResponse, error) {
			fireAt, err := parseWorkspaceTimerTime(req.FireAt, "invalid_fire_at")
			if err != nil {
				return WorkspaceTimerResponse{}, err
			}
			timer, err := store.CreateWorkspaceTimer(ctx, globaldb.WorkspaceTimer{TimerID: req.TimerID, WorkspaceID: req.WorkspaceID, OwnerSessionID: req.OwnerSessionID, SubscriptionID: req.SubscriptionID, SubjectType: req.SubjectType, SubjectID: req.SubjectID, Purpose: req.Purpose, FireAt: fireAt, PayloadJSON: req.PayloadJSON})
			if err != nil {
				return WorkspaceTimerResponse{}, workspaceTimerRPCError(err)
			}
			return workspaceTimerResponse(timer), nil
		},
	}); err != nil {
		return fmt.Errorf("register workspace.timers.create: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[WorkspaceTimerGetRequest, WorkspaceTimerResponse]{
		Name:        "workspace.timers.get",
		Description: "Get a durable workspace timer",
		Handler: func(ctx context.Context, req WorkspaceTimerGetRequest) (WorkspaceTimerResponse, error) {
			timer, err := store.GetWorkspaceTimer(ctx, req.TimerID)
			if err != nil {
				return WorkspaceTimerResponse{}, workspaceTimerRPCError(err)
			}
			return workspaceTimerResponse(timer), nil
		},
	}); err != nil {
		return fmt.Errorf("register workspace.timers.get: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[WorkspaceTimerCancelRequest, WorkspaceTimerResponse]{
		Name:        "workspace.timers.cancel",
		Description: "Cancel a scheduled workspace timer",
		Handler: func(ctx context.Context, req WorkspaceTimerCancelRequest) (WorkspaceTimerResponse, error) {
			timer, err := store.CancelWorkspaceTimer(ctx, req.TimerID)
			if err != nil {
				return WorkspaceTimerResponse{}, workspaceTimerRPCError(err)
			}
			return workspaceTimerResponse(timer), nil
		},
	}); err != nil {
		return fmt.Errorf("register workspace.timers.cancel: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[WorkspaceTimersFireDueRequest, WorkspaceTimersResponse]{
		Name:        "workspace.timers.fire_due",
		Description: "Fire due workspace timers and append timer events",
		Handler: func(ctx context.Context, req WorkspaceTimersFireDueRequest) (WorkspaceTimersResponse, error) {
			now := time.Now().UTC()
			if req.Now != "" {
				parsed, err := parseWorkspaceTimerTime(req.Now, "invalid_now")
				if err != nil {
					return WorkspaceTimersResponse{}, err
				}
				now = parsed
			}
			timers, err := store.FireDueWorkspaceTimers(ctx, now, req.Limit)
			if err != nil {
				return WorkspaceTimersResponse{}, workspaceTimerRPCError(err)
			}
			return WorkspaceTimersResponse{Timers: workspaceTimerResponses(timers)}, nil
		},
	}); err != nil {
		return fmt.Errorf("register workspace.timers.fire_due: %w", err)
	}

	return nil
}

func parseWorkspaceTimerTime(raw, reason string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": reason})
	}
	return parsed, nil
}

func workspaceTimerRPCError(err error) error {
	if errors.Is(err, globaldb.ErrInvalidInput) {
		return rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "invalid_workspace_timer_request"})
	}
	if errors.Is(err, globaldb.ErrNotFound) {
		return rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "workspace_timer_not_found"})
	}
	return err
}

func workspaceTimerResponses(timers []globaldb.WorkspaceTimer) []WorkspaceTimerResponse {
	responses := make([]WorkspaceTimerResponse, 0, len(timers))
	for _, timer := range timers {
		responses = append(responses, workspaceTimerResponse(timer))
	}
	return responses
}

func workspaceTimerResponse(timer globaldb.WorkspaceTimer) WorkspaceTimerResponse {
	return WorkspaceTimerResponse{TimerID: timer.TimerID, WorkspaceID: timer.WorkspaceID, OwnerSessionID: timer.OwnerSessionID, SubscriptionID: timer.SubscriptionID, SubjectType: timer.SubjectType, SubjectID: timer.SubjectID, Purpose: timer.Purpose, Status: timer.Status, FireAt: timer.FireAt.Format(time.RFC3339Nano), PayloadJSON: timer.PayloadJSON, FiredEventID: timer.FiredEventID, CreatedAt: timer.CreatedAt.Format(time.RFC3339Nano), UpdatedAt: timer.UpdatedAt.Format(time.RFC3339Nano)}
}
