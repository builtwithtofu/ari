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
