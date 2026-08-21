package status

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// newTestStore opens an in-memory status store for tests.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.ExecContext(context.Background(), `CREATE TABLE status_updates (
		session_id TEXT PRIMARY KEY,
		done TEXT NOT NULL,
		doing TEXT NOT NULL,
		next TEXT NOT NULL,
		blockers TEXT,
		updated_at INTEGER NOT NULL
	)`)
	require.NoError(t, err)
	return NewStore(db)
}

func TestStoreUpsertAndGet(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	// Reading an unknown session yields an empty update.
	got, err := store.Get(ctx, "sess-1")
	require.NoError(t, err)
	require.False(t, got.Exists())

	// The first upsert inserts the row.
	u, err := store.Upsert(ctx, "sess-1", "did a", "doing b", "next c", "blocked d")
	require.NoError(t, err)
	require.True(t, u.Exists())
	require.Equal(t, "did a", u.Done)
	require.Equal(t, "doing b", u.Doing)
	require.Equal(t, "next c", u.Next)
	require.Equal(t, "blocked d", u.Blockers)
	require.NotZero(t, u.UpdatedAt)

	// A second upsert replaces the row and clears blockers.
	u2, err := store.Upsert(ctx, "sess-1", "did a2", "doing b2", "next c2", "")
	require.NoError(t, err)
	require.True(t, u2.Exists())
	require.Equal(t, "did a2", u2.Done)
	require.Equal(t, "", u2.Blockers)

	got, err = store.Get(ctx, "sess-1")
	require.NoError(t, err)
	require.Equal(t, u2, got)
}

func TestStoreUpsertRequiresFields(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)

	_, err := store.Upsert(context.Background(), "sess-1", "", "doing", "next", "")
	require.Error(t, err)
	_, err = store.Upsert(context.Background(), "sess-1", "done", "", "next", "")
	require.Error(t, err)
	_, err = store.Upsert(context.Background(), "sess-1", "done", "doing", "", "")
	require.Error(t, err)
}

func TestNilStoreIsInert(t *testing.T) {
	t.Parallel()
	var store *Store

	got, err := store.Get(context.Background(), "sess-1")
	require.NoError(t, err)
	require.False(t, got.Exists())

	u, err := store.Upsert(context.Background(), "sess-1", "a", "b", "c", "")
	require.NoError(t, err)
	require.False(t, u.Exists())
}

func TestReminderPrompt(t *testing.T) {
	t.Parallel()
	p := ReminderPrompt()
	require.Contains(t, p, "[Status update]")
	require.Contains(t, p, "status_update")
	require.Contains(t, p, "up-to-date and accurate")
}
