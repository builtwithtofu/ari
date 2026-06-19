package globaldb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb/dbsqlc"
)

type TimelineItem struct {
	ID               string
	WorkspaceID      string
	WorkspaceEventID string
	RunID            string
	SessionID        string
	SourceKind       string
	SourceID         string
	Kind             string
	Status           string
	Sequence         int
	CreatedAt        string
	Text             string
	Metadata         map[string]any
}

func (s *Store) ListTimelineItems(ctx context.Context, workspaceID string) ([]TimelineItem, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, ErrInvalidInput
	}
	rows, err := s.sqlcQueries().ListTimelineItemsByWorkspace(ctx, dbsqlc.ListTimelineItemsByWorkspaceParams{WorkspaceID: workspaceID})
	if err != nil {
		return nil, fmt.Errorf("list timeline items for workspace %q: %w", workspaceID, err)
	}
	items := make([]TimelineItem, 0, len(rows))
	for _, row := range rows {
		item, err := timelineItemFromSQLC(row)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func upsertTimelineItemWithQueries(ctx context.Context, queries *dbsqlc.Queries, item TimelineItem) error {
	item = normalizeTimelineItem(item)
	if err := validateTimelineItem(item); err != nil {
		return err
	}
	metadataJSON, err := marshalTimelineMetadata(item.Metadata)
	if err != nil {
		return err
	}
	sequence := int64(item.Sequence)
	if sequence <= 0 {
		sequence, err = queries.NextTimelineItemSequence(ctx, dbsqlc.NextTimelineItemSequenceParams{WorkspaceID: item.WorkspaceID})
		if err != nil {
			return fmt.Errorf("allocate timeline sequence for workspace %q: %w", item.WorkspaceID, err)
		}
	}
	updatedAt := item.CreatedAt
	rows, err := queries.CreateTimelineItem(ctx, dbsqlc.CreateTimelineItemParams{WorkspaceID: item.WorkspaceID, TimelineItemID: item.ID, WorkspaceEventID: item.WorkspaceEventID, Sequence: sequence, RunID: item.RunID, SessionID: item.SessionID, SourceKind: item.SourceKind, SourceID: item.SourceID, Kind: item.Kind, Status: item.Status, Text: item.Text, MetadataJson: metadataJSON, CreatedAt: item.CreatedAt, UpdatedAt: updatedAt})
	if err != nil {
		return fmt.Errorf("create timeline item %q: %w", item.ID, err)
	}
	if rows != 0 {
		return nil
	}
	existingRow, err := queries.GetTimelineItem(ctx, dbsqlc.GetTimelineItemParams{WorkspaceID: item.WorkspaceID, TimelineItemID: item.ID})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("get timeline item %q for update: %w", item.ID, err)
	}
	if err == nil {
		existing, err := timelineItemFromSQLC(existingRow)
		if err != nil {
			return err
		}
		item.Metadata = mergeTimelineMetadata(existing.Metadata, item.Metadata)
		if item.Text == "" {
			item.Text = existing.Text
		}
		metadataJSON, err = marshalTimelineMetadata(item.Metadata)
		if err != nil {
			return err
		}
	}
	if _, err := queries.UpdateTimelineItem(ctx, dbsqlc.UpdateTimelineItemParams{WorkspaceEventID: item.WorkspaceEventID, RunID: item.RunID, SessionID: item.SessionID, SourceKind: item.SourceKind, SourceID: item.SourceID, Kind: item.Kind, Status: item.Status, Text: item.Text, MetadataJson: metadataJSON, CreatedAt: item.CreatedAt, UpdatedAt: updatedAt, WorkspaceID: item.WorkspaceID, TimelineItemID: item.ID}); err != nil {
		return fmt.Errorf("update timeline item %q: %w", item.ID, err)
	}
	return nil
}

