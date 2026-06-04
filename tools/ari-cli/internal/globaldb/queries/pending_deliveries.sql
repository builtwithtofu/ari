-- name: CreatePendingDelivery :exec
INSERT INTO pending_deliveries (
  delivery_id,
  workspace_id,
  subscription_id,
  target_type,
  target_id,
  delivery_policy_json,
  event_ids_json,
  status,
  attempts,
  next_attempt_at,
  deadline_at,
  last_error,
  created_at,
  updated_at,
  terminal_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetPendingDelivery :one
SELECT delivery_id, workspace_id, subscription_id, target_type, target_id, delivery_policy_json, event_ids_json, status, attempts, next_attempt_at, deadline_at, last_error, created_at, updated_at, terminal_at
FROM pending_deliveries
WHERE delivery_id = ?;

-- name: ListDuePendingDeliveries :many
SELECT delivery_id, workspace_id, subscription_id, target_type, target_id, delivery_policy_json, event_ids_json, status, attempts, next_attempt_at, deadline_at, last_error, created_at, updated_at, terminal_at
FROM pending_deliveries
WHERE status = 'pending' AND next_attempt_at IS NOT NULL AND next_attempt_at <= ?
ORDER BY next_attempt_at ASC, created_at ASC, delivery_id ASC
LIMIT ?;

-- name: RecordPendingDeliveryAttempt :execrows
UPDATE pending_deliveries
SET attempts = attempts + 1,
    next_attempt_at = ?,
    last_error = ?,
    updated_at = ?
WHERE delivery_id = ? AND status = 'pending';

-- name: ClaimDuePendingDeliveryAttempt :execrows
UPDATE pending_deliveries
SET status = 'attempted',
    attempts = attempts + 1,
    next_attempt_at = NULL,
    last_error = '',
    updated_at = ?
WHERE delivery_id = ?
  AND status = 'pending'
  AND next_attempt_at IS NOT NULL
  AND next_attempt_at <= ?;

-- name: SchedulePendingDeliveryRetry :execrows
UPDATE pending_deliveries
SET status = 'pending',
    next_attempt_at = ?,
    last_error = ?,
    updated_at = ?
WHERE delivery_id = ? AND status = 'attempted';

-- name: CompletePendingDelivery :execrows
UPDATE pending_deliveries
SET status = 'completed',
    updated_at = ?,
    terminal_at = ?
WHERE delivery_id = ? AND status IN ('pending', 'attempted');

-- name: FailPendingDelivery :execrows
UPDATE pending_deliveries
SET status = 'failed',
    last_error = ?,
    updated_at = ?,
    terminal_at = ?
WHERE delivery_id = ? AND status IN ('pending', 'attempted');
