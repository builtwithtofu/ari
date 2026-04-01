package globaldb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var (
	ErrInvalidInput = errors.New("invalid globaldb input")
	ErrNotFound     = errors.New("globaldb record not found")
)

const (
	upsertMetaQuery = `INSERT INTO daemon_meta (key, value)
VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET
	value = excluded.value`

	metaByKeyQuery = `SELECT value FROM daemon_meta WHERE key = ?`
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