func timelineItemFromSQLC(row dbsqlc.TimelineItem) (TimelineItem, error) {
	metadata := map[string]any{}
	if strings.TrimSpace(row.MetadataJson) != "" {
		if err := json.Unmarshal([]byte(row.MetadataJson), &metadata); err != nil {
			return TimelineItem{}, fmt.Errorf("decode timeline item %q metadata_json: %w", row.TimelineItemID, err)
		}
	}
	return TimelineItem{ID: row.TimelineItemID, WorkspaceID: row.WorkspaceID, WorkspaceEventID: row.WorkspaceEventID, RunID: row.RunID, SessionID: row.SessionID, SourceKind: row.SourceKind, SourceID: row.SourceID, Kind: row.Kind, Status: row.Status, Sequence: int(row.Sequence), CreatedAt: row.CreatedAt, Text: row.Text, Metadata: metadata}, nil
}

func normalizeTimelineItem(item TimelineItem) TimelineItem {
	item.ID = strings.TrimSpace(item.ID)
	item.WorkspaceID = strings.TrimSpace(item.WorkspaceID)
	item.WorkspaceEventID = strings.TrimSpace(item.WorkspaceEventID)
	item.RunID = strings.TrimSpace(item.RunID)
	item.SessionID = strings.TrimSpace(item.SessionID)
	item.SourceKind = strings.TrimSpace(item.SourceKind)
	item.SourceID = strings.TrimSpace(item.SourceID)
	item.Kind = strings.TrimSpace(item.Kind)
	item.Status = strings.TrimSpace(item.Status)
	item.CreatedAt = strings.TrimSpace(item.CreatedAt)
	item.Text = strings.TrimSpace(item.Text)
	if item.ID == "" {
		item.ID = item.WorkspaceEventID
	}
	if item.Status == "" {
		item.Status = "recorded"
	}
	if item.CreatedAt == "" {
		item.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	return item
}

func validateTimelineItem(item TimelineItem) error {
	if item.WorkspaceID == "" || item.ID == "" || item.WorkspaceEventID == "" || item.SourceKind == "" || item.SourceID == "" || item.Kind == "" || item.Status == "" || item.CreatedAt == "" {
		return ErrInvalidInput
	}
	return nil
}

func marshalTimelineMetadata(metadata map[string]any) (string, error) {
	if metadata == nil {
		metadata = map[string]any{}
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return "", fmt.Errorf("encode timeline metadata: %w", err)
	}
	return string(encoded), nil
}

func mergeTimelineMetadata(existing, incoming map[string]any) map[string]any {
	merged := make(map[string]any, len(existing)+len(incoming))
	for key, value := range existing {
		merged[key] = value
	}
	for key, value := range incoming {
		if timelineMetadataValueEmpty(value) {
			continue
		}
		merged[key] = value
	}
	return merged
}

func timelineMetadataValueEmpty(value any) bool {
	if value == nil {
		return true
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text) == ""
	}
	return false
}

// TimelineProjection materializes the public workspace timeline from durable
// workspace events. Event history remains authoritative; rows are an ordered,
// rebuildable read model.
type TimelineProjection struct{}

func (TimelineProjection) Name() string { return "timeline_items" }

func (TimelineProjection) EventTypes() []string {
	return []string{
		WorkspaceEventWorkerStarted,
		WorkspaceEventWorkerCompleted,
		WorkspaceEventWorkerFailed,
		WorkspaceEventWorkerStopped,
		WorkspaceEventMessageSent,
		WorkspaceEventContextExcerptCreated,
		WorkspaceEventTimerFired,
	}
}

func (TimelineProjection) EventTypePrefixes() []string {
	return []string{"operation.", "command.", "session.", WorkspaceEventHarnessEventPrefix}
}

func (p TimelineProjection) ProjectWorkspaceEvent(ctx context.Context, queries *dbsqlc.Queries, event WorkspaceEvent) error {
	items, err := p.itemsFromWorkspaceEvent(ctx, queries, event)
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := upsertTimelineItemWithQueries(ctx, queries, item); err != nil {
			return err
		}
	}
	return nil
}

