-- name: CreateOperationRecord :exec
INSERT INTO operation_records (
  operation_id,
  workspace_id,
  operation_type,
  actor,
  source,
  scope,
  request_summary,
  result,
  trust_decision,
  parent_operation_id,
  checkpoint_operation_id,
  rollback_point_id,
  rollback_data_json,
  payload_hash,
  payload_snapshot_json,
  created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetOperationRecord :one
SELECT operation_id, workspace_id, operation_type, actor, source, scope, request_summary, result, trust_decision, parent_operation_id, checkpoint_operation_id, rollback_point_id, rollback_data_json, payload_hash, payload_snapshot_json, created_at
FROM operation_records
WHERE operation_id = ?;

-- name: ListOperationRecords :many
SELECT operation_id, workspace_id, operation_type, actor, source, scope, request_summary, result, trust_decision, parent_operation_id, checkpoint_operation_id, rollback_point_id, rollback_data_json, payload_hash, payload_snapshot_json, created_at
FROM operation_records
ORDER BY created_at DESC, operation_id ASC;

-- name: ListOperationRecordsByWorkspace :many
SELECT operation_id, workspace_id, operation_type, actor, source, scope, request_summary, result, trust_decision, parent_operation_id, checkpoint_operation_id, rollback_point_id, rollback_data_json, payload_hash, payload_snapshot_json, created_at
FROM operation_records
WHERE workspace_id = ?
ORDER BY created_at DESC, operation_id ASC;
