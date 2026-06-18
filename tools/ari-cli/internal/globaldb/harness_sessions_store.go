package globaldb

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb/dbsqlc"
)

type HarnessSessionConfig struct {
	AgentID     string
	WorkspaceID string
	Name        string
	Harness     string
	Model       string
	Prompt      string
}

const (
	HarnessSessionUsageSticky    = "sticky"
	HarnessSessionUsageEphemeral = "ephemeral"
)

type HarnessSession struct {
	SessionID             string
	WorkspaceID           string
	AgentID               string
	Harness               string
	Model                 string
	ProviderSessionID     string
	ProviderRunID         string
	ProviderThreadID      string
	CWD                   string
	FolderScopeJSON       string
	Status                string
	Usage                 string
	SourceSessionID       string
	SourceAgentID         string
	PromptHash            string
	ContextPayloadIDsJSON string
	PermissionMode        string
	SandboxMode           string
	ToolScopeJSON         string
	ProviderMetadataJSON  string
}

type RunLogMessage struct {
	MessageID          string
	WorkspaceID        string
	SessionID          string
	AgentID            string
	Sequence           int
	Role               string
	Status             string
	ProviderMessageID  string
	ProviderItemID     string
	ProviderTurnID     string
	ProviderResponseID string
	ProviderCallID     string
	ProviderChannel    string
	ProviderKind       string
	RawMetadataJSON    string
	Parts              []RunLogMessagePart
}

type RunLogMessagePart struct {
	PartID     string `json:"part_id"`
	Sequence   int    `json:"sequence"`
	Kind       string `json:"kind"`
	Text       string `json:"text"`
	MimeType   string `json:"mime_type"`
	URI        string `json:"uri"`
	Name       string `json:"name"`
	ToolName   string `json:"tool_name"`
	ToolCallID string `json:"tool_call_id"`
	RawJSON    string `json:"raw_json"`
}

type ContextExcerpt struct {
	ContextExcerptID string
	WorkspaceID      string
	SourceSessionID  string
	SourceAgentID    string
	TargetAgentID    string
	TargetSessionID  string
	SelectorType     string
	SelectorJSON     string
	Visibility       string
	AppendedMessage  string
	ContentHash      string
	Items            []ContextExcerptItem
}

type ContextExcerptItem struct {
	Sequence        int
	SourceMessageID string
	CopiedRole      string
	CopiedText      string
	CopiedParts     []RunLogMessagePart
}

type AgentMessage struct {
	AgentMessageID     string
	WorkspaceID        string
	SourceAgentID      string
	SourceSessionID    string
	TargetAgentID      string
	TargetSessionID    string
	Body               string
	Status             string
	DeliveredSessionID string
	ContextExcerptIDs  []string
}

type CreateContextExcerptFromTailParams struct {
	ContextExcerptID string
	WorkspaceID      string
	SourceSessionID  string
	SourceAgentID    string
	TargetAgentID    string
	Count            int
	AppendedMessage  string
}

type CreateContextExcerptFromRangeParams struct {
	ContextExcerptID string
	SourceSessionID  string
	TargetAgentID    string
	StartSequence    int
	EndSequence      int
	AppendedMessage  string
}

type CreateContextExcerptFromExplicitIDsParams struct {
	ContextExcerptID string
	SourceSessionID  string
	TargetAgentID    string
	MessageIDs       []string
	AppendedMessage  string
}

type AgentMessageSendParams struct {
	AgentMessageID    string
	SourceSessionID   string
	TargetAgentID     string
	TargetSessionID   string
	Body              string
	ContextExcerptIDs []string
	StartSessionID    string
	// WorkspaceEvent, when set, customizes the message.sent fact appended to
	// workspace event history in the same transaction as the message. The store
	// fills missing workspace, subject, producer, correlation, and artifact ref
	// identity from the resolved message.
	WorkspaceEvent *WorkspaceEvent
}

func defaultAgentMessageWorkspaceEvent(params AgentMessageSendParams, workspaceID, sourceSessionID, sourceAgentID, targetSessionID string) WorkspaceEvent {
	return WorkspaceEvent{WorkspaceID: strings.TrimSpace(workspaceID), EventType: "message.sent", SubjectType: "agent_message", SubjectID: params.AgentMessageID, ProducerType: "session", ProducerID: strings.TrimSpace(sourceSessionID), CorrelationID: targetSessionID, PayloadJSON: defaultAgentMessageWorkspaceEventPayload(params, sourceSessionID, sourceAgentID, targetSessionID), PayloadRefJSON: daemonLocalPayloadRef("agent_message", params.AgentMessageID)}
}

func defaultAgentMessageWorkspaceEventPayload(params AgentMessageSendParams, sourceSessionID, sourceAgentID, targetSessionID string) string {
	encoded, _ := json.Marshal(map[string]string{"agent_message_id": strings.TrimSpace(params.AgentMessageID), "source_session_id": strings.TrimSpace(sourceSessionID), "source_agent_id": strings.TrimSpace(sourceAgentID), "target_agent_id": strings.TrimSpace(params.TargetAgentID), "target_session_id": strings.TrimSpace(targetSessionID), "context_excerpt_count": fmt.Sprintf("%d", len(params.ContextExcerptIDs))})
	return string(encoded)
}

func contextExcerptCreatedWorkspaceEvent(excerpt ContextExcerpt) (WorkspaceEvent, error) {
	payload, err := json.Marshal(map[string]string{
		"context_excerpt_id": excerpt.ContextExcerptID,
		"source_session_id":  excerpt.SourceSessionID,
		"source_agent_id":    excerpt.SourceAgentID,
		"target_agent_id":    excerpt.TargetAgentID,
		"selector_type":      excerpt.SelectorType,
		"item_count":         fmt.Sprintf("%d", len(excerpt.Items)),
	})
	if err != nil {
		return WorkspaceEvent{}, err
	}
	return prepareCoordinatedWorkspaceEvent(WorkspaceEvent{WorkspaceID: excerpt.WorkspaceID, EventType: "context_excerpt.created", SubjectType: "context_excerpt", SubjectID: excerpt.ContextExcerptID, ProducerType: "session", ProducerID: excerpt.SourceSessionID, CorrelationID: excerpt.SourceSessionID, PayloadJSON: string(payload), PayloadRefJSON: daemonLocalPayloadRef("context_excerpt", excerpt.ContextExcerptID)})
}