func (p TimelineProjection) Rebuild(ctx context.Context, store *Store, workspaceID string) error {
	if store == nil {
		return fmt.Errorf("%w: globaldb store is required", ErrInvalidInput)
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return ErrInvalidInput
	}
	return store.withImmediateQueries(ctx, func(ctx context.Context, queries *dbsqlc.Queries) error {
		const pageSize = 500
		events := make([]WorkspaceEvent, 0)
		sequence := int64(0)
		for {
			page, err := listWorkspaceEventsAfterSequenceWithQueries(ctx, queries, workspaceID, sequence, pageSize)
			if err != nil {
				return err
			}
			if len(page) == 0 {
				break
			}
			for _, event := range page {
				sequence = event.Sequence
				if timelineProjectionHandlesEvent(event) {
					events = append(events, event)
				}
			}
			if len(page) < pageSize {
				break
			}
		}
		if _, err := queries.DeleteTimelineItemsByWorkspace(ctx, dbsqlc.DeleteTimelineItemsByWorkspaceParams{WorkspaceID: workspaceID}); err != nil {
			return fmt.Errorf("delete timeline items for workspace %q: %w", workspaceID, err)
		}
		for _, event := range events {
			if err := p.ProjectWorkspaceEvent(ctx, queries, event); err != nil {
				return err
			}
		}
		return nil
	})
}

func (p TimelineProjection) itemsFromWorkspaceEvent(ctx context.Context, queries *dbsqlc.Queries, event WorkspaceEvent) ([]TimelineItem, error) {
	switch {
	case strings.HasPrefix(event.EventType, "delivery."):
		return nil, nil
	case strings.HasPrefix(event.EventType, "operation."):
		return []TimelineItem{operationTimelineItemFromEvent(event)}, nil
	case strings.HasPrefix(event.EventType, "command."):
		return []TimelineItem{commandTimelineItemFromEvent(ctx, queries, event)}, nil
	case event.EventType == WorkspaceEventMessageSent:
		return agentMessageTimelineItemsFromEvent(ctx, queries, event)
	case event.EventType == WorkspaceEventContextExcerptCreated:
		item, ok, err := contextExcerptTimelineItem(ctx, queries, event, event.SubjectID)
		if err != nil || !ok {
			return nil, err
		}
		return []TimelineItem{item}, nil
	case IsFanoutWorkerWorkspaceEvent(event.EventType):
		return []TimelineItem{fanoutTimelineItemFromEvent(event)}, nil
	case strings.HasPrefix(event.EventType, WorkspaceEventHarnessEventPrefix):
		return []TimelineItem{harnessRuntimeTimelineItemFromEvent(event)}, nil
	case strings.HasPrefix(event.EventType, "session."):
		return []TimelineItem{harnessSessionTimelineItemFromEvent(event)}, nil
	case event.EventType == WorkspaceEventTimerFired:
		return []TimelineItem{genericTimelineItemFromEvent(event)}, nil
	default:
		return nil, nil
	}
}

func timelineProjectionHandlesEvent(event WorkspaceEvent) bool {
	if strings.HasPrefix(event.EventType, "delivery.") {
		return false
	}
	if event.EventType == WorkspaceEventMessageSent || event.EventType == WorkspaceEventContextExcerptCreated || event.EventType == WorkspaceEventTimerFired || IsFanoutWorkerWorkspaceEvent(event.EventType) {
		return true
	}
	for _, prefix := range []string{"operation.", "command.", "session.", WorkspaceEventHarnessEventPrefix} {
		if strings.HasPrefix(event.EventType, prefix) {
			return true
		}
	}
	return false
}

