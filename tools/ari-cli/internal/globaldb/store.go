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

	workspaceOwnersByFolderPathQuery = `SELECT
		workspace_folders.workspace_id,
		workspaces.status
FROM workspace_folders
JOIN workspaces ON workspaces.workspace_id = workspace_folders.workspace_id
WHERE workspace_folders.folder_path = ?
ORDER BY workspace_folders.workspace_id ASC`

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
	harness_metadata,
	invocation_class
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

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
	harness_metadata,
	invocation_class
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
	harness_metadata,
	invocation_class
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
	harness_metadata,
	invocation_class
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
	InvocationClass    string
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
	InvocationClass    string
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
	WithImmediateTransaction(ctx context.Context, fn func(DB) error) error
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

func (a *sqlDBAdapter) WithImmediateTransaction(ctx context.Context, fn func(DB) error) error {
	conn, err := a.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = conn.Close()
	}()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, "ROLLBACK")
		}
	}()
	if err := fn(&sqlConnAdapter{conn: conn}); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	committed = true
	return nil
}

type sqlConnAdapter struct {
	conn *sql.Conn
}

func (a *sqlConnAdapter) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return a.conn.ExecContext(ctx, query, args...)
}

func (a *sqlConnAdapter) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	return a.conn.QueryContext(ctx, query, args...)
}

func (a *sqlConnAdapter) WithImmediateTransaction(ctx context.Context, fn func(DB) error) error {
	return fn(a)
}

type Store struct {
	db   DB
	sqlc *dbsqlc.Queries
}

