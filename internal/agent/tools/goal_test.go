package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/goal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// newGoalTestStore opens an in-memory goal store for tool tests.
func newGoalTestStore(t *testing.T) *goal.Store {
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
	return goal.NewStore(db)
}

// runGoalTool invokes the tool function directly with a session-scoped
// context, mirroring the agent's tool dispatch.
func runGoalTool(t *testing.T, tool fantasy.AgentTool, sessionID string, params any) (fantasy.ToolResponse, error) {
	t.Helper()
	input, err := json.Marshal(params)
	require.NoError(t, err)
	ctx := context.WithValue(context.Background(), SessionIDContextKey, sessionID)
	return tool.Run(ctx, fantasy.ToolCall{ID: "t1", Name: "goal-tool", Input: string(input)})
}

func TestUpdateGoalTool(t *testing.T) {
	t.Parallel()
	store := newGoalTestStore(t)

	var notified []goal.Goal
	tool := NewUpdateGoalTool(store, func(g goal.Goal) { notified = append(notified, g) })

	// Creates a goal when none exists.
	_, err := runGoalTool(t, tool, "sess-1", UpdateGoalParams{Text: "Ship the feature"})
	require.NoError(t, err)
	got, err := store.Get(context.Background(), "sess-1")
	require.NoError(t, err)
	assert.Equal(t, "Ship the feature", got.Text)
	assert.Equal(t, goal.StatusActive, got.Status)
	require.Len(t, notified, 1)
	assert.Equal(t, "sess-1", notified[0].SessionID)

	// Replaces the goal text; status stays active.
	_, err = runGoalTool(t, tool, "sess-1", UpdateGoalParams{Text: "Ship the bigger feature"})
	require.NoError(t, err)
	got, err = store.Get(context.Background(), "sess-1")
	require.NoError(t, err)
	assert.Equal(t, "Ship the bigger feature", got.Text)
	require.Len(t, notified, 2)

	// Missing session ID errors.
	ctx := context.Background()
	_, err = tool.Run(ctx, fantasy.ToolCall{ID: "t2", Name: "goal-tool", Input: `{"text":"x"}`})
	require.Error(t, err)
}

func TestCompleteGoalTool(t *testing.T) {
	t.Parallel()
	store := newGoalTestStore(t)
	_, err := store.Set(context.Background(), "sess-1", "Goal")
	require.NoError(t, err)

	var notified []goal.Goal
	tool := NewCompleteGoalTool(store, func(g goal.Goal) { notified = append(notified, g) })

	_, err = runGoalTool(t, tool, "sess-1", CompleteGoalParams{Summary: "All tests pass"})
	require.NoError(t, err)
	got, err := store.Get(context.Background(), "sess-1")
	require.NoError(t, err)
	assert.Equal(t, goal.StatusComplete, got.Status)
	assert.Equal(t, "All tests pass", got.CompleteReason)
	require.Len(t, notified, 1)
	assert.Equal(t, goal.StatusComplete, notified[0].Status)

	// No goal: a friendly text response instead of an error, so the
	// model gets guidance rather than a failed tool call.
	resp, err := runGoalTool(t, tool, "sess-none", CompleteGoalParams{Summary: "done"})
	require.NoError(t, err)
	assert.Contains(t, resp.Content, "No goal is active")
	assert.Len(t, notified, 1)
}

func TestBlockGoalTool(t *testing.T) {
	t.Parallel()
	store := newGoalTestStore(t)
	_, err := store.Set(context.Background(), "sess-1", "Goal")
	require.NoError(t, err)

	var notified []goal.Goal
	tool := NewBlockGoalTool(store, func(g goal.Goal) { notified = append(notified, g) })

	_, err = runGoalTool(t, tool, "sess-1", BlockGoalParams{Reason: "awaiting credentials"})
	require.NoError(t, err)
	got, err := store.Get(context.Background(), "sess-1")
	require.NoError(t, err)
	assert.Equal(t, goal.StatusBlocked, got.Status)
	assert.Equal(t, "awaiting credentials", got.BlockedReason)
	require.Len(t, notified, 1)
	assert.Equal(t, goal.StatusBlocked, notified[0].Status)

	// No goal: a friendly text response instead of an error.
	resp, err := runGoalTool(t, tool, "sess-none", BlockGoalParams{Reason: "x"})
	require.NoError(t, err)
	assert.Contains(t, resp.Content, "No goal is active")
	assert.Len(t, notified, 1)
}
