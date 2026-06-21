package globaldb

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	WorkspaceEventWorkerStarted   = "worker.started"
	WorkspaceEventWorkerCompleted = "worker.completed"
	WorkspaceEventWorkerFailed    = "worker.failed"
	WorkspaceEventWorkerStopped   = "worker.stopped"

	WorkspaceEventCommandStarted   = "command.started"
	WorkspaceEventCommandCompleted = "command.completed"
	WorkspaceEventCommandFailed    = "command.failed"
	WorkspaceEventCommandStopped   = "command.stopped"
	WorkspaceEventCommandUpdated   = "command.updated"

	WorkspaceEventContextExcerptCreated = "context_excerpt.created"
	WorkspaceEventMessageSent           = "message.sent"

	WorkspaceEventSessionCompleted  = "session.completed"
	WorkspaceEventSessionFailed     = "session.failed"
	WorkspaceEventSessionStopped    = "session.stopped"
	WorkspaceEventSessionIdle       = "session.idle"
	WorkspaceEventSessionNeedsInput = "session.needs_input"

	WorkspaceEventHarnessEventPrefix = "harness.event."
	WorkspaceEventHarnessLifecycle   = WorkspaceEventHarnessEventPrefix + "lifecycle"
	WorkspaceEventHarnessAgentText   = WorkspaceEventHarnessEventPrefix + "agent_text"
	WorkspaceEventHarnessTool        = WorkspaceEventHarnessEventPrefix + "tool"
	WorkspaceEventHarnessFileChange  = WorkspaceEventHarnessEventPrefix + "file_change"
	WorkspaceEventHarnessApproval    = WorkspaceEventHarnessEventPrefix + "approval"
	WorkspaceEventHarnessError       = WorkspaceEventHarnessEventPrefix + "error"
	WorkspaceEventHarnessUsage       = WorkspaceEventHarnessEventPrefix + "usage"
	WorkspaceEventHarnessDebug       = WorkspaceEventHarnessEventPrefix + "debug"

	WorkspaceEventSignalSent = "signal.sent"

	WorkspaceEventTimerFired = "timer.fired"

	WorkspaceEventDeliveryAttempted      = "delivery.attempted"
	WorkspaceEventDeliveryCompleted      = "delivery.completed"
	WorkspaceEventDeliveryFailed         = "delivery.failed"
	WorkspaceEventDeliveryRetryScheduled = "delivery.retry_scheduled"
)

func IsFanoutWorkerWorkspaceEvent(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case WorkspaceEventWorkerStarted, WorkspaceEventWorkerCompleted, WorkspaceEventWorkerFailed, WorkspaceEventWorkerStopped:
		return true
	default:
		return false
	}
}

func WorkerEventStatus(eventType string) string {
	switch strings.TrimSpace(eventType) {
	case WorkspaceEventWorkerStarted:
		return "running"
	case WorkspaceEventWorkerCompleted:
		return "completed"
	case WorkspaceEventWorkerFailed:
		return "failed"
	case WorkspaceEventWorkerStopped:
		return "stopped"
	default:
		return strings.TrimSpace(eventType)
	}
}

func WorkspaceEventStringPayload(raw string) map[string]string {
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(payload))
	for key, value := range payload {
		switch typed := value.(type) {
		case string:
			out[key] = typed
		case fmt.Stringer:
			out[key] = typed.String()
		case nil:
			out[key] = ""
		default:
			out[key] = fmt.Sprint(typed)
		}
	}
	return out
}

func WorkspaceTimerTargetSubscriptionIDFromEvent(event WorkspaceEvent) string {
	if strings.TrimSpace(event.EventType) != WorkspaceEventTimerFired {
		return ""
	}
	payload := WorkspaceEventStringPayload(event.PayloadJSON)
	return strings.TrimSpace(payload["target_subscription_id"])
}

func FinalResponseIDFromWorkspaceEventRef(raw string) string {
	ref := WorkspaceEventStringPayload(raw)
	if strings.TrimSpace(ref["kind"]) != "final_response" {
		return ""
	}
	return strings.TrimSpace(ref["id"])
}
