CREATE TABLE IF NOT EXISTS sessions (
    session_id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'active',
    vcs_preference TEXT NOT NULL DEFAULT 'auto',
    origin_root TEXT NOT NULL,
    cleanup_policy TEXT NOT NULL DEFAULT 'manual',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS session_folders (
    session_id TEXT NOT NULL,
    folder_path TEXT NOT NULL,
    vcs_type TEXT NOT NULL DEFAULT 'unknown',
    is_primary INTEGER NOT NULL DEFAULT 0,
    added_at TEXT NOT NULL,
    PRIMARY KEY (session_id, folder_path),
    FOREIGN KEY(session_id) REFERENCES sessions(session_id) ON DELETE CASCADE
);
