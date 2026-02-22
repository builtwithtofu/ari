-- name: GetDecision :one
SELECT * FROM decisions WHERE id = ? LIMIT 1;

-- name: ListDecisions :many
SELECT * FROM decisions ORDER BY created_at DESC;

-- name: CreateDecision :execresult
INSERT INTO decisions (id, title, content, context, consequences, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: UpdateDecision :execresult
UPDATE decisions
SET title = ?, content = ?, context = ?, consequences = ?, updated_at = ?
WHERE id = ?;

-- name: DeleteDecision :exec
DELETE FROM decisions WHERE id = ?;

-- name: GetPlan :one
SELECT * FROM plans WHERE id = ? LIMIT 1;

-- name: ListPlans :many
SELECT * FROM plans ORDER BY created_at DESC;

-- name: ListPlansByStatus :many
SELECT * FROM plans WHERE status = ? ORDER BY created_at DESC;

-- name: CreatePlan :execresult
INSERT INTO plans (id, title, status, content, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: UpdatePlan :execresult
UPDATE plans
SET title = ?, status = ?, content = ?, updated_at = ?
WHERE id = ?;

-- name: DeletePlan :exec
DELETE FROM plans WHERE id = ?;

-- name: GetKnowledge :one
SELECT * FROM knowledge WHERE id = ? LIMIT 1;

-- name: ListKnowledge :many
SELECT * FROM knowledge ORDER BY created_at DESC;

-- name: ListKnowledgeByType :many
SELECT * FROM knowledge WHERE type = ? ORDER BY created_at DESC;

-- name: CreateKnowledge :execresult
INSERT INTO knowledge (id, type, name, content, metadata, created_at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: UpdateKnowledge :execresult
UPDATE knowledge
SET type = ?, name = ?, content = ?, metadata = ?
WHERE id = ?;

-- name: DeleteKnowledge :exec
DELETE FROM knowledge WHERE id = ?;

-- name: GetRelationsFrom :many
SELECT * FROM knowledge_relations WHERE from_id = ?;

-- name: GetRelationsTo :many
SELECT * FROM knowledge_relations WHERE to_id = ?;

-- name: CreateRelation :execresult
INSERT INTO knowledge_relations (from_id, to_id, relation)
VALUES (?, ?, ?);

-- name: DeleteRelation :exec
DELETE FROM knowledge_relations WHERE from_id = ? AND to_id = ? AND relation = ?;