type AgentProfile struct {
	ProfileID       string
	WorkspaceID     string
	Name            string
	Harness         string
	Model           string
	Prompt          string
	InvocationClass string
	DefaultsJSON    string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

const DefaultHelperProfileName = "helper"

type FinalResponse struct {
	FinalResponseID   string
	RunID             string
	WorkspaceID       string
	TaskID            string
	ContextPacketID   string
	ProfileID         string
	Status            string
	Text              string
	EvidenceLinksJSON string
	CreatedAt         time.Time
	UpdatedAt         *time.Time
}

type KnownInt64 struct {
	Known bool
	Value *int64
}

type AgentRunTelemetry struct {
	RunID                   string
	WorkspaceID             string
	TaskID                  string
	ProfileID               string
	ProfileName             string
	Harness                 string
	Model                   string
	InvocationClass         string
	Status                  string
	InputTokensKnown        bool
	InputTokens             *int64
	OutputTokensKnown       bool
	OutputTokens            *int64
	EstimatedCostKnown      bool
	EstimatedCostMicros     *int64
	DurationMSKnown         bool
	DurationMS              *int64
	ExitCodeKnown           bool
	ExitCode                *int64
	OwnedByAri              bool
	PIDKnown                bool
	PID                     *int64
	CPUTimeMSKnown          bool
	CPUTimeMS               *int64
	MemoryRSSBytesPeakKnown bool
	MemoryRSSBytesPeak      *int64
	ChildProcessesPeakKnown bool
	ChildProcessesPeak      *int64
	PortsJSON               string
	OrphanState             string
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

type AgentRunTelemetryGroup struct {
	ProfileID       string
	ProfileName     string
	Harness         string
	Model           string
	InvocationClass string
}

type AgentRunTelemetryRollup struct {
	Group         AgentRunTelemetryGroup
	Runs          int
	Completed     int
	Failed        int
	InputTokens   KnownInt64
	OutputTokens  KnownInt64
	EstimatedCost KnownInt64
	DurationMS    KnownInt64
	ExitCode      KnownInt64
	PID           KnownInt64
	CPUTimeMS     KnownInt64
	MemoryRSS     KnownInt64
	ChildCount    KnownInt64
	OwnedByAri    bool
	PortsJSON     string
	OrphanState   string
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
	store, err := NewStore(&sqlDBAdapter{db: db})
	if err != nil {
		return nil, err
	}
	store.sqlc = dbsqlc.New(db)
	return store, nil
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

func (s *Store) UpsertAgentProfile(ctx context.Context, profile AgentProfile) error {
	profile.ProfileID = strings.TrimSpace(profile.ProfileID)
	profile.WorkspaceID = strings.TrimSpace(profile.WorkspaceID)
	profile.Name = strings.TrimSpace(profile.Name)
	profile.Harness = strings.TrimSpace(profile.Harness)
	profile.Model = strings.TrimSpace(profile.Model)
	profile.InvocationClass = strings.TrimSpace(profile.InvocationClass)
	if profile.ProfileID == "" {
		return fmt.Errorf("%w: profile id is required", ErrInvalidInput)
	}
	if profile.Name == "" {
		return fmt.Errorf("%w: profile name is required", ErrInvalidInput)
	}
	if existing, err := s.getExactAgentProfile(ctx, profile.WorkspaceID, profile.Name); err == nil {
		profile.ProfileID = existing.ProfileID
		if profile.CreatedAt.IsZero() {
			profile.CreatedAt = existing.CreatedAt
		}
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	if strings.TrimSpace(profile.DefaultsJSON) == "" {
		profile.DefaultsJSON = "{}"
	}
	if !json.Valid([]byte(profile.DefaultsJSON)) {
		return fmt.Errorf("%w: profile defaults json is invalid", ErrInvalidInput)
	}
	now := time.Now().UTC()
	if profile.CreatedAt.IsZero() {
		profile.CreatedAt = now
	}
	profile.UpdatedAt = now
	if err := s.sqlcQueries().UpsertAgentProfile(ctx, dbsqlc.UpsertAgentProfileParams{ProfileID: profile.ProfileID, WorkspaceID: sqlNullString(profile.WorkspaceID), Name: profile.Name, Harness: sqlNullString(profile.Harness), Model: sqlNullString(profile.Model), Prompt: sqlNullString(profile.Prompt), InvocationClass: sqlNullString(profile.InvocationClass), DefaultsJson: profile.DefaultsJSON, CreatedAt: profile.CreatedAt.Format(time.RFC3339Nano), UpdatedAt: profile.UpdatedAt.Format(time.RFC3339Nano)}); err != nil {
		return fmt.Errorf("upsert agent profile %q: %w", profile.Name, err)
	}
	return nil
}

func (s *Store) EnsureDefaultHelperProfile(ctx context.Context, workspaceID, harness, prompt string) (AgentProfile, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return AgentProfile{}, fmt.Errorf("%w: workspace id is required", ErrInvalidInput)
	}
	if _, err := s.GetSession(ctx, workspaceID); err != nil {
		return AgentProfile{}, err
	}
	if existing, err := s.getExactAgentProfile(ctx, workspaceID, DefaultHelperProfileName); err == nil {
		return existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		return AgentProfile{}, err
	}
	profileID := "ap_helper_" + strings.ReplaceAll(workspaceID, "-", "_")
	profile := AgentProfile{ProfileID: profileID, WorkspaceID: workspaceID, Name: DefaultHelperProfileName, Harness: strings.TrimSpace(harness), Prompt: strings.TrimSpace(prompt), InvocationClass: "agent", DefaultsJSON: "{}"}
	if err := s.UpsertAgentProfile(ctx, profile); err != nil {
		return AgentProfile{}, err
	}
	return s.getExactAgentProfile(ctx, workspaceID, DefaultHelperProfileName)
}

func (s *Store) GetDefaultHelperProfile(ctx context.Context, workspaceID string) (AgentProfile, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return AgentProfile{}, fmt.Errorf("%w: workspace id is required", ErrInvalidInput)
	}
	return s.getExactAgentProfile(ctx, workspaceID, DefaultHelperProfileName)
}

func (s *Store) getExactAgentProfile(ctx context.Context, workspaceID, name string) (AgentProfile, error) {
	if strings.TrimSpace(workspaceID) != "" {
		profile, err := s.sqlcQueries().GetWorkspaceAgentProfileByName(ctx, dbsqlc.GetWorkspaceAgentProfileByNameParams{WorkspaceID: sqlNullString(workspaceID), Name: name})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return AgentProfile{}, ErrNotFound
			}
			return AgentProfile{}, fmt.Errorf("query exact workspace agent profile: %w", err)
		}
		return agentProfileFromSQLC(profile), nil
	}
	profile, err := s.sqlcQueries().GetGlobalAgentProfileByName(ctx, name)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AgentProfile{}, ErrNotFound
		}
		return AgentProfile{}, fmt.Errorf("query exact global agent profile: %w", err)
	}
	return agentProfileFromSQLC(profile), nil
}

