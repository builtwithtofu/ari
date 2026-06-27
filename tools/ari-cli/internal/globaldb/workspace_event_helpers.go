package globaldb

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	WorkspaceEventDeliveryPrefix  = "delivery."
	WorkspaceEventOperationPrefix = "operation."
	WorkspaceEventCommandPrefix   = "command."
	WorkspaceEventSessionPrefix   = "session."

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

	WorkspaceEventSubjectAgentMessage      = "agent_message"
	WorkspaceEventSubjectCommand           = "command"
	WorkspaceEventSubjectContextExcerpt    = "context_excerpt"
	WorkspaceEventSubjectEventSubscription = "event_subscription"
	WorkspaceEventSubjectFanoutGroup       = "fanout_group"
	WorkspaceEventSubjectHarnessSession    = "harness_session"
	WorkspaceEventSubjectOperation         = "operation"
	WorkspaceEventSubjectPendingDelivery   = "pending_delivery"
	WorkspaceEventSubjectSubscription      = "subscription"
	WorkspaceEventSubjectTimer             = "timer"

	WorkspaceEventProducerCommand           = "command"
	WorkspaceEventProducerClient            = "client"
	WorkspaceEventProducerDaemon            = "daemon"
	WorkspaceEventProducerHarnessLifecycle  = "harness_lifecycle"
	WorkspaceEventProducerSession           = "session"
	WorkspaceEventProducerTimer             = "timer"
	WorkspaceEventProducerWorkspaceDelivery = "workspace_delivery"

	WorkspaceEventPayloadRefFinalResponse       = "final_response"
	WorkspaceEventPayloadRefHarnessRuntimeEvent = "harness_runtime_event"
	WorkspaceEventPayloadRefOperationRecord     = "operation_record"
	WorkspaceEventPayloadRefAgentMessage        = "agent_message"
	WorkspaceEventPayloadRefCommand             = "command"
	WorkspaceEventPayloadRefContextExcerpt      = "context_excerpt"
	WorkspaceEventPayloadRefTimer               = "timer"
)

// WorkspaceEventStringMapJSON marshals the small string payload/ref maps used
// in event rows. Empty maps become an empty JSON object so callers do not need
// to repeat event-row defaults.
func WorkspaceEventStringMapJSON(values map[string]string) string {
	if len(values) == 0 {
		return "{}"
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func WorkspaceEventPayloadRefJSON(kind, id string) string {
	kind = strings.TrimSpace(kind)
	id = strings.TrimSpace(id)
	if kind == "" || id == "" {
		return "{}"
	}
	return WorkspaceEventStringMapJSON(map[string]string{"kind": kind, "id": id})
}

func FinalResponsePayloadRefJSON(finalResponseID string) string {
	return WorkspaceEventPayloadRefJSON(WorkspaceEventPayloadRefFinalResponse, finalResponseID)
}

func OperationWorkspaceEventType(operationType string) string {
	return WorkspaceEventOperationPrefix + strings.TrimSpace(operationType)
}

func WorkspaceEventTypeForWorkerStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "completed":
		return WorkspaceEventWorkerCompleted
	case "failed":
		return WorkspaceEventWorkerFailed
	case "stopped":
		return WorkspaceEventWorkerStopped
	default:
		return ""
	}
}

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

type HarnessSessionWorkspaceEventParams struct {
	WorkspaceID       string
	SessionID         string
	Harness           string
	TaskID            string
	EventType         string
	Status            string
	FinalResponseID   string
	AttentionRequired bool
}

func NewHarnessSessionWorkspaceEvent(params HarnessSessionWorkspaceEventParams) WorkspaceEvent {
	payload := map[string]string{
		"session_id": strings.TrimSpace(params.SessionID),
		"harness":    strings.TrimSpace(params.Harness),
		"status":     strings.TrimSpace(params.Status),
	}
	if taskID := strings.TrimSpace(params.TaskID); taskID != "" {
		payload["task_id"] = taskID
	}
	sessionID := strings.TrimSpace(params.SessionID)
	return WorkspaceEvent{
		WorkspaceID:       strings.TrimSpace(params.WorkspaceID),
		EventType:         strings.TrimSpace(params.EventType),
		SubjectType:       WorkspaceEventSubjectHarnessSession,
		SubjectID:         sessionID,
		ProducerType:      WorkspaceEventProducerDaemon,
		ProducerID:        WorkspaceEventProducerHarnessLifecycle,
		CorrelationID:     sessionID,
		PayloadJSON:       WorkspaceEventStringMapJSON(payload),
		PayloadRefJSON:    FinalResponsePayloadRefJSON(params.FinalResponseID),
		AttentionRequired: params.AttentionRequired,
	}
}