func (s *Store) CreateHarnessSessionConfig(ctx context.Context, agent HarnessSessionConfig) error {
	if strings.TrimSpace(agent.AgentID) == "" || strings.TrimSpace(agent.Name) == "" {
		return ErrInvalidInput
	}
	var workspaceID *string
	if strings.TrimSpace(agent.WorkspaceID) != "" {
		if _, err := s.GetWorkspace(ctx, strings.TrimSpace(agent.WorkspaceID)); err != nil {
			if errors.Is(err, ErrNotFound) {
				return ErrInvalidInput
			}
			return err
		}
		workspaceID = &agent.WorkspaceID
	}
	return s.sqlc.CreateHarnessSessionConfig(ctx, dbsqlc.CreateHarnessSessionConfigParams{AgentID: agent.AgentID, WorkspaceID: workspaceID, Name: agent.Name, Harness: agent.Harness, Model: agent.Model, Prompt: agent.Prompt})
}

func (s *Store) EnsureHarnessSessionConfig(ctx context.Context, agent HarnessSessionConfig) error {
	if strings.TrimSpace(agent.AgentID) == "" || strings.TrimSpace(agent.Name) == "" {
		return ErrInvalidInput
	}
	var workspaceID *string
	if strings.TrimSpace(agent.WorkspaceID) != "" {
		if _, err := s.GetWorkspace(ctx, strings.TrimSpace(agent.WorkspaceID)); err != nil {
			if errors.Is(err, ErrNotFound) {
				return ErrInvalidInput
			}
			return err
		}
		workspaceID = &agent.WorkspaceID
	}
	return s.sqlc.EnsureHarnessSessionConfig(ctx, dbsqlc.EnsureHarnessSessionConfigParams{AgentID: agent.AgentID, WorkspaceID: workspaceID, Name: agent.Name, Harness: agent.Harness, Model: agent.Model, Prompt: agent.Prompt})
}

func (s *Store) GetHarnessSessionConfig(ctx context.Context, agentID string) (HarnessSessionConfig, error) {
	row, err := s.sqlc.GetHarnessSessionConfig(ctx, dbsqlc.GetHarnessSessionConfigParams{AgentID: strings.TrimSpace(agentID)})
	if err != nil {
		if err == sql.ErrNoRows {
			return HarnessSessionConfig{}, ErrNotFound
		}
		return HarnessSessionConfig{}, err
	}
	return agentSessionConfigFromRow(row.AgentID, row.WorkspaceID, row.Name, row.Harness, row.Model, row.Prompt), nil
}

func (s *Store) ListHarnessSessionConfigs(ctx context.Context, workspaceID string) ([]HarnessSessionConfig, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, ErrInvalidInput
	}
	rows, err := s.sqlc.ListHarnessSessionConfigs(ctx, dbsqlc.ListHarnessSessionConfigsParams{WorkspaceID: &workspaceID})
	if err != nil {
		return nil, err
	}
	agents := make([]HarnessSessionConfig, 0, len(rows))
	for _, row := range rows {
		agents = append(agents, agentSessionConfigFromRow(row.AgentID, row.WorkspaceID, row.Name, row.Harness, row.Model, row.Prompt))
	}
	return agents, nil
}

func (s *Store) UpdateHarnessSessionConfig(ctx context.Context, agent HarnessSessionConfig) (HarnessSessionConfig, error) {
	if strings.TrimSpace(agent.AgentID) == "" || strings.TrimSpace(agent.WorkspaceID) == "" || strings.TrimSpace(agent.Name) == "" {
		return HarnessSessionConfig{}, ErrInvalidInput
	}
	before, err := s.GetHarnessSessionConfig(ctx, agent.AgentID)
	if err != nil {
		return HarnessSessionConfig{}, err
	}
	if before.WorkspaceID != agent.WorkspaceID {
		return HarnessSessionConfig{}, ErrNotFound
	}
	if err := s.sqlc.UpdateHarnessSessionConfig(ctx, dbsqlc.UpdateHarnessSessionConfigParams{Name: agent.Name, Harness: agent.Harness, Model: agent.Model, Prompt: agent.Prompt, AgentID: agent.AgentID, WorkspaceID: &agent.WorkspaceID}); err != nil {
		return HarnessSessionConfig{}, err
	}
	return s.GetHarnessSessionConfig(ctx, agent.AgentID)
}

func (s *Store) DeleteHarnessSessionConfig(ctx context.Context, agentID string) error {
	if strings.TrimSpace(agentID) == "" {
		return ErrInvalidInput
	}
	return s.sqlc.DeleteHarnessSessionConfig(ctx, dbsqlc.DeleteHarnessSessionConfigParams{AgentID: strings.TrimSpace(agentID)})
}

func (s *Store) CreateHarnessSessionFromConfig(ctx context.Context, sessionID, agentID, cwd string) (HarnessSession, error) {
	agent, err := s.GetHarnessSessionConfig(ctx, agentID)
	if err != nil {
		return HarnessSession{}, err
	}
	if _, err := s.GetWorkspace(ctx, agent.WorkspaceID); err != nil {
		return HarnessSession{}, err
	}
	run := HarnessSession{SessionID: strings.TrimSpace(sessionID), WorkspaceID: agent.WorkspaceID, AgentID: agent.AgentID, Harness: agent.Harness, Model: agent.Model, CWD: strings.TrimSpace(cwd), Status: "waiting", Usage: HarnessSessionUsageSticky}
	if err := s.CreateHarnessSession(ctx, run); err != nil {
		return HarnessSession{}, err
	}
	return run, nil
}

func agentSessionConfigFromRow(agentID string, workspaceID *string, name, harness, model, prompt string) HarnessSessionConfig {
	workspace := ""
	if workspaceID != nil {
		workspace = *workspaceID
	}
	return HarnessSessionConfig{AgentID: agentID, WorkspaceID: workspace, Name: name, Harness: harness, Model: model, Prompt: prompt}
}