func (s *Store) GetAgentProfile(ctx context.Context, workspaceID, name string) (AgentProfile, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	name = strings.TrimSpace(name)
	if name == "" {
		return AgentProfile{}, fmt.Errorf("%w: profile name is required", ErrInvalidInput)
	}
	if workspaceID != "" {
		profile, err := s.sqlcQueries().GetWorkspaceAgentProfileByName(ctx, dbsqlc.GetWorkspaceAgentProfileByNameParams{WorkspaceID: sqlNullString(workspaceID), Name: name})
		if err == nil {
			return agentProfileFromSQLC(profile), nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return AgentProfile{}, fmt.Errorf("query workspace agent profile: %w", err)
		}
	}
	profile, err := s.sqlcQueries().GetGlobalAgentProfileByName(ctx, name)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AgentProfile{}, ErrNotFound
		}
		return AgentProfile{}, fmt.Errorf("query global agent profile: %w", err)
	}
	return agentProfileFromSQLC(profile), nil
}

func (s *Store) ListAgentProfiles(ctx context.Context, workspaceID string) ([]AgentProfile, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	var rows []dbsqlc.AgentProfile
	var err error
	if workspaceID == "" {
		rows, err = s.sqlcQueries().ListGlobalAgentProfiles(ctx)
	} else {
		rows, err = s.sqlcQueries().ListWorkspaceAgentProfiles(ctx, sqlNullString(workspaceID))
	}
	if err != nil {
		return nil, fmt.Errorf("list agent profiles: %w", err)
	}
	profiles := make([]AgentProfile, 0, len(rows))
	for _, row := range rows {
		profiles = append(profiles, agentProfileFromSQLC(row))
	}
	return profiles, nil
}

func (s *Store) UpsertFinalResponse(ctx context.Context, response FinalResponse) error {
	response.FinalResponseID = strings.TrimSpace(response.FinalResponseID)
	response.RunID = strings.TrimSpace(response.RunID)
	response.WorkspaceID = strings.TrimSpace(response.WorkspaceID)
	response.TaskID = strings.TrimSpace(response.TaskID)
	response.ContextPacketID = strings.TrimSpace(response.ContextPacketID)
	response.ProfileID = strings.TrimSpace(response.ProfileID)
	response.Status = strings.TrimSpace(response.Status)
	response.Text = strings.TrimSpace(response.Text)
	if response.FinalResponseID == "" {
		return fmt.Errorf("%w: final response id is required", ErrInvalidInput)
	}
	if response.RunID == "" {
		return fmt.Errorf("%w: run id is required", ErrInvalidInput)
	}
	if response.WorkspaceID == "" {
		return fmt.Errorf("%w: workspace id is required", ErrInvalidInput)
	}
	if response.TaskID == "" {
		return fmt.Errorf("%w: task id is required", ErrInvalidInput)
	}
	if response.ContextPacketID == "" {
		return fmt.Errorf("%w: context packet id is required", ErrInvalidInput)
	}
	if !validFinalResponseStatus(response.Status) {
		return fmt.Errorf("%w: invalid final response status %q", ErrInvalidInput, response.Status)
	}
	if strings.TrimSpace(response.EvidenceLinksJSON) == "" {
		response.EvidenceLinksJSON = "[]"
	}
	if !json.Valid([]byte(response.EvidenceLinksJSON)) {
		return fmt.Errorf("%w: evidence links json is invalid", ErrInvalidInput)
	}
	now := time.Now().UTC()
	if response.CreatedAt.IsZero() {
		response.CreatedAt = now
	}
	updatedAt := sql.NullString{}
	if response.UpdatedAt != nil {
		updatedAt = sql.NullString{String: response.UpdatedAt.UTC().Format(time.RFC3339Nano), Valid: true}
	}
	if err := s.sqlcQueries().UpsertFinalResponse(ctx, dbsqlc.UpsertFinalResponseParams{FinalResponseID: response.FinalResponseID, RunID: response.RunID, WorkspaceID: response.WorkspaceID, TaskID: response.TaskID, ContextPacketID: response.ContextPacketID, ProfileID: sqlNullString(response.ProfileID), Status: response.Status, Text: response.Text, EvidenceLinks: response.EvidenceLinksJSON, CreatedAt: response.CreatedAt.Format(time.RFC3339Nano), UpdatedAt: updatedAt}); err != nil {
		return fmt.Errorf("upsert final response %q: %w", response.FinalResponseID, err)
	}
	return nil
}