type HarnessRuntimeWorkspaceEventParams struct {
	EventID       string
	WorkspaceID   string
	SessionID     string
	RootSessionID string
	Kind          string
	Sequence      int
	Payload       json.RawMessage
	RunID         string
	ProviderKind  string
	CreatedAt     time.Time
}

func NewHarnessRuntimeWorkspaceEvent(params HarnessRuntimeWorkspaceEventParams) (WorkspaceEvent, error) {
	kind := strings.TrimSpace(params.Kind)
	runtimeEventID := strings.TrimSpace(params.EventID)
	sessionID := strings.TrimSpace(params.SessionID)
	rawPayload := params.Payload
	if len(rawPayload) == 0 {
		rawPayload = json.RawMessage(`{}`)
	}
	if !json.Valid(rawPayload) {
		return WorkspaceEvent{}, fmt.Errorf("harness runtime event %q payload json is invalid", runtimeEventID)
	}
	payload := map[string]any{
		"harness_event_id": runtimeEventID,
		"kind":             kind,
		"sequence":         params.Sequence,
		"session_id":       sessionID,
		"payload":          rawPayload,
	}
	if runID := strings.TrimSpace(params.RunID); runID != "" {
		payload["run_id"] = runID
	}
	if providerKind := strings.TrimSpace(params.ProviderKind); providerKind != "" {
		payload["provider_kind"] = providerKind
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return WorkspaceEvent{}, err
	}
	payloadRef := map[string]string{
		"kind": WorkspaceEventPayloadRefHarnessRuntimeEvent,
		"id":   runtimeEventID,
	}
	if params.Sequence > 0 {
		payloadRef["sequence"] = fmt.Sprintf("%d", params.Sequence)
	}
	return WorkspaceEvent{
		EventID:           runtimeEventID,
		WorkspaceID:       strings.TrimSpace(params.WorkspaceID),
		EventType:         WorkspaceEventHarnessEventPrefix + kind,
		SubjectType:       WorkspaceEventSubjectHarnessSession,
		SubjectID:         sessionID,
		ProducerType:      WorkspaceEventProducerSession,
		ProducerID:        sessionID,
		CorrelationID:     strings.TrimSpace(params.RootSessionID),
		PayloadJSON:       string(encoded),
		PayloadRefJSON:    WorkspaceEventStringMapJSON(payloadRef),
		AttentionRequired: kind == strings.TrimPrefix(WorkspaceEventHarnessError, WorkspaceEventHarnessEventPrefix),
		CreatedAt:         params.CreatedAt,
	}, nil
}

type SessionNeedsInputWorkspaceEventParams struct {
	WorkspaceID    string
	SessionID      string
	RootSessionID  string
	Harness        string
	HarnessEventID string
}

func NewSessionNeedsInputWorkspaceEvent(params SessionNeedsInputWorkspaceEventParams) WorkspaceEvent {
	sessionID := strings.TrimSpace(params.SessionID)
	harnessEventID := strings.TrimSpace(params.HarnessEventID)
	payload := map[string]string{
		"session_id":       sessionID,
		"harness":          strings.TrimSpace(params.Harness),
		"status":           "needs_input",
		"harness_event_id": harnessEventID,
	}
	return WorkspaceEvent{
		WorkspaceID:       strings.TrimSpace(params.WorkspaceID),
		EventType:         WorkspaceEventSessionNeedsInput,
		SubjectType:       WorkspaceEventSubjectHarnessSession,
		SubjectID:         sessionID,
		ProducerType:      WorkspaceEventProducerDaemon,
		ProducerID:        WorkspaceEventProducerHarnessLifecycle,
		CorrelationID:     strings.TrimSpace(params.RootSessionID),
		CausationID:       harnessEventID,
		PayloadJSON:       WorkspaceEventStringMapJSON(payload),
		PayloadRefJSON:    WorkspaceEventPayloadRefJSON(WorkspaceEventPayloadRefHarnessRuntimeEvent, harnessEventID),
		AttentionRequired: true,
	}
}

