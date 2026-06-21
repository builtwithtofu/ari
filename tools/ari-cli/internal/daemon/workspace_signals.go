package daemon

import (
	"context"
	"fmt"

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
			event, err := store.AppendWorkspaceEvent(ctx, globaldb.WorkspaceEvent{EventID: req.EventID, WorkspaceID: req.WorkspaceID, EventType: globaldb.WorkspaceEventSignalSent, SubjectType: req.TargetType, SubjectID: req.TargetID, ProducerType: producerType, ProducerID: req.ProducerID, CorrelationID: req.CorrelationID, CausationID: req.CausationID, PayloadJSON: req.PayloadJSON, AttentionRequired: true})
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
