CREATE TABLE final_responses (
  final_response_id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL,
  workspace_id TEXT NOT NULL,
  task_id TEXT NOT NULL,
  context_packet_id TEXT NOT NULL,
  profile_id TEXT,
  status TEXT NOT NULL CHECK (status IN ('completed', 'failed', 'partial', 'unavailable')),
  text TEXT NOT NULL,
  evidence_links TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL,
  updated_at TEXT,
  FOREIGN KEY(workspace_id) REFERENCES workspaces(workspace_id) ON DELETE CASCADE,
  FOREIGN KEY(profile_id) REFERENCES agent_profiles(profile_id) ON DELETE SET NULL
);

CREATE INDEX final_responses_workspace_created_idx
ON final_responses(workspace_id, created_at DESC, final_response_id ASC);

CREATE UNIQUE INDEX final_responses_run_idx
ON final_responses(run_id);
