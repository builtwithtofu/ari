ALTER TABLE workspaces ADD COLUMN workspace_kind TEXT NOT NULL DEFAULT 'project';

CREATE UNIQUE INDEX IF NOT EXISTS workspaces_single_system_uq
    ON workspaces (workspace_kind)
    WHERE workspace_kind = 'system';