func operationTimelineItemFromEvent(event WorkspaceEvent) TimelineItem {
	payload := WorkspaceEventStringPayload(event.PayloadJSON)
	operationType := strings.TrimSpace(payload["operation_type"])
	if operationType == "" {
		operationType = strings.TrimPrefix(event.EventType, "operation.")
	}
	status := strings.TrimSpace(payload["result"])
	if status == "" {
		status = "recorded"
	}
	metadata := map[string]any{"source": payload["source"]}
	if rollbackPointID := strings.TrimSpace(payload["rollback_point_id"]); rollbackPointID != "" {
		metadata["rollback_point_id"] = rollbackPointID
	}
	return TimelineItem{ID: event.EventID, WorkspaceID: event.WorkspaceID, WorkspaceEventID: event.EventID, SourceKind: "operation", SourceID: event.SubjectID, Kind: operationType, Status: status, CreatedAt: event.CreatedAt.UTC().Format(time.RFC3339Nano), Text: payload["request_summary"], Metadata: metadata}
}

func commandTimelineItemFromEvent(ctx context.Context, queries *dbsqlc.Queries, event WorkspaceEvent) TimelineItem {
	payload := WorkspaceEventStringPayload(event.PayloadJSON)
	text := strings.TrimSpace(payload["command"])
	if command, err := queries.GetCommandByID(ctx, dbsqlc.GetCommandByIDParams{WorkspaceID: event.WorkspaceID, CommandID: event.SubjectID}); err == nil {
		text = timelineCommandLabel(command.Command, command.Args)
	}
	status := strings.TrimSpace(payload["status"])
	switch event.EventType {
	case WorkspaceEventCommandStarted:
		status = "running"
	case WorkspaceEventCommandCompleted:
		status = "completed"
	case WorkspaceEventCommandFailed:
		if strings.TrimSpace(payload["status"]) == "lost" {
			status = "lost"
		} else {
			status = "failed"
		}
	case WorkspaceEventCommandStopped:
		status = "stopped"
	}
	if status == "" {
		status = "recorded"
	}
	return TimelineItem{ID: event.EventID, WorkspaceID: event.WorkspaceID, WorkspaceEventID: event.EventID, SourceKind: "command", SourceID: event.SubjectID, Kind: "lifecycle", Status: status, CreatedAt: event.CreatedAt.UTC().Format(time.RFC3339Nano), Text: text, Metadata: map[string]any{"event_type": event.EventType}}
}

func agentMessageTimelineItemsFromEvent(ctx context.Context, queries *dbsqlc.Queries, event WorkspaceEvent) ([]TimelineItem, error) {
	msg, err := queries.GetAgentMessage(ctx, dbsqlc.GetAgentMessageParams{AgentMessageID: event.SubjectID})
	if errors.Is(err, sql.ErrNoRows) {
		return []TimelineItem{genericTimelineItemFromEvent(event)}, nil
	}
	if err != nil {
		return nil, err
	}
	excerptIDs, err := queries.ListAgentMessageContextExcerptIDs(ctx, dbsqlc.ListAgentMessageContextExcerptIDsParams{AgentMessageID: msg.AgentMessageID})
	if err != nil {
		return nil, err
	}
	items := make([]TimelineItem, 0, len(excerptIDs)+1)
	for _, excerptID := range excerptIDs {
		item, ok, err := contextExcerptTimelineItem(ctx, queries, event, excerptID)
		if err != nil {
			return nil, err
		}
		if ok {
			items = append(items, item)
		}
	}
	items = append(items, TimelineItem{ID: msg.AgentMessageID, WorkspaceID: event.WorkspaceID, WorkspaceEventID: event.EventID, RunID: msg.SourceSessionID, SessionID: msg.SourceSessionID, SourceKind: "agent_message", SourceID: msg.AgentMessageID, Kind: "agent_message", Status: msg.Status, CreatedAt: event.CreatedAt.UTC().Format(time.RFC3339Nano), Text: msg.Body, Metadata: map[string]any{"source_session_id": msg.SourceSessionID, "source_agent_id": msg.SourceAgentID, "target_session_id": msg.TargetSessionID, "target_agent_id": msg.TargetAgentID, "context_excerpt_count": len(excerptIDs)}})
	return items, nil
}

