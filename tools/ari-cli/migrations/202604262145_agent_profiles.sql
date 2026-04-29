CREATE TABLE agent_profiles (
  profile_id TEXT PRIMARY KEY,
  workspace_id TEXT,
  name TEXT NOT NULL,
  harness TEXT,
  model TEXT,
  prompt TEXT,
  invocation_class TEXT,
  defaults_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(workspace_id, name),
  FOREIGN KEY(workspace_id) REFERENCES workspaces(workspace_id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX agent_profiles_global_name_idx
ON agent_profiles(name)
WHERE workspace_id IS NULL;
