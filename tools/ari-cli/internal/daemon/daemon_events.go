package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

const (
	daemonEventSessionMessageSent = "session.message.sent"
	daemonEventSessionCompleted   = "session.lifecycle.completed"
	daemonEventSessionFailed      = "session.lifecycle.failed"
)

type DaemonEventsAfterRequest struct {
	AfterEventID string `json:"after_event_id,omitempty"`
	Limit        int    `json:"limit,omitempty"`
}

type DaemonEventsResponse struct {
	Events []DaemonEventResponse `json:"events"`
}

type DaemonAttentionClearRequest struct {
	EventID string `json:"event_id"`
}

type DaemonAttentionClearResponse struct {
	Cleared bool `json:"cleared"`
}

type DaemonEventResponse struct {
	EventID            string `json:"event_id"`
	WorkspaceID        string `json:"workspace_id,omitempty"`
	SessionID          string `json:"session_id,omitempty"`
	EventType          string `json:"event_type"`
	SubjectType        string `json:"subject_type"`
	SubjectID          string `json:"subject_id"`
	PayloadJSON        string `json:"payload_json"`
	AttentionRequired  bool   `json:"attention_required"`
	AttentionClearedAt string `json:"attention_cleared_at,omitempty"`
	CreatedAt          string `json:"created_at"`
}

func (d *Daemon) registerDaemonEventMethods(registry *rpc.MethodRegistry, store *globaldb.Store) error {
	if err := rpc.RegisterMethod(registry, rpc.Method[DaemonEventsAfterRequest, DaemonEventsResponse]{
		Name:        "daemon.events.after",
		Description: "List persisted daemon events after an event cursor",
		Handler: func(ctx context.Context, req DaemonEventsAfterRequest) (DaemonEventsResponse, error) {
			events, err := store.ListDaemonEventsAfter(ctx, req.AfterEventID, req.Limit)
			if err != nil {
				if errors.Is(err, globaldb.ErrNotFound) {
					return DaemonEventsResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "unknown_event_cursor", "after_event_id": req.AfterEventID})
				}
				return DaemonEventsResponse{}, err
			}
			return DaemonEventsResponse{Events: daemonEventResponses(events)}, nil
		},
	}); err != nil {
		return fmt.Errorf("register daemon.events.after: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[struct{}, DaemonEventsResponse]{
		Name:        "daemon.events.attention",
		Description: "List daemon events still requiring attention",
		Handler: func(ctx context.Context, req struct{}) (DaemonEventsResponse, error) {
			_ = req
			events, err := store.ListDaemonAttentionEvents(ctx)
			if err != nil {
				return DaemonEventsResponse{}, err
			}
			return DaemonEventsResponse{Events: daemonEventResponses(events)}, nil
		},
	}); err != nil {
		return fmt.Errorf("register daemon.events.attention: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[DaemonAttentionClearRequest, DaemonAttentionClearResponse]{
		Name:        "daemon.events.attention.clear",
		Description: "Clear attention for one daemon event",
		Handler: func(ctx context.Context, req DaemonAttentionClearRequest) (DaemonAttentionClearResponse, error) {
			eventID := strings.TrimSpace(req.EventID)
			if eventID == "" {
				return DaemonAttentionClearResponse{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "missing_required_fields", "missing_field": "event_id"})
			}
			if err := store.ClearDaemonEventAttention(ctx, eventID); err != nil {
				return DaemonAttentionClearResponse{}, err
			}
			return DaemonAttentionClearResponse{Cleared: true}, nil
		},
	}); err != nil {
		return fmt.Errorf("register daemon.events.attention.clear: %w", err)
	}
	return nil
}

func daemonEventResponses(events []globaldb.DaemonEvent) []DaemonEventResponse {
	responses := make([]DaemonEventResponse, 0, len(events))
	for _, event := range events {
		resp := DaemonEventResponse{EventID: event.EventID, WorkspaceID: event.WorkspaceID, SessionID: event.SessionID, EventType: event.EventType, SubjectType: event.SubjectType, SubjectID: event.SubjectID, PayloadJSON: event.PayloadJSON, AttentionRequired: event.AttentionRequired, CreatedAt: event.CreatedAt.Format(time.RFC3339Nano)}
		if event.AttentionClearedAt != nil {
			resp.AttentionClearedAt = event.AttentionClearedAt.Format(time.RFC3339Nano)
		}
		responses = append(responses, resp)
	}
	return responses
}

func appendDaemonEvent(ctx context.Context, store *globaldb.Store, event globaldb.DaemonEvent) error {
	if store == nil {
		return nil
	}
	_, err := store.AppendDaemonEvent(ctx, event)
	return err
}

func daemonEventPayload(values map[string]string) string {
	if len(values) == 0 {
		return "{}"
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}