func contextExcerptTimelineItem(ctx context.Context, queries *dbsqlc.Queries, event WorkspaceEvent, excerptID string) (TimelineItem, bool, error) {
	excerptID = strings.TrimSpace(excerptID)
	if excerptID == "" {
		return TimelineItem{}, false, nil
	}
	excerpt, err := queries.GetContextExcerpt(ctx, dbsqlc.GetContextExcerptParams{ContextExcerptID: excerptID})
	if errors.Is(err, sql.ErrNoRows) {
		return TimelineItem{}, false, nil
	}
	if err != nil {
		return TimelineItem{}, false, err
	}
	items, err := queries.ListContextExcerptItems(ctx, dbsqlc.ListContextExcerptItemsParams{ContextExcerptID: excerptID})
	if err != nil {
		return TimelineItem{}, false, err
	}
	workspaceID := strings.TrimSpace(excerpt.WorkspaceID)
	if workspaceID == "" {
		workspaceID = event.WorkspaceID
	}
	return TimelineItem{ID: excerpt.ContextExcerptID, WorkspaceID: workspaceID, WorkspaceEventID: event.EventID, RunID: excerpt.SourceSessionID, SessionID: excerpt.SourceSessionID, SourceKind: "context_excerpt", SourceID: excerpt.ContextExcerptID, Kind: "context_excerpt", Status: "captured", CreatedAt: event.CreatedAt.UTC().Format(time.RFC3339Nano), Metadata: map[string]any{"source_session_id": excerpt.SourceSessionID, "source_agent_id": excerpt.SourceAgentID, "target_agent_id": excerpt.TargetAgentID, "selector_type": excerpt.SelectorType, "item_count": len(items)}}, true, nil
}

func fanoutTimelineItemFromEvent(event WorkspaceEvent) TimelineItem {
	payload := WorkspaceEventStringPayload(event.PayloadJSON)
	memberID := strings.TrimSpace(payload["fanout_member_id"])
	if memberID == "" {
		memberID = event.SubjectID
	}
	metadata := map[string]any{"fanout_group_id": event.CorrelationID, "worker_session_id": event.SubjectID, "target_profile_id": payload["target_profile_id"]}
	if causationID := strings.TrimSpace(event.CausationID); causationID != "" {
		switch event.EventType {
		case WorkspaceEventWorkerStarted:
			metadata["request_agent_message_id"] = causationID
		case WorkspaceEventWorkerCompleted:
			metadata["reply_agent_message_id"] = causationID
		}
	}
	if finalResponseID := FinalResponseIDFromWorkspaceEventRef(event.PayloadRefJSON); finalResponseID != "" {
		metadata["final_response_id"] = finalResponseID
	}
	return TimelineItem{ID: memberID, WorkspaceID: event.WorkspaceID, WorkspaceEventID: event.EventID, RunID: event.SubjectID, SessionID: event.SubjectID, SourceKind: "fanout_member", SourceID: memberID, Kind: "fanout_member", Status: WorkerEventStatus(event.EventType), CreatedAt: event.CreatedAt.UTC().Format(time.RFC3339Nano), Text: payload["target_profile_id"], Metadata: metadata}
}

type harnessRuntimeTimelinePayload struct {
	HarnessEventID string          `json:"harness_event_id"`
	Kind           string          `json:"kind"`
	Sequence       int             `json:"sequence"`
	SessionID      string          `json:"session_id"`
	RunID          string          `json:"run_id"`
	Payload        json.RawMessage `json:"payload"`
	ProviderKind   string          `json:"provider_kind"`
}

