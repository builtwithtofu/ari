package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

type WorkspaceSignalSendRequest struct {
	EventID       string `json:"event_id,omitempty"`
	WorkspaceID   string `json:"workspace_id"`
	TargetType    string `json:"target_type"`
	TargetID      string `json:"target_id"`
	ProducerType  string `json:"producer_type,omitempty"`
	ProducerID    string `json:"producer_id,omitempty"`
	CorrelationID string `json:"correlation_id,omitempty"`
	CausationID   string `json:"causation_id,omitempty"`
	PayloadJSON   string `json:"payload_json,omitempty"`
}

type WorkspaceSignalResponse struct {
	Event WorkspaceEventResponse `json:"event"`
}

func (d *Daemon) registerWorkspaceSignalMethods(registry *rpc.MethodRegistry, store *globaldb.Store) error {
	if err := rpc.RegisterMethod(registry, rpc.Method[WorkspaceSignalSendRequest, WorkspaceSignalResponse]{
		Name:        "workspace.signals.send",
		Description: "Send a fire-and-forget workspace signal",
		Handler: func(ctx context.Context, req WorkspaceSignalSendRequest) (WorkspaceSignalResponse, error) {
			producerType := req.ProducerType
			if producerType == "" {
				producerType = "client"
			}
			workspaceID := strings.TrimSpace(req.WorkspaceID)
			targetType := strings.TrimSpace(req.TargetType)
			targetID := strings.TrimSpace(req.TargetID)
			if err := validateWorkspaceSignalTarget(ctx, store, workspaceID, targetType, targetID); err != nil {
				return WorkspaceSignalResponse{}, err
			}
			event, err := store.AppendWorkspaceEvent(ctx, globaldb.WorkspaceEvent{EventID: req.EventID, WorkspaceID: workspaceID, EventType: globaldb.WorkspaceEventSignalSent, SubjectType: targetType, SubjectID: targetID, ProducerType: producerType, ProducerID: req.ProducerID, CorrelationID: req.CorrelationID, CausationID: req.CausationID, PayloadJSON: req.PayloadJSON, AttentionRequired: true})
			if err != nil {
				return WorkspaceSignalResponse{}, workspaceEventRPCError(err)
			}
			return WorkspaceSignalResponse{Event: workspaceEventResponse(event)}, nil
		},
	}); err != nil {
		return fmt.Errorf("register workspace.signals.send: %w", err)
	}
	return nil
}

func validateWorkspaceSignalTarget(ctx context.Context, store *globaldb.Store, workspaceID, targetType, targetID string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	targetType = strings.TrimSpace(targetType)
	targetID = strings.TrimSpace(targetID)
	if workspaceID == "" || targetType == "" || targetID == "" {
		return nil
	}
	scopeMismatch := func(targetWorkspaceID string) error {
		if strings.TrimSpace(targetWorkspaceID) == workspaceID {
			return nil
		}
		return rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "signal_target_scope_mismatch", "workspace_id": workspaceID, "target_type": targetType, "target_id": targetID, "target_workspace_id": strings.TrimSpace(targetWorkspaceID)})
	}
	resourceNotFound := func(err error) error {
		if errors.Is(err, globaldb.ErrNotFound) {
			return rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "signal_target_not_found", "workspace_id": workspaceID, "target_type": targetType, "target_id": targetID})
		}
		return err
	}

	switch targetType {
	case "fanout_group":
		group, err := store.GetFanoutGroup(ctx, targetID)
		if err != nil {
			return resourceNotFound(err)
		}
		return scopeMismatch(group.WorkspaceID)
	case "harness_session":
		session, err := store.GetHarnessSession(ctx, targetID)
		if err != nil {
			return resourceNotFound(err)
		}
		return scopeMismatch(session.WorkspaceID)
	case "event_subscription", "subscription":
		subscription, err := store.GetEventSubscription(ctx, targetID)
		if err != nil {
			return resourceNotFound(err)
		}
		return scopeMismatch(subscription.WorkspaceID)
	default:
		return nil
	}
}
