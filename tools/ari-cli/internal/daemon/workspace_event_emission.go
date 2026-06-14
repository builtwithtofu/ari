package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
)

const (
	workspaceEventWorkerStarted      = "worker.started"
	workspaceEventWorkerCompleted    = "worker.completed"
	workspaceEventWorkerFailed       = "worker.failed"
	workspaceEventWorkerStopped      = "worker.stopped"
	workspaceEventSessionCompleted   = "session.completed"
	workspaceEventSessionFailed      = "session.failed"
	workspaceEventSessionStopped     = "session.stopped"
	workspaceEventSessionIdle        = "session.idle"
	workspaceEventSessionNeedsInput  = "session.needs_input"
	workspaceEventMessageSent        = "message.sent"
	workspaceEventHarnessEventPrefix = "harness.event."

	workspaceEventSubjectHarnessSession    = "harness_session"
	workspaceEventProducerSession          = "session"
	workspaceEventProducerDaemon           = "daemon"
	workspaceEventProducerHarnessLifecycle = "harness_lifecycle"
)

func appendWorkspaceEvent(ctx context.Context, store *globaldb.Store, event globaldb.WorkspaceEvent) (globaldb.WorkspaceEvent, error) {
	if store == nil {
		return event, nil
	}
	return store.AppendWorkspaceEvent(ctx, event)
}

func appendHarnessSessionWorkspaceEvent(ctx context.Context, store *globaldb.Store, run HarnessSession, eventType, status, finalResponseID string, attentionRequired bool) error {
	payload := map[string]string{
		"session_id": strings.TrimSpace(run.HarnessSessionID),
		"harness":    strings.TrimSpace(run.Executor),
		"status":     strings.TrimSpace(status),
	}
	if strings.TrimSpace(run.TaskID) != "" {
		payload["task_id"] = strings.TrimSpace(run.TaskID)
	}
	_, err := appendWorkspaceEvent(ctx, store, globaldb.WorkspaceEvent{
		WorkspaceID:       run.WorkspaceID,
		EventType:         eventType,
		SubjectType:       workspaceEventSubjectHarnessSession,
		SubjectID:         run.HarnessSessionID,
		ProducerType:      workspaceEventProducerDaemon,
		ProducerID:        workspaceEventProducerHarnessLifecycle,
		CorrelationID:     run.HarnessSessionID,
		PayloadJSON:       daemonEventPayload(payload),
		PayloadRefJSON:    daemonEventPayload(finalResponsePayloadRef(finalResponseID)),
		AttentionRequired: attentionRequired,
	})
	return err
}

// finalResponsePayloadRef builds the {kind, id} artifact link that keeps
// result bodies out of event rows (ADR 0011). An empty id yields an empty ref.
func finalResponsePayloadRef(finalResponseID string) map[string]string {
	ref := map[string]string{}
	if finalResponseID = strings.TrimSpace(finalResponseID); finalResponseID != "" {
		ref["kind"] = "final_response"
		ref["id"] = finalResponseID
	}
	return ref
}

func appendHarnessRuntimeWorkspaceEvents(ctx context.Context, store *globaldb.Store, run HarnessSession, events []HarnessRuntimeEvent) error {
	if store == nil {
		return nil
	}
	for _, runtimeEvent := range events {
		kind := strings.TrimSpace(runtimeEvent.Kind)
		if kind == "" {
			continue
		}
		sessionID := strings.TrimSpace(runtimeEvent.SessionID)
		if sessionID == "" {
			sessionID = strings.TrimSpace(run.HarnessSessionID)
		}
		runtimeEventID := strings.TrimSpace(runtimeEvent.EventID)
		if runtimeEventID == "" {
			runtimeEventID = fmt.Sprintf("%s:event-%d", sessionID, runtimeEvent.Sequence)
		}
		payloadJSON, err := harnessRuntimeWorkspaceEventPayload(runtimeEvent, runtimeEventID, sessionID, kind)
		if err != nil {
			return err
		}
		payloadRef := map[string]string{
			"kind": "harness_runtime_event",
			"id":   runtimeEventID,
		}
		if runtimeEvent.Sequence > 0 {
			payloadRef["sequence"] = fmt.Sprintf("%d", runtimeEvent.Sequence)
		}
		_, err = appendWorkspaceEvent(ctx, store, globaldb.WorkspaceEvent{
			EventID:           runtimeEventID,
			WorkspaceID:       run.WorkspaceID,
			EventType:         workspaceEventHarnessEventPrefix + kind,
			SubjectType:       workspaceEventSubjectHarnessSession,
			SubjectID:         sessionID,
			ProducerType:      workspaceEventProducerSession,
			ProducerID:        sessionID,
			CorrelationID:     strings.TrimSpace(run.HarnessSessionID),
			PayloadJSON:       payloadJSON,
			PayloadRefJSON:    daemonEventPayload(payloadRef),
			AttentionRequired: kind == string(HarnessEventError),
			CreatedAt:         runtimeEvent.CreatedAt,
		})
		if err != nil {
			return err
		}
		if kind == string(HarnessEventApproval) {
			if err := appendSessionNeedsInputWorkspaceEvent(ctx, store, run, sessionID, runtimeEventID); err != nil {
				return err
			}
		}
	}
	return nil
}

