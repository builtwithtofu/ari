CREATE TABLE IF NOT EXISTS daemon_meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

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
    ON workspace_folders (folder_path);

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
    ON commands (workspace_id, started_at DESC);

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
    ON agents (workspace_id, name)
    WHERE name IS NOT NULL;

CREATE INDEX IF NOT EXISTS agents_workspace_id_started_at_idx
    ON agents (workspace_id, started_at DESC);

CREATE INDEX IF NOT EXISTS agents_workspace_invocation_status_idx
    ON agents(workspace_id, invocation_class, status);

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
    UNIQUE(workspace_id, name),
    FOREIGN KEY(workspace_id) REFERENCES workspaces(workspace_id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS agent_profiles_global_name_idx
    ON agent_profiles(name)
    WHERE workspace_id IS NULL;

CREATE TABLE IF NOT EXISTS auth_slots (
    auth_slot_id TEXT PRIMARY KEY,
    harness TEXT NOT NULL,
    label TEXT NOT NULL,
    provider_label TEXT,
    credential_owner TEXT NOT NULL DEFAULT 'provider',
    status TEXT NOT NULL DEFAULT 'unknown',
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS auth_slots_harness_label_idx
    ON auth_slots(harness, label, auth_slot_id);

CREATE TABLE IF NOT EXISTS final_responses (
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

CREATE INDEX IF NOT EXISTS final_responses_workspace_created_idx
    ON final_responses(workspace_id, created_at DESC, final_response_id ASC);

CREATE UNIQUE INDEX IF NOT EXISTS final_responses_run_idx
    ON final_responses(run_id);

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
    owned_by_ari INTEGER NOT NULL DEFAULT 1,
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

CREATE INDEX IF NOT EXISTS agent_run_telemetry_workspace_group_idx
    ON agent_run_telemetry(workspace_id, profile_id, profile_name, harness, model, invocation_class);
