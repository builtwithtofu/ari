package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

type RunLogMessagesTailRequest struct {
	SessionID string `json:"session_id"`
	Count     int    `json:"count"`
}

type RunLogMessagesTailResponse struct {
	Messages []RunLogMessageResponse `json:"messages"`
}

type RunLogMessagesListRequest struct {
	SessionID     string `json:"session_id"`
	AfterSequence int    `json:"after_sequence,omitempty"`
	Limit         int    `json:"limit"`
}

type RunLogMessagesListResponse struct {
	Messages []RunLogMessageResponse `json:"messages"`
}

type RunLogMessageResponse struct {
	MessageID          string                      `json:"message_id"`
	SessionID          string                      `json:"session_id"`
	AgentID            string                      `json:"agent_id"`
	Sequence           int                         `json:"sequence"`
	Role               string                      `json:"role"`
	Status             string                      `json:"status"`
	ProviderMessageID  string                      `json:"provider_message_id,omitempty"`
	ProviderItemID     string                      `json:"provider_item_id,omitempty"`
	ProviderTurnID     string                      `json:"provider_turn_id,omitempty"`
	ProviderResponseID string                      `json:"provider_response_id,omitempty"`
	ProviderCallID     string                      `json:"provider_call_id,omitempty"`
	ProviderChannel    string                      `json:"provider_channel,omitempty"`
	ProviderKind       string                      `json:"provider_kind,omitempty"`
	RawMetadataJSON    string                      `json:"raw_metadata_json,omitempty"`
	Parts              []RunLogMessagePartResponse `json:"parts"`
}