func (s *Store) GetFinalResponseByRunID(ctx context.Context, runID string) (FinalResponse, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return FinalResponse{}, fmt.Errorf("%w: run id is required", ErrInvalidInput)
	}
	row, err := s.sqlcQueries().GetFinalResponseByRunID(ctx, runID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return FinalResponse{}, ErrNotFound
		}
		return FinalResponse{}, fmt.Errorf("query final response by run id: %w", err)
	}
	return finalResponseFromSQLC(row), nil
}

func (s *Store) GetFinalResponseByID(ctx context.Context, finalResponseID string) (FinalResponse, error) {
	finalResponseID = strings.TrimSpace(finalResponseID)
	if finalResponseID == "" {
		return FinalResponse{}, fmt.Errorf("%w: final response id is required", ErrInvalidInput)
	}
	row, err := s.sqlcQueries().GetFinalResponseByID(ctx, finalResponseID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return FinalResponse{}, ErrNotFound
		}
		return FinalResponse{}, fmt.Errorf("query final response by id: %w", err)
	}
	return finalResponseFromSQLC(row), nil
}

func (s *Store) ListFinalResponses(ctx context.Context, workspaceID string) ([]FinalResponse, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf("%w: workspace id is required", ErrInvalidInput)
	}
	rows, err := s.sqlcQueries().ListFinalResponsesByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list final responses: %w", err)
	}
	responses := make([]FinalResponse, 0, len(rows))
	for _, row := range rows {
		responses = append(responses, finalResponseFromSQLC(row))
	}
	return responses, nil
}

func validFinalResponseStatus(status string) bool {
	switch status {
	case "completed", "failed", "partial", "unavailable":
		return true
	default:
		return false
	}
}

