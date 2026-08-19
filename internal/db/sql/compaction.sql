-- name: CreateCompactionSummary :one
INSERT INTO compaction_summaries (
    id,
    session_id,
    parent_ids,
    covered_start,
    covered_end,
    first_retained_message_id,
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
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
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

-- name: DeleteCompactionSummariesBySession :exec
DELETE FROM compaction_summaries
WHERE session_id = ?;

-- name: DeleteCompactionCausalityBySession :exec
DELETE FROM compaction_causality
WHERE session_id = ?;

-- name: GetActiveCompactionSummary :one
SELECT s.*
FROM compaction_summaries s
JOIN sessions ses ON ses.id = s.session_id
WHERE ses.active_summary_id = s.id
  AND s.session_id = ?
LIMIT 1;

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

-- name: UpdateSessionCompaction :exec
UPDATE sessions
SET
    active_summary_id = ?
WHERE id = ?;
