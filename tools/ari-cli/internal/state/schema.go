package state

const SchemaSQL = `
-- Model registry cache from models.dev
CREATE TABLE IF NOT EXISTS model_registry (
    model_id TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    spec_json TEXT NOT NULL,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Model health tracking
CREATE TABLE IF NOT EXISTS model_health (
    model_id TEXT PRIMARY KEY,
    status TEXT CHECK(status IN ('healthy', 'degraded', 'down')),
    consecutive_errors INTEGER DEFAULT 0,
    last_error_at TIMESTAMP,
    last_success_at TIMESTAMP
);

-- Task history for analytics
CREATE TABLE IF NOT EXISTS task_history (
    task_id TEXT PRIMARY KEY,
    model_id TEXT NOT NULL,
    task_type TEXT,
    status TEXT,
    tokens_prompt INTEGER,
    tokens_completion INTEGER,
    cost_usd REAL,
    latency_ms INTEGER,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Create indexes
CREATE INDEX IF NOT EXISTS idx_task_history_model ON task_history(model_id);
CREATE INDEX IF NOT EXISTS idx_task_history_created ON task_history(created_at);
`
