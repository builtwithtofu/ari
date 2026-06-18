-- name: NextTimelineItemSequence :one
SELECT CAST(COALESCE(MAX(sequence), 0) + 1 AS INTEGER) AS next_sequence
FROM timeline_items
WHERE workspace_id = ?;

-- name: CreateTimelineItem :execrows
INSERT INTO timeline_items (
  workspace_id,
  timeline_item_id,
  workspace_event_id,
  sequence,
  run_id,
  session_id,
  source_kind,
  source_id,
  kind,
  status,
  text,
  metadata_json,
  created_at,
  updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(workspace_id, timeline_item_id) DO NOTHING;

-- name: UpdateTimelineItem :execrows
UPDATE timeline_items
SET workspace_event_id = ?,
    run_id = ?,
    session_id = ?,
    source_kind = ?,
    source_id = ?,
    kind = ?,
    status = ?,
    text = ?,
    metadata_json = ?,
    created_at = ?,
    updated_at = ?
WHERE workspace_id = ? AND timeline_item_id = ?;

-- name: GetTimelineItem :one
SELECT workspace_id, timeline_item_id, workspace_event_id, sequence, run_id, session_id, source_kind, source_id, kind, status, text, metadata_json, created_at, updated_at
FROM timeline_items
WHERE workspace_id = ? AND timeline_item_id = ?;

-- name: ListTimelineItemsByWorkspace :many
SELECT workspace_id, timeline_item_id, workspace_event_id, sequence, run_id, session_id, source_kind, source_id, kind, status, text, metadata_json, created_at, updated_at
FROM timeline_items
WHERE workspace_id = ?
ORDER BY sequence ASC, timeline_item_id ASC;

-- name: DeleteTimelineItemsByWorkspace :execrows
DELETE FROM timeline_items
WHERE workspace_id = ?;
