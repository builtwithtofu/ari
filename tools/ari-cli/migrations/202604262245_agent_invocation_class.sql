ALTER TABLE agents ADD COLUMN invocation_class TEXT NOT NULL DEFAULT 'agent';

CREATE INDEX agents_workspace_invocation_status_idx
ON agents(workspace_id, invocation_class, status);
