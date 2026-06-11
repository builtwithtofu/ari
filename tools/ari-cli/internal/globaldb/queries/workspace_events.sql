-- name: NextWorkspaceEventSequence :one
INSERT INTO workspace_event_sequences (workspace_id, next_sequence)
VALUES (?, 1)
ON CONFLICT(workspace_id) DO UPDATE SET
  next_sequence = next_sequence + 1
RETURNING next_sequence;

-- name: CreateWorkspaceEvent :exec
INSERT INTO workspace_events (
  event_id,
  workspace_id,
  sequence,
  event_type,
  subject_type,
  subject_id,
  producer_type,
  producer_id,
  correlation_id,
  causation_id,
  payload_json,
  payload_ref_json,
  attention_required,
  created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListWorkspaceEventsAfterSequence :many
SELECT event_id, workspace_id, sequence, event_type, subject_type, subject_id, producer_type, producer_id, correlation_id, causation_id, payload_json, payload_ref_json, attention_required, created_at
FROM workspace_events
WHERE workspace_id = ? AND sequence > ?
ORDER BY sequence ASC
LIMIT ?;

-- name: GetWorkspaceEvent :one
SELECT event_id, workspace_id, sequence, event_type, subject_type, subject_id, producer_type, producer_id, correlation_id, causation_id, payload_json, payload_ref_json, attention_required, created_at
FROM workspace_events
WHERE event_id = ?;

-- name: CreateEventSubscription :exec
INSERT INTO event_subscriptions (
  subscription_id,
  workspace_id,
  owner_session_id,
  name,
  filter_json,
  delivery_target_type,
  delivery_target_id,
  delivery_policy_json,
  cursor_sequence,
  ack_sequence,
  status,
  completion_condition_json,
  timeout_at,
  created_at,
  updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetEventSubscription :one
SELECT subscription_id, workspace_id, owner_session_id, name, filter_json, delivery_target_type, delivery_target_id, delivery_policy_json, cursor_sequence, ack_sequence, status, completion_condition_json, timeout_at, created_at, updated_at
FROM event_subscriptions
WHERE subscription_id = ?;

-- name: ListActiveEventSubscriptionsByWorkspace :many
SELECT subscription_id, workspace_id, owner_session_id, name, filter_json, delivery_target_type, delivery_target_id, delivery_policy_json, cursor_sequence, ack_sequence, status, completion_condition_json, timeout_at, created_at, updated_at
FROM event_subscriptions
WHERE workspace_id = ? AND status = 'active'
ORDER BY created_at ASC, subscription_id ASC;

-- name: UpdateEventSubscriptionCursor :execrows
UPDATE event_subscriptions
SET cursor_sequence = CASE WHEN cursor_sequence < ? THEN ? ELSE cursor_sequence END,
    ack_sequence = CASE
      WHEN ack_sequence < ? THEN MIN(?, CASE WHEN cursor_sequence < ? THEN ? ELSE cursor_sequence END)
      ELSE ack_sequence
    END,
    updated_at = ?
WHERE subscription_id = ?;

-- name: CancelEventSubscription :execrows
UPDATE event_subscriptions
SET status = 'canceled',
    updated_at = ?
WHERE subscription_id = ? AND status != 'canceled';