func NewSessionIdleWorkspaceEvent(params HarnessSessionWorkspaceEventParams) WorkspaceEvent {
	sessionID := strings.TrimSpace(params.SessionID)
	return WorkspaceEvent{
		WorkspaceID:    strings.TrimSpace(params.WorkspaceID),
		EventType:      WorkspaceEventSessionIdle,
		SubjectType:    WorkspaceEventSubjectHarnessSession,
		SubjectID:      sessionID,
		ProducerType:   WorkspaceEventProducerDaemon,
		ProducerID:     WorkspaceEventProducerHarnessLifecycle,
		CorrelationID:  sessionID,
		PayloadJSON:    WorkspaceEventStringMapJSON(map[string]string{"session_id": sessionID, "harness": strings.TrimSpace(params.Harness), "status": "idle"}),
		PayloadRefJSON: "{}",
	}
}

type FanoutWorkerWorkspaceEventParams struct {
	WorkspaceID           string
	EventType             string
	WorkerSessionID       string
	ProducerID            string
	CausationID           string
	FinalResponseID       string
	AttentionRequired     bool
	FanoutGroupID         string
	FanoutMemberID        string
	SourceSessionID       string
	SourceAgentID         string
	TargetProfileID       string
	RequestAgentMessageID string
}

func NewFanoutWorkerWorkspaceEvent(params FanoutWorkerWorkspaceEventParams) WorkspaceEvent {
	eventType := strings.TrimSpace(params.EventType)
	explicitCausationID := strings.TrimSpace(params.CausationID)
	causationID := explicitCausationID
	requestID := strings.TrimSpace(params.RequestAgentMessageID)
	if causationID == "" && eventType != WorkspaceEventWorkerCompleted {
		causationID = requestID
	}
	sourceSessionID := strings.TrimSpace(params.SourceSessionID)
	if sourceSessionID == "" {
		sourceSessionID = strings.TrimSpace(params.ProducerID)
	}
	payload := map[string]string{
		"status":                   WorkerEventStatus(eventType),
		"fanout_group_id":          strings.TrimSpace(params.FanoutGroupID),
		"fanout_member_id":         strings.TrimSpace(params.FanoutMemberID),
		"source_session_id":        sourceSessionID,
		"source_agent_id":          strings.TrimSpace(params.SourceAgentID),
		"target_profile_id":        strings.TrimSpace(params.TargetProfileID),
		"request_agent_message_id": requestID,
	}
	if eventType == WorkspaceEventWorkerCompleted && explicitCausationID != "" {
		payload["reply_agent_message_id"] = explicitCausationID
	}
	return WorkspaceEvent{
		WorkspaceID:       strings.TrimSpace(params.WorkspaceID),
		EventType:         eventType,
		SubjectType:       WorkspaceEventSubjectHarnessSession,
		SubjectID:         strings.TrimSpace(params.WorkerSessionID),
		ProducerType:      WorkspaceEventProducerSession,
		ProducerID:        strings.TrimSpace(params.ProducerID),
		CorrelationID:     strings.TrimSpace(params.FanoutGroupID),
		CausationID:       causationID,
		PayloadJSON:       WorkspaceEventStringMapJSON(payload),
		PayloadRefJSON:    FinalResponsePayloadRefJSON(params.FinalResponseID),
		AttentionRequired: params.AttentionRequired,
	}
}

type DecodedFanoutWorkerWorkspaceEvent struct {
	FanoutMemberID        string
	FanoutGroupID         string
	WorkspaceID           string
	WorkerSessionID       string
	SourceSessionID       string
	TargetProfileID       string
	RequestAgentMessageID string
	ReplyAgentMessageID   string
	FinalResponseID       string
	Status                string
	CreatedAt             time.Time
}

