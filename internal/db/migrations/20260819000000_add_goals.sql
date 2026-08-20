-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS goals (
    session_id TEXT PRIMARY KEY,
    text TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    complete_reason TEXT,
    blocked_reason TEXT,
    consecutive_prods INTEGER NOT NULL DEFAULT 0,
    total_prods INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS goals;
-- +goose StatementEnd
