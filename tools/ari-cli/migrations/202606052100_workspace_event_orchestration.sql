CREATE TABLE IF NOT EXISTS workspace_events (
  event_id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  sequence INTEGER NOT NULL,
  event_type TEXT NOT NULL,
  subject_type TEXT NOT NULL,
  subject_id TEXT NOT NULL,
  producer_type TEXT NOT NULL DEFAULT '',
  producer_id TEXT NOT NULL DEFAULT '',
  correlation_id TEXT NOT NULL DEFAULT '',
  causation_id TEXT NOT NULL DEFAULT '',
  payload_json TEXT NOT NULL DEFAULT '{}',
  payload_ref_json TEXT NOT NULL DEFAULT '{}',
  attention_required INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  FOREIGN KEY(workspace_id) REFERENCES workspaces(workspace_id) ON DELETE CASCADE,
  UNIQUE(workspace_id, sequence)
);

CREATE INDEX IF NOT EXISTS workspace_events_workspace_sequence_idx
  ON workspace_events(workspace_id, sequence ASC);

CREATE INDEX IF NOT EXISTS workspace_events_workspace_type_idx
  ON workspace_events(workspace_id, event_type, sequence ASC);

CREATE INDEX IF NOT EXISTS workspace_events_subject_idx
  ON workspace_events(workspace_id, subject_type, subject_id, sequence ASC);

CREATE INDEX IF NOT EXISTS workspace_events_correlation_idx
  ON workspace_events(workspace_id, correlation_id, sequence ASC)
  WHERE correlation_id != '';

CREATE TABLE IF NOT EXISTS inbox_items (
  inbox_item_id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  source_session_id TEXT NOT NULL,
  workspace_event_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  fanout_group_id TEXT NOT NULL DEFAULT '',
  fanout_member_id TEXT NOT NULL DEFAULT '',
  worker_session_id TEXT NOT NULL DEFAULT '',
  final_response_id TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'unread',
  attention_required INTEGER NOT NULL DEFAULT 0,
  summary TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(workspace_id) REFERENCES workspaces(workspace_id) ON DELETE CASCADE,
  FOREIGN KEY(workspace_event_id) REFERENCES workspace_events(event_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS inbox_items_session_status_idx
  ON inbox_items(workspace_id, source_session_id, status, created_at DESC, inbox_item_id ASC);

CREATE INDEX IF NOT EXISTS inbox_items_event_idx
  ON inbox_items(workspace_id, workspace_event_id);

CREATE TABLE IF NOT EXISTS event_subscriptions (
  subscription_id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  owner_session_id TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL DEFAULT '',
  filter_json TEXT NOT NULL DEFAULT '{}',
  delivery_target_type TEXT NOT NULL DEFAULT '',
  delivery_target_id TEXT NOT NULL DEFAULT '',
  delivery_policy_json TEXT NOT NULL DEFAULT '{}',
  cursor_sequence INTEGER NOT NULL DEFAULT 0,
  ack_sequence INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'active',
  completion_condition_json TEXT NOT NULL DEFAULT '{}',
  timeout_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(workspace_id) REFERENCES workspaces(workspace_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS event_subscriptions_workspace_owner_idx
  ON event_subscriptions(workspace_id, owner_session_id, status, created_at ASC);

CREATE INDEX IF NOT EXISTS event_subscriptions_workspace_active_idx
  ON event_subscriptions(workspace_id, status, created_at ASC, subscription_id ASC);

CREATE TABLE IF NOT EXISTS workspace_timers (
  timer_id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  owner_session_id TEXT NOT NULL DEFAULT '',
  subscription_id TEXT NOT NULL DEFAULT '',
  subject_type TEXT NOT NULL DEFAULT '',
  subject_id TEXT NOT NULL DEFAULT '',
  purpose TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'scheduled',
  fire_at TEXT NOT NULL,
  payload_json TEXT NOT NULL DEFAULT '{}',
  fired_event_id TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(workspace_id) REFERENCES workspaces(workspace_id) ON DELETE CASCADE,
  FOREIGN KEY(subscription_id) REFERENCES event_subscriptions(subscription_id) ON DELETE SET DEFAULT
);

CREATE INDEX IF NOT EXISTS workspace_timers_due_idx
  ON workspace_timers(status, fire_at ASC, timer_id ASC)
  WHERE status = 'scheduled';

CREATE TABLE IF NOT EXISTS pending_deliveries (
  delivery_id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  subscription_id TEXT NOT NULL,
  target_type TEXT NOT NULL,
  target_id TEXT NOT NULL,
  delivery_policy_json TEXT NOT NULL DEFAULT '{}',
  event_ids_json TEXT NOT NULL DEFAULT '[]',
  status TEXT NOT NULL DEFAULT 'pending',
  attempts INTEGER NOT NULL DEFAULT 0,
  next_attempt_at TEXT,
  deadline_at TEXT,
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  terminal_at TEXT,
  FOREIGN KEY(workspace_id) REFERENCES workspaces(workspace_id) ON DELETE CASCADE,
  FOREIGN KEY(subscription_id) REFERENCES event_subscriptions(subscription_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS pending_deliveries_due_idx
  ON pending_deliveries(status, next_attempt_at ASC, created_at ASC, delivery_id ASC)
  WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS pending_deliveries_subscription_idx
  ON pending_deliveries(workspace_id, subscription_id, status, created_at ASC);
