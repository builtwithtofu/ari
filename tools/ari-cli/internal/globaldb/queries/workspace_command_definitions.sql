-- name: CreateWorkspaceCommandDefinition :exec
INSERT INTO workspace_command_definitions (
  command_id, workspace_id, name, command, args, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: GetWorkspaceCommandDefinitionByID :one
SELECT command_id, workspace_id, name, command, args, created_at, updated_at
FROM workspace_command_definitions
WHERE workspace_id = ? AND command_id = ?;

-- name: GetWorkspaceCommandDefinitionByName :one
SELECT command_id, workspace_id, name, command, args, created_at, updated_at
FROM workspace_command_definitions
WHERE workspace_id = ? AND name = ?;

-- name: ListWorkspaceCommandDefinitionsByWorkspace :many
SELECT command_id, workspace_id, name, command, args, created_at, updated_at
FROM workspace_command_definitions
WHERE workspace_id = ?
ORDER BY created_at DESC, command_id ASC;

-- name: DeleteWorkspaceCommandDefinitionByID :execrows
DELETE FROM workspace_command_definitions
WHERE workspace_id = ? AND command_id = ?;
