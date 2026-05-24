-- name: CreateDaemonEvent :exec
INSERT INTO daemon_events (
  event_id,
  workspace_id,
  session_id,
  event_type,
  subject_type,
  subject_id,
  payload_json,
  attention_required,
  attention_cleared_at,
  created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListDaemonEventsAfter :many
SELECT event_id, workspace_id, session_id, event_type, subject_type, subject_id, payload_json, attention_required, attention_cleared_at, created_at
FROM daemon_events AS event
WHERE event.created_at > (SELECT cursor.created_at FROM daemon_events AS cursor WHERE cursor.event_id = ?)
   OR (? = '')
   OR (event.created_at = (SELECT cursor.created_at FROM daemon_events AS cursor WHERE cursor.event_id = ?) AND event.event_id > ?)
ORDER BY event.created_at ASC, event.event_id ASC
LIMIT ?;

-- name: CountDaemonEventsByID :one
SELECT count(*)
FROM daemon_events
WHERE event_id = ?;

-- name: ListDaemonAttentionEvents :many
SELECT event_id, workspace_id, session_id, event_type, subject_type, subject_id, payload_json, attention_required, attention_cleared_at, created_at
FROM daemon_events
WHERE attention_required = 1 AND attention_cleared_at IS NULL
ORDER BY created_at ASC, event_id ASC;

-- name: ClearDaemonEventAttention :execrows
UPDATE daemon_events
SET attention_cleared_at = ?
WHERE event_id = ? AND attention_required = 1 AND attention_cleared_at IS NULL;
