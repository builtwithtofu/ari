CREATE TABLE IF NOT EXISTS agents (
    agent_id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    name TEXT UNIQUE,
    command TEXT NOT NULL,
    args TEXT NOT NULL DEFAULT '[]',
    status TEXT NOT NULL DEFAULT 'running',
    exit_code INTEGER,
    started_at TEXT NOT NULL,
    stopped_at TEXT,
    FOREIGN KEY(session_id) REFERENCES sessions(session_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS agents_session_id_started_at_idx
    ON agents (session_id, started_at DESC);
