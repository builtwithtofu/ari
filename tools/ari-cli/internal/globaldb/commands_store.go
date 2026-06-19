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

type Command struct {
	CommandID   string
	WorkspaceID string
	Command     string
	Args        string
	Status      string
	ExitCode    *int
	StartedAt   string
	FinishedAt  *string
}

type WorkspaceCommandDefinition struct {
	CommandID   string
	WorkspaceID string
	Name        string
	Command     string
	Args        string
	CreatedAt   string
	UpdatedAt   string
}

type CreateCommandParams struct {
	CommandID   string
	WorkspaceID string
	Command     string
	Args        string
	Status      string
	StartedAt   string
	ExitCode    *int
	FinishedAt  *string
}

type UpdateCommandStatusParams struct {
	WorkspaceID string
	CommandID   string
	Status      string
	ExitCode    *int
	FinishedAt  *string
}

type CreateWorkspaceCommandDefinitionParams struct {
	CommandID   string
	WorkspaceID string
	Name        string
	Command     string
	Args        string
}

func commandFromSQLC(row dbsqlc.Command) Command {
	return Command{CommandID: row.CommandID, WorkspaceID: row.WorkspaceID, Command: row.Command, Args: row.Args, Status: row.Status, ExitCode: intPtrFromInt64(row.ExitCode), StartedAt: row.StartedAt, FinishedAt: row.FinishedAt}
}

