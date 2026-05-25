-- name: UpsertSecretMetadata :exec
INSERT INTO ari_secrets (
  secret_id, name, purpose, scope, backend_kind, fingerprint, redacted_description, metadata_json, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(secret_id) DO UPDATE SET
  name = excluded.name,
  purpose = excluded.purpose,
  scope = excluded.scope,
  backend_kind = excluded.backend_kind,
  fingerprint = excluded.fingerprint,
  redacted_description = excluded.redacted_description,
  metadata_json = excluded.metadata_json,
  updated_at = excluded.updated_at;

-- name: GetSecretMetadata :one
SELECT secret_id, name, purpose, scope, backend_kind, fingerprint, redacted_description, metadata_json, created_at, updated_at
FROM ari_secrets
WHERE secret_id = ?;

-- name: ListSecretMetadata :many
SELECT secret_id, name, purpose, scope, backend_kind, fingerprint, redacted_description, metadata_json, created_at, updated_at
FROM ari_secrets
ORDER BY created_at ASC, secret_id ASC;

-- name: ListSecretMetadataByScope :many
SELECT secret_id, name, purpose, scope, backend_kind, fingerprint, redacted_description, metadata_json, created_at, updated_at
FROM ari_secrets
WHERE scope = ?
ORDER BY created_at ASC, secret_id ASC;

-- name: DeleteSecretMetadata :execrows
DELETE FROM ari_secrets
WHERE secret_id = ?;
