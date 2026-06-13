package globaldb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb/dbsqlc"
)

var (
	ErrInvalidInput     = errors.New("invalid globaldb input")
	ErrNotFound         = errors.New("globaldb record not found")
	ErrPermissionDenied = errors.New("globaldb permission denied")
	ErrDataIntegrity    = errors.New("globaldb data integrity violation")
)

const (
	statusActive        = "active"
	statusSuspended     = "suspended"
	cleanupPolicyManual = "manual"

	vcsTypeGit     = "git"
	vcsTypeJJ      = "jj"
	vcsTypeUnknown = "unknown"

	commandStatusRunning = "running"
	commandStatusExited  = "exited"
	commandStatusLost    = "lost"
	commandStatusStopped = "stopped"
)

type Store struct {
	db             *sql.DB
	sqlc           *dbsqlc.Queries
	agentMessageMu sync.Mutex
}

func NewSQLStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: db is required", ErrInvalidInput)
	}
	return &Store{db: db, sqlc: dbsqlc.New(db)}, nil
}

func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	if key == "" {
		return fmt.Errorf("%w: key is required", ErrInvalidInput)
	}

	if err := s.sqlcQueries().UpsertMeta(ctx, dbsqlc.UpsertMetaParams{Key: key, Value: value}); err != nil {
		return fmt.Errorf("set meta %q: %w", key, err)
	}

	return nil
}

func (s *Store) GetMeta(ctx context.Context, key string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("%w: key is required", ErrInvalidInput)
	}

	value, err := s.sqlcQueries().GetMetaValue(ctx, dbsqlc.GetMetaValueParams{Key: key})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("%w: key %q", ErrNotFound, key)
		}
		return "", err
	}

	return value, nil
}

func (s *Store) CompareAndSwapMeta(ctx context.Context, key, oldValue, newValue string) (bool, error) {
	if key == "" {
		return false, fmt.Errorf("%w: key is required", ErrInvalidInput)
	}

	changed, err := s.sqlcQueries().CompareAndSwapMeta(ctx, dbsqlc.CompareAndSwapMetaParams{Value: newValue, Key: key, Value_2: oldValue})
	if err != nil {
		return false, fmt.Errorf("compare and swap meta %q: %w", key, err)
	}
	return changed == 1, nil
}

func (s *Store) sqlcQueries() *dbsqlc.Queries {
	if s.sqlc != nil {
		return s.sqlc
	}
	s.sqlc = dbsqlc.New(s.db)
	return s.sqlc
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func ptrString(value string) *string { return &value }

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (s *Store) withImmediateQueries(ctx context.Context, fn func(*dbsqlc.Queries) error) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, "ROLLBACK")
		}
	}()
	if err := fn(dbsqlc.New(conn)); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	committed = true
	return nil
}

func optionalInt(value *int) *int64 {
	if value == nil {
		return nil
	}
	out := int64(*value)
	return &out
}

func intPtrFromInt64(value *int64) *int {
	if value == nil {
		return nil
	}
	out := int(*value)
	return &out
}

func isConstraintError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "constraint failed") || strings.Contains(message, "unique constraint")
}
