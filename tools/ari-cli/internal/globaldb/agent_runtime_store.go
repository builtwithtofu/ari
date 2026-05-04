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

type AgentSessionConfig struct {
	AgentID     string
	WorkspaceID string
	Name        string
	Harness     string
	Model       string
	Prompt      string
}

type AgentSession struct {
	SessionID             string
	RunID                 string
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
}

func (s *Store) CreateAgentSessionConfig(ctx context.Context, agent AgentSessionConfig) error {
	if strings.TrimSpace(agent.AgentID) == "" || strings.TrimSpace(agent.Name) == "" {
		return ErrInvalidInput
	}
	var workspaceID *string
	if strings.TrimSpace(agent.WorkspaceID) != "" {
		if _, err := s.GetSession(ctx, strings.TrimSpace(agent.WorkspaceID)); err != nil {
			if errors.Is(err, ErrNotFound) {
				return ErrInvalidInput
			}
			return err
		}
		workspaceID = &agent.WorkspaceID
	}
	return s.sqlc.CreateAgentSessionConfig(ctx, dbsqlc.CreateAgentSessionConfigParams{AgentID: agent.AgentID, WorkspaceID: workspaceID, Name: agent.Name, Harness: agent.Harness, Model: agent.Model, Prompt: agent.Prompt})
}

func (s *Store) EnsureAgentSessionConfig(ctx context.Context, agent AgentSessionConfig) error {
	if strings.TrimSpace(agent.AgentID) == "" || strings.TrimSpace(agent.Name) == "" {
		return ErrInvalidInput
	}
	var workspaceID *string
	if strings.TrimSpace(agent.WorkspaceID) != "" {
		if _, err := s.GetSession(ctx, strings.TrimSpace(agent.WorkspaceID)); err != nil {
			if errors.Is(err, ErrNotFound) {
				return ErrInvalidInput
			}
			return err
		}
		workspaceID = &agent.WorkspaceID
	}
	return s.sqlc.EnsureAgentSessionConfig(ctx, dbsqlc.EnsureAgentSessionConfigParams{AgentID: agent.AgentID, WorkspaceID: workspaceID, Name: agent.Name, Harness: agent.Harness, Model: agent.Model, Prompt: agent.Prompt})
}

func (s *Store) GetAgentSessionConfig(ctx context.Context, agentID string) (AgentSessionConfig, error) {
	row, err := s.sqlc.GetAgentSessionConfig(ctx, dbsqlc.GetAgentSessionConfigParams{AgentID: strings.TrimSpace(agentID)})
	if err != nil {
		if err == sql.ErrNoRows {
			return AgentSessionConfig{}, ErrNotFound
		}
		return AgentSessionConfig{}, err
	}
	return agentSessionConfigFromRow(row.AgentID, row.WorkspaceID, row.Name, row.Harness, row.Model, row.Prompt), nil
}

func (s *Store) ListAgentSessionConfigs(ctx context.Context, workspaceID string) ([]AgentSessionConfig, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, ErrInvalidInput
	}
	rows, err := s.sqlc.ListAgentSessionConfigs(ctx, dbsqlc.ListAgentSessionConfigsParams{WorkspaceID: &workspaceID})
	if err != nil {
		return nil, err
	}
	agents := make([]AgentSessionConfig, 0, len(rows))
	for _, row := range rows {
		agents = append(agents, agentSessionConfigFromRow(row.AgentID, row.WorkspaceID, row.Name, row.Harness, row.Model, row.Prompt))
	}
	return agents, nil
}

func (s *Store) UpdateAgentSessionConfig(ctx context.Context, agent AgentSessionConfig) (AgentSessionConfig, error) {
	if strings.TrimSpace(agent.AgentID) == "" || strings.TrimSpace(agent.WorkspaceID) == "" || strings.TrimSpace(agent.Name) == "" {
		return AgentSessionConfig{}, ErrInvalidInput
	}
	before, err := s.GetAgentSessionConfig(ctx, agent.AgentID)
	if err != nil {
		return AgentSessionConfig{}, err
	}
	if before.WorkspaceID != agent.WorkspaceID {
		return AgentSessionConfig{}, ErrNotFound
	}
	if err := s.sqlc.UpdateAgentSessionConfig(ctx, dbsqlc.UpdateAgentSessionConfigParams{Name: agent.Name, Harness: agent.Harness, Model: agent.Model, Prompt: agent.Prompt, AgentID: agent.AgentID, WorkspaceID: &agent.WorkspaceID}); err != nil {
		return AgentSessionConfig{}, err
	}
	return s.GetAgentSessionConfig(ctx, agent.AgentID)
}

