CREATE TABLE IF NOT EXISTS workspaces (
  workspace_id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  status TEXT NOT NULL DEFAULT 'active',
  vcs_preference TEXT NOT NULL DEFAULT 'auto',
  origin_root TEXT NOT NULL,
  cleanup_policy TEXT NOT NULL DEFAULT 'manual',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS workspace_folders (
  workspace_id TEXT NOT NULL,
  folder_path TEXT NOT NULL,
  vcs_type TEXT NOT NULL DEFAULT 'unknown',
  is_primary INTEGER NOT NULL DEFAULT 0,
  added_at TEXT NOT NULL,
  PRIMARY KEY (workspace_id, folder_path),
  FOREIGN KEY(workspace_id) REFERENCES workspaces(workspace_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS workspace_folders_folder_path_idx
  ON workspace_folders(folder_path, workspace_id);

CREATE TABLE IF NOT EXISTS daemon_meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS commands (
  command_id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  command TEXT NOT NULL,
  args TEXT NOT NULL DEFAULT '[]',
  status TEXT NOT NULL DEFAULT 'running',
  exit_code INTEGER,
  started_at TEXT NOT NULL,
  finished_at TEXT,
  FOREIGN KEY(workspace_id) REFERENCES workspaces(workspace_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS commands_workspace_id_started_at_idx
  ON commands(workspace_id, started_at DESC);

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

CREATE UNIQUE INDEX IF NOT EXISTS workspace_command_definitions_workspace_name_uq
  ON workspace_command_definitions(workspace_id, name);

CREATE TABLE IF NOT EXISTS agents (
  agent_id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  name TEXT,
  command TEXT NOT NULL,
  args TEXT NOT NULL DEFAULT '[]',
  status TEXT NOT NULL DEFAULT 'running',
  exit_code INTEGER,
  started_at TEXT NOT NULL,
  stopped_at TEXT,
  harness TEXT,
  harness_resumable_id TEXT,
  harness_metadata TEXT NOT NULL DEFAULT '{}',
  invocation_class TEXT NOT NULL DEFAULT 'agent',
  FOREIGN KEY(workspace_id) REFERENCES workspaces(workspace_id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS agents_workspace_id_name_uq
  ON agents(workspace_id, name)
  WHERE name IS NOT NULL;

CREATE INDEX IF NOT EXISTS agents_workspace_id_started_at_idx
  ON agents(workspace_id, started_at DESC);

CREATE INDEX IF NOT EXISTS agents_workspace_invocation_status_idx
  ON agents(workspace_id, invocation_class, status);

CREATE TABLE IF NOT EXISTS auth_slots (
  auth_slot_id TEXT PRIMARY KEY,
  harness TEXT NOT NULL,
  label TEXT NOT NULL,
  provider_label TEXT,
  credential_owner TEXT NOT NULL,
  status TEXT NOT NULL,
  metadata_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS auth_slots_harness_label_uq
  ON auth_slots(harness, label);

INSERT INTO auth_slots (auth_slot_id, harness, label, provider_label, credential_owner, status, metadata_json, created_at, updated_at)
VALUES
  ('codex-default', 'codex', 'Default', NULL, 'provider', 'unknown', '{}', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  ('claude-default', 'claude', 'Default', NULL, 'provider', 'unknown', '{}', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  ('opencode-default', 'opencode', 'Default', NULL, 'provider', 'unknown', '{}', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));

CREATE TABLE IF NOT EXISTS agent_profiles (
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
  auth_slot_id TEXT,
  auth_pool_json TEXT NOT NULL DEFAULT '{}',
  FOREIGN KEY(workspace_id) REFERENCES workspaces(workspace_id) ON DELETE CASCADE,
  FOREIGN KEY(auth_slot_id) REFERENCES auth_slots(auth_slot_id) ON DELETE SET NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS agent_profiles_global_name_idx
  ON agent_profiles(name)
  WHERE workspace_id IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS agent_profiles_workspace_name_idx
  ON agent_profiles(workspace_id, name)
  WHERE workspace_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS final_responses (
  final_response_id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL UNIQUE,
  workspace_id TEXT NOT NULL,
  task_id TEXT NOT NULL,
  context_packet_id TEXT NOT NULL,
  profile_id TEXT,
  status TEXT NOT NULL,
  text TEXT NOT NULL DEFAULT '',
  evidence_links TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL,
  updated_at TEXT,
  FOREIGN KEY(workspace_id) REFERENCES workspaces(workspace_id) ON DELETE CASCADE,
  FOREIGN KEY(profile_id) REFERENCES agent_profiles(profile_id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS final_responses_workspace_created_idx
  ON final_responses(workspace_id, created_at DESC, final_response_id ASC);

CREATE TABLE IF NOT EXISTS agent_run_telemetry (
  run_id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  task_id TEXT NOT NULL,
  profile_id TEXT,
  profile_name TEXT,
  harness TEXT NOT NULL,
  model TEXT NOT NULL DEFAULT 'unknown',
  invocation_class TEXT NOT NULL DEFAULT 'agent',
  status TEXT NOT NULL,
  input_tokens_known INTEGER NOT NULL DEFAULT 0,
  input_tokens INTEGER,
  output_tokens_known INTEGER NOT NULL DEFAULT 0,
  output_tokens INTEGER,
  estimated_cost_known INTEGER NOT NULL DEFAULT 0,
  estimated_cost_micros INTEGER,
  duration_ms_known INTEGER NOT NULL DEFAULT 0,
  duration_ms INTEGER,
  exit_code_known INTEGER NOT NULL DEFAULT 0,
  exit_code INTEGER,
  owned_by_ari INTEGER NOT NULL DEFAULT 0,
  pid_known INTEGER NOT NULL DEFAULT 0,
  pid INTEGER,
  cpu_time_ms_known INTEGER NOT NULL DEFAULT 0,
  cpu_time_ms INTEGER,
  memory_rss_bytes_peak_known INTEGER NOT NULL DEFAULT 0,
  memory_rss_bytes_peak INTEGER,
  child_processes_peak_known INTEGER NOT NULL DEFAULT 0,
  child_processes_peak INTEGER,
  ports_json TEXT NOT NULL DEFAULT '[]',
  orphan_state TEXT NOT NULL DEFAULT 'unknown',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(workspace_id) REFERENCES workspaces(workspace_id) ON DELETE CASCADE,
  FOREIGN KEY(profile_id) REFERENCES agent_profiles(profile_id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS agent_run_telemetry_workspace_created_idx
  ON agent_run_telemetry(workspace_id, created_at DESC, run_id ASC);

CREATE TABLE IF NOT EXISTS agent_session_configs (
  agent_id TEXT PRIMARY KEY,
  workspace_id TEXT,
  name TEXT NOT NULL,
  harness TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL DEFAULT '',
  prompt TEXT NOT NULL DEFAULT '',
  auth_slot_id TEXT NOT NULL DEFAULT '',
  auth_pool_json TEXT NOT NULL DEFAULT '{}',
  tool_scope_json TEXT NOT NULL DEFAULT '{}',
  permission_policy_json TEXT NOT NULL DEFAULT '{}',
  context_policy_json TEXT NOT NULL DEFAULT '{}',
  defaults_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  FOREIGN KEY(workspace_id) REFERENCES workspaces(workspace_id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS agent_session_configs_global_name_idx
  ON agent_session_configs(name)
  WHERE workspace_id IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS agent_session_configs_workspace_name_idx
  ON agent_session_configs(workspace_id, name)
  WHERE workspace_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS agent_sessions (
  session_id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  agent_id TEXT NOT NULL,
  harness TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL DEFAULT '',
  provider_session_id TEXT NOT NULL DEFAULT '',
  provider_run_id TEXT NOT NULL DEFAULT '',
  provider_thread_id TEXT NOT NULL DEFAULT '',
  cwd TEXT NOT NULL DEFAULT '',
  folder_scope_json TEXT NOT NULL DEFAULT '[]',
  status TEXT NOT NULL,
  usage TEXT NOT NULL DEFAULT 'durable',
  source_session_id TEXT NOT NULL DEFAULT '',
  source_agent_id TEXT NOT NULL DEFAULT '',
  prompt_hash TEXT NOT NULL DEFAULT '',
  context_payload_ids_json TEXT NOT NULL DEFAULT '[]',
  permission_mode TEXT NOT NULL DEFAULT '',
  sandbox_mode TEXT NOT NULL DEFAULT '',
  tool_scope_json TEXT NOT NULL DEFAULT '{}',
  provider_metadata_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  FOREIGN KEY(workspace_id) REFERENCES workspaces(workspace_id) ON DELETE CASCADE,
  FOREIGN KEY(agent_id) REFERENCES agent_session_configs(agent_id) ON DELETE CASCADE,
  UNIQUE(session_id, workspace_id, agent_id)
);

CREATE INDEX IF NOT EXISTS agent_sessions_workspace_status_idx
  ON agent_sessions(workspace_id, status, created_at DESC, session_id ASC);

CREATE TABLE IF NOT EXISTS run_log_messages (
  message_id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  session_id TEXT NOT NULL,
  agent_id TEXT NOT NULL,
  sequence INTEGER NOT NULL,
  role TEXT NOT NULL,
  status TEXT NOT NULL,
  provider_message_id TEXT NOT NULL DEFAULT '',
  provider_item_id TEXT NOT NULL DEFAULT '',
  provider_turn_id TEXT NOT NULL DEFAULT '',
  provider_response_id TEXT NOT NULL DEFAULT '',
  provider_call_id TEXT NOT NULL DEFAULT '',
  provider_channel TEXT NOT NULL DEFAULT '',
  provider_kind TEXT NOT NULL DEFAULT '',
  raw_metadata_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  FOREIGN KEY(workspace_id) REFERENCES workspaces(workspace_id) ON DELETE CASCADE,
  FOREIGN KEY(session_id) REFERENCES agent_sessions(session_id) ON DELETE CASCADE,
  FOREIGN KEY(agent_id) REFERENCES agent_session_configs(agent_id) ON DELETE CASCADE,
  FOREIGN KEY(session_id, workspace_id, agent_id) REFERENCES agent_sessions(session_id, workspace_id, agent_id) ON DELETE CASCADE,
  UNIQUE(session_id, sequence)
);

CREATE INDEX IF NOT EXISTS run_log_messages_session_sequence_idx
  ON run_log_messages(session_id, sequence ASC, message_id ASC);

CREATE TABLE IF NOT EXISTS run_log_message_parts (
  part_id TEXT PRIMARY KEY,
  message_id TEXT NOT NULL,
  sequence INTEGER NOT NULL,
  kind TEXT NOT NULL,
  text TEXT NOT NULL DEFAULT '',
  mime_type TEXT NOT NULL DEFAULT '',
  uri TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL DEFAULT '',
  tool_name TEXT NOT NULL DEFAULT '',
  tool_call_id TEXT NOT NULL DEFAULT '',
  raw_json TEXT NOT NULL DEFAULT '{}',
  FOREIGN KEY(message_id) REFERENCES run_log_messages(message_id) ON DELETE CASCADE,
  UNIQUE(message_id, sequence)
);

CREATE TABLE IF NOT EXISTS context_excerpts (
  context_excerpt_id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  source_session_id TEXT NOT NULL,
  source_agent_id TEXT NOT NULL,
  target_agent_id TEXT NOT NULL DEFAULT '',
  target_session_id TEXT NOT NULL DEFAULT '',
  selector_type TEXT NOT NULL,
  selector_json TEXT NOT NULL DEFAULT '{}',
  visibility TEXT NOT NULL DEFAULT 'visible_context',
  appended_message TEXT NOT NULL DEFAULT '',
  content_hash TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  FOREIGN KEY(workspace_id) REFERENCES workspaces(workspace_id) ON DELETE CASCADE,
  FOREIGN KEY(source_session_id) REFERENCES agent_sessions(session_id) ON DELETE CASCADE,
  FOREIGN KEY(source_agent_id) REFERENCES agent_session_configs(agent_id) ON DELETE CASCADE,
  FOREIGN KEY(source_session_id, workspace_id, source_agent_id) REFERENCES agent_sessions(session_id, workspace_id, agent_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS context_excerpt_items (
  context_excerpt_id TEXT NOT NULL,
  sequence INTEGER NOT NULL,
  source_message_id TEXT NOT NULL,
  source_session_id TEXT NOT NULL,
  source_agent_id TEXT NOT NULL,
  copied_role TEXT NOT NULL,
  copied_text TEXT NOT NULL DEFAULT '',
  copied_parts_json TEXT NOT NULL DEFAULT '[]',
  PRIMARY KEY(context_excerpt_id, sequence),
  FOREIGN KEY(context_excerpt_id) REFERENCES context_excerpts(context_excerpt_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS agent_messages (
  agent_message_id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  source_agent_id TEXT NOT NULL DEFAULT '',
  source_session_id TEXT NOT NULL DEFAULT '',
  target_agent_id TEXT NOT NULL,
  target_session_id TEXT NOT NULL DEFAULT '',
  body TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  delivered_session_id TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  FOREIGN KEY(workspace_id) REFERENCES workspaces(workspace_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS agent_message_context_excerpts (
  agent_message_id TEXT NOT NULL,
  context_excerpt_id TEXT NOT NULL,
  sequence INTEGER NOT NULL,
  PRIMARY KEY(agent_message_id, context_excerpt_id),
  FOREIGN KEY(agent_message_id) REFERENCES agent_messages(agent_message_id) ON DELETE CASCADE,
  FOREIGN KEY(context_excerpt_id) REFERENCES context_excerpts(context_excerpt_id) ON DELETE CASCADE
);
