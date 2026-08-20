package goal

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// newTestStore opens an in-memory SQLite database with the goals table.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.ExecContext(context.Background(), `CREATE TABLE goals (
		session_id TEXT PRIMARY KEY,
		text TEXT NOT NULL,
		status TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		complete_reason TEXT,
		blocked_reason TEXT,
		consecutive_prods INTEGER NOT NULL DEFAULT 0,
		total_prods INTEGER NOT NULL DEFAULT 0
	)`)
	require.NoError(t, err)
	return NewStore(db)
}

func TestStoreSetGet(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	// No goal initially: zero-value goal, no error.
	g, err := store.Get(ctx, "sess-1")
	require.NoError(t, err)
	assert.Equal(t, Goal{}, g)
	assert.False(t, g.Exists())

	// Set persists an active goal.
	g, err = store.Set(ctx, "sess-1", "Make every test pass")
	require.NoError(t, err)
	assert.Equal(t, "sess-1", g.SessionID)
	assert.Equal(t, "Make every test pass", g.Text)
	assert.Equal(t, StatusActive, g.Status)
	assert.True(t, g.Active())
	assert.NotZero(t, g.CreatedAt)
	assert.NotZero(t, g.UpdatedAt)

	got, err := store.Get(ctx, "sess-1")
	require.NoError(t, err)
	assert.Equal(t, g, got)

	// Set on an existing goal replaces text and reactivates it.
	blocked, err := store.Block(ctx, "sess-1", "missing credentials")
	require.NoError(t, err)
	assert.Equal(t, StatusBlocked, blocked.Status)

	reactivated, err := store.Set(ctx, "sess-1", "A different goal")
	require.NoError(t, err)
	assert.Equal(t, StatusActive, reactivated.Status)
	assert.Equal(t, "A different goal", reactivated.Text)
	assert.LessOrEqual(t, blocked.UpdatedAt, reactivated.UpdatedAt)
}

func TestStoreLifecycleTransitions(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	_, err := store.Set(ctx, "sess-1", "Ship it")
	require.NoError(t, err)

	// Complete carries a summary.
	complete, err := store.Complete(ctx, "sess-1", "All tests pass, feature merged")
	require.NoError(t, err)
	assert.Equal(t, StatusComplete, complete.Status)
	assert.Equal(t, "All tests pass, feature merged", complete.CompleteReason)
	assert.True(t, complete.IsTerminal())

	// Block carries a reason.
	_, err = store.Set(ctx, "sess-2", "Ship it again")
	require.NoError(t, err)
	blocked, err := store.Block(ctx, "sess-2", "awaiting API access")
	require.NoError(t, err)
	assert.Equal(t, StatusBlocked, blocked.Status)
	assert.Equal(t, "awaiting API access", blocked.BlockedReason)
	assert.True(t, blocked.IsTerminal())

	// Resume reactivates a blocked goal and resets the prod counter.
	if _, err := store.BumpProd(ctx, "sess-2"); err != nil {
		t.Fatal(err)
	}
	resumed, err := store.Resume(ctx, "sess-2")
	require.NoError(t, err)
	assert.Equal(t, StatusActive, resumed.Status)
	assert.Zero(t, resumed.ConsecutiveProds)
	assert.Equal(t, "", resumed.BlockedReason)

	// Stall marks the cap.
	_, err = store.BumpProd(ctx, "sess-2")
	require.NoError(t, err)
	assert.Equal(t, 1, mustGet(t, store, ctx, "sess-2").ConsecutiveProds)
	stalled, err := store.Stall(ctx, "sess-2")
	require.NoError(t, err)
	assert.Equal(t, StatusStalled, stalled.Status)
	assert.True(t, stalled.IsTerminal())
}

func mustGet(t *testing.T, store *Store, ctx context.Context, sessionID string) Goal {
	t.Helper()
	g, err := store.Get(ctx, sessionID)
	require.NoError(t, err)
	return g
}

func TestStoreClearAndUpdate(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	_, err := store.Set(ctx, "sess-1", "Goal")
	require.NoError(t, err)

	// Update rewrites the text and keeps the goal active.
	updated, err := store.Update(ctx, "sess-1", "Bigger goal")
	require.NoError(t, err)
	assert.Equal(t, "Bigger goal", updated.Text)
	assert.Equal(t, StatusActive, updated.Status)

	// Clear removes the row entirely.
	require.NoError(t, store.Clear(ctx, "sess-1"))
	got, err := store.Get(ctx, "sess-1")
	require.NoError(t, err)
	assert.False(t, got.Exists())

	// Operations on a cleared goal degrade to ErrNoGoal; clear itself is
	// idempotent.
	_, err = store.Block(ctx, "sess-1", "reason")
	assert.ErrorIs(t, err, ErrNoGoal)
	require.NoError(t, store.Clear(ctx, "sess-1"))
}

func TestStoreBumpProd(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	_, err := store.Set(ctx, "sess-1", "Goal")
	require.NoError(t, err)

	for i := 1; i <= 3; i++ {
		g, err := store.BumpProd(ctx, "sess-1")
		require.NoError(t, err)
		assert.Equal(t, i, g.ConsecutiveProds)
		assert.Equal(t, i, g.TotalProds)
	}

	// ResetProd clears only the consecutive counter.
	require.NoError(t, store.ResetProd(ctx, "sess-1"))
	g, err := store.Get(ctx, "sess-1")
	require.NoError(t, err)
	assert.Zero(t, g.ConsecutiveProds)
	assert.Equal(t, 3, g.TotalProds)
}

func TestStoreNilStore(t *testing.T) {
	t.Parallel()
	store := NewStore(nil)
	ctx := context.Background()

	got, err := store.Get(ctx, "sess-1")
	require.NoError(t, err)
	assert.False(t, got.Exists())

	_, err = store.Set(ctx, "sess-1", "x")
	assert.ErrorIs(t, err, ErrNoGoal)
	_, err = store.Complete(ctx, "sess-1", "done")
	assert.ErrorIs(t, err, ErrNoGoal)
	// Clear and ResetProd are inert on nil stores.
	require.NoError(t, store.Clear(ctx, "sess-1"))
	require.NoError(t, store.ResetProd(ctx, "sess-1"))
}

func TestCheckPrompt(t *testing.T) {
	t.Parallel()
	prompt := CheckPrompt("Make every test pass", 1)
	assert.Contains(t, prompt, "Make every test pass")
	assert.Contains(t, prompt, "Goal check #1")
	assert.NotContains(t, prompt, "consecutive")

	prompt2 := CheckPrompt("Make every test pass", 2)
	assert.Contains(t, prompt2, "Goal check #2")
	assert.Contains(t, prompt2, "consecutive")
}

func TestGoalHelpers(t *testing.T) {
	t.Parallel()
	assert.False(t, (Goal{}).Exists())
	assert.False(t, (Goal{}).Active())
	assert.False(t, (Goal{}).IsTerminal())

	assert.True(t, Goal{SessionID: "s", Status: StatusActive}.Active())
	assert.True(t, Goal{SessionID: "s", Status: StatusActive}.Exists())
	assert.False(t, Goal{SessionID: "s", Status: StatusActive}.IsTerminal())

	for _, s := range []Status{StatusComplete, StatusBlocked, StatusStalled} {
		assert.True(t, Goal{SessionID: "s", Status: s}.IsTerminal(), s)
		assert.False(t, Goal{SessionID: "s", Status: s}.Active(), s)
	}
}

var _ = time.Now
