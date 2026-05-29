CREATE TABLE IF NOT EXISTS fanout_groups (
  fanout_group_id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  source_session_id TEXT NOT NULL,
  source_agent_id TEXT NOT NULL DEFAULT '',
  request_agent_message_id TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'running',
  body TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  FOREIGN KEY(workspace_id) REFERENCES workspaces(workspace_id) ON DELETE CASCADE,
  FOREIGN KEY(source_session_id) REFERENCES harness_sessions(session_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS fanout_groups_source_idx
  ON fanout_groups(workspace_id, source_session_id, created_at DESC, fanout_group_id ASC);

CREATE TABLE IF NOT EXISTS fanout_members (
  fanout_member_id TEXT PRIMARY KEY,
  fanout_group_id TEXT NOT NULL,
  workspace_id TEXT NOT NULL,
  worker_session_id TEXT NOT NULL,
  target_profile_id TEXT NOT NULL DEFAULT '',
  request_agent_message_id TEXT NOT NULL DEFAULT '',
  reply_agent_message_id TEXT NOT NULL DEFAULT '',
  final_response_id TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'running',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  FOREIGN KEY(fanout_group_id) REFERENCES fanout_groups(fanout_group_id) ON DELETE CASCADE,
  FOREIGN KEY(workspace_id) REFERENCES workspaces(workspace_id) ON DELETE CASCADE,
  FOREIGN KEY(worker_session_id) REFERENCES harness_sessions(session_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS fanout_members_group_idx
  ON fanout_members(fanout_group_id, created_at ASC, fanout_member_id ASC);

CREATE INDEX IF NOT EXISTS fanout_members_worker_idx
  ON fanout_members(worker_session_id);

CREATE TABLE IF NOT EXISTS sticky_inbox_items (
  inbox_item_id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  target_session_id TEXT NOT NULL,
  fanout_group_id TEXT NOT NULL DEFAULT '',
  fanout_member_id TEXT NOT NULL DEFAULT '',
  worker_session_id TEXT NOT NULL DEFAULT '',
  final_response_id TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'unread',
  summary TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  FOREIGN KEY(workspace_id) REFERENCES workspaces(workspace_id) ON DELETE CASCADE,
  FOREIGN KEY(target_session_id) REFERENCES harness_sessions(session_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS sticky_inbox_items_target_idx
  ON sticky_inbox_items(workspace_id, target_session_id, status, created_at DESC, inbox_item_id ASC);