func (s *Store) CreateHarnessSession(ctx context.Context, run HarnessSession) error {
	if strings.TrimSpace(run.SessionID) == "" || strings.TrimSpace(run.WorkspaceID) == "" || strings.TrimSpace(run.AgentID) == "" || strings.TrimSpace(run.Status) == "" {
		return ErrInvalidInput
	}
	agent, err := s.sqlc.GetHarnessSessionConfig(ctx, dbsqlc.GetHarnessSessionConfigParams{AgentID: strings.TrimSpace(run.AgentID)})
	if err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	if agent.WorkspaceID != nil && *agent.WorkspaceID != strings.TrimSpace(run.WorkspaceID) {
		return ErrInvalidInput
	}
	usage := strings.TrimSpace(run.Usage)
	if usage == "" {
		usage = HarnessSessionUsageSticky
	}
	return s.sqlc.CreateHarnessSession(ctx, createHarnessSessionParams(run, usage))
}

func createHarnessSessionParams(run HarnessSession, usage string) dbsqlc.CreateHarnessSessionParams {
	return dbsqlc.CreateHarnessSessionParams{SessionID: run.SessionID, WorkspaceID: run.WorkspaceID, AgentID: run.AgentID, Harness: run.Harness, Model: run.Model, ProviderSessionID: run.ProviderSessionID, ProviderRunID: run.ProviderRunID, ProviderThreadID: run.ProviderThreadID, Cwd: run.CWD, FolderScopeJson: defaultJSON(run.FolderScopeJSON, "[]"), Status: run.Status, Usage: usage, SourceSessionID: run.SourceSessionID, SourceAgentID: run.SourceAgentID, PromptHash: run.PromptHash, ContextPayloadIdsJson: defaultJSON(run.ContextPayloadIDsJSON, "[]"), PermissionMode: run.PermissionMode, SandboxMode: run.SandboxMode, ToolScopeJson: defaultJSON(run.ToolScopeJSON, "{}"), ProviderMetadataJson: defaultJSON(run.ProviderMetadataJSON, "{}")}
}

