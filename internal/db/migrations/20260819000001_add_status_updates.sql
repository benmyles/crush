-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS status_updates (
    session_id TEXT PRIMARY KEY,
    done TEXT NOT NULL,
    doing TEXT NOT NULL,
    next TEXT NOT NULL,
    blockers TEXT,
    updated_at INTEGER NOT NULL,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS status_updates;
-- +goose StatementEnd