func workspaceCommandDefinitionFromSQLC(row dbsqlc.WorkspaceCommandDefinition) WorkspaceCommandDefinition {
	return WorkspaceCommandDefinition{CommandID: row.CommandID, WorkspaceID: row.WorkspaceID, Name: row.Name, Command: row.Command, Args: row.Args, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func commandWorkspaceEvent(command Command) (WorkspaceEvent, error) {
	payload := map[string]string{
		"command_id": command.CommandID,
		"command":    command.Command,
		"args":       command.Args,
		"status":     command.Status,
	}
	if command.ExitCode != nil {
		payload["exit_code"] = fmt.Sprintf("%d", *command.ExitCode)
	}
	if command.FinishedAt != nil {
		payload["finished_at"] = strings.TrimSpace(*command.FinishedAt)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return WorkspaceEvent{}, fmt.Errorf("marshal command workspace event payload for %q: %w", command.CommandID, err)
	}
	return prepareCoordinatedWorkspaceEvent(WorkspaceEvent{WorkspaceID: command.WorkspaceID, EventType: commandWorkspaceEventType(command), SubjectType: "command", SubjectID: command.CommandID, ProducerType: "daemon", ProducerID: "command", CorrelationID: command.CommandID, PayloadJSON: string(encoded), PayloadRefJSON: daemonLocalPayloadRef("command", command.CommandID), AttentionRequired: commandWorkspaceEventNeedsAttention(command)})
}

func commandWorkspaceEventType(command Command) string {
	switch strings.TrimSpace(command.Status) {
	case commandStatusRunning:
		return "command.started"
	case commandStatusStopped:
		return "command.stopped"
	case commandStatusLost:
		return "command.failed"
	case commandStatusExited:
		if command.ExitCode != nil && *command.ExitCode != 0 {
			return "command.failed"
		}
		return "command.completed"
	default:
		return "command.updated"
	}
}

func commandWorkspaceEventNeedsAttention(command Command) bool {
	if strings.TrimSpace(command.Status) == commandStatusLost {
		return true
	}
	return command.ExitCode != nil && *command.ExitCode != 0
}

func daemonLocalPayloadRef(kind, id string) string {
	encoded, _ := json.Marshal(map[string]string{"kind": strings.TrimSpace(kind), "id": strings.TrimSpace(id)})
	return string(encoded)
}

func (s *Store) CreateCommand(ctx context.Context, params CreateCommandParams) error {
	if params.CommandID = strings.TrimSpace(params.CommandID); params.CommandID == "" {
		return fmt.Errorf("%w: command id is required", ErrInvalidInput)
	}
	if params.WorkspaceID = strings.TrimSpace(params.WorkspaceID); params.WorkspaceID == "" {
		return fmt.Errorf("%w: workspace id is required", ErrInvalidInput)
	}
	if _, err := s.GetWorkspace(ctx, params.WorkspaceID); err != nil {
		return err
	}
	if params.Command = strings.TrimSpace(params.Command); params.Command == "" {
		return fmt.Errorf("%w: command is required", ErrInvalidInput)
	}
	if params.Args = strings.TrimSpace(params.Args); params.Args == "" {
		params.Args = "[]"
	}
	var decodedArgs any
	if err := json.Unmarshal([]byte(params.Args), &decodedArgs); err != nil {
		return fmt.Errorf("%w: command args must be a json array", ErrInvalidInput)
	}
	if _, ok := decodedArgs.([]any); !ok {
		return fmt.Errorf("%w: command args must be a json array", ErrInvalidInput)
	}
	if params.Status = strings.TrimSpace(params.Status); params.Status == "" {
		params.Status = commandStatusRunning
	}
	if !isValidCommandStatus(params.Status) {
		return fmt.Errorf("%w: invalid command status %q", ErrInvalidInput, params.Status)
	}
	if params.StartedAt = strings.TrimSpace(params.StartedAt); params.StartedAt == "" {
		params.StartedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}

	return s.withImmediateQueries(ctx, func(ctx context.Context, queries *dbsqlc.Queries) error {
		if err := queries.CreateCommand(ctx, dbsqlc.CreateCommandParams{CommandID: params.CommandID, WorkspaceID: params.WorkspaceID, Command: params.Command, Args: params.Args, Status: params.Status, ExitCode: optionalInt(params.ExitCode), StartedAt: params.StartedAt, FinishedAt: params.FinishedAt}); err != nil {
			return fmt.Errorf("create command %q: %w", params.CommandID, err)
		}
		command := Command{CommandID: params.CommandID, WorkspaceID: params.WorkspaceID, Command: params.Command, Args: params.Args, Status: params.Status, ExitCode: params.ExitCode, StartedAt: params.StartedAt, FinishedAt: params.FinishedAt}
		event, err := commandWorkspaceEvent(command)
		if err != nil {
			return err
		}
		return appendCoordinatedWorkspaceEventWithQueries(ctx, queries, &event)
	})
}

func (s *Store) GetCommand(ctx context.Context, workspaceID, commandID string) (*Command, error) {
	if workspaceID = strings.TrimSpace(workspaceID); workspaceID == "" {
		return nil, fmt.Errorf("%w: workspace id is required", ErrInvalidInput)
	}
	if commandID = strings.TrimSpace(commandID); commandID == "" {
		return nil, fmt.Errorf("%w: command id is required", ErrInvalidInput)
	}

	row, err := s.sqlcQueries().GetCommandByID(ctx, dbsqlc.GetCommandByIDParams{WorkspaceID: workspaceID, CommandID: commandID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: command id %q for workspace %q", ErrNotFound, commandID, workspaceID)
		}
		return nil, err
	}
	command := commandFromSQLC(row)
	return &command, nil
}

func (s *Store) ListCommands(ctx context.Context, workspaceID string) ([]Command, error) {
	if workspaceID = strings.TrimSpace(workspaceID); workspaceID == "" {
		return nil, fmt.Errorf("%w: workspace id is required", ErrInvalidInput)
	}

	rows, err := s.sqlcQueries().ListCommandsByWorkspace(ctx, dbsqlc.ListCommandsByWorkspaceParams{WorkspaceID: workspaceID})
	if err != nil {
		return nil, err
	}
	out := make([]Command, 0, len(rows))
	for _, row := range rows {
		out = append(out, commandFromSQLC(row))
	}
	return out, nil
}

func (s *Store) UpdateCommandStatus(ctx context.Context, params UpdateCommandStatusParams) error {
	if params.WorkspaceID = strings.TrimSpace(params.WorkspaceID); params.WorkspaceID == "" {
		return fmt.Errorf("%w: workspace id is required", ErrInvalidInput)
	}
	if params.CommandID = strings.TrimSpace(params.CommandID); params.CommandID == "" {
		return fmt.Errorf("%w: command id is required", ErrInvalidInput)
	}
	if params.Status = strings.TrimSpace(params.Status); params.Status == "" {
		return fmt.Errorf("%w: status is required", ErrInvalidInput)
	}
	if !isValidCommandStatus(params.Status) {
		return fmt.Errorf("%w: invalid command status %q", ErrInvalidInput, params.Status)
	}

	return s.withImmediateQueries(ctx, func(ctx context.Context, queries *dbsqlc.Queries) error {
		rowsAffected, err := queries.UpdateCommandStatus(ctx, dbsqlc.UpdateCommandStatusParams{Status: params.Status, ExitCode: optionalInt(params.ExitCode), FinishedAt: params.FinishedAt, WorkspaceID: params.WorkspaceID, CommandID: params.CommandID})
		if err != nil {
			return fmt.Errorf("update command status %q: %w", params.CommandID, err)
		}
		if rowsAffected == 0 {
			return fmt.Errorf("%w: command id %q for workspace %q", ErrNotFound, params.CommandID, params.WorkspaceID)
		}
		row, err := queries.GetCommandByID(ctx, dbsqlc.GetCommandByIDParams{WorkspaceID: params.WorkspaceID, CommandID: params.CommandID})
		if err != nil {
			return fmt.Errorf("get updated command %q: %w", params.CommandID, err)
		}
		event, err := commandWorkspaceEvent(commandFromSQLC(row))
		if err != nil {
			return err
		}
		return appendCoordinatedWorkspaceEventWithQueries(ctx, queries, &event)
	})
}

func (s *Store) MarkRunningCommandsLost(ctx context.Context) error {
	return s.withImmediateQueries(ctx, func(ctx context.Context, queries *dbsqlc.Queries) error {
		running, err := queries.ListRunningCommands(ctx)
		if err != nil {
			return fmt.Errorf("list running commands: %w", err)
		}
		if err := queries.MarkRunningCommandsLost(ctx); err != nil {
			return fmt.Errorf("mark running commands lost: %w", err)
		}
		for _, row := range running {
			command := commandFromSQLC(row)
			command.Status = commandStatusLost
			event, err := commandWorkspaceEvent(command)
			if err != nil {
				return err
			}
			if err := appendCoordinatedWorkspaceEventWithQueries(ctx, queries, &event); err != nil {
				return fmt.Errorf("append lost command workspace event %q: %w", command.CommandID, err)
			}
		}
		return nil
	})
}

func (s *Store) CreateWorkspaceCommandDefinition(ctx context.Context, params CreateWorkspaceCommandDefinitionParams) error {
	if params.CommandID = strings.TrimSpace(params.CommandID); params.CommandID == "" {
		return fmt.Errorf("%w: command id is required", ErrInvalidInput)
	}
	if params.WorkspaceID = strings.TrimSpace(params.WorkspaceID); params.WorkspaceID == "" {
		return fmt.Errorf("%w: workspace id is required", ErrInvalidInput)
	}
	if params.Name = strings.TrimSpace(params.Name); params.Name == "" {
		return fmt.Errorf("%w: command name is required", ErrInvalidInput)
	}
	if params.Command = strings.TrimSpace(params.Command); params.Command == "" {
		return fmt.Errorf("%w: command is required", ErrInvalidInput)
	}
	if params.Args = strings.TrimSpace(params.Args); params.Args == "" {
		params.Args = "[]"
	}
	if !json.Valid([]byte(params.Args)) {
		return fmt.Errorf("%w: command args must be valid json", ErrInvalidInput)
	}
	trimmedArgs := strings.TrimSpace(params.Args)
	if !strings.HasPrefix(trimmedArgs, "[") || !strings.HasSuffix(trimmedArgs, "]") {
		return fmt.Errorf("%w: command args must be a json string array", ErrInvalidInput)
	}
	decodedArgs := make([]string, 0)
	if err := json.Unmarshal([]byte(params.Args), &decodedArgs); err != nil {
		return fmt.Errorf("%w: command args must be a json string array", ErrInvalidInput)
	}

	return s.withImmediateQueries(ctx, func(ctx context.Context, queries *dbsqlc.Queries) error {
		return createWorkspaceCommandDefinitionInTransaction(ctx, queries, params)
	})
}

func createWorkspaceCommandDefinitionInTransaction(ctx context.Context, queries *dbsqlc.Queries, params CreateWorkspaceCommandDefinitionParams) error {
	_, err := queries.GetWorkspaceByID(ctx, dbsqlc.GetWorkspaceByIDParams{WorkspaceID: params.WorkspaceID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: workspace id %q", ErrNotFound, params.WorkspaceID)
		}
		return err
	}
	if existingByID, err := getWorkspaceCommandDefinition(ctx, queries, params.WorkspaceID, params.Name); err == nil && existingByID != nil {
		return fmt.Errorf("%w: command name %q collides with existing command id", ErrInvalidInput, params.Name)
	} else if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if existingByName, err := getWorkspaceCommandDefinitionByName(ctx, queries, params.WorkspaceID, params.CommandID); err == nil && existingByName != nil {
		return fmt.Errorf("%w: command id %q collides with existing command name", ErrInvalidInput, params.CommandID)
	} else if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := queries.CreateWorkspaceCommandDefinition(ctx, dbsqlc.CreateWorkspaceCommandDefinitionParams{CommandID: params.CommandID, WorkspaceID: params.WorkspaceID, Name: params.Name, Command: params.Command, Args: params.Args, CreatedAt: now, UpdatedAt: now}); err != nil {
		if isConstraintError(err) {
			return fmt.Errorf("%w: command definition %q already exists in workspace", ErrInvalidInput, params.CommandID)
		}
		return fmt.Errorf("create workspace command definition %q: %w", params.CommandID, err)
	}

	return nil
}

func (s *Store) GetWorkspaceCommandDefinition(ctx context.Context, workspaceID, commandID string) (*WorkspaceCommandDefinition, error) {
	if workspaceID = strings.TrimSpace(workspaceID); workspaceID == "" {
		return nil, fmt.Errorf("%w: workspace id is required", ErrInvalidInput)
	}
	if commandID = strings.TrimSpace(commandID); commandID == "" {
		return nil, fmt.Errorf("%w: command id is required", ErrInvalidInput)
	}

	return getWorkspaceCommandDefinition(ctx, s.sqlcQueries(), workspaceID, commandID)
}

func getWorkspaceCommandDefinition(ctx context.Context, queries *dbsqlc.Queries, workspaceID, commandID string) (*WorkspaceCommandDefinition, error) {
	row, err := queries.GetWorkspaceCommandDefinitionByID(ctx, dbsqlc.GetWorkspaceCommandDefinitionByIDParams{WorkspaceID: workspaceID, CommandID: commandID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: command id %q for workspace %q", ErrNotFound, commandID, workspaceID)
		}
		return nil, err
	}
	def := workspaceCommandDefinitionFromSQLC(row)
	return &def, nil
}

func (s *Store) GetWorkspaceCommandDefinitionByName(ctx context.Context, workspaceID, name string) (*WorkspaceCommandDefinition, error) {
	if workspaceID = strings.TrimSpace(workspaceID); workspaceID == "" {
		return nil, fmt.Errorf("%w: workspace id is required", ErrInvalidInput)
	}
	if name = strings.TrimSpace(name); name == "" {
		return nil, fmt.Errorf("%w: command name is required", ErrInvalidInput)
	}

	return getWorkspaceCommandDefinitionByName(ctx, s.sqlcQueries(), workspaceID, name)
}

func getWorkspaceCommandDefinitionByName(ctx context.Context, queries *dbsqlc.Queries, workspaceID, name string) (*WorkspaceCommandDefinition, error) {
	row, err := queries.GetWorkspaceCommandDefinitionByName(ctx, dbsqlc.GetWorkspaceCommandDefinitionByNameParams{WorkspaceID: workspaceID, Name: name})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: command name %q for workspace %q", ErrNotFound, name, workspaceID)
		}
		return nil, err
	}
	def := workspaceCommandDefinitionFromSQLC(row)
	return &def, nil
}

