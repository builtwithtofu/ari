-- name: UpsertSecretGrant :exec
INSERT INTO ari_secret_grants (
  grant_id, secret_id, subject_type, subject_id, purpose, created_at, expires_at
) VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(secret_id, subject_type, subject_id, purpose) DO UPDATE SET
  expires_at = excluded.expires_at;

-- name: GetSecretGrant :one
SELECT grant_id, secret_id, subject_type, subject_id, purpose, created_at, expires_at
FROM ari_secret_grants
WHERE grant_id = ?;

-- name: ListSecretGrantsBySubject :many
SELECT grant_id, secret_id, subject_type, subject_id, purpose, created_at, expires_at
FROM ari_secret_grants
WHERE subject_type = ? AND subject_id = ?
ORDER BY created_at ASC, grant_id ASC;

-- name: CountActiveSecretGrants :one
SELECT COUNT(*)
FROM ari_secret_grants
WHERE secret_id = ?
  AND subject_type = ?
  AND subject_id = ?
  AND purpose = ?
  AND (expires_at IS NULL OR expires_at > ?);

-- name: DeleteSecretGrant :execrows
DELETE FROM ari_secret_grants
WHERE grant_id = ?;

-- name: DeleteSecretGrantsBySecret :execrows
DELETE FROM ari_secret_grants
WHERE secret_id = ?;
