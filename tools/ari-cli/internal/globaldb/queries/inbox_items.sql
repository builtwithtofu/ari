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
  attention_required,
  summary,
  created_at,
  updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
  attention_required = excluded.attention_required,
  summary = excluded.summary,
  updated_at = excluded.updated_at;

-- name: GetInboxItem :one
SELECT
  i.inbox_item_id,
  i.workspace_id,
  i.source_session_id,
  i.workspace_event_id,
  i.event_type,
  i.fanout_group_id,
  i.fanout_member_id,
  i.worker_session_id,
  i.final_response_id,
  i.kind,
  COALESCE(rs.status, 'unread') AS status,
  i.attention_required,
  i.summary,
  i.created_at,
  COALESCE(rs.updated_at, i.updated_at) AS updated_at
FROM inbox_items i
LEFT JOIN inbox_item_read_states rs
  ON rs.workspace_id = i.workspace_id
 AND rs.source_session_id = i.source_session_id
 AND rs.inbox_item_id = i.inbox_item_id
WHERE i.inbox_item_id = ?;

-- name: ListInboxItemsBySession :many
SELECT
  i.inbox_item_id,
  i.workspace_id,
  i.source_session_id,
  i.workspace_event_id,
  i.event_type,
  i.fanout_group_id,
  i.fanout_member_id,
  i.worker_session_id,
  i.final_response_id,
  i.kind,
  COALESCE(rs.status, 'unread') AS status,
  i.attention_required,
  i.summary,
  i.created_at,
  COALESCE(rs.updated_at, i.updated_at) AS updated_at
FROM inbox_items i
LEFT JOIN inbox_item_read_states rs
  ON rs.workspace_id = i.workspace_id
 AND rs.source_session_id = i.source_session_id
 AND rs.inbox_item_id = i.inbox_item_id
WHERE i.workspace_id = ? AND i.source_session_id = ?
ORDER BY i.created_at DESC, i.inbox_item_id ASC;

-- name: CountInboxItemsBySession :one
SELECT
  COUNT(*) AS total_count,
  COALESCE(SUM(CASE WHEN COALESCE(rs.status, 'unread') = 'unread' THEN 1 ELSE 0 END), 0) AS unread_count,
  COALESCE(SUM(CASE WHEN COALESCE(rs.status, 'unread') = 'read' THEN 1 ELSE 0 END), 0) AS read_count
FROM inbox_items i
LEFT JOIN inbox_item_read_states rs
  ON rs.workspace_id = i.workspace_id
 AND rs.source_session_id = i.source_session_id
 AND rs.inbox_item_id = i.inbox_item_id
WHERE i.workspace_id = ? AND i.source_session_id = ?;

-- name: MarkInboxItemRead :execrows
INSERT INTO inbox_item_read_states (
  workspace_id,
  source_session_id,
  inbox_item_id,
  status,
  read_at,
  updated_at
)
SELECT i.workspace_id, i.source_session_id, i.inbox_item_id, 'read', ?, ?
FROM inbox_items i
WHERE i.workspace_id = ? AND i.source_session_id = ? AND i.inbox_item_id = ?
ON CONFLICT(workspace_id, source_session_id, inbox_item_id) DO UPDATE SET
  status = 'read',
  read_at = excluded.read_at,
  updated_at = excluded.updated_at
WHERE inbox_item_read_states.status != 'read';

-- name: DeleteInboxItemsByWorkspace :execrows
DELETE FROM inbox_items
WHERE workspace_id = ?;