func (s *Store) ListWorkspaceCommandDefinitions(ctx context.Context, workspaceID string) ([]WorkspaceCommandDefinition, error) {
	if workspaceID = strings.TrimSpace(workspaceID); workspaceID == "" {
		return nil, fmt.Errorf("%w: workspace id is required", ErrInvalidInput)
	}

	rows, err := s.sqlcQueries().ListWorkspaceCommandDefinitionsByWorkspace(ctx, dbsqlc.ListWorkspaceCommandDefinitionsByWorkspaceParams{WorkspaceID: workspaceID})
	if err != nil {
		return nil, err
	}
	out := make([]WorkspaceCommandDefinition, 0, len(rows))
	for _, row := range rows {
		out = append(out, workspaceCommandDefinitionFromSQLC(row))
	}
	return out, nil
}

func (s *Store) DeleteWorkspaceCommandDefinition(ctx context.Context, workspaceID, commandID string) error {
	if workspaceID = strings.TrimSpace(workspaceID); workspaceID == "" {
		return fmt.Errorf("%w: workspace id is required", ErrInvalidInput)
	}
	if commandID = strings.TrimSpace(commandID); commandID == "" {
		return fmt.Errorf("%w: command id is required", ErrInvalidInput)
	}

	rowsAffected, err := s.sqlcQueries().DeleteWorkspaceCommandDefinitionByID(ctx, dbsqlc.DeleteWorkspaceCommandDefinitionByIDParams{WorkspaceID: workspaceID, CommandID: commandID})
	if err != nil {
		return fmt.Errorf("delete workspace command definition %q: %w", commandID, err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("%w: command id %q for workspace %q", ErrNotFound, commandID, workspaceID)
	}

	return nil
}

func isValidCommandStatus(status string) bool {
	switch status {
	case commandStatusRunning, commandStatusExited, commandStatusLost, commandStatusStopped:
		return true
	default:
		return false
	}
}
