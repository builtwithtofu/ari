-- name: CreateAgent :exec
INSERT INTO agents (
  agent_id, workspace_id, name, command, args, status, exit_code, started_at, stopped_at,
  harness, harness_resumable_id, harness_metadata, invocation_class
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetAgentByID :one
SELECT agent_id, workspace_id, name, command, args, status, exit_code, started_at, stopped_at,
  harness, harness_resumable_id, harness_metadata, invocation_class
FROM agents
WHERE workspace_id = ? AND agent_id = ?;

-- name: GetAgentByName :one
SELECT agent_id, workspace_id, name, command, args, status, exit_code, started_at, stopped_at,
  harness, harness_resumable_id, harness_metadata, invocation_class
FROM agents
WHERE workspace_id = ? AND name = ?;

-- name: ListAgentsByWorkspace :many
SELECT agent_id, workspace_id, name, command, args, status, exit_code, started_at, stopped_at,
  harness, harness_resumable_id, harness_metadata, invocation_class
FROM agents
WHERE workspace_id = ?
ORDER BY started_at DESC, agent_id ASC;

-- name: UpdateAgentStatus :execrows
UPDATE agents
SET status = ?,
  exit_code = ?,
  stopped_at = ?
WHERE workspace_id = ? AND agent_id = ?;

-- name: MarkRunningAgentsLost :exec
UPDATE agents
SET status = 'lost'
WHERE status = 'running';
