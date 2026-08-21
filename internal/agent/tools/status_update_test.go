package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"charm.land/fantasy"
	statuspkg "github.com/charmbracelet/crush/internal/status"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// newStatusTestStore opens an in-memory status store for tool tests.
func newStatusTestStore(t *testing.T) *statuspkg.Store {
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
	return statuspkg.NewStore(db)
}

// runStatusTool invokes the tool function directly with a session-scoped
// context, mirroring the agent's tool dispatch.
func runStatusTool(t *testing.T, tool fantasy.AgentTool, sessionID string, params StatusUpdateParams) (fantasy.ToolResponse, error) {
	t.Helper()
	input, err := json.Marshal(params)
	require.NoError(t, err)
	ctx := context.WithValue(context.Background(), SessionIDContextKey, sessionID)
	return tool.Run(ctx, fantasy.ToolCall{ID: "t1", Name: StatusUpdateToolName, Input: string(input)})
}

func TestStatusUpdateTool(t *testing.T) {
	t.Parallel()
	store := newStatusTestStore(t)

	var notified []statuspkg.Update
	tool := NewStatusUpdateTool(store, func(u statuspkg.Update) { notified = append(notified, u) })

	// Records the first update.
	resp, err := runStatusTool(t, tool, "sess-1", StatusUpdateParams{
		Done: "Added the caching layer", Doing: "Writing migration tests",
		Next: "Run the full suite", Blockers: "CI is down",
	})
	require.NoError(t, err)
	require.Contains(t, resp.Content, "Status update recorded")
	got, err := store.Get(context.Background(), "sess-1")
	require.NoError(t, err)
	require.True(t, got.Exists())
	require.Equal(t, "Added the caching layer", got.Done)
	require.Equal(t, "Writing migration tests", got.Doing)
	require.Equal(t, "Run the full suite", got.Next)
	require.Equal(t, "CI is down", got.Blockers)
	require.Len(t, notified, 1)
	require.Equal(t, "sess-1", notified[0].SessionID)

	// A later update replaces the previous one.
	_, err = runStatusTool(t, tool, "sess-1", StatusUpdateParams{
		Done: "Migration tests", Doing: "Fixing lint", Next: "Commit",
	})
	require.NoError(t, err)
	got, err = store.Get(context.Background(), "sess-1")
	require.NoError(t, err)
	require.Equal(t, "Migration tests", got.Done)
	require.Equal(t, "", got.Blockers)
	require.Len(t, notified, 2)

	// Missing session ID errors.
	_, err = tool.Run(context.Background(), fantasy.ToolCall{
		ID: "t2", Name: StatusUpdateToolName,
		Input: `{"done":"a","doing":"b","next":"c"}`,
	})
	require.Error(t, err)
}