func DecodeFanoutWorkerWorkspaceEvent(event WorkspaceEvent) (DecodedFanoutWorkerWorkspaceEvent, bool, error) {
	if !IsFanoutWorkerWorkspaceEvent(event.EventType) {
		return DecodedFanoutWorkerWorkspaceEvent{}, false, nil
	}
	payload := WorkspaceEventStringPayload(event.PayloadJSON)
	decoded := DecodedFanoutWorkerWorkspaceEvent{
		FanoutMemberID:        strings.TrimSpace(payload["fanout_member_id"]),
		FanoutGroupID:         strings.TrimSpace(payload["fanout_group_id"]),
		WorkspaceID:           strings.TrimSpace(event.WorkspaceID),
		WorkerSessionID:       strings.TrimSpace(event.SubjectID),
		SourceSessionID:       strings.TrimSpace(payload["source_session_id"]),
		TargetProfileID:       strings.TrimSpace(payload["target_profile_id"]),
		RequestAgentMessageID: strings.TrimSpace(payload["request_agent_message_id"]),
		ReplyAgentMessageID:   strings.TrimSpace(payload["reply_agent_message_id"]),
		FinalResponseID:       FinalResponseIDFromWorkspaceEventRef(event.PayloadRefJSON),
		Status:                WorkerEventStatus(event.EventType),
		CreatedAt:             event.CreatedAt,
	}
	if decoded.FanoutMemberID == "" {
		return DecodedFanoutWorkerWorkspaceEvent{}, false, nil
	}
	if decoded.FanoutGroupID == "" {
		decoded.FanoutGroupID = strings.TrimSpace(event.CorrelationID)
	}
	switch event.EventType {
	case WorkspaceEventWorkerStarted:
		decoded.RequestAgentMessageID = strings.TrimSpace(event.CausationID)
	case WorkspaceEventWorkerCompleted:
		decoded.ReplyAgentMessageID = strings.TrimSpace(event.CausationID)
	}
	return decoded, true, nil
}

type OperationWorkspaceEventParams struct {
	WorkspaceID       string
	OperationID       string
	OperationType     string
	Source            string
	Scope             string
	Result            string
	RequestSummary    string
	RollbackPointID   string
	AttentionRequired bool
}

func NewOperationWorkspaceEvent(params OperationWorkspaceEventParams) WorkspaceEvent {
	operationID := strings.TrimSpace(params.OperationID)
	payload := map[string]string{
		"operation_id":    operationID,
		"operation_type":  strings.TrimSpace(params.OperationType),
		"source":          strings.TrimSpace(params.Source),
		"scope":           strings.TrimSpace(params.Scope),
		"result":          strings.TrimSpace(params.Result),
		"request_summary": strings.TrimSpace(params.RequestSummary),
	}
	if rollbackPointID := strings.TrimSpace(params.RollbackPointID); rollbackPointID != "" {
		payload["rollback_point_id"] = rollbackPointID
	}
	return WorkspaceEvent{
		WorkspaceID:       strings.TrimSpace(params.WorkspaceID),
		EventType:         OperationWorkspaceEventType(params.OperationType),
		SubjectType:       WorkspaceEventSubjectOperation,
		SubjectID:         operationID,
		ProducerType:      WorkspaceEventProducerDaemon,
		ProducerID:        strings.TrimSpace(params.Source),
		CorrelationID:     operationID,
		PayloadJSON:       WorkspaceEventStringMapJSON(payload),
		PayloadRefJSON:    WorkspaceEventPayloadRefJSON(WorkspaceEventPayloadRefOperationRecord, operationID),
		AttentionRequired: params.AttentionRequired,
	}
}

type CommandWorkspaceEventParams struct {
	WorkspaceID string
	CommandID   string
	Command     string
	Args        string
	Status      string
	ExitCode    *int
	FinishedAt  *string
}

func NewCommandWorkspaceEvent(params CommandWorkspaceEventParams) WorkspaceEvent {
	commandID := strings.TrimSpace(params.CommandID)
	payload := map[string]string{
		"command_id": commandID,
		"command":    strings.TrimSpace(params.Command),
		"args":       strings.TrimSpace(params.Args),
		"status":     strings.TrimSpace(params.Status),
	}
	if params.ExitCode != nil {
		payload["exit_code"] = fmt.Sprintf("%d", *params.ExitCode)
	}
	if params.FinishedAt != nil {
		payload["finished_at"] = strings.TrimSpace(*params.FinishedAt)
	}
	return WorkspaceEvent{
		WorkspaceID:       strings.TrimSpace(params.WorkspaceID),
		EventType:         CommandWorkspaceEventType(params.Status, params.ExitCode),
		SubjectType:       WorkspaceEventSubjectCommand,
		SubjectID:         commandID,
		ProducerType:      WorkspaceEventProducerDaemon,
		ProducerID:        WorkspaceEventProducerCommand,
		CorrelationID:     commandID,
		PayloadJSON:       WorkspaceEventStringMapJSON(payload),
		PayloadRefJSON:    WorkspaceEventPayloadRefJSON(WorkspaceEventPayloadRefCommand, commandID),
		AttentionRequired: CommandWorkspaceEventNeedsAttention(params.Status, params.ExitCode),
	}
}

