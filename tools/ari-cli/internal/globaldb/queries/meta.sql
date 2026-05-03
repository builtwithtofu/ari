-- name: UpsertMeta :exec
INSERT INTO daemon_meta (key, value)
VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET
  value = excluded.value;

-- name: GetMetaValue :one
SELECT value FROM daemon_meta WHERE key = ?;

-- name: CompareAndSwapMeta :execrows
UPDATE daemon_meta
SET value = ?
WHERE key = ?
  AND value = ?;