type RunLogMessagePartResponse struct {
	PartID     string `json:"part_id"`
	Sequence   int    `json:"sequence"`
	Kind       string `json:"kind"`
	Text       string `json:"text,omitempty"`
	MimeType   string `json:"mime_type,omitempty"`
	URI        string `json:"uri,omitempty"`
	Name       string `json:"name,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	RawJSON    string `json:"raw_json,omitempty"`
}

type ContextExcerptCreateFromTailRequest struct {
	ContextExcerptID string `json:"context_excerpt_id"`
	SourceSessionID  string `json:"source_session_id"`
	TargetAgentID    string `json:"target_agent_id,omitempty"`
	Count            int    `json:"count"`
	AppendedMessage  string `json:"appended_message,omitempty"`
}

type ContextExcerptCreateFromRangeRequest struct {
	ContextExcerptID string `json:"context_excerpt_id"`
	SourceSessionID  string `json:"source_session_id"`
	TargetAgentID    string `json:"target_agent_id,omitempty"`
	StartSequence    int    `json:"start_sequence"`
	EndSequence      int    `json:"end_sequence"`
	AppendedMessage  string `json:"appended_message,omitempty"`
}

type ContextExcerptCreateFromExplicitIDsRequest struct {
	ContextExcerptID string   `json:"context_excerpt_id"`
	SourceSessionID  string   `json:"source_session_id"`
	TargetAgentID    string   `json:"target_agent_id,omitempty"`
	MessageIDs       []string `json:"message_ids"`
	AppendedMessage  string   `json:"appended_message,omitempty"`
}

type ContextExcerptGetRequest struct {
	ContextExcerptID string `json:"context_excerpt_id"`
}

type ContextExcerptResponse struct {
	ContextExcerptID string                       `json:"context_excerpt_id"`
	WorkspaceID      string                       `json:"workspace_id"`
	SourceSessionID  string                       `json:"source_session_id"`
	SourceAgentID    string                       `json:"source_agent_id"`
	TargetAgentID    string                       `json:"target_agent_id,omitempty"`
	TargetSessionID  string                       `json:"target_session_id,omitempty"`
	SelectorType     string                       `json:"selector_type"`
	SelectorJSON     string                       `json:"selector_json"`
	Visibility       string                       `json:"visibility"`
	AppendedMessage  string                       `json:"appended_message,omitempty"`
	ContentHash      string                       `json:"content_hash"`
	Items            []ContextExcerptItemResponse `json:"items"`
}

type ContextExcerptItemResponse struct {
	Sequence        int                         `json:"sequence"`
	SourceMessageID string                      `json:"source_message_id"`
	CopiedRole      string                      `json:"copied_role"`
	CopiedText      string                      `json:"copied_text"`
	CopiedParts     []RunLogMessagePartResponse `json:"copied_parts"`
}

type AgentMessageSendRequest struct {
	AgentMessageID    string   `json:"agent_message_id"`
	FanoutGroupID     string   `json:"fanout_group_id,omitempty"`
	WorkspaceID       string   `json:"workspace_id,omitempty"`
	SourceSessionID   string   `json:"source_session_id"`
	TargetAgentID     string   `json:"target_agent_id"`
	TargetProfileIDs  []string `json:"target_profile_ids,omitempty"`
	TargetSessionID   string   `json:"target_session_id,omitempty"`
	Body              string   `json:"body"`
	ContextExcerptIDs []string `json:"context_excerpt_ids,omitempty"`
	StartSessionID    string   `json:"start_session_id,omitempty"`
}

type AgentMessageSendResponse struct {
	AgentMessage  AgentMessageResponse   `json:"agent_message"`
	FanoutGroupID string                 `json:"fanout_group_id,omitempty"`
	FanoutMembers []FanoutMemberResponse `json:"fanout_members,omitempty"`
}

type FanoutMemberResponse struct {
	FanoutMemberID  string                  `json:"fanout_member_id"`
	TargetProfileID string                  `json:"target_profile_id"`
	Session         globaldb.HarnessSession `json:"session"`
	Request         AgentMessageResponse    `json:"request"`
}

type AgentMessageResponse struct {
	AgentMessageID     string   `json:"agent_message_id"`
	WorkspaceID        string   `json:"workspace_id"`
	SourceAgentID      string   `json:"source_agent_id"`
	SourceSessionID    string   `json:"source_session_id"`
	TargetAgentID      string   `json:"target_agent_id"`
	TargetSessionID    string   `json:"target_session_id"`
	Body               string   `json:"body"`
	Status             string   `json:"status"`
	DeliveredSessionID string   `json:"delivered_session_id"`
	ContextExcerptIDs  []string `json:"context_excerpt_ids,omitempty"`
}

type TelemetryKnownInt64 struct {
	Known bool   `json:"known"`
	Value *int64 `json:"value,omitempty"`
}

type TelemetryRollupRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

type TelemetryRollupResponse struct {
	Rollups []TelemetryRollup `json:"rollups"`
}

type TelemetryRollupGroup struct {
	ProfileID       string `json:"profile_id,omitempty"`
	Profile         string `json:"profile,omitempty"`
	Harness         string `json:"harness"`
	Model           string `json:"model"`
	InvocationClass string `json:"invocation_class"`
}

type TelemetryProcessRollup struct {
	OwnedByAri         bool                     `json:"owned_by_ari"`
	PID                TelemetryKnownInt64      `json:"pid"`
	CPUTimeMS          TelemetryKnownInt64      `json:"cpu_time_ms"`
	MemoryRSSBytesPeak TelemetryKnownInt64      `json:"memory_rss_bytes_peak"`
	ChildProcessesPeak TelemetryKnownInt64      `json:"child_processes_peak"`
	Ports              []ProcessPortObservation `json:"ports"`
	OrphanState        string                   `json:"orphan_state"`
	ExitCode           TelemetryKnownInt64      `json:"exit_code"`
}

type TelemetryRollup struct {
	Group         TelemetryRollupGroup   `json:"group"`
	Runs          int                    `json:"runs"`
	Completed     int                    `json:"completed"`
	Failed        int                    `json:"failed"`
	InputTokens   TelemetryKnownInt64    `json:"input_tokens"`
	OutputTokens  TelemetryKnownInt64    `json:"output_tokens"`
	EstimatedCost TelemetryKnownInt64    `json:"estimated_cost"`
	DurationMS    TelemetryKnownInt64    `json:"duration_ms"`
	Process       TelemetryProcessRollup `json:"process"`
}

type FinalResponseEvidenceLink struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type FinalResponseGetRequest struct {
	FinalResponseID string `json:"final_response_id,omitempty"`
	SessionID       string `json:"session_id,omitempty"`
}

type FinalResponseListRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

type FinalResponseListResponse struct {
	FinalResponses []FinalResponseResponse `json:"final_responses"`
}

type FinalResponseResponse struct {
	FinalResponseID string                      `json:"final_response_id"`
	SessionID       string                      `json:"session_id"`
	WorkspaceID     string                      `json:"workspace_id"`
	TaskID          string                      `json:"task_id"`
	ContextPacketID string                      `json:"context_packet_id"`
	ProfileID       string                      `json:"profile_id,omitempty"`
	Status          string                      `json:"status"`
	Presentation    Presentation                `json:"presentation"`
	Text            string                      `json:"text"`
	EvidenceLinks   []FinalResponseEvidenceLink `json:"evidence_links"`
	CreatedAt       string                      `json:"created_at"`
	UpdatedAt       string                      `json:"updated_at,omitempty"`
}

func getFinalResponse(ctx context.Context, store *globaldb.Store, req FinalResponseGetRequest) (FinalResponseResponse, error) {
	var stored globaldb.FinalResponse
	var err error
	if strings.TrimSpace(req.FinalResponseID) != "" {
		stored, err = store.GetFinalResponseByID(ctx, req.FinalResponseID)
	} else if strings.TrimSpace(req.SessionID) != "" {
		stored, err = store.GetFinalResponseBySessionID(ctx, req.SessionID)
	} else {
		return FinalResponseResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "final_response_id or session_id is required", map[string]any{"reason": "missing_final_response_ref"})
	}
	if err != nil {
		if errors.Is(err, globaldb.ErrNotFound) {
			return FinalResponseResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "final response is not available", map[string]any{"reason": "unknown_final_response"})
		}
		return FinalResponseResponse{}, err
	}
	return finalResponseResponseFromStore(stored), nil
}

func listFinalResponses(ctx context.Context, store *globaldb.Store, req FinalResponseListRequest) (FinalResponseListResponse, error) {
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" {
		return FinalResponseListResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "workspace_id is required", map[string]any{"reason": "missing_workspace_id"})
	}
	stored, err := store.ListFinalResponses(ctx, workspaceID)
	if err != nil {
		return FinalResponseListResponse{}, mapWorkspaceStoreError(err, workspaceID)
	}
	responses := make([]FinalResponseResponse, 0, len(stored))
	for _, response := range stored {
		responses = append(responses, finalResponseResponseFromStore(response))
	}
	return FinalResponseListResponse{FinalResponses: responses}, nil
}

func finalResponseResponseFromStore(stored globaldb.FinalResponse) FinalResponseResponse {
	links := []FinalResponseEvidenceLink{}
	if strings.TrimSpace(stored.EvidenceLinksJSON) != "" {
		_ = json.Unmarshal([]byte(stored.EvidenceLinksJSON), &links)
	}
	updatedAt := ""
	if stored.UpdatedAt != nil {
		updatedAt = stored.UpdatedAt.Format(time.RFC3339Nano)
	}
	return presentFinalResponse(FinalResponseResponse{FinalResponseID: stored.FinalResponseID, SessionID: stored.HarnessSessionID, WorkspaceID: stored.WorkspaceID, TaskID: stored.TaskID, ContextPacketID: stored.ContextPacketID, ProfileID: stored.ProfileID, Status: stored.Status, Text: stored.Text, EvidenceLinks: links, CreatedAt: stored.CreatedAt.Format(time.RFC3339Nano), UpdatedAt: updatedAt})
}

func telemetryRollup(ctx context.Context, store *globaldb.Store, req TelemetryRollupRequest) (TelemetryRollupResponse, error) {
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" {
		return TelemetryRollupResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "workspace_id is required", map[string]any{"reason": "missing_workspace_id"})
	}
	rollups, err := store.RollupHarnessSessionTelemetry(ctx, workspaceID)
	if err != nil {
		return TelemetryRollupResponse{}, mapWorkspaceStoreError(err, workspaceID)
	}
	out := make([]TelemetryRollup, 0, len(rollups))
	for _, rollup := range rollups {
		out = append(out, telemetryRollupFromStore(rollup))
	}
	return TelemetryRollupResponse{Rollups: out}, nil
}

func telemetryRollupFromStore(rollup globaldb.HarnessSessionTelemetryRollup) TelemetryRollup {
	ports := []ProcessPortObservation{}
	if strings.TrimSpace(rollup.PortsJSON) != "" {
		_ = json.Unmarshal([]byte(rollup.PortsJSON), &ports)
	}
	orphanState := strings.TrimSpace(rollup.OrphanState)
	if orphanState == "" {
		orphanState = "unknown"
	}
	return TelemetryRollup{Group: TelemetryRollupGroup{ProfileID: rollup.Group.ProfileID, Profile: rollup.Group.ProfileName, Harness: rollup.Group.Harness, Model: rollup.Group.Model, InvocationClass: rollup.Group.InvocationClass}, Runs: rollup.Runs, Completed: rollup.Completed, Failed: rollup.Failed, InputTokens: telemetryKnownInt64FromStore(rollup.InputTokens), OutputTokens: telemetryKnownInt64FromStore(rollup.OutputTokens), EstimatedCost: telemetryKnownInt64FromStore(rollup.EstimatedCost), DurationMS: telemetryKnownInt64FromStore(rollup.DurationMS), Process: TelemetryProcessRollup{OwnedByAri: rollup.OwnedByAri, PID: telemetryKnownInt64FromStore(rollup.PID), CPUTimeMS: telemetryKnownInt64FromStore(rollup.CPUTimeMS), MemoryRSSBytesPeak: telemetryKnownInt64FromStore(rollup.MemoryRSS), ChildProcessesPeak: telemetryKnownInt64FromStore(rollup.ChildCount), Ports: ports, OrphanState: orphanState, ExitCode: telemetryKnownInt64FromStore(rollup.ExitCode)}}
}

func telemetryKnownInt64FromStore(value globaldb.KnownInt64) TelemetryKnownInt64 {
	return TelemetryKnownInt64{Known: value.Known, Value: value.Value}
}

func tailRunLogMessages(ctx context.Context, store *globaldb.Store, req RunLogMessagesTailRequest) (RunLogMessagesTailResponse, error) {
	if strings.TrimSpace(req.SessionID) == "" {
		return RunLogMessagesTailResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "session_id is required", nil)
	}
	if req.Count <= 0 {
		return RunLogMessagesTailResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "count must be greater than zero", nil)
	}
	messages, err := store.TailRunLogMessages(ctx, req.SessionID, req.Count)
	if err != nil {
		if errors.Is(err, globaldb.ErrInvalidInput) || errors.Is(err, globaldb.ErrNotFound) {
			return RunLogMessagesTailResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
		}
		return RunLogMessagesTailResponse{}, err
	}
	resp := RunLogMessagesTailResponse{Messages: make([]RunLogMessageResponse, 0, len(messages))}
	for _, msg := range messages {
		resp.Messages = append(resp.Messages, runLogMessageResponse(msg))
	}
	return resp, nil
}

func listRunLogMessages(ctx context.Context, store *globaldb.Store, req RunLogMessagesListRequest) (RunLogMessagesListResponse, error) {
	if strings.TrimSpace(req.SessionID) == "" {
		return RunLogMessagesListResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "session_id is required", nil)
	}
	if req.AfterSequence < 0 {
		return RunLogMessagesListResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "after_sequence must be zero or greater", nil)
	}
	if req.Limit <= 0 {
		return RunLogMessagesListResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "limit must be greater than zero", nil)
	}
	messages, err := store.ListRunLogMessages(ctx, req.SessionID, req.AfterSequence, req.Limit)
	if err != nil {
		if errors.Is(err, globaldb.ErrInvalidInput) || errors.Is(err, globaldb.ErrNotFound) {
			return RunLogMessagesListResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
		}
		return RunLogMessagesListResponse{}, err
	}
	resp := RunLogMessagesListResponse{Messages: make([]RunLogMessageResponse, 0, len(messages))}
	for _, msg := range messages {
		resp.Messages = append(resp.Messages, runLogMessageResponse(msg))
	}
	return resp, nil
}

func createContextExcerptFromTail(ctx context.Context, store *globaldb.Store, req ContextExcerptCreateFromTailRequest) (ContextExcerptResponse, error) {
	excerpt, err := store.CreateContextExcerptFromTail(ctx, globaldb.CreateContextExcerptFromTailParams{ContextExcerptID: strings.TrimSpace(req.ContextExcerptID), SourceSessionID: strings.TrimSpace(req.SourceSessionID), TargetAgentID: strings.TrimSpace(req.TargetAgentID), Count: req.Count, AppendedMessage: req.AppendedMessage})
	if err != nil {
		if errors.Is(err, globaldb.ErrInvalidInput) || errors.Is(err, globaldb.ErrNotFound) {
			return ContextExcerptResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
		}
		return ContextExcerptResponse{}, err
	}
	return contextExcerptResponse(excerpt), nil
}

func createContextExcerptFromRange(ctx context.Context, store *globaldb.Store, req ContextExcerptCreateFromRangeRequest) (ContextExcerptResponse, error) {
	excerpt, err := store.CreateContextExcerptFromRange(ctx, globaldb.CreateContextExcerptFromRangeParams{ContextExcerptID: strings.TrimSpace(req.ContextExcerptID), SourceSessionID: strings.TrimSpace(req.SourceSessionID), TargetAgentID: strings.TrimSpace(req.TargetAgentID), StartSequence: req.StartSequence, EndSequence: req.EndSequence, AppendedMessage: req.AppendedMessage})
	if err != nil {
		if errors.Is(err, globaldb.ErrInvalidInput) || errors.Is(err, globaldb.ErrNotFound) {
			return ContextExcerptResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
		}
		return ContextExcerptResponse{}, err
	}
	return contextExcerptResponse(excerpt), nil
}

func createContextExcerptFromExplicitIDs(ctx context.Context, store *globaldb.Store, req ContextExcerptCreateFromExplicitIDsRequest) (ContextExcerptResponse, error) {
	excerpt, err := store.CreateContextExcerptFromExplicitIDs(ctx, globaldb.CreateContextExcerptFromExplicitIDsParams{ContextExcerptID: strings.TrimSpace(req.ContextExcerptID), SourceSessionID: strings.TrimSpace(req.SourceSessionID), TargetAgentID: strings.TrimSpace(req.TargetAgentID), MessageIDs: req.MessageIDs, AppendedMessage: req.AppendedMessage})
	if err != nil {
		if errors.Is(err, globaldb.ErrInvalidInput) || errors.Is(err, globaldb.ErrNotFound) {
			return ContextExcerptResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
		}
		return ContextExcerptResponse{}, err
	}
	return contextExcerptResponse(excerpt), nil
}

func getContextExcerpt(ctx context.Context, store *globaldb.Store, req ContextExcerptGetRequest) (ContextExcerptResponse, error) {
	if strings.TrimSpace(req.ContextExcerptID) == "" {
		return ContextExcerptResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "context_excerpt_id is required", nil)
	}
	excerpt, err := store.GetContextExcerpt(ctx, strings.TrimSpace(req.ContextExcerptID))
	if err != nil {
		if errors.Is(err, globaldb.ErrNotFound) {
			return ContextExcerptResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
		}
		return ContextExcerptResponse{}, err
	}
	return contextExcerptResponse(excerpt), nil
}

func contextExcerptResponse(excerpt globaldb.ContextExcerpt) ContextExcerptResponse {
	resp := ContextExcerptResponse{ContextExcerptID: excerpt.ContextExcerptID, WorkspaceID: excerpt.WorkspaceID, SourceSessionID: excerpt.SourceSessionID, SourceAgentID: excerpt.SourceAgentID, TargetAgentID: excerpt.TargetAgentID, TargetSessionID: excerpt.TargetSessionID, SelectorType: excerpt.SelectorType, SelectorJSON: excerpt.SelectorJSON, Visibility: excerpt.Visibility, AppendedMessage: excerpt.AppendedMessage, ContentHash: excerpt.ContentHash, Items: make([]ContextExcerptItemResponse, 0, len(excerpt.Items))}
	for _, item := range excerpt.Items {
		partResponses := make([]RunLogMessagePartResponse, 0, len(item.CopiedParts))
		for _, part := range item.CopiedParts {
			partResponses = append(partResponses, runLogMessagePartResponse(part))
		}
		resp.Items = append(resp.Items, ContextExcerptItemResponse{Sequence: item.Sequence, SourceMessageID: item.SourceMessageID, CopiedRole: item.CopiedRole, CopiedText: item.CopiedText, CopiedParts: partResponses})
	}
	return resp
}

func runLogMessagePartResponse(part globaldb.RunLogMessagePart) RunLogMessagePartResponse {
	return RunLogMessagePartResponse{PartID: part.PartID, Sequence: part.Sequence, Kind: part.Kind, Text: part.Text, MimeType: part.MimeType, URI: part.URI, Name: part.Name, ToolName: part.ToolName, ToolCallID: part.ToolCallID, RawJSON: part.RawJSON}
}

func runLogMessageResponse(msg globaldb.RunLogMessage) RunLogMessageResponse {
	resp := RunLogMessageResponse{MessageID: msg.MessageID, SessionID: msg.SessionID, AgentID: msg.AgentID, Sequence: msg.Sequence, Role: msg.Role, Status: msg.Status, ProviderMessageID: msg.ProviderMessageID, ProviderItemID: msg.ProviderItemID, ProviderTurnID: msg.ProviderTurnID, ProviderResponseID: msg.ProviderResponseID, ProviderCallID: msg.ProviderCallID, ProviderChannel: msg.ProviderChannel, ProviderKind: msg.ProviderKind, RawMetadataJSON: msg.RawMetadataJSON, Parts: make([]RunLogMessagePartResponse, 0, len(msg.Parts))}
	for _, part := range msg.Parts {
		resp.Parts = append(resp.Parts, runLogMessagePartResponse(part))
	}
	return resp
}

func sendAgentMessage(ctx context.Context, store *globaldb.Store, req AgentMessageSendRequest) (AgentMessageSendResponse, error) {
	agentMessageID := strings.TrimSpace(req.AgentMessageID)
	if agentMessageID == "" {
		generated, err := newAriULID()
		if err != nil {
			return AgentMessageSendResponse{}, err
		}
		agentMessageID = "am_" + generated
	}
	sourceSessionID := strings.TrimSpace(req.SourceSessionID)
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	body := strings.TrimSpace(req.Body)
	targetAgentID := strings.TrimSpace(req.TargetAgentID)
	targetSessionID := strings.TrimSpace(req.TargetSessionID)
	startSessionID := strings.TrimSpace(req.StartSessionID)
	contextExcerptIDs := trimNonEmptyStrings(req.ContextExcerptIDs)
	if strings.TrimSpace(req.FanoutGroupID) != "" || len(trimNonEmptyStrings(req.TargetProfileIDs)) > 0 {
		return AgentMessageSendResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "fanout fields are only supported by session.fanout", map[string]any{"reason": "fanout_fields_unsupported", "start_invoked": false})
	}
	effectiveTargetSessionID := targetSessionID
	if effectiveTargetSessionID == "" {
		effectiveTargetSessionID = startSessionID
	}
	if sourceSessionID == "" || body == "" || (targetAgentID == "" && targetSessionID == "" && startSessionID == "") {
		missingField := ""
		switch {
		case sourceSessionID == "":
			missingField = "source_session_id"
		case body == "":
			missingField = "body"
		default:
			missingField = "target_agent_id_or_target_session_id"
		}
		return AgentMessageSendResponse{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "missing_required_fields", "missing_field": missingField, "start_invoked": false})
	}
	if workspaceID != "" {
		sourceRun, sourceErr := store.GetHarnessSession(ctx, sourceSessionID)
		if sourceErr != nil {
			if errors.Is(sourceErr, globaldb.ErrNotFound) {
				return AgentMessageSendResponse{}, rpc.NewHandlerError(rpc.InvalidParams, sourceErr.Error(), map[string]any{"reason": "unknown_source_session", "source_session_id": sourceSessionID, "workspace_id": workspaceID, "start_invoked": false})
			}
			return AgentMessageSendResponse{}, sourceErr
		}
		if strings.TrimSpace(sourceRun.WorkspaceID) != workspaceID {
			return AgentMessageSendResponse{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "source_workspace_mismatch", "source_session_id": sourceSessionID, "source_workspace_id": sourceRun.WorkspaceID, "workspace_id": workspaceID, "start_invoked": false})
		}
	}
	dm, err := store.SendAgentMessage(ctx, globaldb.AgentMessageSendParams{AgentMessageID: agentMessageID, SourceSessionID: sourceSessionID, TargetAgentID: targetAgentID, TargetSessionID: targetSessionID, Body: body, ContextExcerptIDs: contextExcerptIDs, StartSessionID: startSessionID, WorkspaceEvent: &globaldb.WorkspaceEvent{EventType: workspaceEventMessageSent, SubjectType: "agent_message", SubjectID: agentMessageID, ProducerType: workspaceEventProducerSession, ProducerID: sourceSessionID, PayloadJSON: daemonEventPayload(map[string]string{"source_session_id": sourceSessionID, "target_agent_id": targetAgentID, "target_session_id": effectiveTargetSessionID})}})
	if err != nil {
		errText := strings.ToLower(err.Error())
		if strings.Contains(errText, "unique constraint failed") && strings.Contains(errText, "agent_messages.agent_message_id") {
			return AgentMessageSendResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "agent_message_id_conflict", "agent_message_id": agentMessageID, "start_invoked": false})
		}
		if errors.Is(err, globaldb.ErrInvalidInput) || errors.Is(err, globaldb.ErrNotFound) {
			resolvedTargetAgentID := targetAgentID
			if resolvedTargetAgentID == "" && effectiveTargetSessionID != "" {
				if targetRun, targetErr := store.GetHarnessSession(ctx, effectiveTargetSessionID); targetErr == nil {
					resolvedTargetAgentID = strings.TrimSpace(targetRun.AgentID)
				}
			}
			if errors.Is(err, globaldb.ErrNotFound) && len(contextExcerptIDs) > 0 {
				contextExcerptID := contextExcerptIDs[0]
				if contextExcerptID != "" {
					if _, excerptErr := store.GetContextExcerpt(ctx, contextExcerptID); errors.Is(excerptErr, globaldb.ErrNotFound) {
						return AgentMessageSendResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "unknown_context_excerpt", "context_excerpt_id": contextExcerptID, "start_invoked": false})
					}
				}
			}
			if errors.Is(err, globaldb.ErrInvalidInput) && len(contextExcerptIDs) > 0 {
				contextExcerptID := contextExcerptIDs[0]
				if contextExcerptID != "" {
					if excerpt, excerptErr := store.GetContextExcerpt(ctx, contextExcerptID); excerptErr == nil {
						if excerpt.TargetAgentID != "" && resolvedTargetAgentID != "" && excerpt.TargetAgentID != resolvedTargetAgentID {
							return AgentMessageSendResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "context_excerpt_mismatch", "context_excerpt_id": contextExcerptID, "target_session_id": effectiveTargetSessionID, "target_agent_id": resolvedTargetAgentID, "start_invoked": false})
						}
					}
				}
			}
			if errors.Is(err, globaldb.ErrInvalidInput) && effectiveTargetSessionID != "" && targetAgentID != "" {
				if targetRun, targetErr := store.GetHarnessSession(ctx, effectiveTargetSessionID); targetErr == nil {
					if strings.TrimSpace(targetRun.AgentID) != "" && targetRun.AgentID != targetAgentID {
						return AgentMessageSendResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "target_session_mismatch", "target_session_id": effectiveTargetSessionID, "target_agent_id": targetAgentID, "start_invoked": false})
					}
				}
			}
			if errors.Is(err, globaldb.ErrInvalidInput) && effectiveTargetSessionID != "" {
				if sourceRun, sourceErr := store.GetHarnessSession(ctx, sourceSessionID); sourceErr == nil {
					if targetRun, targetErr := store.GetHarnessSession(ctx, effectiveTargetSessionID); targetErr == nil {
						if strings.TrimSpace(targetRun.WorkspaceID) != "" && targetRun.WorkspaceID != sourceRun.WorkspaceID {
							return AgentMessageSendResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "target_workspace_mismatch", "target_session_id": effectiveTargetSessionID, "source_workspace_id": sourceRun.WorkspaceID, "target_workspace_id": targetRun.WorkspaceID, "start_invoked": false})
						}
					}
				}
			}
			if errors.Is(err, globaldb.ErrInvalidInput) && targetAgentID != "" {
				if sourceRun, sourceErr := store.GetHarnessSession(ctx, sourceSessionID); sourceErr == nil {
					if targetCfg, targetErr := store.GetHarnessSessionConfig(ctx, targetAgentID); targetErr == nil {
						if strings.TrimSpace(targetCfg.WorkspaceID) != "" && targetCfg.WorkspaceID != sourceRun.WorkspaceID {
							return AgentMessageSendResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "target_workspace_mismatch", "target_agent_id": targetAgentID, "source_workspace_id": sourceRun.WorkspaceID, "target_workspace_id": targetCfg.WorkspaceID, "start_invoked": false})
						}
					}
				}
			}
			if errors.Is(err, globaldb.ErrNotFound) {
				if sourceSessionID != "" {
					if _, sourceErr := store.GetHarnessSession(ctx, sourceSessionID); errors.Is(sourceErr, globaldb.ErrNotFound) {
						return AgentMessageSendResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "unknown_source_session", "source_session_id": sourceSessionID, "start_invoked": false})
					}
				}
				if effectiveTargetSessionID != "" && (targetSessionID != "" || targetAgentID == "") {
					if _, targetErr := store.GetHarnessSession(ctx, effectiveTargetSessionID); errors.Is(targetErr, globaldb.ErrNotFound) {
						return AgentMessageSendResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "unknown_target_session", "target_session_id": effectiveTargetSessionID, "start_invoked": false})
					}
				}
				if targetAgentID != "" {
					return AgentMessageSendResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "unknown_target_agent", "target_agent_id": targetAgentID, "start_invoked": false})
				}
			}
			return AgentMessageSendResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "invalid_session_message", "agent_message_id": agentMessageID, "source_session_id": sourceSessionID, "target_session_id": effectiveTargetSessionID, "target_agent_id": targetAgentID, "start_invoked": false})
		}
		return AgentMessageSendResponse{}, err
	}
	return AgentMessageSendResponse{AgentMessage: AgentMessageResponse{AgentMessageID: dm.AgentMessageID, WorkspaceID: dm.WorkspaceID, SourceAgentID: dm.SourceAgentID, SourceSessionID: dm.SourceSessionID, TargetAgentID: dm.TargetAgentID, TargetSessionID: dm.TargetSessionID, Body: dm.Body, Status: dm.Status, DeliveredSessionID: dm.DeliveredSessionID, ContextExcerptIDs: dm.ContextExcerptIDs}}, nil
}

func trimNonEmptyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func agentMessageResponse(dm globaldb.AgentMessage) AgentMessageResponse {
	return AgentMessageResponse{AgentMessageID: dm.AgentMessageID, WorkspaceID: dm.WorkspaceID, SourceAgentID: dm.SourceAgentID, SourceSessionID: dm.SourceSessionID, TargetAgentID: dm.TargetAgentID, TargetSessionID: dm.TargetSessionID, Body: dm.Body, Status: dm.Status, DeliveredSessionID: dm.DeliveredSessionID, ContextExcerptIDs: dm.ContextExcerptIDs}
}

func storeFinalResponse(ctx context.Context, store *globaldb.Store, result HarnessCallResult, profile ...Profile) (string, error) {
	responseID, err := newAriULID()
	if err != nil {
		return "", err
	}
	finalResponseID := "fr_" + responseID
	profileID := ""
	if len(profile) > 0 {
		profileID = strings.TrimSpace(profile[0].ProfileID)
	}
	links := []FinalResponseEvidenceLink{{Kind: "context_packet", ID: result.HarnessSession.ContextPacketID}, {Kind: "harness_session", ID: result.HarnessSession.HarnessSessionID}}
	for _, item := range result.Items {
		if strings.TrimSpace(item.ID) != "" {
			links = append(links, FinalResponseEvidenceLink{Kind: "timeline_item", ID: item.ID})
		}
	}
	encodedLinks, err := json.Marshal(links)
	if err != nil {
		return "", err
	}
	if err := store.UpsertFinalResponse(ctx, globaldb.FinalResponse{FinalResponseID: finalResponseID, HarnessSessionID: result.HarnessSession.HarnessSessionID, WorkspaceID: result.HarnessSession.WorkspaceID, TaskID: result.HarnessSession.TaskID, ContextPacketID: result.HarnessSession.ContextPacketID, ProfileID: profileID, Status: result.FinalResponse.Status, Text: result.FinalResponse.Text, EvidenceLinksJSON: string(encodedLinks)}); err != nil {
		return "", err
	}
	return finalResponseID, nil
}

func storeHarnessSessionTelemetry(ctx context.Context, store *globaldb.Store, result HarnessCallResult, sample ProcessMetricsSample, profile ...Profile) error {
	profileID := ""
	profileName := ""
	invocationClass := string(HarnessInvocationSticky)
	if len(profile) > 0 {
		profileID = strings.TrimSpace(profile[0].ProfileID)
		profileName = strings.TrimSpace(profile[0].Name)
		if profile[0].InvocationClass != "" {
			invocationClass = string(profile[0].InvocationClass)
		}
	}
	portsJSON := "[]"
	if len(sample.Ports) > 0 {
		encoded, err := json.Marshal(sample.Ports)
		if err != nil {
			return err
		}
		portsJSON = string(encoded)
	}
	model := strings.TrimSpace(result.Telemetry.Model)
	if model == "" {
		model = "unknown"
	}
	durationMS, durationKnown := agentSessionDurationMS(result.HarnessSession)
	return store.UpsertHarnessSessionTelemetry(ctx, globaldb.HarnessSessionTelemetry{HarnessSessionID: result.HarnessSession.HarnessSessionID, WorkspaceID: result.HarnessSession.WorkspaceID, TaskID: result.HarnessSession.TaskID, ProfileID: profileID, ProfileName: profileName, Harness: result.HarnessSession.Executor, Model: model, InvocationClass: invocationClass, Status: result.HarnessSession.Status, InputTokensKnown: result.Telemetry.InputTokens != nil, InputTokens: result.Telemetry.InputTokens, OutputTokensKnown: result.Telemetry.OutputTokens != nil, OutputTokens: result.Telemetry.OutputTokens, DurationMSKnown: durationKnown, DurationMS: durationMS, OwnedByAri: sample.OwnedByAri, PIDKnown: sample.PID.Known, PID: sample.PID.Value, CPUTimeMSKnown: sample.CPUTimeMS.Known, CPUTimeMS: sample.CPUTimeMS.Value, MemoryRSSBytesPeakKnown: sample.MemoryRSSBytesPeak.Known, MemoryRSSBytesPeak: sample.MemoryRSSBytesPeak.Value, ChildProcessesPeakKnown: sample.ChildProcessesPeak.Known, ChildProcessesPeak: sample.ChildProcessesPeak.Value, PortsJSON: portsJSON, OrphanState: sample.OrphanState, ExitCodeKnown: sample.ExitCode.Known, ExitCode: sample.ExitCode.Value})
}