func CommandWorkspaceEventType(status string, exitCode *int) string {
	switch strings.TrimSpace(status) {
	case "running":
		return WorkspaceEventCommandStarted
	case "stopped":
		return WorkspaceEventCommandStopped
	case "lost":
		return WorkspaceEventCommandFailed
	case "exited":
		if exitCode != nil && *exitCode != 0 {
			return WorkspaceEventCommandFailed
		}
		return WorkspaceEventCommandCompleted
	default:
		return WorkspaceEventCommandUpdated
	}
}

func CommandWorkspaceEventNeedsAttention(status string, exitCode *int) bool {
	if strings.TrimSpace(status) == "lost" {
		return true
	}
	return exitCode != nil && *exitCode != 0
}

type AgentMessageWorkspaceEventParams struct {
	WorkspaceID         string
	AgentMessageID      string
	SourceSessionID     string
	SourceAgentID       string
	TargetAgentID       string
	TargetSessionID     string
	ContextExcerptCount int
	PayloadJSON         string
}

func NewAgentMessageWorkspaceEvent(params AgentMessageWorkspaceEventParams) WorkspaceEvent {
	agentMessageID := strings.TrimSpace(params.AgentMessageID)
	payloadJSON := strings.TrimSpace(params.PayloadJSON)
	if payloadJSON == "" || payloadJSON == "{}" {
		payloadJSON = WorkspaceEventStringMapJSON(map[string]string{
			"agent_message_id":      agentMessageID,
			"source_session_id":     strings.TrimSpace(params.SourceSessionID),
			"source_agent_id":       strings.TrimSpace(params.SourceAgentID),
			"target_agent_id":       strings.TrimSpace(params.TargetAgentID),
			"target_session_id":     strings.TrimSpace(params.TargetSessionID),
			"context_excerpt_count": fmt.Sprintf("%d", params.ContextExcerptCount),
		})
	}
	return WorkspaceEvent{
		WorkspaceID:    strings.TrimSpace(params.WorkspaceID),
		EventType:      WorkspaceEventMessageSent,
		SubjectType:    WorkspaceEventSubjectAgentMessage,
		SubjectID:      agentMessageID,
		ProducerType:   WorkspaceEventProducerSession,
		ProducerID:     strings.TrimSpace(params.SourceSessionID),
		CorrelationID:  strings.TrimSpace(params.TargetSessionID),
		PayloadJSON:    payloadJSON,
		PayloadRefJSON: WorkspaceEventPayloadRefJSON(WorkspaceEventPayloadRefAgentMessage, agentMessageID),
	}
}

type ContextExcerptCreatedWorkspaceEventParams struct {
	WorkspaceID      string
	ContextExcerptID string
	SourceSessionID  string
	SourceAgentID    string
	TargetAgentID    string
	SelectorType     string
	ItemCount        int
}

func NewContextExcerptCreatedWorkspaceEvent(params ContextExcerptCreatedWorkspaceEventParams) WorkspaceEvent {
	contextExcerptID := strings.TrimSpace(params.ContextExcerptID)
	return WorkspaceEvent{
		WorkspaceID:    strings.TrimSpace(params.WorkspaceID),
		EventType:      WorkspaceEventContextExcerptCreated,
		SubjectType:    WorkspaceEventSubjectContextExcerpt,
		SubjectID:      contextExcerptID,
		ProducerType:   WorkspaceEventProducerSession,
		ProducerID:     strings.TrimSpace(params.SourceSessionID),
		CorrelationID:  strings.TrimSpace(params.SourceSessionID),
		PayloadJSON:    WorkspaceEventStringMapJSON(map[string]string{"context_excerpt_id": contextExcerptID, "source_session_id": strings.TrimSpace(params.SourceSessionID), "source_agent_id": strings.TrimSpace(params.SourceAgentID), "target_agent_id": strings.TrimSpace(params.TargetAgentID), "selector_type": strings.TrimSpace(params.SelectorType), "item_count": fmt.Sprintf("%d", params.ItemCount)}),
		PayloadRefJSON: WorkspaceEventPayloadRefJSON(WorkspaceEventPayloadRefContextExcerpt, contextExcerptID),
	}
}

