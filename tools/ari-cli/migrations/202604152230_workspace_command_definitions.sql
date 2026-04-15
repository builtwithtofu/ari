CREATE TABLE IF NOT EXISTS workspace_command_definitions (
    command_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    name TEXT NOT NULL,
    command TEXT NOT NULL,
    args TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY(workspace_id) REFERENCES workspaces(workspace_id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS workspace_command_definitions_workspace_id_name_uq
    ON workspace_command_definitions (workspace_id, name);

CREATE INDEX IF NOT EXISTS workspace_command_definitions_workspace_id_created_at_idx
    ON workspace_command_definitions (workspace_id, created_at DESC);
