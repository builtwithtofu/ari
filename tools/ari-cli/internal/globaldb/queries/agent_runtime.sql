-- name: CreateAgentSessionConfig :exec
INSERT INTO agent_session_configs (agent_id, workspace_id, name, harness, model, prompt)
VALUES (?, ?, ?, ?, ?, ?);

-- name: EnsureAgentSessionConfig :exec
INSERT OR IGNORE INTO agent_session_configs (agent_id, workspace_id, name, harness, model, prompt)
VALUES (?, ?, ?, ?, ?, ?);

-- name: CreateAgentSession :exec
INSERT INTO agent_sessions (
  session_id,
  workspace_id,
  agent_id,
  harness,
  model,
  provider_session_id,
  provider_run_id,
  provider_thread_id,
  cwd,
  folder_scope_json,
  status,
  usage,
  source_session_id,
  source_agent_id,
  prompt_hash,
  context_payload_ids_json,
  permission_mode,
  sandbox_mode,
  tool_scope_json,
  provider_metadata_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetAgentSession :one
SELECT session_id, workspace_id, agent_id, harness, model, provider_session_id, provider_run_id, provider_thread_id, cwd, folder_scope_json, status, usage, source_session_id, source_agent_id, prompt_hash, context_payload_ids_json, permission_mode, sandbox_mode, tool_scope_json, provider_metadata_json
FROM agent_sessions
WHERE session_id = ?;

-- name: ListAgentSessions :many
SELECT session_id, workspace_id, agent_id, harness, model, provider_session_id, provider_run_id, provider_thread_id, cwd, folder_scope_json, status, usage, source_session_id, source_agent_id, prompt_hash, context_payload_ids_json, permission_mode, sandbox_mode, tool_scope_json, provider_metadata_json
FROM agent_sessions
WHERE workspace_id = ?
ORDER BY created_at ASC, session_id ASC;

-- name: GetAgentSessionIdentity :one
SELECT workspace_id, agent_id
FROM agent_sessions
WHERE session_id = ?;

-- name: AppendRunLogMessage :exec
INSERT INTO run_log_messages (
  message_id,
  workspace_id,
  session_id,
  agent_id,
  sequence,
  role,
  status,
  provider_message_id,
  provider_item_id,
  provider_turn_id,
  provider_response_id,
  provider_call_id,
  provider_channel,
  provider_kind,
  raw_metadata_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: AppendRunLogMessagePart :exec
INSERT INTO run_log_message_parts (
  part_id,
  message_id,
  sequence,
  kind,
  text,
  mime_type,
  uri,
  name,
  tool_name,
  tool_call_id,
  raw_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: TailRunLogMessages :many
SELECT
  message_id,
  workspace_id,
  session_id,
  agent_id,
  sequence,
  role,
  status,
  provider_message_id,
  provider_item_id,
  provider_turn_id,
  provider_response_id,
  provider_call_id,
  provider_channel,
  provider_kind,
  raw_metadata_json
FROM (
  SELECT
    message_id,
    workspace_id,
    session_id,
    agent_id,
    sequence,
    role,
    status,
    provider_message_id,
    provider_item_id,
    provider_turn_id,
    provider_response_id,
    provider_call_id,
    provider_channel,
    provider_kind,
    raw_metadata_json
  FROM run_log_messages
  WHERE session_id = ?
  ORDER BY sequence DESC, message_id DESC
  LIMIT ?
)
ORDER BY sequence ASC, message_id ASC;

-- name: ListRunLogMessages :many
SELECT
  message_id,
  workspace_id,
  session_id,
  agent_id,
  sequence,
  role,
  status,
  provider_message_id,
  provider_item_id,
  provider_turn_id,
  provider_response_id,
  provider_call_id,
  provider_channel,
  provider_kind,
  raw_metadata_json
FROM run_log_messages
WHERE session_id = ? AND sequence > ?
ORDER BY sequence ASC, message_id ASC
LIMIT ?;

-- name: NextRunLogMessageSequence :one
SELECT CAST(COALESCE(MAX(sequence), 0) + 1 AS INTEGER) AS next_sequence
FROM run_log_messages
WHERE session_id = ?;

-- name: ListRunLogMessageParts :many
SELECT
  part_id,
  sequence,
  kind,
  text,
  mime_type,
  uri,
  name,
  tool_name,
  tool_call_id,
  raw_json
FROM run_log_message_parts
WHERE message_id = ?
ORDER BY sequence ASC, part_id ASC;

-- name: CreateContextExcerpt :exec
INSERT INTO context_excerpts (
  context_excerpt_id,
  workspace_id,
  source_session_id,
  source_agent_id,
  target_agent_id,
  selector_type,
  selector_json,
  appended_message,
  content_hash
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: CreateContextExcerptItem :exec
INSERT INTO context_excerpt_items (
  context_excerpt_id,
  sequence,
  source_message_id,
  source_session_id,
  source_agent_id,
  copied_role,
  copied_text,
  copied_parts_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetContextExcerpt :one
SELECT
  context_excerpt_id,
  workspace_id,
  source_session_id,
  source_agent_id,
  target_agent_id,
  target_session_id,
  selector_type,
  selector_json,
  visibility,
  appended_message,
  content_hash
FROM context_excerpts
WHERE context_excerpt_id = ?;

-- name: ListContextExcerptItems :many
SELECT
  sequence,
  source_message_id,
  copied_role,
  copied_text,
  copied_parts_json
FROM context_excerpt_items
WHERE context_excerpt_id = ?
ORDER BY sequence ASC;

-- name: CreateAgentMessage :exec
INSERT INTO agent_messages (
  agent_message_id,
  workspace_id,
  source_agent_id,
  source_session_id,
  target_agent_id,
  target_session_id,
  body,
  status,
  delivered_session_id
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: CreateAgentMessageContextExcerpt :exec
INSERT INTO agent_message_context_excerpts (agent_message_id, context_excerpt_id, sequence)
VALUES (?, ?, ?);

-- name: GetAgentSessionConfig :one
SELECT agent_id, workspace_id, name, harness, model, prompt
FROM agent_session_configs
WHERE agent_id = ?;

-- name: ListAgentSessionConfigs :many
SELECT agent_id, workspace_id, name, harness, model, prompt
FROM agent_session_configs
WHERE workspace_id = ?
ORDER BY name ASC, agent_id ASC;

-- name: UpdateAgentSessionConfig :exec
UPDATE agent_session_configs
SET name = ?, harness = ?, model = ?, prompt = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE agent_id = ? AND workspace_id = ?;

-- name: DeleteAgentSessionConfig :exec
DELETE FROM agent_session_configs
WHERE agent_id = ?;