func (s *Store) UpsertAgentRunTelemetry(ctx context.Context, telemetry AgentRunTelemetry) error {
	telemetry.RunID = strings.TrimSpace(telemetry.RunID)
	telemetry.WorkspaceID = strings.TrimSpace(telemetry.WorkspaceID)
	telemetry.TaskID = strings.TrimSpace(telemetry.TaskID)
	telemetry.ProfileID = strings.TrimSpace(telemetry.ProfileID)
	telemetry.ProfileName = strings.TrimSpace(telemetry.ProfileName)
	telemetry.Harness = strings.TrimSpace(telemetry.Harness)
	telemetry.Model = strings.TrimSpace(telemetry.Model)
	telemetry.InvocationClass = strings.TrimSpace(telemetry.InvocationClass)
	telemetry.Status = strings.TrimSpace(telemetry.Status)
	if telemetry.RunID == "" {
		return fmt.Errorf("%w: run id is required", ErrInvalidInput)
	}
	if telemetry.WorkspaceID == "" {
		return fmt.Errorf("%w: workspace id is required", ErrInvalidInput)
	}
	if telemetry.TaskID == "" {
		return fmt.Errorf("%w: task id is required", ErrInvalidInput)
	}
	if telemetry.Harness == "" {
		return fmt.Errorf("%w: harness is required", ErrInvalidInput)
	}
	if telemetry.Model == "" {
		telemetry.Model = "unknown"
	}
	if telemetry.InvocationClass == "" {
		telemetry.InvocationClass = "agent"
	}
	if telemetry.Status == "" {
		telemetry.Status = "unknown"
	}
	if strings.TrimSpace(telemetry.PortsJSON) == "" {
		telemetry.PortsJSON = "[]"
	}
	if !json.Valid([]byte(telemetry.PortsJSON)) {
		return fmt.Errorf("%w: ports json is invalid", ErrInvalidInput)
	}
	if strings.TrimSpace(telemetry.OrphanState) == "" {
		telemetry.OrphanState = "unknown"
	}
	now := time.Now().UTC()
	if telemetry.CreatedAt.IsZero() {
		telemetry.CreatedAt = now
	}
	if telemetry.UpdatedAt.IsZero() {
		telemetry.UpdatedAt = now
	}
	params := dbsqlc.UpsertAgentRunTelemetryParams{RunID: telemetry.RunID, WorkspaceID: telemetry.WorkspaceID, TaskID: telemetry.TaskID, ProfileID: sqlNullString(telemetry.ProfileID), ProfileName: sqlNullString(telemetry.ProfileName), Harness: telemetry.Harness, Model: telemetry.Model, InvocationClass: telemetry.InvocationClass, Status: telemetry.Status, InputTokensKnown: boolInt64(telemetry.InputTokensKnown), InputTokens: sqlNullInt64(telemetry.InputTokens), OutputTokensKnown: boolInt64(telemetry.OutputTokensKnown), OutputTokens: sqlNullInt64(telemetry.OutputTokens), EstimatedCostKnown: boolInt64(telemetry.EstimatedCostKnown), EstimatedCostMicros: sqlNullInt64(telemetry.EstimatedCostMicros), DurationMsKnown: boolInt64(telemetry.DurationMSKnown), DurationMs: sqlNullInt64(telemetry.DurationMS), ExitCodeKnown: boolInt64(telemetry.ExitCodeKnown), ExitCode: sqlNullInt64(telemetry.ExitCode), OwnedByAri: boolInt64(telemetry.OwnedByAri), PidKnown: boolInt64(telemetry.PIDKnown), Pid: sqlNullInt64(telemetry.PID), CpuTimeMsKnown: boolInt64(telemetry.CPUTimeMSKnown), CpuTimeMs: sqlNullInt64(telemetry.CPUTimeMS), MemoryRssBytesPeakKnown: boolInt64(telemetry.MemoryRSSBytesPeakKnown), MemoryRssBytesPeak: sqlNullInt64(telemetry.MemoryRSSBytesPeak), ChildProcessesPeakKnown: boolInt64(telemetry.ChildProcessesPeakKnown), ChildProcessesPeak: sqlNullInt64(telemetry.ChildProcessesPeak), PortsJson: telemetry.PortsJSON, OrphanState: telemetry.OrphanState, CreatedAt: telemetry.CreatedAt.Format(time.RFC3339Nano), UpdatedAt: telemetry.UpdatedAt.Format(time.RFC3339Nano)}
	if err := s.sqlcQueries().UpsertAgentRunTelemetry(ctx, params); err != nil {
		return fmt.Errorf("upsert agent run telemetry %q: %w", telemetry.RunID, err)
	}
	return nil
}