// appendSessionNeedsInputWorkspaceEvent normalizes a harness approval signal
// into the Ari-level session.needs_input fact so orchestrators and humans can
// subscribe to "a session is blocked on input" without provider ontology.
func appendSessionNeedsInputWorkspaceEvent(ctx context.Context, store *globaldb.Store, run HarnessSession, sessionID, runtimeEventID string) error {
	payload := map[string]string{
		"session_id":       sessionID,
		"harness":          strings.TrimSpace(run.Executor),
		"status":           "needs_input",
		"harness_event_id": runtimeEventID,
	}
	_, err := appendWorkspaceEvent(ctx, store, globaldb.WorkspaceEvent{
		WorkspaceID:       run.WorkspaceID,
		EventType:         workspaceEventSessionNeedsInput,
		SubjectType:       workspaceEventSubjectHarnessSession,
		SubjectID:         sessionID,
		ProducerType:      workspaceEventProducerDaemon,
		ProducerID:        workspaceEventProducerHarnessLifecycle,
		CorrelationID:     strings.TrimSpace(run.HarnessSessionID),
		CausationID:       runtimeEventID,
		PayloadJSON:       daemonEventPayload(payload),
		PayloadRefJSON:    daemonEventPayload(map[string]string{"kind": "harness_runtime_event", "id": runtimeEventID}),
		AttentionRequired: true,
	})
	return err
}

// appendSessionIdleWorkspaceEvent records that a sticky session finished a
// turn and is available for the next input — the durable fact behind
// wake-when-idle orchestration. Ephemeral sessions terminate instead of
// idling and must not emit it.
func appendSessionIdleWorkspaceEvent(ctx context.Context, store *globaldb.Store, run HarnessSession) error {
	payload := map[string]string{
		"session_id": strings.TrimSpace(run.HarnessSessionID),
		"harness":    strings.TrimSpace(run.Executor),
		"status":     "idle",
	}
	_, err := appendWorkspaceEvent(ctx, store, globaldb.WorkspaceEvent{
		WorkspaceID:    run.WorkspaceID,
		EventType:      workspaceEventSessionIdle,
		SubjectType:    workspaceEventSubjectHarnessSession,
		SubjectID:      run.HarnessSessionID,
		ProducerType:   workspaceEventProducerDaemon,
		ProducerID:     workspaceEventProducerHarnessLifecycle,
		CorrelationID:  run.HarnessSessionID,
		PayloadJSON:    daemonEventPayload(payload),
		PayloadRefJSON: "{}",
	})
	return err
}

func harnessRuntimeWorkspaceEventPayload(runtimeEvent HarnessRuntimeEvent, runtimeEventID, sessionID, kind string) (string, error) {
	rawPayload := runtimeEvent.Payload
	if len(rawPayload) == 0 {
		rawPayload = json.RawMessage(`{}`)
	}
	if !json.Valid(rawPayload) {
		return "", fmt.Errorf("harness runtime event %q payload json is invalid", runtimeEventID)
	}
	payload := map[string]any{
		"harness_event_id": runtimeEventID,
		"kind":             kind,
		"sequence":         runtimeEvent.Sequence,
		"session_id":       sessionID,
		"payload":          rawPayload,
	}
	if runID := strings.TrimSpace(runtimeEvent.RunID); runID != "" {
		payload["run_id"] = runID
	}
	if providerKind := strings.TrimSpace(runtimeEvent.ProviderKind); providerKind != "" {
		payload["provider_kind"] = providerKind
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// appendFanoutWorkerWorkspaceEvent records a fanout worker fact: the daemon
// derives the event and both projections (member row, terminal inbox item),
// then the store commits them in one transaction so event history and
// materialized projections can never diverge.
func appendFanoutWorkerWorkspaceEvent(ctx context.Context, store *globaldb.Store, member globaldb.FanoutMember, eventType, producerID, causationID, finalResponseID string, attentionRequired bool) error {
	if store == nil {
		return nil
	}
	status := workerEventStatus(eventType)
	payload := map[string]string{
		"status":                   status,
		"fanout_group_id":          member.FanoutGroupID,
		"fanout_member_id":         member.FanoutMemberID,
		"target_profile_id":        member.TargetProfileID,
		"request_agent_message_id": member.RequestAgentMessageID,
	}
	if strings.TrimSpace(causationID) == "" {
		causationID = member.RequestAgentMessageID
	}
	if eventType == workspaceEventWorkerCompleted {
		payload["reply_agent_message_id"] = strings.TrimSpace(causationID)
	}
	event := globaldb.WorkspaceEvent{
		WorkspaceID:       member.WorkspaceID,
		EventType:         eventType,
		SubjectType:       workspaceEventSubjectHarnessSession,
		SubjectID:         member.WorkerSessionID,
		ProducerType:      workspaceEventProducerSession,
		ProducerID:        strings.TrimSpace(producerID),
		CorrelationID:     member.FanoutGroupID,
		CausationID:       strings.TrimSpace(causationID),
		PayloadJSON:       daemonEventPayload(payload),
		PayloadRefJSON:    daemonEventPayload(finalResponsePayloadRef(finalResponseID)),
		AttentionRequired: attentionRequired,
	}
	_, err := appendWorkspaceEvent(ctx, store, event)
	return err
}

func finalResponseIDFromWorkspaceEvent(event globaldb.WorkspaceEvent) string {
	return finalResponseIDFromWorkspaceEventRef(event.PayloadRefJSON)
}

func workerEventStatus(eventType string) string {
	switch eventType {
	case workspaceEventWorkerStarted:
		return "running"
	case workspaceEventWorkerCompleted:
		return "completed"
	case workspaceEventWorkerFailed:
		return "failed"
	case workspaceEventWorkerStopped:
		return "stopped"
	default:
		return strings.TrimSpace(eventType)
	}
}

func workspaceEventTypeForFanoutWorkerStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "completed":
		return workspaceEventWorkerCompleted
	case "failed":
		return workspaceEventWorkerFailed
	case "stopped":
		return workspaceEventWorkerStopped
	default:
		return ""
	}
}
