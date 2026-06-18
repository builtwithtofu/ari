-- name: CreateFanoutGroup :exec
INSERT INTO fanout_groups (
  fanout_group_id,
  workspace_id,
  source_session_id,
  source_agent_id,
  request_agent_message_id,
  status,
  body,
  created_at,
  updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetFanoutGroup :one
SELECT fanout_group_id, workspace_id, source_session_id, source_agent_id, request_agent_message_id, status, body, created_at, updated_at
FROM fanout_groups
WHERE fanout_group_id = ?;