func (s *Store) RollupAgentRunTelemetry(ctx context.Context, workspaceID string) ([]AgentRunTelemetryRollup, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf("%w: workspace id is required", ErrInvalidInput)
	}
	rows, err := s.sqlcQueries().ListAgentRunTelemetryByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list agent run telemetry: %w", err)
	}
	byGroup := map[AgentRunTelemetryGroup]*AgentRunTelemetryRollup{}
	order := []AgentRunTelemetryGroup{}
	for _, row := range rows {
		group := AgentRunTelemetryGroup{ProfileID: row.ProfileID.String, ProfileName: row.ProfileName.String, Harness: row.Harness, Model: row.Model, InvocationClass: row.InvocationClass}
		rollup := byGroup[group]
		if rollup == nil {
			rollup = &AgentRunTelemetryRollup{Group: group}
			byGroup[group] = rollup
			order = append(order, group)
		}
		rollup.Runs++
		switch row.Status {
		case "completed":
			rollup.Completed++
		case "failed":
			rollup.Failed++
		}
		addKnownInt64(&rollup.InputTokens, row.InputTokensKnown, row.InputTokens)
		addKnownInt64(&rollup.OutputTokens, row.OutputTokensKnown, row.OutputTokens)
		addKnownInt64(&rollup.EstimatedCost, row.EstimatedCostKnown, row.EstimatedCostMicros)
		addKnownInt64(&rollup.DurationMS, row.DurationMsKnown, row.DurationMs)
		addKnownInt64(&rollup.ExitCode, row.ExitCodeKnown, row.ExitCode)
		addKnownInt64(&rollup.PID, row.PidKnown, row.Pid)
		addKnownInt64(&rollup.CPUTimeMS, row.CpuTimeMsKnown, row.CpuTimeMs)
		maxKnownInt64(&rollup.MemoryRSS, row.MemoryRssBytesPeakKnown, row.MemoryRssBytesPeak)
		maxKnownInt64(&rollup.ChildCount, row.ChildProcessesPeakKnown, row.ChildProcessesPeak)
		rollup.OwnedByAri = rollup.OwnedByAri || row.OwnedByAri != 0
		if rollup.PortsJSON == "" && strings.TrimSpace(row.PortsJson) != "" && strings.TrimSpace(row.PortsJson) != "[]" {
			rollup.PortsJSON = row.PortsJson
		}
		if (rollup.OrphanState == "" || rollup.OrphanState == "unknown") && strings.TrimSpace(row.OrphanState) != "" {
			rollup.OrphanState = row.OrphanState
		}
	}
	rollups := make([]AgentRunTelemetryRollup, 0, len(order))
	for _, group := range order {
		if byGroup[group].Runs != 1 {
			byGroup[group].PID = KnownInt64{}
			byGroup[group].ExitCode = KnownInt64{}
		}
		rollups = append(rollups, *byGroup[group])
	}
	return rollups, nil
}

func boolInt64(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

func sqlNullInt64(value *int64) sql.NullInt64 {
	if value == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *value, Valid: true}
}

func addKnownInt64(total *KnownInt64, known int64, value sql.NullInt64) {
	if known == 0 || !value.Valid {
		return
	}
	if total.Value == nil {
		zero := int64(0)
		total.Value = &zero
	}
	total.Known = true
	*total.Value += value.Int64
}

func maxKnownInt64(total *KnownInt64, known int64, value sql.NullInt64) {
	if known == 0 || !value.Valid {
		return
	}
	if total.Value == nil || value.Int64 > *total.Value {
		v := value.Int64
		total.Value = &v
	}
	total.Known = true
}

func (s *Store) sqlcQueries() *dbsqlc.Queries {
	if s.sqlc != nil {
		return s.sqlc
	}
	adapter, ok := s.db.(*sqlDBAdapter)
	if !ok {
		panic("sqlc queries require SQL store")
	}
	s.sqlc = dbsqlc.New(adapter.db)
	return s.sqlc
}

func sqlNullString(value string) sql.NullString {
	value = strings.TrimSpace(value)
	return sql.NullString{String: value, Valid: value != ""}
}

func agentProfileFromSQLC(row dbsqlc.AgentProfile) AgentProfile {
	createdAt, _ := time.Parse(time.RFC3339Nano, row.CreatedAt)
	updatedAt, _ := time.Parse(time.RFC3339Nano, row.UpdatedAt)
	return AgentProfile{ProfileID: row.ProfileID, WorkspaceID: row.WorkspaceID.String, Name: row.Name, Harness: row.Harness.String, Model: row.Model.String, Prompt: row.Prompt.String, InvocationClass: row.InvocationClass.String, DefaultsJSON: row.DefaultsJson, CreatedAt: createdAt, UpdatedAt: updatedAt}
}