func (s *Store) DeleteAgentSessionConfig(ctx context.Context, agentID string) error {
	if strings.TrimSpace(agentID) == "" {
		return ErrInvalidInput
	}
	return s.sqlc.DeleteAgentSessionConfig(ctx, dbsqlc.DeleteAgentSessionConfigParams{AgentID: strings.TrimSpace(agentID)})
}

func (s *Store) CreateSessionFromAgentSessionConfig(ctx context.Context, sessionID, agentID, cwd string) (AgentSession, error) {
	agent, err := s.GetAgentSessionConfig(ctx, agentID)
	if err != nil {
		return AgentSession{}, err
	}
	if _, err := s.GetSession(ctx, agent.WorkspaceID); err != nil {
		return AgentSession{}, err
	}
	run := AgentSession{SessionID: strings.TrimSpace(sessionID), RunID: strings.TrimSpace(sessionID), WorkspaceID: agent.WorkspaceID, AgentID: agent.AgentID, Harness: agent.Harness, Model: agent.Model, CWD: strings.TrimSpace(cwd), Status: "waiting", Usage: "durable"}
	if err := s.CreateAgentSession(ctx, run); err != nil {
		return AgentSession{}, err
	}
	return run, nil
}

func agentSessionConfigFromRow(agentID string, workspaceID *string, name, harness, model, prompt string) AgentSessionConfig {
	workspace := ""
	if workspaceID != nil {
		workspace = *workspaceID
	}
	return AgentSessionConfig{AgentID: agentID, WorkspaceID: workspace, Name: name, Harness: harness, Model: model, Prompt: prompt}
}

func (s *Store) CreateAgentSession(ctx context.Context, run AgentSession) error {
	if strings.TrimSpace(run.SessionID) == "" {
		run.SessionID = strings.TrimSpace(run.RunID)
	}
	if strings.TrimSpace(run.SessionID) == "" || strings.TrimSpace(run.WorkspaceID) == "" || strings.TrimSpace(run.AgentID) == "" || strings.TrimSpace(run.Status) == "" {
		return ErrInvalidInput
	}
	agent, err := s.sqlc.GetAgentSessionConfig(ctx, dbsqlc.GetAgentSessionConfigParams{AgentID: strings.TrimSpace(run.AgentID)})
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
		usage = "durable"
	}
	return s.sqlc.CreateAgentSession(ctx, createAgentSessionParams(run, usage))
}

func createAgentSessionParams(run AgentSession, usage string) dbsqlc.CreateAgentSessionParams {
	return dbsqlc.CreateAgentSessionParams{SessionID: run.SessionID, WorkspaceID: run.WorkspaceID, AgentID: run.AgentID, Harness: run.Harness, Model: run.Model, ProviderSessionID: run.ProviderSessionID, ProviderRunID: run.ProviderRunID, ProviderThreadID: run.ProviderThreadID, Cwd: run.CWD, FolderScopeJson: defaultJSON(run.FolderScopeJSON, "[]"), Status: run.Status, Usage: usage, SourceSessionID: run.SourceSessionID, SourceAgentID: run.SourceAgentID, PromptHash: run.PromptHash, ContextPayloadIdsJson: defaultJSON(run.ContextPayloadIDsJSON, "[]"), PermissionMode: run.PermissionMode, SandboxMode: run.SandboxMode, ToolScopeJson: defaultJSON(run.ToolScopeJSON, "{}"), ProviderMetadataJson: defaultJSON(run.ProviderMetadataJSON, "{}")}
}

