-- name: CreateWorkspaceTimer :exec
INSERT INTO workspace_timers (
  timer_id,
  workspace_id,
  owner_session_id,
  subscription_id,
  subject_type,
  subject_id,
  purpose,
  status,
  fire_at,
  payload_json,
  fired_event_id,
  created_at,
  updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetWorkspaceTimer :one
SELECT timer_id, workspace_id, owner_session_id, subscription_id, subject_type, subject_id, purpose, status, fire_at, payload_json, fired_event_id, created_at, updated_at
FROM workspace_timers
WHERE timer_id = ?;

-- name: ListDueWorkspaceTimers :many
SELECT timer_id, workspace_id, owner_session_id, subscription_id, subject_type, subject_id, purpose, status, fire_at, payload_json, fired_event_id, created_at, updated_at
FROM workspace_timers
WHERE status = 'scheduled' AND fire_at <= ?
ORDER BY fire_at ASC, timer_id ASC
LIMIT ?;

-- name: MarkWorkspaceTimerFired :execrows
UPDATE workspace_timers
SET status = 'fired',
    fired_event_id = ?,
    updated_at = ?
WHERE timer_id = ? AND status = 'scheduled';

-- name: CancelWorkspaceTimer :execrows
UPDATE workspace_timers
SET status = 'canceled',
    updated_at = ?
WHERE timer_id = ? AND status = 'scheduled';