func finalResponseFromSQLC(row dbsqlc.FinalResponse) FinalResponse {
	createdAt, _ := time.Parse(time.RFC3339Nano, row.CreatedAt)
	var updatedAt *time.Time
	if row.UpdatedAt.Valid {
		parsed, _ := time.Parse(time.RFC3339Nano, row.UpdatedAt.String)
		updatedAt = &parsed
	}
	return FinalResponse{FinalResponseID: row.FinalResponseID, RunID: row.RunID, WorkspaceID: row.WorkspaceID, TaskID: row.TaskID, ContextPacketID: row.ContextPacketID, ProfileID: row.ProfileID.String, Status: row.Status, Text: row.Text, EvidenceLinksJSON: row.EvidenceLinks, CreatedAt: createdAt, UpdatedAt: updatedAt}
}

func (s *Store) CreateSession(ctx context.Context, id, name, originRoot, cleanupPolicy, vcsPreference string) error {
	if id = strings.TrimSpace(id); id == "" {
		return fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	if name = strings.TrimSpace(name); name == "" {
		return fmt.Errorf("%w: session name is required", ErrInvalidInput)
	}
	originRoot = strings.TrimSpace(originRoot)
	if err := validateCleanupPolicy(cleanupPolicy); err != nil {
		return err
	}
	if err := validateVCSPreference(vcsPreference); err != nil {
		return err
	}
	if originRoot == "" {
		return fmt.Errorf("%w: origin root is required", ErrInvalidInput)
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

	return s.db.WithImmediateTransaction(ctx, func(tx DB) error {
		return addFolderInTransaction(ctx, tx, sessionID, folderPath, vcsType, isPrimary)
	})
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

func addFolderInTransaction(ctx context.Context, db DB, sessionID, folderPath, vcsType string, isPrimary bool) error {
	sessions, err := querySessions(ctx, db, sessionByIDQuery, sessionID)
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		return fmt.Errorf("%w: session id %q", ErrNotFound, sessionID)
	}
	if sessions[0].Status == statusClosed {
		return fmt.Errorf("%w: session id %q", ErrSessionClosed, sessionID)
	}
	owners, err := workspaceOwnersByFolderPath(ctx, db, folderPath)
	if err != nil {
		return err
	}
	for _, owner := range owners {
		if owner.WorkspaceID != sessionID && owner.Status != statusClosed {
			return fmt.Errorf("%w: folder %q already belongs to workspace %q", ErrInvalidInput, folderPath, owner.WorkspaceID)
		}
	}

	primary := 0
	if isPrimary {
		primary = 1
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.ExecContext(ctx, insertSessionFolderQuery, sessionID, folderPath, vcsType, primary, now); err != nil {
		return fmt.Errorf("add session folder %q: %w", folderPath, err)
	}

	if isPrimary {
		if _, err := db.ExecContext(ctx, promotePrimaryFolderQuery, folderPath, sessionID); err != nil {
			return fmt.Errorf("promote session primary folder %q: %w", folderPath, err)
		}
	}

	return nil
}

type workspaceFolderOwner struct {
	WorkspaceID string
	Status      string
}

func workspaceOwnersByFolderPath(ctx context.Context, db DB, folderPath string) ([]workspaceFolderOwner, error) {
	folderPath = strings.TrimSpace(folderPath)
	if folderPath == "" {
		return nil, fmt.Errorf("%w: folder path is required", ErrInvalidInput)
	}

	rows, err := db.QueryContext(ctx, workspaceOwnersByFolderPathQuery, folderPath)
	if err != nil {
		return nil, fmt.Errorf("lookup workspaces by folder path %q: %w", folderPath, err)
	}
	defer func() {
		_ = rows.Close()
	}()

	owners := make([]workspaceFolderOwner, 0)
	for rows.Next() {
		var owner workspaceFolderOwner
		if err := rows.Scan(&owner.WorkspaceID, &owner.Status); err != nil {
			return nil, fmt.Errorf("scan workspace by folder path %q: %w", folderPath, err)
		}
		owner.WorkspaceID = strings.TrimSpace(owner.WorkspaceID)
		owner.Status = strings.TrimSpace(owner.Status)
		if owner.WorkspaceID == "" {
			return nil, fmt.Errorf("%w: folder %q has empty workspace id", ErrInvalidInput, folderPath)
		}
		if owner.Status == "" {
			return nil, fmt.Errorf("%w: folder %q owner %q has empty workspace status", ErrInvalidInput, folderPath, owner.WorkspaceID)
		}
		owners = append(owners, owner)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("lookup workspace by folder path %q rows: %w", folderPath, err)
	}

	return owners, nil
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

	return s.db.WithImmediateTransaction(ctx, func(tx DB) error {
		return createWorkspaceCommandDefinitionInTransaction(ctx, tx, params)
	})
}

func createWorkspaceCommandDefinitionInTransaction(ctx context.Context, db DB, params CreateWorkspaceCommandDefinitionParams) error {
	sessions, err := querySessions(ctx, db, sessionByIDQuery, params.WorkspaceID)
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		return fmt.Errorf("%w: session id %q", ErrNotFound, params.WorkspaceID)
	}
	if sessions[0].Status == statusClosed {
		return fmt.Errorf("%w: session id %q", ErrSessionClosed, params.WorkspaceID)
	}
	if existingByID, err := getWorkspaceCommandDefinition(ctx, db, params.WorkspaceID, params.Name); err == nil && existingByID != nil {
		return fmt.Errorf("%w: command name %q collides with existing command id", ErrInvalidInput, params.Name)
	} else if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if existingByName, err := getWorkspaceCommandDefinitionByName(ctx, db, params.WorkspaceID, params.CommandID); err == nil && existingByName != nil {
		return fmt.Errorf("%w: command id %q collides with existing command name", ErrInvalidInput, params.CommandID)
	} else if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.ExecContext(
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
		if isConstraintError(err) {
			return fmt.Errorf("%w: command definition %q already exists in workspace", ErrInvalidInput, params.CommandID)
		}
		return fmt.Errorf("create workspace command definition %q: %w", params.CommandID, err)
	}

	return nil
}

func isConstraintError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "constraint failed") || strings.Contains(message, "unique constraint")
}

