package state

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func New(dbPath string) (*Store, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open state db: %w", err)
	}

	store := &Store{db: db}
	if err := store.initSchema(); err != nil {
		return nil, fmt.Errorf("init schema: %w", err)
	}

	return store, nil
}

func (s *Store) initSchema() error {
	_, err := s.db.Exec(SchemaSQL)
	return err
}

func (s *Store) Close() error {
	return s.db.Close()
}

type TaskRecord struct {
	TaskID           string
	ModelID          string
	TaskType         string
	Status           string
	TokensPrompt     int
	TokensCompletion int
	CostUSD          float64
	LatencyMs        int
}

func (s *Store) RecordTask(task TaskRecord) error {
	_, err := s.db.Exec(`
        INSERT INTO task_history
        (task_id, model_id, task_type, status, tokens_prompt, tokens_completion, cost_usd, latency_ms)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)
    `, task.TaskID, task.ModelID, task.TaskType, task.Status,
		task.TokensPrompt, task.TokensCompletion, task.CostUSD, task.LatencyMs)
	return err
}

// UpdateModelHealth updates the health status for a model
func (s *Store) UpdateModelHealth(modelID string, status string, isError bool) error {
	if isError {
		_, err := s.db.Exec(`
			INSERT INTO model_health (model_id, status, consecutive_errors, last_error_at)
			VALUES (?, ?, 1, CURRENT_TIMESTAMP)
			ON CONFLICT(model_id) DO UPDATE SET
				status = excluded.status,
				consecutive_errors = consecutive_errors + 1,
				last_error_at = excluded.last_error_at
		`, modelID, status)
		return err
	}

	_, err := s.db.Exec(`
		INSERT INTO model_health (model_id, status, consecutive_errors, last_success_at)
		VALUES (?, ?, 0, CURRENT_TIMESTAMP)
		ON CONFLICT(model_id) DO UPDATE SET
			status = excluded.status,
			consecutive_errors = 0,
			last_success_at = excluded.last_success_at
	`, modelID, status)
	return err
}
