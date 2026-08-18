-- +goose Up
-- +goose StatementBegin
-- Compaction summary DAG: leaf summaries (direct summary of a message span) and
-- condensed summaries (summary of summaries), with provenance back to the raw
-- messages they cover. Raw messages are never mutated; summary nodes are derived
-- views over the immutable message store.
CREATE TABLE IF NOT EXISTS compaction_summaries (
    id               TEXT PRIMARY KEY,
    session_id       TEXT NOT NULL,
    parent_ids       TEXT NOT NULL DEFAULT '[]',   -- JSON array of compaction_summaries.id (DAG parents)
    covered_start    INTEGER,                       -- absolute ordinal of the first compacted raw message (session-relative)
    covered_end      INTEGER,                       -- absolute ordinal of the last compacted raw message (session-relative)
    first_retained_message_id TEXT,                 -- id of the first raw message retained after compaction (anchor for ActiveContext)
    kind             TEXT NOT NULL CHECK (kind IN ('leaf', 'condensed')),
    level            INTEGER NOT NULL DEFAULT 0,    -- escalation level that produced it (0=preserve_details,1=bullet_points,2=deterministic)
    summary_text     TEXT NOT NULL,
    layout           TEXT NOT NULL DEFAULT '{}',    -- JSON char offsets per composed part
    checkpoint       TEXT,                          -- isolated structured checkpoint text
    token_count      INTEGER NOT NULL DEFAULT 0,
    model_provider   TEXT,
    model_id         TEXT,
    reasoning        TEXT,
    covered_message_ids TEXT NOT NULL DEFAULT '[]', -- JSON: raw message ids this summary replaces
    created_at       INTEGER NOT NULL,
    FOREIGN KEY (session_id) REFERENCES sessions (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_compaction_summaries_session ON compaction_summaries (session_id, created_at);

-- Causality graph: action -> result -> state-changed edges, extracted
-- deterministically (no model). Surfaces structured memory that
-- similarity-only retrieval misses.
CREATE TABLE IF NOT EXISTS compaction_causality (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    summary_id    TEXT,
    session_id    TEXT NOT NULL,
    turn          INTEGER NOT NULL,
    tool_call_id  TEXT,
    tool          TEXT NOT NULL,
    args_hash     TEXT,
    is_error      INTEGER NOT NULL DEFAULT 0,
    files_changed TEXT NOT NULL DEFAULT '[]',        -- JSON array of paths
    created_at    INTEGER NOT NULL,
    FOREIGN KEY (summary_id) REFERENCES compaction_summaries (id) ON DELETE SET NULL,
    FOREIGN KEY (session_id) REFERENCES sessions (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_compaction_causality_session ON compaction_causality (session_id, turn);
CREATE INDEX IF NOT EXISTS idx_compaction_causality_tool ON compaction_causality (tool);

-- Full-text search over the immutable message store for recall_grep.
-- content_rowid ties the FTS table to messages.rowid so searches return the
-- stable physical row id used by the exact-recovery index. External-content
-- FTS5 is kept in sync by the triggers below so recall_grep sees every
-- message without a separate indexing step.
-- The FTS column MUST be named `parts` to match the external-content table's
-- column, otherwise FTS5 reads the content table and errors with
-- `no such column: T.parts`. `content_rowid='rowid'` ties FTS rows to the
-- physical message row id used by the exact-recovery index.
CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
    parts,
    content='messages',
    content_rowid='rowid',
    tokenize='unicode61'
);

-- Backfill the index for messages that predate the FTS table. The
-- 'rebuild' command re-reads the content table and populates every row
-- atomically; it is the correct way to sync an external-content FTS table
-- after creation.
INSERT INTO messages_fts(messages_fts) VALUES ('rebuild');

CREATE TRIGGER IF NOT EXISTS messages_fts_ai AFTER INSERT ON messages BEGIN
    INSERT INTO messages_fts(rowid, parts) VALUES (new.rowid, new.parts);
END;
CREATE TRIGGER IF NOT EXISTS messages_fts_ad AFTER DELETE ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, rowid, parts) VALUES ('delete', old.rowid, old.parts);
END;
CREATE TRIGGER IF NOT EXISTS messages_fts_au AFTER UPDATE ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, rowid, parts) VALUES ('delete', old.rowid, old.parts);
    INSERT INTO messages_fts(rowid, parts) VALUES (new.rowid, new.parts);
END;

-- The session row points at the DAG node currently in the active context.
ALTER TABLE sessions ADD COLUMN active_summary_id TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS messages_fts_au;
DROP TRIGGER IF EXISTS messages_fts_ad;
DROP TRIGGER IF EXISTS messages_fts_ai;
DROP TABLE IF EXISTS messages_fts;

ALTER TABLE sessions DROP COLUMN active_summary_id;

DROP INDEX IF EXISTS idx_compaction_causality_tool;
DROP INDEX IF EXISTS idx_compaction_causality_session;
DROP TABLE IF EXISTS compaction_causality;

DROP INDEX IF EXISTS idx_compaction_summaries_session;
DROP TABLE IF EXISTS compaction_summaries;
-- +goose StatementEnd