type TimerFiredWorkspaceEventParams struct {
	WorkspaceID          string
	TimerID              string
	Purpose              string
	OwnerSessionID       string
	TargetSubscriptionID string
	SubjectType          string
	SubjectID            string
	PayloadJSON          string
}

func NewTimerFiredWorkspaceEvent(params TimerFiredWorkspaceEventParams) WorkspaceEvent {
	payload := map[string]any{}
	if raw := strings.TrimSpace(params.PayloadJSON); raw != "" {
		var objectPayload map[string]any
		if err := json.Unmarshal([]byte(raw), &objectPayload); err == nil && objectPayload != nil {
			payload = objectPayload
		} else {
			var rawPayload any
			if err := json.Unmarshal([]byte(raw), &rawPayload); err == nil {
				payload["payload"] = rawPayload
			}
		}
	}
	payload["timer_id"] = strings.TrimSpace(params.TimerID)
	payload["purpose"] = strings.TrimSpace(params.Purpose)
	payload["owner_session_id"] = strings.TrimSpace(params.OwnerSessionID)
	payload["target_subscription_id"] = strings.TrimSpace(params.TargetSubscriptionID)
	payload["subject_type"] = strings.TrimSpace(params.SubjectType)
	payload["subject_id"] = strings.TrimSpace(params.SubjectID)
	encoded, err := json.Marshal(payload)
	if err != nil {
		encoded = []byte("{}")
	}
	timerID := strings.TrimSpace(params.TimerID)
	correlationID := strings.TrimSpace(params.Purpose)
	if correlationID == "" {
		correlationID = timerID
	}
	return WorkspaceEvent{EventID: newWorkspaceEventID(), WorkspaceID: strings.TrimSpace(params.WorkspaceID), EventType: WorkspaceEventTimerFired, SubjectType: WorkspaceEventSubjectTimer, SubjectID: timerID, ProducerType: WorkspaceEventProducerDaemon, ProducerID: WorkspaceEventProducerTimer, CorrelationID: correlationID, CausationID: strings.TrimSpace(params.TargetSubscriptionID), PayloadJSON: string(encoded), PayloadRefJSON: WorkspaceEventPayloadRefJSON(WorkspaceEventPayloadRefTimer, timerID), AttentionRequired: strings.TrimSpace(params.OwnerSessionID) != "" || strings.TrimSpace(params.TargetSubscriptionID) != ""}
}

type DeliveryWorkspaceEventParams struct {
	WorkspaceID    string
	DeliveryID     string
	SubscriptionID string
	TargetType     string
	TargetID       string
	EventIDs       []string
	EventType      string
	Status         string
	Attempts       int64
	LastError      string
	NextAttemptAt  *time.Time
}

func NewDeliveryWorkspaceEvent(params DeliveryWorkspaceEventParams) WorkspaceEvent {
	payload := map[string]any{"delivery_id": strings.TrimSpace(params.DeliveryID), "subscription_id": strings.TrimSpace(params.SubscriptionID), "target_type": strings.TrimSpace(params.TargetType), "target_id": strings.TrimSpace(params.TargetID), "status": strings.TrimSpace(params.Status), "attempts": fmt.Sprintf("%d", params.Attempts), "event_ids": params.EventIDs}
	if lastError := strings.TrimSpace(params.LastError); lastError != "" {
		payload["last_error"] = lastError
	}
	if params.NextAttemptAt != nil && !params.NextAttemptAt.IsZero() {
		payload["next_attempt_at"] = params.NextAttemptAt.UTC().Format(time.RFC3339Nano)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		encoded = []byte("{}")
	}
	causationID := ""
	if len(params.EventIDs) > 0 {
		causationID = strings.TrimSpace(params.EventIDs[0])
	}
	return WorkspaceEvent{EventID: newWorkspaceEventID(), WorkspaceID: strings.TrimSpace(params.WorkspaceID), EventType: strings.TrimSpace(params.EventType), SubjectType: WorkspaceEventSubjectPendingDelivery, SubjectID: strings.TrimSpace(params.DeliveryID), ProducerType: WorkspaceEventProducerDaemon, ProducerID: WorkspaceEventProducerWorkspaceDelivery, CorrelationID: strings.TrimSpace(params.SubscriptionID), CausationID: causationID, PayloadJSON: string(encoded), AttentionRequired: strings.TrimSpace(params.EventType) == WorkspaceEventDeliveryFailed}
}

