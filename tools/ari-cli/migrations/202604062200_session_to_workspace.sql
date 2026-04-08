ALTER TABLE sessions RENAME TO workspaces;
ALTER TABLE workspaces RENAME COLUMN session_id TO workspace_id;

ALTER TABLE session_folders RENAME TO workspace_folders;

ALTER TABLE workspace_folders RENAME COLUMN session_id TO workspace_id;
ALTER TABLE commands RENAME COLUMN session_id TO workspace_id;
ALTER TABLE agents RENAME COLUMN session_id TO workspace_id;

DROP INDEX IF EXISTS commands_session_id_started_at_idx;
CREATE INDEX IF NOT EXISTS commands_workspace_id_started_at_idx
    ON commands (workspace_id, started_at DESC);

DROP INDEX IF EXISTS agents_session_id_name_uq;
CREATE UNIQUE INDEX IF NOT EXISTS agents_workspace_id_name_uq
    ON agents (workspace_id, name)
    WHERE name IS NOT NULL;

DROP INDEX IF EXISTS agents_session_id_started_at_idx;
CREATE INDEX IF NOT EXISTS agents_workspace_id_started_at_idx
    ON agents (workspace_id, started_at DESC);