func defaultJSON(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func agentSessionFromRow(sessionID, workspaceID, agentID, harness, model, providerSessionID, providerRunID, providerThreadID, cwd, folderScopeJSON, status, usage, sourceSessionID, sourceAgentID, promptHash, contextPayloadIDsJSON, permissionMode, sandboxMode, toolScopeJSON, providerMetadataJSON string) AgentSession {
	return AgentSession{SessionID: sessionID, RunID: sessionID, WorkspaceID: workspaceID, AgentID: agentID, Harness: harness, Model: model, ProviderSessionID: providerSessionID, ProviderRunID: providerRunID, ProviderThreadID: providerThreadID, CWD: cwd, FolderScopeJSON: folderScopeJSON, Status: status, Usage: usage, SourceSessionID: sourceSessionID, SourceAgentID: sourceAgentID, PromptHash: promptHash, ContextPayloadIDsJSON: contextPayloadIDsJSON, PermissionMode: permissionMode, SandboxMode: sandboxMode, ToolScopeJSON: toolScopeJSON, ProviderMetadataJSON: providerMetadataJSON}
}

func (s *Store) UpdateAgentSessionStatus(ctx context.Context, sessionID, status string) error {
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(status) == "" {
		return ErrInvalidInput
	}
	result, err := s.db.ExecContext(ctx, `UPDATE agent_sessions SET status = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE session_id = ?`, strings.TrimSpace(status), strings.TrimSpace(sessionID))
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) GetAgentSession(ctx context.Context, sessionID string) (AgentSession, error) {
	row, err := s.sqlc.GetAgentSession(ctx, dbsqlc.GetAgentSessionParams{SessionID: strings.TrimSpace(sessionID)})
	if err != nil {
		if err == sql.ErrNoRows {
			return AgentSession{}, ErrNotFound
		}
		return AgentSession{}, err
	}
	return agentSessionFromRow(row.SessionID, row.WorkspaceID, row.AgentID, row.Harness, row.Model, row.ProviderSessionID, row.ProviderRunID, row.ProviderThreadID, row.Cwd, row.FolderScopeJson, row.Status, row.Usage, row.SourceSessionID, row.SourceAgentID, row.PromptHash, row.ContextPayloadIdsJson, row.PermissionMode, row.SandboxMode, row.ToolScopeJson, row.ProviderMetadataJson), nil
}

func (s *Store) ListAgentSessions(ctx context.Context, workspaceID string) ([]AgentSession, error) {
	rows, err := s.sqlc.ListAgentSessions(ctx, dbsqlc.ListAgentSessionsParams{WorkspaceID: workspaceID})
	if err != nil {
		return nil, err
	}
	runs := make([]AgentSession, 0, len(rows))
	for _, row := range rows {
		runs = append(runs, agentSessionFromRow(row.SessionID, row.WorkspaceID, row.AgentID, row.Harness, row.Model, row.ProviderSessionID, row.ProviderRunID, row.ProviderThreadID, row.Cwd, row.FolderScopeJson, row.Status, row.Usage, row.SourceSessionID, row.SourceAgentID, row.PromptHash, row.ContextPayloadIdsJson, row.PermissionMode, row.SandboxMode, row.ToolScopeJson, row.ProviderMetadataJson))
	}
	return runs, nil
}

func (s *Store) AppendRunLogMessage(ctx context.Context, msg RunLogMessage) error {
	if strings.TrimSpace(msg.MessageID) == "" || strings.TrimSpace(msg.SessionID) == "" || msg.Sequence <= 0 || strings.TrimSpace(msg.Role) == "" {
		return ErrInvalidInput
	}
	run, err := s.sqlc.GetAgentSession(ctx, dbsqlc.GetAgentSessionParams{SessionID: msg.SessionID})
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
	if _, err := s.sqlc.GetAgentSession(ctx, dbsqlc.GetAgentSessionParams{SessionID: sessionID}); err != nil {
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
	if _, err := s.sqlc.GetAgentSession(ctx, dbsqlc.GetAgentSessionParams{SessionID: sessionID}); err != nil {
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
	run, err := s.sqlc.GetAgentSession(ctx, dbsqlc.GetAgentSessionParams{SessionID: params.SourceSessionID})
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

func (s *Store) messagesForExcerptSelector(ctx context.Context, sourceSessionID string) (dbsqlc.GetAgentSessionRow, []RunLogMessage, error) {
	run, err := s.sqlc.GetAgentSession(ctx, dbsqlc.GetAgentSessionParams{SessionID: sourceSessionID})
	if err != nil {
		if err == sql.ErrNoRows {
			return dbsqlc.GetAgentSessionRow{}, nil, ErrNotFound
		}
		return dbsqlc.GetAgentSessionRow{}, nil, err
	}
	messages, err := s.ListRunLogMessages(ctx, sourceSessionID, 0, 1000000)
	return run, messages, err
}

func (s *Store) createContextExcerptFromMessages(ctx context.Context, spec contextExcerptCreateSpec, run dbsqlc.GetAgentSessionRow, messages []RunLogMessage) (ContextExcerpt, error) {
	items := make([]ContextExcerptItem, 0, len(messages))
	h := sha256.New()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ContextExcerpt{}, err
	}
	defer func() { _ = tx.Rollback() }()
	qtx := s.sqlc.WithTx(tx)
	for i, msg := range messages {
		text := messageText(msg)
		parts := copyMessageParts(msg.Parts)
		partsJSON, _ := json.Marshal(parts)
		_, _ = fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00", msg.MessageID, msg.Role, text, string(partsJSON))
		items = append(items, ContextExcerptItem{Sequence: i + 1, SourceMessageID: msg.MessageID, CopiedRole: msg.Role, CopiedText: text, CopiedParts: parts})
	}
	_, _ = fmt.Fprintf(h, "workspace:%s\x00source_run:%s\x00source_agent:%s\x00target_agent:%s\x00selector:%s\x00selector_json:%s\x00visibility:visible_context\x00appended:%s\x00", run.WorkspaceID, spec.SourceSessionID, run.AgentID, spec.TargetAgentID, spec.SelectorType, spec.SelectorJSON, spec.AppendedMessage)
	contentHash := "sha256:" + hex.EncodeToString(h.Sum(nil))
	if err := qtx.CreateContextExcerpt(ctx, dbsqlc.CreateContextExcerptParams{ContextExcerptID: spec.ContextExcerptID, WorkspaceID: run.WorkspaceID, SourceSessionID: spec.SourceSessionID, SourceAgentID: run.AgentID, TargetAgentID: spec.TargetAgentID, SelectorType: spec.SelectorType, SelectorJson: spec.SelectorJSON, AppendedMessage: spec.AppendedMessage, ContentHash: contentHash}); err != nil {
		return ContextExcerpt{}, err
	}
	for _, item := range items {
		partsJSON, _ := json.Marshal(item.CopiedParts)
		if err := qtx.CreateContextExcerptItem(ctx, dbsqlc.CreateContextExcerptItemParams{ContextExcerptID: spec.ContextExcerptID, Sequence: int64(item.Sequence), SourceMessageID: item.SourceMessageID, SourceSessionID: spec.SourceSessionID, SourceAgentID: run.AgentID, CopiedRole: item.CopiedRole, CopiedText: item.CopiedText, CopiedPartsJson: string(partsJSON)}); err != nil {
			return ContextExcerpt{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return ContextExcerpt{}, err
	}
	return ContextExcerpt{ContextExcerptID: spec.ContextExcerptID, WorkspaceID: run.WorkspaceID, SourceSessionID: spec.SourceSessionID, SourceAgentID: run.AgentID, TargetAgentID: spec.TargetAgentID, SelectorType: spec.SelectorType, SelectorJSON: spec.SelectorJSON, Visibility: "visible_context", AppendedMessage: spec.AppendedMessage, ContentHash: contentHash, Items: items}, nil
}

func (s *Store) SendAgentMessage(ctx context.Context, params AgentMessageSendParams) (AgentMessage, error) {
	s.agentMessageMu.Lock()
	defer s.agentMessageMu.Unlock()

	if strings.TrimSpace(params.AgentMessageID) == "" || strings.TrimSpace(params.SourceSessionID) == "" || strings.TrimSpace(params.TargetAgentID) == "" || strings.TrimSpace(params.Body) == "" {
		return AgentMessage{}, ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AgentMessage{}, err
	}
	defer func() { _ = tx.Rollback() }()
	qtx := s.sqlc.WithTx(tx)

	source, err := qtx.GetAgentSession(ctx, dbsqlc.GetAgentSessionParams{SessionID: params.SourceSessionID})
	if err != nil {
		if err == sql.ErrNoRows {
			return AgentMessage{}, ErrNotFound
		}
		return AgentMessage{}, err
	}
	targetAgent, err := qtx.GetAgentSessionConfig(ctx, dbsqlc.GetAgentSessionConfigParams{AgentID: params.TargetAgentID})
	if err != nil {
		if err == sql.ErrNoRows {
			return AgentMessage{}, ErrNotFound
		}
		return AgentMessage{}, err
	}
	if targetAgent.WorkspaceID != nil && *targetAgent.WorkspaceID != source.WorkspaceID {
		return AgentMessage{}, ErrInvalidInput
	}
	targetSessionID := strings.TrimSpace(params.TargetSessionID)
	if targetSessionID == "" {
		targetSessionID = strings.TrimSpace(params.StartSessionID)
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
		targetRun, err := qtx.GetAgentSession(ctx, dbsqlc.GetAgentSessionParams{SessionID: targetSessionID})
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
		if err := qtx.CreateAgentSession(ctx, createAgentSessionParams(AgentSession{SessionID: targetSessionID, WorkspaceID: source.WorkspaceID, AgentID: targetAgent.AgentID, Harness: targetAgent.Harness, Model: targetAgent.Model, Status: "waiting"}, "agent_message")); err != nil {
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
	if err := tx.Commit(); err != nil {
		return AgentMessage{}, err
	}
	return AgentMessage{AgentMessageID: params.AgentMessageID, WorkspaceID: source.WorkspaceID, SourceAgentID: source.AgentID, SourceSessionID: source.SessionID, TargetAgentID: targetAgent.AgentID, TargetSessionID: targetSessionID, Body: params.Body, Status: "delivered", DeliveredSessionID: targetSessionID, ContextExcerptIDs: params.ContextExcerptIDs}, nil
}

func (s *Store) ListContextExcerpts(ctx context.Context, workspaceID string) ([]ContextExcerpt, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, ErrInvalidInput
	}
	rows, err := s.db.QueryContext(ctx, `SELECT context_excerpt_id, workspace_id, source_session_id, source_agent_id, target_agent_id, target_session_id, selector_type, selector_json, visibility, appended_message, content_hash FROM context_excerpts WHERE workspace_id = ? ORDER BY created_at ASC, context_excerpt_id ASC`, workspaceID)
	if err != nil {
		return nil, err
	}
	excerpts := make([]ContextExcerpt, 0)
	for rows.Next() {
		var excerpt ContextExcerpt
		if err := rows.Scan(&excerpt.ContextExcerptID, &excerpt.WorkspaceID, &excerpt.SourceSessionID, &excerpt.SourceAgentID, &excerpt.TargetAgentID, &excerpt.TargetSessionID, &excerpt.SelectorType, &excerpt.SelectorJSON, &excerpt.Visibility, &excerpt.AppendedMessage, &excerpt.ContentHash); err != nil {
			_ = rows.Close()
			return nil, err
		}
		excerpts = append(excerpts, excerpt)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
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
	rows, err := s.db.QueryContext(ctx, `SELECT agent_message_id, workspace_id, source_agent_id, source_session_id, target_agent_id, target_session_id, body, status, delivered_session_id FROM agent_messages WHERE workspace_id = ? ORDER BY created_at ASC, agent_message_id ASC`, workspaceID)
	if err != nil {
		return nil, err
	}
	messages := make([]AgentMessage, 0)
	for rows.Next() {
		var dm AgentMessage
		if err := rows.Scan(&dm.AgentMessageID, &dm.WorkspaceID, &dm.SourceAgentID, &dm.SourceSessionID, &dm.TargetAgentID, &dm.TargetSessionID, &dm.Body, &dm.Status, &dm.DeliveredSessionID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		messages = append(messages, dm)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range messages {
		excerptRows, err := s.db.QueryContext(ctx, `SELECT context_excerpt_id FROM agent_message_context_excerpts WHERE agent_message_id = ? ORDER BY sequence ASC`, messages[i].AgentMessageID)
		if err != nil {
			return nil, err
		}
		for excerptRows.Next() {
			var contextExcerptID string
			if err := excerptRows.Scan(&contextExcerptID); err != nil {
				_ = excerptRows.Close()
				return nil, err
			}
			messages[i].ContextExcerptIDs = append(messages[i].ContextExcerptIDs, contextExcerptID)
		}
		if err := excerptRows.Close(); err != nil {
			return nil, err
		}
		if err := excerptRows.Err(); err != nil {
			return nil, err
		}
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