type SignalWorkspaceEventParams struct {
	EventID       string
	WorkspaceID   string
	TargetType    string
	TargetID      string
	ProducerType  string
	ProducerID    string
	CorrelationID string
	CausationID   string
	PayloadJSON   string
}

func NewSignalWorkspaceEvent(params SignalWorkspaceEventParams) WorkspaceEvent {
	producerType := strings.TrimSpace(params.ProducerType)
	if producerType == "" {
		producerType = WorkspaceEventProducerClient
	}
	return WorkspaceEvent{EventID: strings.TrimSpace(params.EventID), WorkspaceID: strings.TrimSpace(params.WorkspaceID), EventType: WorkspaceEventSignalSent, SubjectType: strings.TrimSpace(params.TargetType), SubjectID: strings.TrimSpace(params.TargetID), ProducerType: producerType, ProducerID: strings.TrimSpace(params.ProducerID), CorrelationID: strings.TrimSpace(params.CorrelationID), CausationID: strings.TrimSpace(params.CausationID), PayloadJSON: strings.TrimSpace(params.PayloadJSON), AttentionRequired: true}
}

type DecodedWorkspaceEventPayloadRef struct {
	Kind     string
	ID       string
	Sequence string
}

func DecodeWorkspaceEventPayloadRef(raw string) DecodedWorkspaceEventPayloadRef {
	ref := WorkspaceEventStringPayload(raw)
	return DecodedWorkspaceEventPayloadRef{Kind: strings.TrimSpace(ref["kind"]), ID: strings.TrimSpace(ref["id"]), Sequence: strings.TrimSpace(ref["sequence"])}
}

type DecodedOperationWorkspaceEvent struct {
	OperationType   string
	Source          string
	Scope           string
	Result          string
	RequestSummary  string
	RollbackPointID string
}

func DecodeOperationWorkspaceEvent(event WorkspaceEvent) (DecodedOperationWorkspaceEvent, bool) {
	if !strings.HasPrefix(strings.TrimSpace(event.EventType), WorkspaceEventOperationPrefix) {
		return DecodedOperationWorkspaceEvent{}, false
	}
	payload := WorkspaceEventStringPayload(event.PayloadJSON)
	operationType := strings.TrimSpace(payload["operation_type"])
	if operationType == "" {
		operationType = strings.TrimPrefix(event.EventType, WorkspaceEventOperationPrefix)
	}
	return DecodedOperationWorkspaceEvent{OperationType: operationType, Source: strings.TrimSpace(payload["source"]), Scope: strings.TrimSpace(payload["scope"]), Result: strings.TrimSpace(payload["result"]), RequestSummary: strings.TrimSpace(payload["request_summary"]), RollbackPointID: strings.TrimSpace(payload["rollback_point_id"])}, true
}

type DecodedCommandWorkspaceEvent struct {
	Command string
	Status  string
}

func DecodeCommandWorkspaceEvent(event WorkspaceEvent) (DecodedCommandWorkspaceEvent, bool) {
	if !strings.HasPrefix(strings.TrimSpace(event.EventType), WorkspaceEventCommandPrefix) {
		return DecodedCommandWorkspaceEvent{}, false
	}
	payload := WorkspaceEventStringPayload(event.PayloadJSON)
	return DecodedCommandWorkspaceEvent{Command: strings.TrimSpace(payload["command"]), Status: strings.TrimSpace(payload["status"])}, true
}

type DecodedHarnessSessionWorkspaceEvent struct {
	SessionID       string
	Harness         string
	Status          string
	FinalResponseID string
}

