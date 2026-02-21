package globaldb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
)

var (
	ErrInvalidInput = errors.New("invalid globaldb input")
	ErrNotFound     = errors.New("globaldb record not found")
)

const (
	upsertProjectQuery = `INSERT INTO projects (project_id, project_identity, identity_kind, created_at, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(project_id) DO UPDATE SET
	project_identity = excluded.project_identity,
	identity_kind = excluded.identity_kind,
	updated_at = excluded.updated_at`

	projectByIDQuery = `SELECT project_id, project_identity, identity_kind, created_at, updated_at
FROM projects
WHERE project_id = ?`

	projectByIdentityQuery = `SELECT project_id, project_identity, identity_kind, created_at, updated_at
FROM projects
WHERE project_identity = ?
ORDER BY created_at ASC, project_id ASC`

	listProjectsQuery = `SELECT project_id, project_identity, identity_kind, created_at, updated_at
FROM projects
ORDER BY created_at ASC, project_id ASC`

	upsertSessionQuery = `INSERT INTO sessions (session_id, project_id, status, created_at, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(session_id) DO UPDATE SET
	project_id = excluded.project_id,
	status = excluded.status,
	updated_at = excluded.updated_at`

	sessionByIDQuery = `SELECT session_id, project_id, status, created_at, updated_at
FROM sessions
WHERE session_id = ?`

	listSessionsByProjectQuery = `SELECT session_id, project_id, status, created_at, updated_at
FROM sessions
WHERE project_id = ?
ORDER BY created_at ASC, session_id ASC`

	deleteSessionByIDQuery = `DELETE FROM sessions WHERE session_id = ?`
)

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

type Project struct {
	ProjectID       string
	ProjectIdentity string
	IdentityKind    string
	CreatedAt       string
	UpdatedAt       string
}

type Session struct {
	SessionID string
	ProjectID string
	Status    string
	CreatedAt string
	UpdatedAt string
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

func (s *Store) UpsertProject(ctx context.Context, project Project) error {
	if project.ProjectID == "" || project.ProjectIdentity == "" || project.CreatedAt == "" || project.UpdatedAt == "" {
		return fmt.Errorf("%w: project_id, project_identity, created_at, and updated_at are required", ErrInvalidInput)
	}

	identityKind := project.IdentityKind
	if identityKind == "" {
		identityKind = ProjectIdentityKindOpaque
	}

	if identityKind != ProjectIdentityKindOpaque && identityKind != ProjectIdentityKindRawPath {
		return fmt.Errorf("%w: identity_kind must be %q or %q", ErrInvalidInput, ProjectIdentityKindOpaque, ProjectIdentityKindRawPath)
	}

	if identityKind == ProjectIdentityKindOpaque && filepath.IsAbs(project.ProjectIdentity) {
		return fmt.Errorf("%w: project_identity must be non-raw by default", ErrInvalidInput)
	}

	if _, err := s.db.ExecContext(ctx, upsertProjectQuery, project.ProjectID, project.ProjectIdentity, identityKind, project.CreatedAt, project.UpdatedAt); err != nil {
		return fmt.Errorf("upsert project %q: %w", project.ProjectID, err)
	}

	return nil
}

func (s *Store) GetProjectByID(ctx context.Context, projectID string) (Project, error) {
	if projectID == "" {
		return Project{}, fmt.Errorf("%w: project_id is required", ErrInvalidInput)
	}

	projects, err := queryProjects(ctx, s.db, projectByIDQuery, projectID)
	if err != nil {
		return Project{}, err
	}
	if len(projects) == 0 {
		return Project{}, fmt.Errorf("%w: project %q", ErrNotFound, projectID)
	}

	return projects[0], nil
}

func (s *Store) GetProjectByIdentity(ctx context.Context, projectIdentity string) (Project, error) {
	if projectIdentity == "" {
		return Project{}, fmt.Errorf("%w: project_identity is required", ErrInvalidInput)
	}

	projects, err := queryProjects(ctx, s.db, projectByIdentityQuery, projectIdentity)
	if err != nil {
		return Project{}, err
	}
	if len(projects) == 0 {
		return Project{}, fmt.Errorf("%w: project_identity %q", ErrNotFound, projectIdentity)
	}

	return projects[0], nil
}

func (s *Store) ListProjects(ctx context.Context) ([]Project, error) {
	return queryProjects(ctx, s.db, listProjectsQuery)
}

func (s *Store) UpsertSession(ctx context.Context, session Session) error {
	if session.SessionID == "" || session.ProjectID == "" || session.Status == "" || session.CreatedAt == "" || session.UpdatedAt == "" {
		return fmt.Errorf("%w: session_id, project_id, status, created_at, and updated_at are required", ErrInvalidInput)
	}

	if _, err := s.db.ExecContext(ctx, upsertSessionQuery, session.SessionID, session.ProjectID, session.Status, session.CreatedAt, session.UpdatedAt); err != nil {
		return fmt.Errorf("upsert session %q: %w", session.SessionID, err)
	}

	return nil
}

func (s *Store) GetSessionByID(ctx context.Context, sessionID string) (Session, error) {
	if sessionID == "" {
		return Session{}, fmt.Errorf("%w: session_id is required", ErrInvalidInput)
	}

	sessions, err := querySessions(ctx, s.db, sessionByIDQuery, sessionID)
	if err != nil {
		return Session{}, err
	}
	if len(sessions) == 0 {
		return Session{}, fmt.Errorf("%w: session %q", ErrNotFound, sessionID)
	}

	return sessions[0], nil
}

func (s *Store) ListSessionsByProject(ctx context.Context, projectID string) ([]Session, error) {
	if projectID == "" {
		return nil, fmt.Errorf("%w: project_id is required", ErrInvalidInput)
	}
	return querySessions(ctx, s.db, listSessionsByProjectQuery, projectID)
}

func (s *Store) DeleteSessionByID(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("%w: session_id is required", ErrInvalidInput)
	}

	res, err := s.db.ExecContext(ctx, deleteSessionByIDQuery, sessionID)
	if err != nil {
		return fmt.Errorf("delete session %q: %w", sessionID, err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete session %q: rows affected: %w", sessionID, err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("%w: session %q", ErrNotFound, sessionID)
	}

	return nil
}

func queryProjects(ctx context.Context, db DB, query string, args ...any) ([]Project, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Project, 0)
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ProjectID, &p.ProjectIdentity, &p.IdentityKind, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
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
	defer rows.Close()

	out := make([]Session, 0)
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.SessionID, &s.ProjectID, &s.Status, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return out, nil
}
