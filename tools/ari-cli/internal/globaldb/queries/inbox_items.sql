-- name: CreateInboxItem :exec
INSERT INTO inbox_items (
  inbox_item_id,
  workspace_id,
  source_session_id,
  workspace_event_id,
  event_type,
  fanout_group_id,
  fanout_member_id,
  worker_session_id,
  final_response_id,
  kind,
  status,
  attention_required,
  summary,
  created_at,
  updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(inbox_item_id) DO UPDATE SET
  workspace_id = excluded.workspace_id,
  source_session_id = excluded.source_session_id,
  workspace_event_id = excluded.workspace_event_id,
  event_type = excluded.event_type,
  fanout_group_id = excluded.fanout_group_id,
  fanout_member_id = excluded.fanout_member_id,
  worker_session_id = excluded.worker_session_id,
  final_response_id = excluded.final_response_id,
  kind = excluded.kind,
  status = inbox_items.status,
  attention_required = excluded.attention_required,
  summary = excluded.summary,
  updated_at = excluded.updated_at;

-- name: GetInboxItem :one
SELECT inbox_item_id, workspace_id, source_session_id, workspace_event_id, event_type, fanout_group_id, fanout_member_id, worker_session_id, final_response_id, kind, status, attention_required, summary, created_at, updated_at
FROM inbox_items
WHERE inbox_item_id = ?;

-- name: ListInboxItemsBySession :many
SELECT inbox_item_id, workspace_id, source_session_id, workspace_event_id, event_type, fanout_group_id, fanout_member_id, worker_session_id, final_response_id, kind, status, attention_required, summary, created_at, updated_at
FROM inbox_items
WHERE workspace_id = ? AND source_session_id = ?
ORDER BY created_at DESC, inbox_item_id ASC;

-- name: CountInboxItemsBySession :one
SELECT
  COUNT(*) AS total_count,
  COALESCE(SUM(CASE WHEN status = 'unread' THEN 1 ELSE 0 END), 0) AS unread_count,
  COALESCE(SUM(CASE WHEN status = 'read' THEN 1 ELSE 0 END), 0) AS read_count
FROM inbox_items
WHERE workspace_id = ? AND source_session_id = ?;

-- name: MarkInboxItemRead :execrows
UPDATE inbox_items
SET status = 'read',
    updated_at = ?
WHERE workspace_id = ? AND source_session_id = ? AND inbox_item_id = ? AND status != 'read';