func DecodeHarnessSessionWorkspaceEvent(event WorkspaceEvent) (DecodedHarnessSessionWorkspaceEvent, bool) {
	if !strings.HasPrefix(strings.TrimSpace(event.EventType), WorkspaceEventSessionPrefix) {
		return DecodedHarnessSessionWorkspaceEvent{}, false
	}
	payload := WorkspaceEventStringPayload(event.PayloadJSON)
	sessionID := strings.TrimSpace(payload["session_id"])
	if sessionID == "" {
		sessionID = strings.TrimSpace(event.SubjectID)
	}
	status := strings.TrimSpace(payload["status"])
	if status == "" {
		status = strings.TrimPrefix(event.EventType, WorkspaceEventSessionPrefix)
	}
	return DecodedHarnessSessionWorkspaceEvent{SessionID: sessionID, Harness: strings.TrimSpace(payload["harness"]), Status: status, FinalResponseID: FinalResponseIDFromWorkspaceEventRef(event.PayloadRefJSON)}, true
}

type DecodedHarnessRuntimeWorkspaceEvent struct {
	HarnessEventID string          `json:"harness_event_id"`
	Kind           string          `json:"kind"`
	Sequence       int             `json:"sequence"`
	SessionID      string          `json:"session_id"`
	RunID          string          `json:"run_id"`
	Payload        json.RawMessage `json:"payload"`
	ProviderKind   string          `json:"provider_kind"`
}

func DecodeHarnessRuntimeWorkspaceEvent(event WorkspaceEvent) (DecodedHarnessRuntimeWorkspaceEvent, bool) {
	if !strings.HasPrefix(strings.TrimSpace(event.EventType), WorkspaceEventHarnessEventPrefix) {
		return DecodedHarnessRuntimeWorkspaceEvent{}, false
	}
	var outer DecodedHarnessRuntimeWorkspaceEvent
	_ = json.Unmarshal([]byte(event.PayloadJSON), &outer)
	outer.HarnessEventID = strings.TrimSpace(outer.HarnessEventID)
	outer.Kind = strings.TrimSpace(outer.Kind)
	outer.SessionID = strings.TrimSpace(outer.SessionID)
	outer.RunID = strings.TrimSpace(outer.RunID)
	outer.ProviderKind = strings.TrimSpace(outer.ProviderKind)
	if outer.SessionID == "" {
		outer.SessionID = strings.TrimSpace(event.SubjectID)
	}
	if outer.RunID == "" {
		outer.RunID = outer.SessionID
	}
	if outer.Kind == "" {
		outer.Kind = strings.TrimPrefix(event.EventType, WorkspaceEventHarnessEventPrefix)
	}
	return outer, true
}

type DecodedDeliveryWorkspaceEvent struct {
	DeliveryID     string
	SubscriptionID string
	TargetType     string
	TargetID       string
	Status         string
	LastError      string
}

func DecodeDeliveryWorkspaceEvent(event WorkspaceEvent) (DecodedDeliveryWorkspaceEvent, bool) {
	if !strings.HasPrefix(strings.TrimSpace(event.EventType), WorkspaceEventDeliveryPrefix) {
		return DecodedDeliveryWorkspaceEvent{}, false
	}
	payload := WorkspaceEventStringPayload(event.PayloadJSON)
	return DecodedDeliveryWorkspaceEvent{DeliveryID: strings.TrimSpace(payload["delivery_id"]), SubscriptionID: strings.TrimSpace(payload["subscription_id"]), TargetType: strings.TrimSpace(payload["target_type"]), TargetID: strings.TrimSpace(payload["target_id"]), Status: strings.TrimSpace(payload["status"]), LastError: strings.TrimSpace(payload["last_error"])}, true
}

type DecodedSignalWorkspaceEvent struct {
	SourceSessionID string
	TargetSessionID string
	Action          string
}

func DecodeSignalWorkspaceEvent(event WorkspaceEvent) (DecodedSignalWorkspaceEvent, bool) {
	if strings.TrimSpace(event.EventType) != WorkspaceEventSignalSent {
		return DecodedSignalWorkspaceEvent{}, false
	}
	payload := WorkspaceEventStringPayload(event.PayloadJSON)
	return DecodedSignalWorkspaceEvent{SourceSessionID: strings.TrimSpace(payload["source_session_id"]), TargetSessionID: strings.TrimSpace(payload["target_session_id"]), Action: strings.TrimSpace(payload["action"])}, true
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
	if strings.TrimSpace(ref["kind"]) != WorkspaceEventPayloadRefFinalResponse {
		return ""
	}
	return strings.TrimSpace(ref["id"])
}
