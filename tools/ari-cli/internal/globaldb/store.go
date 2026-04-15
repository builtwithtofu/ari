package globaldb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInvalidInput  = errors.New("invalid globaldb input")
	ErrNotFound      = errors.New("globaldb record not found")
	ErrSessionClosed = errors.New("workspace is closed")
	ErrLastFolder    = errors.New("cannot remove last workspace folder")
)

const (
	upsertMetaQuery = `INSERT INTO daemon_meta (key, value)
VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET
	value = excluded.value`

	metaByKeyQuery = `SELECT value FROM daemon_meta WHERE key = ?`

	insertSessionQuery = `INSERT INTO workspaces (
		workspace_id, name, status, vcs_preference, origin_root, cleanup_policy, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`

	sessionByIDQuery = `SELECT
		workspace_id,
		name,
	status,
	vcs_preference,
	origin_root,
	cleanup_policy,
	created_at,
	updated_at
FROM workspaces
WHERE workspace_id = ?`

	sessionByNameQuery = `SELECT
		workspace_id,
	name,
	status,
	vcs_preference,
	origin_root,
	cleanup_policy,
	created_at,
	updated_at
FROM workspaces
WHERE name = ?`

	listSessionsQuery = `SELECT
		workspace_id,
	name,
	status,
	vcs_preference,
	origin_root,
	cleanup_policy,
	created_at,
	updated_at
FROM workspaces
ORDER BY created_at DESC, workspace_id ASC`

	updateSessionStatusQuery = `UPDATE workspaces
SET status = ?, updated_at = ?
WHERE workspace_id = ?`

	deleteSessionQuery = `DELETE FROM workspaces WHERE workspace_id = ?`

	insertSessionFolderQuery = `INSERT INTO workspace_folders (
		workspace_id, folder_path, vcs_type, is_primary, added_at
) VALUES (?, ?, ?, ?, ?)`

	deleteSessionFolderQuery = `DELETE FROM workspace_folders
WHERE workspace_id = ?
  AND folder_path = ?
  AND (SELECT COUNT(*) FROM workspace_folders WHERE workspace_id = ?) > 1`

	promotePrimaryFolderQuery = `UPDATE workspace_folders
SET is_primary = CASE
	WHEN folder_path = ? THEN 1
	ELSE 0
END
WHERE workspace_id = ?`

	listSessionFoldersQuery = `SELECT
		workspace_id,
		folder_path,
	vcs_type,
	is_primary,
	added_at
FROM workspace_folders
WHERE workspace_id = ?
ORDER BY added_at ASC, folder_path ASC`

	workspaceIDByFolderPathQuery = `SELECT workspace_id
FROM workspace_folders
WHERE folder_path = ?
LIMIT 1`

	insertCommandQuery = `INSERT INTO commands (
		command_id,
		workspace_id,
	command,
	args,
	status,
	exit_code,
	started_at,
	finished_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`

	commandByIDQuery = `SELECT
		command_id,
		workspace_id,
	command,
	args,
	status,
	exit_code,
	started_at,
	finished_at
FROM commands
WHERE workspace_id = ? AND command_id = ?`

	listCommandsBySessionQuery = `SELECT
		command_id,
		workspace_id,
	command,
	args,
	status,
	exit_code,
	started_at,
	finished_at
FROM commands
WHERE workspace_id = ?
ORDER BY started_at DESC, command_id ASC`

	updateCommandStatusQuery = `UPDATE commands
SET status = ?,
	exit_code = ?,
	finished_at = ?
WHERE workspace_id = ? AND command_id = ?`

	markRunningCommandsLostQuery = `UPDATE commands
SET status = 'lost'
WHERE status = 'running'`

	insertWorkspaceCommandDefinitionQuery = `INSERT INTO workspace_command_definitions (
		command_id,
		workspace_id,
		name,
		command,
		args,
		created_at,
		updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?)`

	workspaceCommandDefinitionByIDQuery = `SELECT
		command_id,
		workspace_id,
		name,
		command,
		args,
		created_at,
		updated_at
FROM workspace_command_definitions
WHERE workspace_id = ? AND command_id = ?`

	workspaceCommandDefinitionByNameQuery = `SELECT
		command_id,
		workspace_id,
		name,
		command,
		args,
		created_at,
		updated_at
FROM workspace_command_definitions
WHERE workspace_id = ? AND name = ?`

	listWorkspaceCommandDefinitionsBySessionQuery = `SELECT
		command_id,
		workspace_id,
		name,
		command,
		args,
		created_at,
		updated_at
FROM workspace_command_definitions
WHERE workspace_id = ?
ORDER BY created_at DESC, command_id ASC`

	deleteWorkspaceCommandDefinitionByIDQuery = `DELETE FROM workspace_command_definitions
WHERE workspace_id = ? AND command_id = ?`

	insertAgentQuery = `INSERT INTO agents (
		agent_id,
		workspace_id,
	name,
	command,
	args,
	status,
	exit_code,
	started_at,
	stopped_at,
	harness,
	harness_resumable_id,
	harness_metadata
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	agentByIDQuery = `SELECT
		agent_id,
		workspace_id,
	name,
	command,
	args,
	status,
	exit_code,
	started_at,
	stopped_at,
	harness,
	harness_resumable_id,
	harness_metadata
FROM agents
WHERE workspace_id = ? AND agent_id = ?`

	agentByNameQuery = `SELECT
		agent_id,
		workspace_id,
	name,
	command,
	args,
	status,
	exit_code,
	started_at,
	stopped_at,
	harness,
	harness_resumable_id,
	harness_metadata
FROM agents
WHERE workspace_id = ? AND name = ?`

	listAgentsBySessionQuery = `SELECT
		agent_id,
		workspace_id,
	name,
	command,
	args,
	status,
	exit_code,
	started_at,
	stopped_at,
	harness,
	harness_resumable_id,
	harness_metadata
FROM agents
WHERE workspace_id = ?
ORDER BY started_at DESC, agent_id ASC`

	updateAgentStatusQuery = `UPDATE agents
SET status = ?,
	exit_code = ?,
	stopped_at = ?
WHERE workspace_id = ? AND agent_id = ?`

	markRunningAgentsLostQuery = `UPDATE agents
SET status = 'lost'
WHERE status = 'running'`
)

const (
	statusActive    = "active"
	statusSuspended = "suspended"
	statusClosed    = "closed"

	cleanupPolicyManual  = "manual"
	cleanupPolicyOnClose = "on_close"

	vcsTypeGit     = "git"
	vcsTypeJJ      = "jj"
	vcsTypeUnknown = "unknown"

	commandStatusRunning = "running"
	commandStatusExited  = "exited"
	commandStatusLost    = "lost"

	agentStatusRunning = "running"
	agentStatusStopped = "stopped"
	agentStatusExited  = "exited"
	agentStatusLost    = "lost"
)

type Session struct {
	ID            string
	Name          string
	Status        string
	VCSPreference string
	OriginRoot    string
	CleanupPolicy string
	CreatedAt     string
	UpdatedAt     string
}

type SessionFolder struct {
	WorkspaceID string
	FolderPath  string
	VCSType     string
	IsPrimary   bool
	AddedAt     string
}

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

type Agent struct {
	AgentID            string
	WorkspaceID        string
	Name               *string
	Command            string
	Args               string
	Status             string
	ExitCode           *int
	StartedAt          string
	StoppedAt          *string
	Harness            *string
	HarnessResumableID *string
	HarnessMetadata    string
}

type CreateAgentParams struct {
	AgentID            string
	WorkspaceID        string
	Name               *string
	Command            string
	Args               string
	Status             string
	ExitCode           *int
	StartedAt          string
	StoppedAt          *string
	Harness            *string
	HarnessResumableID *string
	HarnessMetadata    string
}

type UpdateAgentStatusParams struct {
	WorkspaceID string
	AgentID     string
	Status      string
	ExitCode    *int
	StoppedAt   *string
}

type Rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close() error
}

type DB interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (Rows, error)
}

type sqlDBAdapter struct {
	db *sql.DB
}

func (a *sqlDBAdapter) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return a.db.ExecContext(ctx, query, args...)
}

func (a *sqlDBAdapter) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	return a.db.QueryContext(ctx, query, args...)
}

type Store struct {
	db DB
}

func NewStore(db DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: db is required", ErrInvalidInput)
	}
	return &Store{db: db}, nil
}

func NewSQLStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: db is required", ErrInvalidInput)
	}
	return NewStore(&sqlDBAdapter{db: db})
}

func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	if key == "" {
		return fmt.Errorf("%w: key is required", ErrInvalidInput)
	}

	if _, err := s.db.ExecContext(ctx, upsertMetaQuery, key, value); err != nil {
		return fmt.Errorf("set meta %q: %w", key, err)
	}

	return nil
}

func (s *Store) GetMeta(ctx context.Context, key string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("%w: key is required", ErrInvalidInput)
	}

	values, err := queryMetaValues(ctx, s.db, metaByKeyQuery, key)
	if err != nil {
		return "", err
	}
	if len(values) == 0 {
		return "", fmt.Errorf("%w: key %q", ErrNotFound, key)
	}

	return values[0], nil
}

func (s *Store) CreateSession(ctx context.Context, id, name, originRoot, cleanupPolicy, vcsPreference string) error {
	if id = strings.TrimSpace(id); id == "" {
		return fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	if name = strings.TrimSpace(name); name == "" {
		return fmt.Errorf("%w: session name is required", ErrInvalidInput)
	}
	if originRoot = strings.TrimSpace(originRoot); originRoot == "" {
		return fmt.Errorf("%w: origin root is required", ErrInvalidInput)
	}
	if err := validateCleanupPolicy(cleanupPolicy); err != nil {
		return err
	}
	if err := validateVCSPreference(vcsPreference); err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := s.db.ExecContext(ctx, insertSessionQuery, id, name, statusActive, vcsPreference, originRoot, cleanupPolicy, now, now); err != nil {
		return fmt.Errorf("create session %q: %w", id, err)
	}

	return nil
}

func (s *Store) GetSession(ctx context.Context, id string) (*Session, error) {
	if id = strings.TrimSpace(id); id == "" {
		return nil, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}

	sessions, err := querySessions(ctx, s.db, sessionByIDQuery, id)
	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, fmt.Errorf("%w: session id %q", ErrNotFound, id)
	}

	return &sessions[0], nil
}

func (s *Store) GetSessionByName(ctx context.Context, name string) (*Session, error) {
	if name = strings.TrimSpace(name); name == "" {
		return nil, fmt.Errorf("%w: session name is required", ErrInvalidInput)
	}

	sessions, err := querySessions(ctx, s.db, sessionByNameQuery, name)
	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, fmt.Errorf("%w: session name %q", ErrNotFound, name)
	}

	return &sessions[0], nil
}

func (s *Store) ListSessions(ctx context.Context) ([]Session, error) {
	return querySessions(ctx, s.db, listSessionsQuery)
}

func (s *Store) UpdateSessionStatus(ctx context.Context, id, status string) error {
	if id = strings.TrimSpace(id); id == "" {
		return fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	if status = strings.TrimSpace(status); status == "" {
		return fmt.Errorf("%w: session status is required", ErrInvalidInput)
	}
	if !isValidSessionStatus(status) {
		return fmt.Errorf("%w: invalid status %q", ErrInvalidInput, status)
	}

	session, err := s.GetSession(ctx, id)
	if err != nil {
		return err
	}

	if !canTransitionSessionStatus(session.Status, status) {
		if session.Status == statusClosed {
			return fmt.Errorf("%w: session id %q", ErrSessionClosed, id)
		}
		return fmt.Errorf("%w: invalid session transition %q -> %q", ErrInvalidInput, session.Status, status)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx, updateSessionStatusQuery, status, now, id)
	if err != nil {
		return fmt.Errorf("update session status %q: %w", id, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update session status %q rows affected: %w", id, err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("%w: session id %q", ErrNotFound, id)
	}

	return nil
}

func (s *Store) DeleteSession(ctx context.Context, id string) error {
	if id = strings.TrimSpace(id); id == "" {
		return fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}

	result, err := s.db.ExecContext(ctx, deleteSessionQuery, id)
	if err != nil {
		return fmt.Errorf("delete session %q: %w", id, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete session %q rows affected: %w", id, err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("%w: session id %q", ErrNotFound, id)
	}

	return nil
}

func (s *Store) AddFolder(ctx context.Context, sessionID, folderPath, vcsType string, isPrimary bool) error {
	if sessionID = strings.TrimSpace(sessionID); sessionID == "" {
		return fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	if folderPath = strings.TrimSpace(folderPath); folderPath == "" {
		return fmt.Errorf("%w: folder path is required", ErrInvalidInput)
	}
	if vcsType = strings.TrimSpace(vcsType); vcsType == "" {
		return fmt.Errorf("%w: vcs type is required", ErrInvalidInput)
	}
	if !isValidVCSType(vcsType) {
		return fmt.Errorf("%w: invalid vcs type %q", ErrInvalidInput, vcsType)
	}

	session, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if session.Status == statusClosed {
		return fmt.Errorf("%w: session id %q", ErrSessionClosed, sessionID)
	}
	existingWorkspaceID, err := s.workspaceIDByFolderPath(ctx, folderPath)
	if err != nil {
		return err
	}
	if existingWorkspaceID != "" && existingWorkspaceID != sessionID {
		return fmt.Errorf("%w: folder %q already belongs to workspace %q", ErrInvalidInput, folderPath, existingWorkspaceID)
	}

	primary := 0
	if isPrimary {
		primary = 1
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := s.db.ExecContext(ctx, insertSessionFolderQuery, sessionID, folderPath, vcsType, primary, now); err != nil {
		return fmt.Errorf("add session folder %q: %w", folderPath, err)
	}

	if isPrimary {
		if _, err := s.db.ExecContext(ctx, promotePrimaryFolderQuery, folderPath, sessionID); err != nil {
			return fmt.Errorf("promote session primary folder %q: %w", folderPath, err)
		}
	}

	return nil
}

func (s *Store) RemoveFolder(ctx context.Context, sessionID, folderPath string) error {
	if sessionID = strings.TrimSpace(sessionID); sessionID == "" {
		return fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	if folderPath = strings.TrimSpace(folderPath); folderPath == "" {
		return fmt.Errorf("%w: folder path is required", ErrInvalidInput)
	}

	session, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if session.Status == statusClosed {
		return fmt.Errorf("%w: session id %q", ErrSessionClosed, sessionID)
	}

	result, err := s.db.ExecContext(ctx, deleteSessionFolderQuery, sessionID, folderPath, sessionID)
	if err != nil {
		return fmt.Errorf("remove session folder %q: %w", folderPath, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("remove session folder %q rows affected: %w", folderPath, err)
	}
	if rowsAffected == 0 {
		folders, listErr := s.ListFolders(ctx, sessionID)
		if listErr != nil {
			return listErr
		}

		for _, folder := range folders {
			if folder.FolderPath == folderPath {
				return fmt.Errorf("%w: session id %q", ErrLastFolder, sessionID)
			}
		}

		return fmt.Errorf("%w: folder %q for session %q", ErrNotFound, folderPath, sessionID)
	}

	folders, err := s.ListFolders(ctx, sessionID)
	if err != nil {
		return err
	}
	if len(folders) == 0 {
		return fmt.Errorf("%w: session id %q", ErrLastFolder, sessionID)
	}

	hasPrimary := false
	for _, folder := range folders {
		if folder.IsPrimary {
			hasPrimary = true
			break
		}
	}
	if !hasPrimary {
		if _, err := s.db.ExecContext(ctx, promotePrimaryFolderQuery, folders[0].FolderPath, sessionID); err != nil {
			return fmt.Errorf("promote session primary folder %q: %w", folders[0].FolderPath, err)
		}
	}

	return nil
}

func (s *Store) ListFolders(ctx context.Context, sessionID string) ([]SessionFolder, error) {
	if sessionID = strings.TrimSpace(sessionID); sessionID == "" {
		return nil, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}

	session, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	_ = session

	rows, err := s.db.QueryContext(ctx, listSessionFoldersQuery, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	var out []SessionFolder
	for rows.Next() {
		var item SessionFolder
		var isPrimary int
		if err := rows.Scan(&item.WorkspaceID, &item.FolderPath, &item.VCSType, &isPrimary, &item.AddedAt); err != nil {
			return nil, err
		}
		item.IsPrimary = isPrimary != 0
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return out, nil
}

func (s *Store) workspaceIDByFolderPath(ctx context.Context, folderPath string) (string, error) {
	folderPath = strings.TrimSpace(folderPath)
	if folderPath == "" {
		return "", fmt.Errorf("%w: folder path is required", ErrInvalidInput)
	}

	rows, err := s.db.QueryContext(ctx, workspaceIDByFolderPathQuery, folderPath)
	if err != nil {
		return "", fmt.Errorf("lookup workspace by folder path %q: %w", folderPath, err)
	}
	defer func() {
		_ = rows.Close()
	}()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", fmt.Errorf("lookup workspace by folder path %q rows: %w", folderPath, err)
		}
		return "", nil
	}

	var workspaceID string
	if err := rows.Scan(&workspaceID); err != nil {
		return "", fmt.Errorf("scan workspace by folder path %q: %w", folderPath, err)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("lookup workspace by folder path %q rows: %w", folderPath, err)
	}

	if workspaceID = strings.TrimSpace(workspaceID); workspaceID == "" {
		return "", fmt.Errorf("%w: folder %q has empty workspace id", ErrInvalidInput, folderPath)
	}
	return workspaceID, nil
}

func (s *Store) CreateCommand(ctx context.Context, params CreateCommandParams) error {
	if params.CommandID = strings.TrimSpace(params.CommandID); params.CommandID == "" {
		return fmt.Errorf("%w: command id is required", ErrInvalidInput)
	}
	if params.WorkspaceID = strings.TrimSpace(params.WorkspaceID); params.WorkspaceID == "" {
		return fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	if _, err := s.GetSession(ctx, params.WorkspaceID); err != nil {
		return err
	}
	if params.Command = strings.TrimSpace(params.Command); params.Command == "" {
		return fmt.Errorf("%w: command is required", ErrInvalidInput)
	}
	if params.Args = strings.TrimSpace(params.Args); params.Args == "" {
		params.Args = "[]"
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

	if _, err := s.db.ExecContext(
		ctx,
		insertCommandQuery,
		params.CommandID,
		params.WorkspaceID,
		params.Command,
		params.Args,
		params.Status,
		params.ExitCode,
		params.StartedAt,
		params.FinishedAt,
	); err != nil {
		return fmt.Errorf("create command %q: %w", params.CommandID, err)
	}

	return nil
}

func (s *Store) GetCommand(ctx context.Context, sessionID, commandID string) (*Command, error) {
	if sessionID = strings.TrimSpace(sessionID); sessionID == "" {
		return nil, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	if commandID = strings.TrimSpace(commandID); commandID == "" {
		return nil, fmt.Errorf("%w: command id is required", ErrInvalidInput)
	}

	commands, err := queryCommands(ctx, s.db, commandByIDQuery, sessionID, commandID)
	if err != nil {
		return nil, err
	}
	if len(commands) == 0 {
		return nil, fmt.Errorf("%w: command id %q for session %q", ErrNotFound, commandID, sessionID)
	}

	return &commands[0], nil
}

func (s *Store) ListCommands(ctx context.Context, sessionID string) ([]Command, error) {
	if sessionID = strings.TrimSpace(sessionID); sessionID == "" {
		return nil, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}

	return queryCommands(ctx, s.db, listCommandsBySessionQuery, sessionID)
}

func (s *Store) UpdateCommandStatus(ctx context.Context, params UpdateCommandStatusParams) error {
	if params.WorkspaceID = strings.TrimSpace(params.WorkspaceID); params.WorkspaceID == "" {
		return fmt.Errorf("%w: session id is required", ErrInvalidInput)
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

	result, err := s.db.ExecContext(ctx, updateCommandStatusQuery, params.Status, params.ExitCode, params.FinishedAt, params.WorkspaceID, params.CommandID)
	if err != nil {
		return fmt.Errorf("update command status %q: %w", params.CommandID, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update command status %q rows affected: %w", params.CommandID, err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("%w: command id %q for session %q", ErrNotFound, params.CommandID, params.WorkspaceID)
	}

	return nil
}

func (s *Store) MarkRunningCommandsLost(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, markRunningCommandsLostQuery); err != nil {
		return fmt.Errorf("mark running commands lost: %w", err)
	}

	return nil
}

func (s *Store) CreateWorkspaceCommandDefinition(ctx context.Context, params CreateWorkspaceCommandDefinitionParams) error {
	if params.CommandID = strings.TrimSpace(params.CommandID); params.CommandID == "" {
		return fmt.Errorf("%w: command id is required", ErrInvalidInput)
	}
	if params.WorkspaceID = strings.TrimSpace(params.WorkspaceID); params.WorkspaceID == "" {
		return fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	if _, err := s.GetSession(ctx, params.WorkspaceID); err != nil {
		return err
	}
	if params.Name = strings.TrimSpace(params.Name); params.Name == "" {
		return fmt.Errorf("%w: command name is required", ErrInvalidInput)
	}
	if params.Command = strings.TrimSpace(params.Command); params.Command == "" {
		return fmt.Errorf("%w: command is required", ErrInvalidInput)
	}
	if existingByCommandID, err := s.GetWorkspaceCommandDefinition(ctx, params.WorkspaceID, params.CommandID); err == nil && existingByCommandID != nil {
		return fmt.Errorf("%w: command id %q already exists in workspace", ErrInvalidInput, params.CommandID)
	} else if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if existingByName, err := s.GetWorkspaceCommandDefinitionByName(ctx, params.WorkspaceID, params.Name); err == nil && existingByName != nil {
		return fmt.Errorf("%w: command name %q already exists in workspace", ErrInvalidInput, params.Name)
	} else if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if params.Args = strings.TrimSpace(params.Args); params.Args == "" {
		params.Args = "[]"
	}
	if existingByID, err := s.GetWorkspaceCommandDefinition(ctx, params.WorkspaceID, params.Name); err == nil && existingByID != nil {
		return fmt.Errorf("%w: command name %q collides with existing command id", ErrInvalidInput, params.Name)
	} else if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if existingByName, err := s.GetWorkspaceCommandDefinitionByName(ctx, params.WorkspaceID, params.CommandID); err == nil && existingByName != nil {
		return fmt.Errorf("%w: command id %q collides with existing command name", ErrInvalidInput, params.CommandID)
	} else if err != nil && !errors.Is(err, ErrNotFound) {
		return err
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

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := s.db.ExecContext(
		ctx,
		insertWorkspaceCommandDefinitionQuery,
		params.CommandID,
		params.WorkspaceID,
		params.Name,
		params.Command,
		params.Args,
		now,
		now,
	); err != nil {
		return fmt.Errorf("create workspace command definition %q: %w", params.CommandID, err)
	}

	return nil
}

func (s *Store) GetWorkspaceCommandDefinition(ctx context.Context, sessionID, commandID string) (*WorkspaceCommandDefinition, error) {
	if sessionID = strings.TrimSpace(sessionID); sessionID == "" {
		return nil, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	if commandID = strings.TrimSpace(commandID); commandID == "" {
		return nil, fmt.Errorf("%w: command id is required", ErrInvalidInput)
	}

	defs, err := queryWorkspaceCommandDefinitions(ctx, s.db, workspaceCommandDefinitionByIDQuery, sessionID, commandID)
	if err != nil {
		return nil, err
	}
	if len(defs) == 0 {
		return nil, fmt.Errorf("%w: command id %q for session %q", ErrNotFound, commandID, sessionID)
	}

	return &defs[0], nil
}

func (s *Store) GetWorkspaceCommandDefinitionByName(ctx context.Context, sessionID, name string) (*WorkspaceCommandDefinition, error) {
	if sessionID = strings.TrimSpace(sessionID); sessionID == "" {
		return nil, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	if name = strings.TrimSpace(name); name == "" {
		return nil, fmt.Errorf("%w: command name is required", ErrInvalidInput)
	}

	defs, err := queryWorkspaceCommandDefinitions(ctx, s.db, workspaceCommandDefinitionByNameQuery, sessionID, name)
	if err != nil {
		return nil, err
	}
	if len(defs) == 0 {
		return nil, fmt.Errorf("%w: command name %q for session %q", ErrNotFound, name, sessionID)
	}

	return &defs[0], nil
}

func (s *Store) ListWorkspaceCommandDefinitions(ctx context.Context, sessionID string) ([]WorkspaceCommandDefinition, error) {
	if sessionID = strings.TrimSpace(sessionID); sessionID == "" {
		return nil, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}

	return queryWorkspaceCommandDefinitions(ctx, s.db, listWorkspaceCommandDefinitionsBySessionQuery, sessionID)
}

func (s *Store) DeleteWorkspaceCommandDefinition(ctx context.Context, sessionID, commandID string) error {
	if sessionID = strings.TrimSpace(sessionID); sessionID == "" {
		return fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	if commandID = strings.TrimSpace(commandID); commandID == "" {
		return fmt.Errorf("%w: command id is required", ErrInvalidInput)
	}

	result, err := s.db.ExecContext(ctx, deleteWorkspaceCommandDefinitionByIDQuery, sessionID, commandID)
	if err != nil {
		return fmt.Errorf("delete workspace command definition %q: %w", commandID, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete workspace command definition %q rows affected: %w", commandID, err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("%w: command id %q for session %q", ErrNotFound, commandID, sessionID)
	}

	return nil
}

func (s *Store) CreateAgent(ctx context.Context, params CreateAgentParams) error {
	if params.AgentID = strings.TrimSpace(params.AgentID); params.AgentID == "" {
		return fmt.Errorf("%w: agent id is required", ErrInvalidInput)
	}
	if params.WorkspaceID = strings.TrimSpace(params.WorkspaceID); params.WorkspaceID == "" {
		return fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	if _, err := s.GetSession(ctx, params.WorkspaceID); err != nil {
		return err
	}
	if params.Name != nil {
		trimmedName := strings.TrimSpace(*params.Name)
		if trimmedName == "" {
			params.Name = nil
		} else {
			params.Name = &trimmedName
		}
	}
	if params.Command = strings.TrimSpace(params.Command); params.Command == "" {
		return fmt.Errorf("%w: command is required", ErrInvalidInput)
	}
	if params.Args = strings.TrimSpace(params.Args); params.Args == "" {
		params.Args = "[]"
	}
	if params.Status = strings.TrimSpace(params.Status); params.Status == "" {
		params.Status = agentStatusRunning
	}
	if !isValidAgentStatus(params.Status) {
		return fmt.Errorf("%w: invalid agent status %q", ErrInvalidInput, params.Status)
	}
	if params.StartedAt = strings.TrimSpace(params.StartedAt); params.StartedAt == "" {
		params.StartedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if params.Harness != nil {
		trimmedHarness := strings.TrimSpace(*params.Harness)
		if trimmedHarness == "" {
			params.Harness = nil
		} else {
			params.Harness = &trimmedHarness
		}
	}
	if params.HarnessResumableID != nil {
		trimmedResumableID := strings.TrimSpace(*params.HarnessResumableID)
		if trimmedResumableID == "" {
			params.HarnessResumableID = nil
		} else {
			params.HarnessResumableID = &trimmedResumableID
		}
	}
	if params.HarnessMetadata = strings.TrimSpace(params.HarnessMetadata); params.HarnessMetadata == "" {
		params.HarnessMetadata = "{}"
	}
	if !json.Valid([]byte(params.HarnessMetadata)) {
		return fmt.Errorf("%w: harness metadata must be valid json", ErrInvalidInput)
	}

	if _, err := s.db.ExecContext(
		ctx,
		insertAgentQuery,
		params.AgentID,
		params.WorkspaceID,
		params.Name,
		params.Command,
		params.Args,
		params.Status,
		params.ExitCode,
		params.StartedAt,
		params.StoppedAt,
		params.Harness,
		params.HarnessResumableID,
		params.HarnessMetadata,
	); err != nil {
		return fmt.Errorf("create agent %q: %w", params.AgentID, err)
	}

	return nil
}

func (s *Store) GetAgent(ctx context.Context, sessionID, agentID string) (*Agent, error) {
	if sessionID = strings.TrimSpace(sessionID); sessionID == "" {
		return nil, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	if agentID = strings.TrimSpace(agentID); agentID == "" {
		return nil, fmt.Errorf("%w: agent id is required", ErrInvalidInput)
	}

	agents, err := queryAgents(ctx, s.db, agentByIDQuery, sessionID, agentID)
	if err != nil {
		return nil, err
	}
	if len(agents) == 0 {
		return nil, fmt.Errorf("%w: agent id %q for session %q", ErrNotFound, agentID, sessionID)
	}

	return &agents[0], nil
}

func (s *Store) GetAgentByName(ctx context.Context, sessionID, name string) (*Agent, error) {
	if sessionID = strings.TrimSpace(sessionID); sessionID == "" {
		return nil, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	if name = strings.TrimSpace(name); name == "" {
		return nil, fmt.Errorf("%w: agent name is required", ErrInvalidInput)
	}

	agents, err := queryAgents(ctx, s.db, agentByNameQuery, sessionID, name)
	if err != nil {
		return nil, err
	}
	if len(agents) == 0 {
		return nil, fmt.Errorf("%w: agent name %q for session %q", ErrNotFound, name, sessionID)
	}

	return &agents[0], nil
}

func (s *Store) ListAgents(ctx context.Context, sessionID string) ([]Agent, error) {
	if sessionID = strings.TrimSpace(sessionID); sessionID == "" {
		return nil, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}

	return queryAgents(ctx, s.db, listAgentsBySessionQuery, sessionID)
}

func (s *Store) UpdateAgentStatus(ctx context.Context, params UpdateAgentStatusParams) error {
	if params.WorkspaceID = strings.TrimSpace(params.WorkspaceID); params.WorkspaceID == "" {
		return fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	if params.AgentID = strings.TrimSpace(params.AgentID); params.AgentID == "" {
		return fmt.Errorf("%w: agent id is required", ErrInvalidInput)
	}
	if params.Status = strings.TrimSpace(params.Status); params.Status == "" {
		return fmt.Errorf("%w: status is required", ErrInvalidInput)
	}
	if !isValidAgentStatus(params.Status) {
		return fmt.Errorf("%w: invalid agent status %q", ErrInvalidInput, params.Status)
	}

	result, err := s.db.ExecContext(ctx, updateAgentStatusQuery, params.Status, params.ExitCode, params.StoppedAt, params.WorkspaceID, params.AgentID)
	if err != nil {
		return fmt.Errorf("update agent status %q: %w", params.AgentID, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update agent status %q rows affected: %w", params.AgentID, err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("%w: agent id %q for session %q", ErrNotFound, params.AgentID, params.WorkspaceID)
	}

	return nil
}

func (s *Store) MarkRunningAgentsLost(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, markRunningAgentsLostQuery); err != nil {
		return fmt.Errorf("mark running agents lost: %w", err)
	}

	return nil
}

func queryMetaValues(ctx context.Context, db DB, query string, args ...any) ([]string, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	out := make([]string, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return out, nil
}

func querySessions(ctx context.Context, db DB, query string, args ...any) ([]Session, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	out := make([]Session, 0)
	for rows.Next() {
		var item Session
		if err := rows.Scan(
			&item.ID,
			&item.Name,
			&item.Status,
			&item.VCSPreference,
			&item.OriginRoot,
			&item.CleanupPolicy,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return out, nil
}

func queryCommands(ctx context.Context, db DB, query string, args ...any) ([]Command, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	out := make([]Command, 0)
	for rows.Next() {
		var item Command
		if err := rows.Scan(
			&item.CommandID,
			&item.WorkspaceID,
			&item.Command,
			&item.Args,
			&item.Status,
			&item.ExitCode,
			&item.StartedAt,
			&item.FinishedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return out, nil
}

func queryAgents(ctx context.Context, db DB, query string, args ...any) ([]Agent, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	out := make([]Agent, 0)
	for rows.Next() {
		var item Agent
		if err := rows.Scan(
			&item.AgentID,
			&item.WorkspaceID,
			&item.Name,
			&item.Command,
			&item.Args,
			&item.Status,
			&item.ExitCode,
			&item.StartedAt,
			&item.StoppedAt,
			&item.Harness,
			&item.HarnessResumableID,
			&item.HarnessMetadata,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return out, nil
}

func queryWorkspaceCommandDefinitions(ctx context.Context, db DB, query string, args ...any) ([]WorkspaceCommandDefinition, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	out := make([]WorkspaceCommandDefinition, 0)
	for rows.Next() {
		var item WorkspaceCommandDefinition
		if err := rows.Scan(
			&item.CommandID,
			&item.WorkspaceID,
			&item.Name,
			&item.Command,
			&item.Args,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return out, nil
}

func isValidSessionStatus(status string) bool {
	switch status {
	case statusActive, statusSuspended, statusClosed:
		return true
	default:
		return false
	}
}

func canTransitionSessionStatus(from, to string) bool {
	if from == to {
		return from != statusClosed
	}

	switch from {
	case statusActive:
		return to == statusSuspended || to == statusClosed
	case statusSuspended:
		return to == statusActive || to == statusClosed
	case statusClosed:
		return false
	default:
		return false
	}
}

func validateCleanupPolicy(cleanupPolicy string) error {
	cleanupPolicy = strings.TrimSpace(cleanupPolicy)
	if cleanupPolicy == "" {
		return fmt.Errorf("%w: cleanup policy is required", ErrInvalidInput)
	}

	if cleanupPolicy != cleanupPolicyManual && cleanupPolicy != cleanupPolicyOnClose {
		return fmt.Errorf("%w: invalid cleanup policy %q", ErrInvalidInput, cleanupPolicy)
	}

	return nil
}

func validateVCSPreference(vcsPreference string) error {
	vcsPreference = strings.TrimSpace(vcsPreference)
	if vcsPreference == "" {
		return fmt.Errorf("%w: vcs preference is required", ErrInvalidInput)
	}

	if vcsPreference != "auto" && vcsPreference != "jj" && vcsPreference != "git" {
		return fmt.Errorf("%w: invalid vcs preference %q", ErrInvalidInput, vcsPreference)
	}

	return nil
}

func isValidVCSType(vcsType string) bool {
	switch vcsType {
	case vcsTypeGit, vcsTypeJJ, vcsTypeUnknown:
		return true
	default:
		return false
	}
}

func isValidCommandStatus(status string) bool {
	switch status {
	case commandStatusRunning, commandStatusExited, commandStatusLost:
		return true
	default:
		return false
	}
}

func isValidAgentStatus(status string) bool {
	switch status {
	case agentStatusRunning, agentStatusStopped, agentStatusExited, agentStatusLost:
		return true
	default:
		return false
	}
}