func defaultJSON(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func agentSessionFromRow(sessionID, workspaceID, agentID, harness, model, providerSessionID, providerRunID, providerThreadID, cwd, folderScopeJSON, status, usage, sourceSessionID, sourceAgentID, promptHash, contextPayloadIDsJSON, permissionMode, sandboxMode, toolScopeJSON, providerMetadataJSON string) HarnessSession {
	return HarnessSession{SessionID: sessionID, WorkspaceID: workspaceID, AgentID: agentID, Harness: harness, Model: model, ProviderSessionID: providerSessionID, ProviderRunID: providerRunID, ProviderThreadID: providerThreadID, CWD: cwd, FolderScopeJSON: folderScopeJSON, Status: status, Usage: usage, SourceSessionID: sourceSessionID, SourceAgentID: sourceAgentID, PromptHash: promptHash, ContextPayloadIDsJSON: contextPayloadIDsJSON, PermissionMode: permissionMode, SandboxMode: sandboxMode, ToolScopeJSON: toolScopeJSON, ProviderMetadataJSON: providerMetadataJSON}
}

func (s *Store) UpdateHarnessSessionStatus(ctx context.Context, sessionID, status string) error {
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(status) == "" {
		return ErrInvalidInput
	}
	count, err := s.sqlc.UpdateHarnessSessionStatus(ctx, dbsqlc.UpdateHarnessSessionStatusParams{Status: strings.TrimSpace(status), SessionID: strings.TrimSpace(sessionID)})
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdateHarnessSessionProvider(ctx context.Context, sessionID, providerSessionID, providerRunID, providerMetadataJSON string) error {
	if strings.TrimSpace(sessionID) == "" {
		return ErrInvalidInput
	}
	count, err := s.sqlc.UpdateHarnessSessionProvider(ctx, dbsqlc.UpdateHarnessSessionProviderParams{ProviderSessionID: strings.TrimSpace(providerSessionID), ProviderRunID: strings.TrimSpace(providerRunID), ProviderMetadataJson: defaultJSON(providerMetadataJSON, "{}"), SessionID: strings.TrimSpace(sessionID)})
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) GetHarnessSession(ctx context.Context, sessionID string) (HarnessSession, error) {
	row, err := s.sqlc.GetHarnessSession(ctx, dbsqlc.GetHarnessSessionParams{SessionID: strings.TrimSpace(sessionID)})
	if err != nil {
		if err == sql.ErrNoRows {
			return HarnessSession{}, ErrNotFound
		}
		return HarnessSession{}, err
	}
	return agentSessionFromRow(row.SessionID, row.WorkspaceID, row.AgentID, row.Harness, row.Model, row.ProviderSessionID, row.ProviderRunID, row.ProviderThreadID, row.Cwd, row.FolderScopeJson, row.Status, row.Usage, row.SourceSessionID, row.SourceAgentID, row.PromptHash, row.ContextPayloadIdsJson, row.PermissionMode, row.SandboxMode, row.ToolScopeJson, row.ProviderMetadataJson), nil
}

func (s *Store) ListHarnessSessions(ctx context.Context, workspaceID string) ([]HarnessSession, error) {
	rows, err := s.sqlc.ListHarnessSessions(ctx, dbsqlc.ListHarnessSessionsParams{WorkspaceID: workspaceID})
	if err != nil {
		return nil, err
	}
	runs := make([]HarnessSession, 0, len(rows))
	for _, row := range rows {
		runs = append(runs, agentSessionFromRow(row.SessionID, row.WorkspaceID, row.AgentID, row.Harness, row.Model, row.ProviderSessionID, row.ProviderRunID, row.ProviderThreadID, row.Cwd, row.FolderScopeJson, row.Status, row.Usage, row.SourceSessionID, row.SourceAgentID, row.PromptHash, row.ContextPayloadIdsJson, row.PermissionMode, row.SandboxMode, row.ToolScopeJson, row.ProviderMetadataJson))
	}
	return runs, nil
}

func (s *Store) AppendRunLogMessage(ctx context.Context, msg RunLogMessage) error {
	if strings.TrimSpace(msg.MessageID) == "" || strings.TrimSpace(msg.SessionID) == "" || msg.Sequence <= 0 || strings.TrimSpace(msg.Role) == "" {
		return ErrInvalidInput
	}
	run, err := s.sqlc.GetHarnessSession(ctx, dbsqlc.GetHarnessSessionParams{SessionID: msg.SessionID})
	if err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	status := strings.TrimSpace(msg.Status)
	if status == "" {
		status = "completed"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	qtx := s.sqlc.WithTx(tx)
	msg.WorkspaceID = run.WorkspaceID
	msg.AgentID = run.AgentID
	msg.Status = status
	if err := appendRunLogMessageTx(ctx, qtx, msg); err != nil {
		return err
	}
	return tx.Commit()
}

func appendRunLogMessageTx(ctx context.Context, qtx *dbsqlc.Queries, msg RunLogMessage) error {
	if err := qtx.AppendRunLogMessage(ctx, dbsqlc.AppendRunLogMessageParams{MessageID: msg.MessageID, WorkspaceID: msg.WorkspaceID, SessionID: msg.SessionID, AgentID: msg.AgentID, Sequence: int64(msg.Sequence), Role: msg.Role, Status: msg.Status, ProviderMessageID: msg.ProviderMessageID, ProviderItemID: msg.ProviderItemID, ProviderTurnID: msg.ProviderTurnID, ProviderResponseID: msg.ProviderResponseID, ProviderCallID: msg.ProviderCallID, ProviderChannel: msg.ProviderChannel, ProviderKind: msg.ProviderKind, RawMetadataJson: defaultJSON(msg.RawMetadataJSON, "{}")}); err != nil {
		return err
	}
	for _, part := range msg.Parts {
		if strings.TrimSpace(part.PartID) == "" || part.Sequence <= 0 || strings.TrimSpace(part.Kind) == "" {
			return ErrInvalidInput
		}
		if err := qtx.AppendRunLogMessagePart(ctx, dbsqlc.AppendRunLogMessagePartParams{PartID: part.PartID, MessageID: msg.MessageID, Sequence: int64(part.Sequence), Kind: part.Kind, Text: part.Text, MimeType: part.MimeType, Uri: part.URI, Name: part.Name, ToolName: part.ToolName, ToolCallID: part.ToolCallID, RawJson: defaultRawJSON(part.RawJSON)}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) TailRunLogMessages(ctx context.Context, sessionID string, count int) ([]RunLogMessage, error) {
	if strings.TrimSpace(sessionID) == "" || count <= 0 {
		return nil, ErrInvalidInput
	}
	if _, err := s.sqlc.GetHarnessSession(ctx, dbsqlc.GetHarnessSessionParams{SessionID: sessionID}); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	rows, err := s.sqlc.TailRunLogMessages(ctx, dbsqlc.TailRunLogMessagesParams{SessionID: sessionID, Limit: int64(count)})
	if err != nil {
		return nil, err
	}
	messages := make([]RunLogMessage, 0, len(rows))
	for _, row := range rows {
		msg := RunLogMessage{MessageID: row.MessageID, WorkspaceID: row.WorkspaceID, SessionID: row.SessionID, AgentID: row.AgentID, Sequence: int(row.Sequence), Role: row.Role, Status: row.Status, ProviderMessageID: row.ProviderMessageID, ProviderItemID: row.ProviderItemID, ProviderTurnID: row.ProviderTurnID, ProviderResponseID: row.ProviderResponseID, ProviderCallID: row.ProviderCallID, ProviderChannel: row.ProviderChannel, ProviderKind: row.ProviderKind, RawMetadataJSON: row.RawMetadataJson}
		parts, err := s.messageParts(ctx, msg.MessageID)
		if err != nil {
			return nil, err
		}
		msg.Parts = parts
		messages = append(messages, msg)
	}
	return messages, nil
}

func (s *Store) ListRunLogMessages(ctx context.Context, sessionID string, afterSequence, limit int) ([]RunLogMessage, error) {
	if strings.TrimSpace(sessionID) == "" || afterSequence < 0 || limit <= 0 {
		return nil, ErrInvalidInput
	}
	if _, err := s.sqlc.GetHarnessSession(ctx, dbsqlc.GetHarnessSessionParams{SessionID: sessionID}); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	rows, err := s.sqlc.ListRunLogMessages(ctx, dbsqlc.ListRunLogMessagesParams{SessionID: sessionID, Sequence: int64(afterSequence), Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	messages := make([]RunLogMessage, 0, len(rows))
	for _, row := range rows {
		msg := RunLogMessage{MessageID: row.MessageID, WorkspaceID: row.WorkspaceID, SessionID: row.SessionID, AgentID: row.AgentID, Sequence: int(row.Sequence), Role: row.Role, Status: row.Status, ProviderMessageID: row.ProviderMessageID, ProviderItemID: row.ProviderItemID, ProviderTurnID: row.ProviderTurnID, ProviderResponseID: row.ProviderResponseID, ProviderCallID: row.ProviderCallID, ProviderChannel: row.ProviderChannel, ProviderKind: row.ProviderKind, RawMetadataJSON: row.RawMetadataJson}
		parts, err := s.messageParts(ctx, msg.MessageID)
		if err != nil {
			return nil, err
		}
		msg.Parts = parts
		messages = append(messages, msg)
	}
	return messages, nil
}

func (s *Store) messageParts(ctx context.Context, messageID string) ([]RunLogMessagePart, error) {
	rows, err := s.sqlc.ListRunLogMessageParts(ctx, dbsqlc.ListRunLogMessagePartsParams{MessageID: messageID})
	if err != nil {
		return nil, err
	}
	parts := make([]RunLogMessagePart, 0, len(rows))
	for _, row := range rows {
		parts = append(parts, RunLogMessagePart{PartID: row.PartID, Sequence: int(row.Sequence), Kind: row.Kind, Text: row.Text, MimeType: row.MimeType, URI: row.Uri, Name: row.Name, ToolName: row.ToolName, ToolCallID: row.ToolCallID, RawJSON: row.RawJson})
	}
	return parts, nil
}

func (s *Store) CreateContextExcerptFromTail(ctx context.Context, params CreateContextExcerptFromTailParams) (ContextExcerpt, error) {
	if strings.TrimSpace(params.ContextExcerptID) == "" || params.Count <= 0 {
		return ContextExcerpt{}, ErrInvalidInput
	}
	run, err := s.sqlc.GetHarnessSession(ctx, dbsqlc.GetHarnessSessionParams{SessionID: params.SourceSessionID})
	if err != nil {
		if err == sql.ErrNoRows {
			return ContextExcerpt{}, ErrNotFound
		}
		return ContextExcerpt{}, err
	}
	messages, err := s.TailRunLogMessages(ctx, params.SourceSessionID, params.Count)
	if err != nil {
		return ContextExcerpt{}, err
	}
	selector, _ := json.Marshal(map[string]any{"mode": "last_n", "count": params.Count})
	return s.createContextExcerptFromMessages(ctx, contextExcerptCreateSpec{ContextExcerptID: params.ContextExcerptID, SourceSessionID: params.SourceSessionID, TargetAgentID: params.TargetAgentID, SelectorType: "last_n", SelectorJSON: string(selector), AppendedMessage: params.AppendedMessage}, run, messages)
}

func (s *Store) CreateContextExcerptFromRange(ctx context.Context, params CreateContextExcerptFromRangeParams) (ContextExcerpt, error) {
	if strings.TrimSpace(params.ContextExcerptID) == "" || strings.TrimSpace(params.SourceSessionID) == "" || params.StartSequence <= 0 || params.EndSequence < params.StartSequence {
		return ContextExcerpt{}, ErrInvalidInput
	}
	run, messages, err := s.messagesForExcerptSelector(ctx, params.SourceSessionID)
	if err != nil {
		return ContextExcerpt{}, err
	}
	selected := make([]RunLogMessage, 0, params.EndSequence-params.StartSequence+1)
	for _, msg := range messages {
		if msg.Sequence >= params.StartSequence && msg.Sequence <= params.EndSequence {
			selected = append(selected, msg)
		}
	}
	selector, _ := json.Marshal(map[string]any{"mode": "range", "start_sequence": params.StartSequence, "end_sequence": params.EndSequence})
	return s.createContextExcerptFromMessages(ctx, contextExcerptCreateSpec{ContextExcerptID: params.ContextExcerptID, SourceSessionID: params.SourceSessionID, TargetAgentID: params.TargetAgentID, SelectorType: "range", SelectorJSON: string(selector), AppendedMessage: params.AppendedMessage}, run, selected)
}

func (s *Store) CreateContextExcerptFromExplicitIDs(ctx context.Context, params CreateContextExcerptFromExplicitIDsParams) (ContextExcerpt, error) {
	if strings.TrimSpace(params.ContextExcerptID) == "" || strings.TrimSpace(params.SourceSessionID) == "" || len(params.MessageIDs) == 0 {
		return ContextExcerpt{}, ErrInvalidInput
	}
	run, messages, err := s.messagesForExcerptSelector(ctx, params.SourceSessionID)
	if err != nil {
		return ContextExcerpt{}, err
	}
	byID := make(map[string]RunLogMessage, len(messages))
	for _, msg := range messages {
		byID[msg.MessageID] = msg
	}
	selected := make([]RunLogMessage, 0, len(params.MessageIDs))
	messageIDs := make([]string, 0, len(params.MessageIDs))
	for _, rawID := range params.MessageIDs {
		messageID := strings.TrimSpace(rawID)
		if messageID == "" {
			return ContextExcerpt{}, ErrInvalidInput
		}
		msg, ok := byID[messageID]
		if !ok {
			return ContextExcerpt{}, ErrNotFound
		}
		messageIDs = append(messageIDs, messageID)
		selected = append(selected, msg)
	}
	selector, _ := json.Marshal(map[string]any{"mode": "explicit_ids", "message_ids": messageIDs})
	return s.createContextExcerptFromMessages(ctx, contextExcerptCreateSpec{ContextExcerptID: params.ContextExcerptID, SourceSessionID: params.SourceSessionID, TargetAgentID: params.TargetAgentID, SelectorType: "explicit_ids", SelectorJSON: string(selector), AppendedMessage: params.AppendedMessage}, run, selected)
}

type contextExcerptCreateSpec struct {
	ContextExcerptID string
	SourceSessionID  string
	TargetAgentID    string
	SelectorType     string
	SelectorJSON     string
	AppendedMessage  string
}

func (s *Store) messagesForExcerptSelector(ctx context.Context, sourceSessionID string) (dbsqlc.GetHarnessSessionRow, []RunLogMessage, error) {
	run, err := s.sqlc.GetHarnessSession(ctx, dbsqlc.GetHarnessSessionParams{SessionID: sourceSessionID})
	if err != nil {
		if err == sql.ErrNoRows {
			return dbsqlc.GetHarnessSessionRow{}, nil, ErrNotFound
		}
		return dbsqlc.GetHarnessSessionRow{}, nil, err
	}
	messages, err := s.ListRunLogMessages(ctx, sourceSessionID, 0, 1000000)
	return run, messages, err
}

func (s *Store) createContextExcerptFromMessages(ctx context.Context, spec contextExcerptCreateSpec, run dbsqlc.GetHarnessSessionRow, messages []RunLogMessage) (ContextExcerpt, error) {
	items := make([]ContextExcerptItem, 0, len(messages))
	h := sha256.New()
	for i, msg := range messages {
		text := messageText(msg)
		parts := copyMessageParts(msg.Parts)
		partsJSON, _ := json.Marshal(parts)
		_, _ = fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00", msg.MessageID, msg.Role, text, string(partsJSON))
		items = append(items, ContextExcerptItem{Sequence: i + 1, SourceMessageID: msg.MessageID, CopiedRole: msg.Role, CopiedText: text, CopiedParts: parts})
	}
	_, _ = fmt.Fprintf(h, "workspace:%s\x00source_run:%s\x00source_agent:%s\x00target_agent:%s\x00selector:%s\x00selector_json:%s\x00visibility:visible_context\x00appended:%s\x00", run.WorkspaceID, spec.SourceSessionID, run.AgentID, spec.TargetAgentID, spec.SelectorType, spec.SelectorJSON, spec.AppendedMessage)
	contentHash := "sha256:" + hex.EncodeToString(h.Sum(nil))
	excerpt := ContextExcerpt{ContextExcerptID: spec.ContextExcerptID, WorkspaceID: run.WorkspaceID, SourceSessionID: spec.SourceSessionID, SourceAgentID: run.AgentID, TargetAgentID: spec.TargetAgentID, SelectorType: spec.SelectorType, SelectorJSON: spec.SelectorJSON, Visibility: "visible_context", AppendedMessage: spec.AppendedMessage, ContentHash: contentHash, Items: items}
	if err := s.withImmediateQueries(ctx, func(txCtx context.Context, qtx *dbsqlc.Queries) error {
		if err := qtx.CreateContextExcerpt(ctx, dbsqlc.CreateContextExcerptParams{ContextExcerptID: spec.ContextExcerptID, WorkspaceID: run.WorkspaceID, SourceSessionID: spec.SourceSessionID, SourceAgentID: run.AgentID, TargetAgentID: spec.TargetAgentID, SelectorType: spec.SelectorType, SelectorJson: spec.SelectorJSON, AppendedMessage: spec.AppendedMessage, ContentHash: contentHash}); err != nil {
			return err
		}
		for _, item := range items {
			partsJSON, _ := json.Marshal(item.CopiedParts)
			if err := qtx.CreateContextExcerptItem(ctx, dbsqlc.CreateContextExcerptItemParams{ContextExcerptID: spec.ContextExcerptID, Sequence: int64(item.Sequence), SourceMessageID: item.SourceMessageID, SourceSessionID: spec.SourceSessionID, SourceAgentID: run.AgentID, CopiedRole: item.CopiedRole, CopiedText: item.CopiedText, CopiedPartsJson: string(partsJSON)}); err != nil {
				return err
			}
		}
		event, err := contextExcerptCreatedWorkspaceEvent(excerpt)
		if err != nil {
			return err
		}
		return appendCoordinatedWorkspaceEventWithQueries(ctx, qtx, &event)
	}); err != nil {
		return ContextExcerpt{}, err
	}
	return excerpt, nil
}

func (s *Store) SendAgentMessage(ctx context.Context, params AgentMessageSendParams) (AgentMessage, error) {
	s.agentMessageMu.Lock()
	defer s.agentMessageMu.Unlock()

	if strings.TrimSpace(params.AgentMessageID) == "" || strings.TrimSpace(params.SourceSessionID) == "" || strings.TrimSpace(params.Body) == "" {
		return AgentMessage{}, ErrInvalidInput
	}
	targetSessionID := strings.TrimSpace(params.TargetSessionID)
	if targetSessionID == "" {
		targetSessionID = strings.TrimSpace(params.StartSessionID)
	}
	targetAgentID := strings.TrimSpace(params.TargetAgentID)
	if targetAgentID == "" && targetSessionID == "" {
		return AgentMessage{}, ErrInvalidInput
	}
	// BEGIN IMMEDIATE (withImmediateQueries) rather than a deferred
	// transaction: the workspace event sequence is a MAX+1 read that must not
	// race concurrent appenders.
	var message AgentMessage
	if err := s.withImmediateQueries(ctx, func(txCtx context.Context, qtx *dbsqlc.Queries) error {
		sent, err := sendAgentMessageWithQueries(ctx, qtx, params, targetSessionID, targetAgentID)
		if err != nil {
			return err
		}
		message = sent
		return nil
	}); err != nil {
		return AgentMessage{}, err
	}
	return message, nil
}

func sendAgentMessageWithQueries(ctx context.Context, qtx *dbsqlc.Queries, params AgentMessageSendParams, targetSessionID, targetAgentID string) (AgentMessage, error) {
	source, err := qtx.GetHarnessSession(ctx, dbsqlc.GetHarnessSessionParams{SessionID: params.SourceSessionID})
	if err != nil {
		if err == sql.ErrNoRows {
			return AgentMessage{}, ErrNotFound
		}
		return AgentMessage{}, err
	}
	if targetAgentID == "" {
		targetRun, err := qtx.GetHarnessSession(ctx, dbsqlc.GetHarnessSessionParams{SessionID: targetSessionID})
		if err != nil {
			if err == sql.ErrNoRows {
				return AgentMessage{}, ErrNotFound
			}
			return AgentMessage{}, err
		}
		if targetRun.WorkspaceID != source.WorkspaceID {
			return AgentMessage{}, ErrInvalidInput
		}
		targetAgentID = targetRun.AgentID
	}
	targetAgent, err := qtx.GetHarnessSessionConfig(ctx, dbsqlc.GetHarnessSessionConfigParams{AgentID: targetAgentID})
	if err != nil {
		if err == sql.ErrNoRows {
			return AgentMessage{}, ErrNotFound
		}
		return AgentMessage{}, err
	}
	if targetAgent.WorkspaceID != nil && *targetAgent.WorkspaceID != source.WorkspaceID {
		return AgentMessage{}, ErrInvalidInput
	}
	if targetSessionID == "" {
		return AgentMessage{}, ErrInvalidInput
	}
	excerpts := make([]ContextExcerpt, 0, len(params.ContextExcerptIDs))
	for _, contextExcerptID := range params.ContextExcerptIDs {
		excerpt, err := getContextExcerptWithQueries(ctx, qtx, strings.TrimSpace(contextExcerptID))
		if err != nil {
			return AgentMessage{}, err
		}
		if excerpt.WorkspaceID != source.WorkspaceID || excerpt.TargetAgentID != "" && excerpt.TargetAgentID != targetAgent.AgentID {
			return AgentMessage{}, ErrInvalidInput
		}
		if strings.TrimSpace(excerpt.TargetSessionID) != "" && excerpt.TargetSessionID != targetSessionID {
			return AgentMessage{}, ErrInvalidInput
		}
		excerpts = append(excerpts, excerpt)
	}
	if strings.TrimSpace(params.TargetSessionID) != "" {
		targetRun, err := qtx.GetHarnessSession(ctx, dbsqlc.GetHarnessSessionParams{SessionID: targetSessionID})
		if err != nil {
			if err == sql.ErrNoRows {
				return AgentMessage{}, ErrNotFound
			}
			return AgentMessage{}, err
		}
		if targetRun.WorkspaceID != source.WorkspaceID || targetRun.AgentID != targetAgent.AgentID {
			return AgentMessage{}, ErrInvalidInput
		}
	}
	if strings.TrimSpace(params.TargetSessionID) == "" {
		if err := qtx.CreateHarnessSession(ctx, createHarnessSessionParams(HarnessSession{SessionID: targetSessionID, WorkspaceID: source.WorkspaceID, AgentID: targetAgent.AgentID, Harness: targetAgent.Harness, Model: targetAgent.Model, Status: "waiting"}, HarnessSessionUsageSticky)); err != nil {
			return AgentMessage{}, err
		}
	}
	if err := qtx.CreateAgentMessage(ctx, dbsqlc.CreateAgentMessageParams{AgentMessageID: params.AgentMessageID, WorkspaceID: source.WorkspaceID, SourceAgentID: source.AgentID, SourceSessionID: source.SessionID, TargetAgentID: targetAgent.AgentID, TargetSessionID: targetSessionID, Body: params.Body, Status: "delivered", DeliveredSessionID: targetSessionID}); err != nil {
		return AgentMessage{}, err
	}
	for i, contextExcerptID := range params.ContextExcerptIDs {
		trimmedContextExcerptID := strings.TrimSpace(contextExcerptID)
		if err := qtx.CreateAgentMessageContextExcerpt(ctx, dbsqlc.CreateAgentMessageContextExcerptParams{AgentMessageID: params.AgentMessageID, ContextExcerptID: trimmedContextExcerptID, Sequence: int64(i + 1)}); err != nil {
			return AgentMessage{}, err
		}
	}
	nextSequence, err := qtx.NextRunLogMessageSequence(ctx, dbsqlc.NextRunLogMessageSequenceParams{SessionID: targetSessionID})
	if err != nil {
		return AgentMessage{}, err
	}
	for excerptIndex, excerpt := range excerpts {
		for _, item := range excerpt.Items {
			messageID := fmt.Sprintf("%s-excerpt-%d-%d", params.AgentMessageID, excerptIndex+1, item.Sequence)
			if err := appendRunLogMessageTx(ctx, qtx, RunLogMessage{MessageID: messageID, SessionID: targetSessionID, WorkspaceID: source.WorkspaceID, AgentID: targetAgent.AgentID, Sequence: int(nextSequence), Role: item.CopiedRole, Status: "completed", Parts: deliveredExcerptParts(messageID, item.CopiedParts)}); err != nil {
				return AgentMessage{}, err
			}
			nextSequence++
		}
		if strings.TrimSpace(excerpt.AppendedMessage) != "" {
			messageID := fmt.Sprintf("%s-excerpt-%d-appended", params.AgentMessageID, excerptIndex+1)
			if err := appendRunLogMessageTx(ctx, qtx, RunLogMessage{MessageID: messageID, SessionID: targetSessionID, WorkspaceID: source.WorkspaceID, AgentID: targetAgent.AgentID, Sequence: int(nextSequence), Role: "user", Status: "completed", Parts: []RunLogMessagePart{{PartID: messageID + "-part-1", Sequence: 1, Kind: "text", Text: excerpt.AppendedMessage}}}); err != nil {
				return AgentMessage{}, err
			}
			nextSequence++
		}
	}
	if err := appendRunLogMessageTx(ctx, qtx, RunLogMessage{MessageID: params.AgentMessageID + "-message", SessionID: targetSessionID, WorkspaceID: source.WorkspaceID, AgentID: targetAgent.AgentID, Sequence: int(nextSequence), Role: "user", Status: "completed", Parts: []RunLogMessagePart{{PartID: params.AgentMessageID + "-part-1", Sequence: 1, Kind: "text", Text: params.Body}}}); err != nil {
		return AgentMessage{}, err
	}
	event := defaultAgentMessageWorkspaceEvent(params, source.WorkspaceID, source.SessionID, source.AgentID, targetSessionID)
	if params.WorkspaceEvent != nil {
		event = *params.WorkspaceEvent
		if strings.TrimSpace(event.EventType) == "" {
			event.EventType = "message.sent"
		}
		if strings.TrimSpace(event.SubjectType) == "" {
			event.SubjectType = "agent_message"
		}
		if strings.TrimSpace(event.ProducerType) == "" {
			event.ProducerType = "session"
		}
		if strings.TrimSpace(event.ProducerID) == "" {
			event.ProducerID = source.SessionID
		}
		if strings.TrimSpace(event.PayloadJSON) == "" || strings.TrimSpace(event.PayloadJSON) == "{}" {
			event.PayloadJSON = defaultAgentMessageWorkspaceEventPayload(params, source.SessionID, source.AgentID, targetSessionID)
		}
		if strings.TrimSpace(event.PayloadRefJSON) == "" || strings.TrimSpace(event.PayloadRefJSON) == "{}" {
			event.PayloadRefJSON = daemonLocalPayloadRef("agent_message", params.AgentMessageID)
		}
	}
	if strings.TrimSpace(event.WorkspaceID) == "" {
		event.WorkspaceID = source.WorkspaceID
	} else if strings.TrimSpace(event.WorkspaceID) != source.WorkspaceID {
		return AgentMessage{}, ErrInvalidInput
	}
	if strings.TrimSpace(event.SubjectID) == "" {
		event.SubjectID = params.AgentMessageID
	}
	if strings.TrimSpace(event.CorrelationID) == "" {
		event.CorrelationID = targetSessionID
	}
	prepared, err := prepareCoordinatedWorkspaceEvent(event)
	if err != nil {
		return AgentMessage{}, err
	}
	if err := appendCoordinatedWorkspaceEventWithQueries(ctx, qtx, &prepared); err != nil {
		return AgentMessage{}, err
	}
	return AgentMessage{AgentMessageID: params.AgentMessageID, WorkspaceID: source.WorkspaceID, SourceAgentID: source.AgentID, SourceSessionID: source.SessionID, TargetAgentID: targetAgent.AgentID, TargetSessionID: targetSessionID, Body: params.Body, Status: "delivered", DeliveredSessionID: targetSessionID, ContextExcerptIDs: params.ContextExcerptIDs}, nil
}

func (s *Store) ListContextExcerpts(ctx context.Context, workspaceID string) ([]ContextExcerpt, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, ErrInvalidInput
	}
	rows, err := s.sqlc.ListContextExcerptsByWorkspace(ctx, dbsqlc.ListContextExcerptsByWorkspaceParams{WorkspaceID: workspaceID})
	if err != nil {
		return nil, err
	}
	excerpts := make([]ContextExcerpt, 0, len(rows))
	for _, row := range rows {
		excerpts = append(excerpts, ContextExcerpt{ContextExcerptID: row.ContextExcerptID, WorkspaceID: row.WorkspaceID, SourceSessionID: row.SourceSessionID, SourceAgentID: row.SourceAgentID, TargetAgentID: row.TargetAgentID, TargetSessionID: row.TargetSessionID, SelectorType: row.SelectorType, SelectorJSON: row.SelectorJson, Visibility: row.Visibility, AppendedMessage: row.AppendedMessage, ContentHash: row.ContentHash})
	}
	for i := range excerpts {
		items, err := s.sqlc.ListContextExcerptItems(ctx, dbsqlc.ListContextExcerptItemsParams{ContextExcerptID: excerpts[i].ContextExcerptID})
		if err != nil {
			return nil, err
		}
		for _, row := range items {
			parts, err := decodeCopiedParts(row.CopiedPartsJson)
			if err != nil {
				return nil, err
			}
			excerpts[i].Items = append(excerpts[i].Items, ContextExcerptItem{Sequence: int(row.Sequence), SourceMessageID: row.SourceMessageID, CopiedRole: row.CopiedRole, CopiedText: row.CopiedText, CopiedParts: parts})
		}
	}
	return excerpts, nil
}

func (s *Store) ListAgentMessages(ctx context.Context, workspaceID string) ([]AgentMessage, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, ErrInvalidInput
	}
	rows, err := s.sqlc.ListAgentMessagesByWorkspace(ctx, dbsqlc.ListAgentMessagesByWorkspaceParams{WorkspaceID: workspaceID})
	if err != nil {
		return nil, err
	}
	messages := make([]AgentMessage, 0, len(rows))
	for _, row := range rows {
		messages = append(messages, AgentMessage{AgentMessageID: row.AgentMessageID, WorkspaceID: row.WorkspaceID, SourceAgentID: row.SourceAgentID, SourceSessionID: row.SourceSessionID, TargetAgentID: row.TargetAgentID, TargetSessionID: row.TargetSessionID, Body: row.Body, Status: row.Status, DeliveredSessionID: row.DeliveredSessionID})
	}
	for i := range messages {
		excerptIDs, err := s.sqlc.ListAgentMessageContextExcerptIDs(ctx, dbsqlc.ListAgentMessageContextExcerptIDsParams{AgentMessageID: messages[i].AgentMessageID})
		if err != nil {
			return nil, err
		}
		messages[i].ContextExcerptIDs = append(messages[i].ContextExcerptIDs, excerptIDs...)
	}
	return messages, nil
}

func (s *Store) GetContextExcerpt(ctx context.Context, contextExcerptID string) (ContextExcerpt, error) {
	return getContextExcerptWithQueries(ctx, s.sqlc, contextExcerptID)
}

func getContextExcerptWithQueries(ctx context.Context, queries *dbsqlc.Queries, contextExcerptID string) (ContextExcerpt, error) {
	row, err := queries.GetContextExcerpt(ctx, dbsqlc.GetContextExcerptParams{ContextExcerptID: contextExcerptID})
	if err != nil {
		if err == sql.ErrNoRows {
			return ContextExcerpt{}, ErrNotFound
		}
		return ContextExcerpt{}, err
	}
	excerpt := ContextExcerpt{ContextExcerptID: row.ContextExcerptID, WorkspaceID: row.WorkspaceID, SourceSessionID: row.SourceSessionID, SourceAgentID: row.SourceAgentID, TargetAgentID: row.TargetAgentID, TargetSessionID: row.TargetSessionID, SelectorType: row.SelectorType, SelectorJSON: row.SelectorJson, Visibility: row.Visibility, AppendedMessage: row.AppendedMessage, ContentHash: row.ContentHash}
	rows, err := queries.ListContextExcerptItems(ctx, dbsqlc.ListContextExcerptItemsParams{ContextExcerptID: contextExcerptID})
	if err != nil {
		return ContextExcerpt{}, err
	}
	for _, row := range rows {
		parts, err := decodeCopiedParts(row.CopiedPartsJson)
		if err != nil {
			return ContextExcerpt{}, err
		}
		excerpt.Items = append(excerpt.Items, ContextExcerptItem{Sequence: int(row.Sequence), SourceMessageID: row.SourceMessageID, CopiedRole: row.CopiedRole, CopiedText: row.CopiedText, CopiedParts: parts})
	}
	return excerpt, nil
}

func copyMessageParts(parts []RunLogMessagePart) []RunLogMessagePart {
	copied := make([]RunLogMessagePart, 0, len(parts))
	for _, part := range parts {
		copied = append(copied, RunLogMessagePart{Sequence: part.Sequence, Kind: part.Kind, Text: part.Text, MimeType: part.MimeType, URI: part.URI, Name: part.Name, ToolName: part.ToolName, ToolCallID: part.ToolCallID, RawJSON: defaultRawJSON(part.RawJSON)})
	}
	return copied
}

func deliveredExcerptParts(messageID string, copied []RunLogMessagePart) []RunLogMessagePart {
	delivered := make([]RunLogMessagePart, 0, len(copied))
	for i, part := range copied {
		sequence := part.Sequence
		if sequence <= 0 {
			sequence = i + 1
		}
		delivered = append(delivered, RunLogMessagePart{PartID: fmt.Sprintf("%s-part-%d", messageID, sequence), Sequence: sequence, Kind: part.Kind, Text: part.Text, MimeType: part.MimeType, URI: part.URI, Name: part.Name, ToolName: part.ToolName, ToolCallID: part.ToolCallID, RawJSON: defaultRawJSON(part.RawJSON)})
	}
	return delivered
}

func defaultRawJSON(raw string) string {
	return defaultJSON(raw, "{}")
}

func decodeCopiedParts(raw string) ([]RunLogMessagePart, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var parts []RunLogMessagePart
	if err := json.Unmarshal([]byte(raw), &parts); err != nil {
		return nil, err
	}
	return parts, nil
}

func messageText(msg RunLogMessage) string {
	parts := make([]string, 0, len(msg.Parts))
	for _, part := range msg.Parts {
		if part.Kind == "text" && strings.TrimSpace(part.Text) != "" {
			parts = append(parts, part.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func (s *Store) MarkRunningHarnessSessionsLost(ctx context.Context) error {
	if err := s.sqlcQueries().MarkRunningHarnessSessionsLost(ctx); err != nil {
		return fmt.Errorf("mark running harness sessions lost: %w", err)
	}

	return nil
}