func harnessRuntimeTimelineItemFromEvent(event WorkspaceEvent) TimelineItem {
	var outer harnessRuntimeTimelinePayload
	_ = json.Unmarshal([]byte(event.PayloadJSON), &outer)
	inner := timelineWorkspaceEventJSONPayload(outer.Payload)
	sessionID := strings.TrimSpace(outer.SessionID)
	if sessionID == "" {
		sessionID = event.SubjectID
	}
	runID := strings.TrimSpace(outer.RunID)
	if runID == "" {
		runID = sessionID
	}
	kind := strings.TrimSpace(outer.Kind)
	status := strings.TrimSpace(timelineAnyString(inner["status"]))
	text := strings.TrimSpace(timelineAnyString(inner["text"]))
	switch kind {
	case "agent_text":
		kind = "run_log_message"
		if status == "" {
			status = "completed"
		}
	case "lifecycle":
		kind = "lifecycle"
		if status == "" {
			status = "running"
		}
	case "error":
		kind = "error"
		status = "failed"
		if text == "" {
			text = timelineAnyString(inner["message"])
		}
	case "usage":
		kind = "telemetry"
		if status == "" {
			status = "recorded"
		}
	default:
		if kind == "" {
			kind = strings.TrimPrefix(event.EventType, WorkspaceEventHarnessEventPrefix)
		}
		if status == "" {
			status = "recorded"
		}
		if text == "" {
			text = timelineAnyString(inner["message"])
		}
	}
	itemID := strings.TrimSpace(timelineAnyString(inner["timeline_item_id"]))
	if itemID == "" {
		itemID = event.EventID
	}
	return TimelineItem{ID: itemID, WorkspaceID: event.WorkspaceID, WorkspaceEventID: event.EventID, RunID: runID, SessionID: sessionID, SourceKind: "harness_session", SourceID: sessionID, Kind: kind, Status: status, CreatedAt: event.CreatedAt.UTC().Format(time.RFC3339Nano), Text: text, Metadata: map[string]any{"event_type": event.EventType, "provider_kind": outer.ProviderKind, "harness_event_id": outer.HarnessEventID}}
}

func harnessSessionTimelineItemFromEvent(event WorkspaceEvent) TimelineItem {
	payload := WorkspaceEventStringPayload(event.PayloadJSON)
	status := strings.TrimSpace(payload["status"])
	if status == "" {
		status = strings.TrimPrefix(event.EventType, "session.")
	}
	return TimelineItem{ID: event.EventID, WorkspaceID: event.WorkspaceID, WorkspaceEventID: event.EventID, RunID: event.SubjectID, SessionID: event.SubjectID, SourceKind: "harness_session", SourceID: event.SubjectID, Kind: "lifecycle", Status: status, CreatedAt: event.CreatedAt.UTC().Format(time.RFC3339Nano), Text: payload["harness"], Metadata: map[string]any{"event_type": event.EventType, "final_response_id": FinalResponseIDFromWorkspaceEventRef(event.PayloadRefJSON)}}
}

func genericTimelineItemFromEvent(event WorkspaceEvent) TimelineItem {
	payload := WorkspaceEventStringPayload(event.PayloadJSON)
	status := strings.TrimSpace(payload["status"])
	if status == "" {
		status = "recorded"
	}
	text := strings.TrimSpace(payload["summary"])
	if text == "" {
		text = strings.TrimSpace(payload["message"])
	}
	return TimelineItem{ID: event.EventID, WorkspaceID: event.WorkspaceID, WorkspaceEventID: event.EventID, SourceKind: event.SubjectType, SourceID: event.SubjectID, Kind: event.EventType, Status: status, CreatedAt: event.CreatedAt.UTC().Format(time.RFC3339Nano), Text: text, Metadata: map[string]any{"event_type": event.EventType, "correlation_id": event.CorrelationID, "causation_id": event.CausationID}}
}

func timelineCommandLabel(command, rawArgs string) string {
	command = strings.TrimSpace(command)
	args := strings.TrimSpace(rawArgs)
	if args == "" || args == "[]" {
		return command
	}
	var decoded []string
	if err := json.Unmarshal([]byte(args), &decoded); err != nil {
		return command
	}
	if len(decoded) == 0 {
		return command
	}
	return strings.TrimSpace(command + " " + strings.Join(decoded, " "))
}

func timelineWorkspaceEventJSONPayload(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return map[string]any{}
	}
	return payload
}

func timelineAnyString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return ""
	}
}
