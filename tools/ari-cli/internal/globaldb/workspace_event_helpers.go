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

func FinalResponseIDFromWorkspaceEventRef(raw string) string {
	ref := WorkspaceEventStringPayload(raw)
	if strings.TrimSpace(ref["kind"]) != "final_response" {
		return ""
	}
	return strings.TrimSpace(ref["id"])
}
