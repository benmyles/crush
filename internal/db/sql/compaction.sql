-- name: CreateCompactionSummary :one
INSERT INTO compaction_summaries (
    id,
    session_id,
    parent_ids,
    covered_start,
    covered_end,
    kind,
    level,
    summary_text,
    layout,
    checkpoint,
    token_count,
    model_provider,
    model_id,
    reasoning,
    covered_message_ids,
    created_at
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
)
RETURNING *;

-- name: GetCompactionSummary :one
SELECT *
FROM compaction_summaries
WHERE id = ? LIMIT 1;

-- name: ListCompactionSummariesBySession :many
SELECT *
FROM compaction_summaries
WHERE session_id = ?
ORDER BY created_at ASC;

-- name: GetActiveCompactionSummary :one
SELECT s.*
FROM compaction_summaries s
JOIN sessions ses ON ses.id = s.session_id
WHERE ses.active_summary_id = s.id
  AND s.session_id = ?
LIMIT 1;

-- name: ListChildCompactionSummaries :many
SELECT *
FROM compaction_summaries
WHERE session_id = ?
  AND id IN (sqlc.arg(parent_ids))
ORDER BY created_at ASC;

-- name: DeleteCompactionSummary :exec
DELETE FROM compaction_summaries
WHERE id = ?;

-- name: CreateCompactionCausality :exec
INSERT INTO compaction_causality (
    summary_id,
    session_id,
    turn,
    tool_call_id,
    tool,
    args_hash,
    is_error,
    files_changed,
    created_at
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?
);

-- name: ListCompactionCausalityBySession :many
SELECT *
FROM compaction_causality
WHERE session_id = ?
ORDER BY turn ASC;

-- name: CreateCompactionFileRef :one
INSERT INTO compaction_file_refs (
    id,
    session_id,
    path,
    mime,
    token_count,
    exploration,
    first_seen_at
) VALUES (
    ?, ?, ?, ?, ?, ?, ?
)
RETURNING *;

-- name: GetCompactionFileRef :one
SELECT *
FROM compaction_file_refs
WHERE id = ? LIMIT 1;

-- name: GetCompactionFileRefByPath :one
SELECT *
FROM compaction_file_refs
WHERE session_id = ? AND path = ?
ORDER BY first_seen_at DESC
LIMIT 1;

-- name: CreateCompactionEmbedding :exec
INSERT INTO compaction_embeddings (
    message_id,
    summary_id,
    session_id,
    embedding,
    created_at
) VALUES (
    ?, ?, ?, ?, ?
)
ON CONFLICT(message_id) DO UPDATE SET
    summary_id = excluded.summary_id,
    embedding = excluded.embedding,
    created_at = excluded.created_at;

-- name: ListCompactionEmbeddingsBySession :many
SELECT *
FROM compaction_embeddings
WHERE session_id = ?
ORDER BY created_at ASC;

-- name: UpdateSessionCompaction :exec
UPDATE sessions
SET
    active_summary_id = ?,
    reserve_tokens = ?,
    keep_recent_tokens = ?
WHERE id = ?;

-- name: GetMessagesByCreatedRange :many
SELECT *
FROM messages
WHERE session_id = ?
  AND created_at >= ? AND created_at <= ?
ORDER BY created_at ASC;
