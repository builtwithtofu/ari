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
SELECT pd.delivery_id, pd.workspace_id, pd.subscription_id, pd.target_type, pd.target_id, pd.delivery_policy_json, pd.event_ids_json, pd.status, pd.attempts, pd.next_attempt_at, pd.deadline_at, pd.last_error, pd.created_at, pd.updated_at, pd.terminal_at
FROM pending_deliveries pd
JOIN event_subscriptions es ON es.subscription_id = pd.subscription_id
WHERE pd.status = 'pending'
  AND es.status = 'active'
  AND pd.next_attempt_at IS NOT NULL
  AND pd.next_attempt_at <= ?
  AND (pd.deadline_at IS NULL OR pd.deadline_at > ?)
  AND (es.timeout_at IS NULL OR es.timeout_at > ?)
ORDER BY pd.next_attempt_at ASC, pd.created_at ASC, pd.delivery_id ASC
LIMIT ?;

-- name: ListDuePendingDeliveriesForScope :many
SELECT pd.delivery_id, pd.workspace_id, pd.subscription_id, pd.target_type, pd.target_id, pd.delivery_policy_json, pd.event_ids_json, pd.status, pd.attempts, pd.next_attempt_at, pd.deadline_at, pd.last_error, pd.created_at, pd.updated_at, pd.terminal_at
FROM pending_deliveries pd
JOIN event_subscriptions es ON es.subscription_id = pd.subscription_id
WHERE pd.status = 'pending'
  AND pd.workspace_id = ?
  AND es.status = 'active'
  AND (es.owner_session_id = '' OR es.owner_session_id = ?)
  AND pd.next_attempt_at IS NOT NULL
  AND pd.next_attempt_at <= ?
  AND (pd.deadline_at IS NULL OR pd.deadline_at > ?)
  AND (es.timeout_at IS NULL OR es.timeout_at > ?)
ORDER BY pd.next_attempt_at ASC, pd.created_at ASC, pd.delivery_id ASC
LIMIT ?;

-- name: ListExpiredPendingDeliveries :many
SELECT delivery_id, workspace_id, subscription_id, target_type, target_id, delivery_policy_json, event_ids_json, status, attempts, next_attempt_at, deadline_at, last_error, created_at, updated_at, terminal_at
FROM pending_deliveries
WHERE status = 'pending'
  AND deadline_at IS NOT NULL
  AND deadline_at <= ?
ORDER BY deadline_at ASC, created_at ASC, delivery_id ASC;

-- name: RequeueStaleAttemptedPendingDeliveries :execrows
UPDATE pending_deliveries
SET status = 'pending',
    next_attempt_at = ?,
    last_error = 'delivery attempt interrupted before completion',
    updated_at = ?
WHERE status = 'attempted'
  AND terminal_at IS NULL
  AND updated_at <= ?;

-- name: ListPendingDeliveriesForSubscription :many
SELECT delivery_id, workspace_id, subscription_id, target_type, target_id, delivery_policy_json, event_ids_json, status, attempts, next_attempt_at, deadline_at, last_error, created_at, updated_at, terminal_at
FROM pending_deliveries
WHERE subscription_id = ? AND status IN ('pending', 'attempted')
ORDER BY created_at ASC, delivery_id ASC;

-- name: ListCompletedPendingDeliveriesForSubscription :many
SELECT delivery_id, workspace_id, subscription_id, target_type, target_id, delivery_policy_json, event_ids_json, status, attempts, next_attempt_at, deadline_at, last_error, created_at, updated_at, terminal_at
FROM pending_deliveries
WHERE subscription_id = ? AND status = 'completed'
ORDER BY terminal_at ASC, created_at ASC, delivery_id ASC;

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
  AND next_attempt_at <= ?
  AND (deadline_at IS NULL OR deadline_at > ?)
  AND EXISTS (
    SELECT 1
    FROM event_subscriptions es
    WHERE es.subscription_id = pending_deliveries.subscription_id
      AND es.status = 'active'
  );

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
