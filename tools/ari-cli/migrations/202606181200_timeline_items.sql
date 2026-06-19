CREATE TABLE IF NOT EXISTS timeline_items (
  workspace_id TEXT NOT NULL,
  timeline_item_id TEXT NOT NULL,
  workspace_event_id TEXT NOT NULL,
  sequence INTEGER NOT NULL,
  run_id TEXT NOT NULL DEFAULT '',
  session_id TEXT NOT NULL DEFAULT '',
  source_kind TEXT NOT NULL,
  source_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  status TEXT NOT NULL,
  text TEXT NOT NULL DEFAULT '',
  metadata_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY(workspace_id, timeline_item_id),
  UNIQUE(workspace_id, sequence),
  FOREIGN KEY(workspace_id) REFERENCES workspaces(workspace_id) ON DELETE CASCADE,
  FOREIGN KEY(workspace_id, workspace_event_id)
    REFERENCES workspace_events(workspace_id, event_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS timeline_items_workspace_sequence_idx
  ON timeline_items(workspace_id, sequence ASC, timeline_item_id ASC);

CREATE INDEX IF NOT EXISTS timeline_items_event_idx
  ON timeline_items(workspace_id, workspace_event_id);
