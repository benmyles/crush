package db

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite" //nolint:revive
)

// TestMigration_FTSWorksOnPopulatedDB reproduces the review finding B1:
// the compaction migration's FTS5 table must build correctly even when
// the messages table already has rows (the normal upgrade path). The
// original migration named the FTS column `text` while the content table
// column is `parts`, so the rebuild read the content table and errored
// with `no such column: T.text` on any non-empty database.
func TestMigration_FTSWorksOnPopulatedDB(t *testing.T) {
	t.Cleanup(ResetPool)

	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "crush.db")

	// Open a raw connection and stand up the pre-migration schema:
	// sessions + messages, with one populated message row.
	conn, err := openDB(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	_, err = conn.ExecContext(context.Background(), `CREATE TABLE sessions (
		id TEXT PRIMARY KEY,
		parent_session_id TEXT,
		title TEXT NOT NULL,
		message_count INTEGER NOT NULL DEFAULT 0,
		prompt_tokens INTEGER NOT NULL DEFAULT 0,
		completion_tokens INTEGER NOT NULL DEFAULT 0,
		cost REAL NOT NULL DEFAULT 0.0,
		updated_at INTEGER NOT NULL,
		created_at INTEGER NOT NULL
	);`)
	require.NoError(t, err)
	_, err = conn.ExecContext(context.Background(), `CREATE TABLE messages (
		id TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		role TEXT NOT NULL,
		parts TEXT NOT NULL DEFAULT '[]',
		model TEXT,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		finished_at INTEGER,
		FOREIGN KEY (session_id) REFERENCES sessions (id) ON DELETE CASCADE
	);`)
	require.NoError(t, err)
	_, err = conn.ExecContext(context.Background(), `CREATE TABLE files (
		id TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		path TEXT NOT NULL,
		content TEXT NOT NULL,
		version INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		FOREIGN KEY (session_id) REFERENCES sessions (id) ON DELETE CASCADE,
		UNIQUE(path, session_id, version)
	);`)
	require.NoError(t, err)

	// Insert a session and a message with searchable text in `parts`.
	_, err = conn.ExecContext(context.Background(), `INSERT INTO sessions (id, title, updated_at, created_at) VALUES ('s1', 'pre', 1, 1);`)
	require.NoError(t, err)
	_, err = conn.ExecContext(context.Background(), `INSERT INTO messages (id, session_id, role, parts, created_at, updated_at) VALUES ('m1', 's1', 'user', '[{"type":"text","text":"findme_unique_token_in_parts"}]', 2, 2);`)
	require.NoError(t, err)

	// Run all migrations on top of the pre-existing schema. This is the
	// upgrade path a real user hits. SetBaseFS is already initialized in
	// package init; SetDialect creates the goose version table.
	if testing.Testing() {
		goose.SetLogger(goose.NopLogger())
	}
	require.NoError(t, goose.SetDialect("sqlite3"))
	err = goose.Up(conn, "migrations")
	require.NoError(t, err, "migration must succeed on a populated database")

	// The FTS index must have been built from the existing message and
	// MATCH must find it.
	var hit int
	err = conn.QueryRowContext(context.Background(),
		`SELECT count(*) FROM messages_fts WHERE messages_fts MATCH 'findme_unique_token_in_parts';`,
	).Scan(&hit)
	require.NoError(t, err, "FTS MATCH query must not error")
	require.Equal(t, 1, hit, "pre-migration message must be searchable via FTS after migration")

	// Inserting a new message after migration must keep FTS in sync.
	_, err = conn.ExecContext(context.Background(), `INSERT INTO messages (id, session_id, role, parts, created_at, updated_at) VALUES ('m2', 's1', 'assistant', '[{"type":"text","text":"post_migration_token"}]', 3, 3);`)
	require.NoError(t, err)
	err = conn.QueryRowContext(context.Background(),
		`SELECT count(*) FROM messages_fts WHERE messages_fts MATCH 'post_migration_token';`,
	).Scan(&hit)
	require.NoError(t, err)
	require.Equal(t, 1, hit, "post-migration insert must be indexed by the FTS trigger")
}
