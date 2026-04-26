-- name: UpsertAgentProfile :exec
INSERT INTO agent_profiles (
  profile_id,
  workspace_id,
  name,
  harness,
  model,
  prompt,
  invocation_class,
  defaults_json,
  created_at,
  updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(profile_id) DO UPDATE SET
  workspace_id = excluded.workspace_id,
  name = excluded.name,
  harness = excluded.harness,
  model = excluded.model,
  prompt = excluded.prompt,
  invocation_class = excluded.invocation_class,
  defaults_json = excluded.defaults_json,
  updated_at = excluded.updated_at;

-- name: GetWorkspaceAgentProfileByName :one
SELECT
  profile_id,
  workspace_id,
  name,
  harness,
  model,
  prompt,
  invocation_class,
  defaults_json,
  created_at,
  updated_at
FROM agent_profiles
WHERE workspace_id = ? AND name = ?
LIMIT 1;

-- name: GetGlobalAgentProfileByName :one
SELECT
  profile_id,
  workspace_id,
  name,
  harness,
  model,
  prompt,
  invocation_class,
  defaults_json,
  created_at,
  updated_at
FROM agent_profiles
WHERE workspace_id IS NULL AND name = ?
LIMIT 1;

-- name: ListGlobalAgentProfiles :many
SELECT
  profile_id,
  workspace_id,
  name,
  harness,
  model,
  prompt,
  invocation_class,
  defaults_json,
  created_at,
  updated_at
FROM agent_profiles
WHERE workspace_id IS NULL
ORDER BY name ASC, profile_id ASC;

-- name: ListWorkspaceAgentProfiles :many
SELECT
  profile_id,
  workspace_id,
  name,
  harness,
  model,
  prompt,
  invocation_class,
  defaults_json,
  created_at,
  updated_at
FROM agent_profiles
WHERE workspace_id = ?
ORDER BY name ASC, profile_id ASC;
