PRAGMA foreign_keys = OFF;

CREATE TABLE agents_new (
    agent_id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    name TEXT,
    command TEXT NOT NULL,
    args TEXT NOT NULL DEFAULT '[]',
    status TEXT NOT NULL DEFAULT 'running',
    exit_code INTEGER,
    started_at TEXT NOT NULL,
    stopped_at TEXT,
    FOREIGN KEY(session_id) REFERENCES sessions(session_id) ON DELETE CASCADE
);

INSERT INTO agents_new (
    agent_id,
    session_id,
    name,
    command,
    args,
    status,
    exit_code,
    started_at,
    stopped_at
)
SELECT
    agent_id,
    session_id,
    name,
    command,
    args,
    status,
    exit_code,
    started_at,
    stopped_at
FROM agents;

DROP TABLE agents;
ALTER TABLE agents_new RENAME TO agents;

CREATE UNIQUE INDEX IF NOT EXISTS agents_session_id_name_uq
    ON agents (session_id, name)
    WHERE name IS NOT NULL;

CREATE INDEX IF NOT EXISTS agents_session_id_started_at_idx
    ON agents (session_id, started_at DESC);

PRAGMA foreign_keys = ON;
