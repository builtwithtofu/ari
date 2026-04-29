-- name: UpsertAuthSlot :exec
INSERT INTO auth_slots (
  auth_slot_id, harness, label, provider_label, credential_owner, status, metadata_json, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(auth_slot_id) DO UPDATE SET
  harness = excluded.harness,
  label = excluded.label,
  provider_label = excluded.provider_label,
  credential_owner = excluded.credential_owner,
  status = excluded.status,
  metadata_json = excluded.metadata_json,
  updated_at = excluded.updated_at;

-- name: GetAuthSlot :one
SELECT auth_slot_id, harness, label, provider_label, credential_owner, status, metadata_json, created_at, updated_at
FROM auth_slots
WHERE auth_slot_id = ?;

-- name: ListAuthSlots :many
SELECT auth_slot_id, harness, label, provider_label, credential_owner, status, metadata_json, created_at, updated_at
FROM auth_slots
ORDER BY harness ASC, label ASC, auth_slot_id ASC;

-- name: ListAuthSlotsByHarness :many
SELECT auth_slot_id, harness, label, provider_label, credential_owner, status, metadata_json, created_at, updated_at
FROM auth_slots
WHERE harness = ?
ORDER BY label ASC, auth_slot_id ASC;
