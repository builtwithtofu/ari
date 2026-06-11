-- name: CreateSecretAuditEvent :exec
INSERT INTO secret_audit_events (
  event_id,
  event_type,
  subject_type,
  subject_id,
  payload_json,
  created_at
) VALUES (?, ?, ?, ?, ?, ?);

-- name: ListSecretAuditEvents :many
SELECT event_id, event_type, subject_type, subject_id, payload_json, created_at
FROM secret_audit_events
ORDER BY created_at ASC, event_id ASC
LIMIT ?;
