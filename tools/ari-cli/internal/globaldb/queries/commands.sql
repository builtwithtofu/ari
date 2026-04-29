-- name: CreateCommand :exec
INSERT INTO commands (
  command_id, workspace_id, command, args, status, exit_code, started_at, finished_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetCommandByID :one
SELECT command_id, workspace_id, command, args, status, exit_code, started_at, finished_at
FROM commands
WHERE workspace_id = ? AND command_id = ?;

-- name: ListCommandsByWorkspace :many
SELECT command_id, workspace_id, command, args, status, exit_code, started_at, finished_at
FROM commands
WHERE workspace_id = ?
ORDER BY started_at DESC, command_id ASC;

-- name: UpdateCommandStatus :execrows
UPDATE commands
SET status = ?,
  exit_code = ?,
  finished_at = ?
WHERE workspace_id = ? AND command_id = ?;

-- name: MarkRunningCommandsLost :exec
UPDATE commands
SET status = 'lost'
WHERE status = 'running';