func (s *Store) GetWorkspaceCommandDefinition(ctx context.Context, sessionID, commandID string) (*WorkspaceCommandDefinition, error) {
	if sessionID = strings.TrimSpace(sessionID); sessionID == "" {
		return nil, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	if commandID = strings.TrimSpace(commandID); commandID == "" {
		return nil, fmt.Errorf("%w: command id is required", ErrInvalidInput)
	}

	return getWorkspaceCommandDefinition(ctx, s.db, sessionID, commandID)
}

func getWorkspaceCommandDefinition(ctx context.Context, db DB, sessionID, commandID string) (*WorkspaceCommandDefinition, error) {
	defs, err := queryWorkspaceCommandDefinitions(ctx, db, workspaceCommandDefinitionByIDQuery, sessionID, commandID)
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

	return getWorkspaceCommandDefinitionByName(ctx, s.db, sessionID, name)
}

func getWorkspaceCommandDefinitionByName(ctx context.Context, db DB, sessionID, name string) (*WorkspaceCommandDefinition, error) {
	defs, err := queryWorkspaceCommandDefinitions(ctx, db, workspaceCommandDefinitionByNameQuery, sessionID, name)
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
	if params.InvocationClass = strings.TrimSpace(params.InvocationClass); params.InvocationClass == "" {
		params.InvocationClass = "agent"
	}
	if params.InvocationClass != "agent" && params.InvocationClass != "temporary" {
		return fmt.Errorf("%w: invalid invocation class %q", ErrInvalidInput, params.InvocationClass)
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
		params.InvocationClass,
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
		if err := rows.Scan(&item.ID, &item.Name, &item.Status, &item.VCSPreference, &item.OriginRoot, &item.CleanupPolicy, &item.CreatedAt, &item.UpdatedAt); err != nil {
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
			&item.InvocationClass,
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
