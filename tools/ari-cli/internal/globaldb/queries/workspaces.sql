-- name: CreateWorkspace :exec
INSERT INTO workspaces (
  workspace_id, name, status, vcs_preference, origin_root, cleanup_policy, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetWorkspaceByID :one
SELECT workspace_id, name, status, vcs_preference, origin_root, cleanup_policy, created_at, updated_at
FROM workspaces
WHERE workspace_id = ?;

-- name: GetWorkspaceByName :one
SELECT workspace_id, name, status, vcs_preference, origin_root, cleanup_policy, created_at, updated_at
FROM workspaces
WHERE name = ?;

-- name: ListWorkspaces :many
SELECT workspace_id, name, status, vcs_preference, origin_root, cleanup_policy, created_at, updated_at
FROM workspaces
ORDER BY created_at DESC, workspace_id ASC;

-- name: UpdateWorkspaceStatus :execrows
UPDATE workspaces
SET status = ?, updated_at = ?
WHERE workspace_id = ?;

-- name: DeleteWorkspace :execrows
DELETE FROM workspaces WHERE workspace_id = ?;

-- name: CreateWorkspaceFolder :exec
INSERT INTO workspace_folders (
  workspace_id, folder_path, vcs_type, is_primary, added_at
) VALUES (?, ?, ?, ?, ?);

-- name: DeleteWorkspaceFolderIfNotLast :execrows
DELETE FROM workspace_folders
WHERE workspace_folders.workspace_id = ?
  AND workspace_folders.folder_path = ?
  AND (SELECT COUNT(*) FROM workspace_folders AS counted WHERE counted.workspace_id = ?) > 1;

-- name: PromotePrimaryWorkspaceFolder :exec
UPDATE workspace_folders
SET is_primary = CASE
  WHEN folder_path = ? THEN 1
  ELSE 0
END
WHERE workspace_id = ?;

-- name: ListWorkspaceFolders :many
SELECT workspace_id, folder_path, vcs_type, is_primary, added_at
FROM workspace_folders
WHERE workspace_id = ?
ORDER BY added_at ASC, folder_path ASC;

-- name: ListWorkspaceOwnersByFolderPath :many
SELECT workspace_folders.workspace_id, workspaces.status
FROM workspace_folders
JOIN workspaces ON workspaces.workspace_id = workspace_folders.workspace_id
WHERE workspace_folders.folder_path = ?
ORDER BY workspace_folders.workspace_id ASC;
