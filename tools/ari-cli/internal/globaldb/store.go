package globaldb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInvalidInput  = errors.New("invalid globaldb input")
	ErrNotFound      = errors.New("globaldb record not found")
	ErrSessionClosed = errors.New("session is closed")
	ErrLastFolder    = errors.New("cannot remove last session folder")
)

const (
	upsertMetaQuery = `INSERT INTO daemon_meta (key, value)
VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET
	value = excluded.value`

	metaByKeyQuery = `SELECT value FROM daemon_meta WHERE key = ?`

	insertSessionQuery = `INSERT INTO sessions (
	session_id, name, status, vcs_preference, origin_root, cleanup_policy, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`

	sessionByIDQuery = `SELECT
	session_id,
	name,
	status,
	vcs_preference,
	origin_root,
	cleanup_policy,
	created_at,
	updated_at
FROM sessions
WHERE session_id = ?`

	sessionByNameQuery = `SELECT
	session_id,
	name,
	status,
	vcs_preference,
	origin_root,
	cleanup_policy,
	created_at,
	updated_at
FROM sessions
WHERE name = ?`

	listSessionsQuery = `SELECT
	session_id,
	name,
	status,
	vcs_preference,
	origin_root,
	cleanup_policy,
	created_at,
	updated_at
FROM sessions
ORDER BY created_at DESC, session_id ASC`

	updateSessionStatusQuery = `UPDATE sessions
SET status = ?, updated_at = ?
WHERE session_id = ?`

	deleteSessionQuery = `DELETE FROM sessions WHERE session_id = ?`

	insertSessionFolderQuery = `INSERT INTO session_folders (
	session_id, folder_path, vcs_type, is_primary, added_at
) VALUES (?, ?, ?, ?, ?)`

	deleteSessionFolderQuery = `DELETE FROM session_folders
WHERE session_id = ?
  AND folder_path = ?
  AND (SELECT COUNT(*) FROM session_folders WHERE session_id = ?) > 1`

	promotePrimaryFolderQuery = `UPDATE session_folders
SET is_primary = CASE
	WHEN folder_path = ? THEN 1
	ELSE 0
END
WHERE session_id = ?`

	listSessionFoldersQuery = `SELECT
	session_id,
	folder_path,
	vcs_type,
	is_primary,
	added_at
FROM session_folders
WHERE session_id = ?
ORDER BY added_at ASC, folder_path ASC`
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
	SessionID  string
	FolderPath string
	VCSType    string
	IsPrimary  bool
	AddedAt    string
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
		if err := rows.Scan(&item.SessionID, &item.FolderPath, &item.VCSType, &isPrimary, &item.AddedAt); err != nil {
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
		if from == statusClosed {
			return false
		}
		return true
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
