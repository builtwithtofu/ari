-- name: UpsertFinalResponse :exec
INSERT INTO final_responses (
  final_response_id,
  session_id,
  workspace_id,
  task_id,
  context_packet_id,
  profile_id,
  status,
  text,
  evidence_links,
  created_at,
  updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(session_id) DO UPDATE SET
  status = excluded.status,
  text = excluded.text,
  evidence_links = excluded.evidence_links,
  updated_at = excluded.updated_at;

-- name: GetFinalResponseByID :one
SELECT
  final_response_id,
  session_id,
  workspace_id,
  task_id,
  context_packet_id,
  profile_id,
  status,
  text,
  evidence_links,
  created_at,
  updated_at
FROM final_responses
WHERE final_response_id = ?
LIMIT 1;

-- name: GetFinalResponseBySessionID :one
SELECT
  final_response_id,
  session_id,
  workspace_id,
  task_id,
  context_packet_id,
  profile_id,
  status,
  text,
  evidence_links,
  created_at,
  updated_at
FROM final_responses
WHERE session_id = ?
LIMIT 1;

-- name: ListFinalResponsesByWorkspace :many
SELECT
  final_response_id,
  session_id,
  workspace_id,
  task_id,
  context_packet_id,
  profile_id,
  status,
  text,
  evidence_links,
  created_at,
  updated_at
FROM final_responses
WHERE workspace_id = ?
ORDER BY created_at DESC, final_response_id ASC;
