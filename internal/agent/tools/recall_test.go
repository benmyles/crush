package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/db"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite" //nolint:revive
)

func fantasyToolCall(name, input string) fantasy.ToolCall {
	return fantasy.ToolCall{ID: "test-call", Name: name, Input: input}
}

func newRecallTestDB(t *testing.T) (*sql.DB, *db.Queries) {
	t.Helper()
	dataDir := t.TempDir()
	conn, err := db.Connect(context.Background(), dataDir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Release(dataDir) })
	q := db.New(conn)
	return conn, q
}

func addRecallMessage(t *testing.T, q *db.Queries, sessionID, id, role string, partsJSON string) {
	t.Helper()
	_, err := q.CreateMessage(context.Background(), db.CreateMessageParams{
		ID:        id,
		SessionID: sessionID,
		Role:      role,
		Parts:     partsJSON,
	})
	require.NoError(t, err)
}

func TestSnippetFromParts_DecodesWrappedParts(t *testing.T) {
	t.Parallel()
	// Crush wraps parts as {"type":"text","data":{"text":"..."}}.
	parts := `[{"type":"text","data":{"text":"find this text"}},{"type":"tool_call","data":{"name":"bash","input":"{\"cmd\":\"ls\"}"}}]`
	got := snippetFromParts(parts, 200)
	require.Contains(t, got, "find this text")
	require.Contains(t, got, "tool_call: bash")
}

func TestSnippetFromParts_Empty(t *testing.T) {
	t.Parallel()
	require.Empty(t, snippetFromParts("[]", 200))
	require.Empty(t, snippetFromParts("not json", 200))
}

func TestRecallGrep_FindsTextAndGroupsBySummary(t *testing.T) {
	t.Parallel()
	conn, q := newRecallTestDB(t)
	ctx := context.Background()

	_, err := q.CreateSession(ctx, db.CreateSessionParams{ID: "s1", Title: "test"})
	require.NoError(t, err)

	// Insert a message with wrapped parts containing searchable text.
	addRecallMessage(t, q, "s1", "m1", "user",
		`[{"type":"text","data":{"text":"add JWT middleware to src/auth.go"}}]`)
	addRecallMessage(t, q, "s1", "m2", "assistant",
		`[{"type":"text","data":{"text":"done"}}]`)

	// Create a summary covering m1 so the grouping index maps m1 -> sum1.
	_, err = q.CreateCompactionSummary(ctx, db.CreateCompactionSummaryParams{
		ID:                "sum1",
		SessionID:         "s1",
		ParentIds:         "[]",
		Kind:              "leaf",
		SummaryText:       "summary",
		Layout:            "{}",
		CoveredMessageIds: `["m1"]`,
		CreatedAt:         100,
	})
	require.NoError(t, err)
	require.NoError(t, q.UpdateSessionCompaction(ctx, db.UpdateSessionCompactionParams{
		ActiveSummaryID: sql.NullString{String: "sum1", Valid: true},
		ID:              "s1",
	}))

	tool := NewRecallGrepTool(conn, q)
	input, err := json.Marshal(RecallGrepParams{Pattern: "auth.go", SessionID: "s1"})
	require.NoError(t, err)
	resp, err := tool.Run(ctx, fantasyToolCall(RecallGrepToolName, string(input)))
	require.NoError(t, err)
	require.Contains(t, resp.Content, "add JWT middleware")
	require.Contains(t, resp.Content, "covered by summary sum1")
	// The seq anchor is the session-absolute ordinal (m1 is the first
	// non-summary message), matching the ledger/recovery-note numbering.
	require.Contains(t, resp.Content, "[seq 1] m1")

	// The session id defaults to the tool-call context when omitted.
	ctxWithSession := context.WithValue(ctx, SessionIDContextKey, "s1")
	input, err = json.Marshal(RecallGrepParams{Pattern: "done"})
	require.NoError(t, err)
	resp, err = tool.Run(ctxWithSession, fantasyToolCall(RecallGrepToolName, string(input)))
	require.NoError(t, err)
	require.Contains(t, resp.Content, "[seq 2] m2")
}

func TestRecallGrep_FTSQuotingHandlesPunctuation(t *testing.T) {
	t.Parallel()
	conn, q := newRecallTestDB(t)
	ctx := context.Background()
	_, err := q.CreateSession(ctx, db.CreateSessionParams{ID: "s2", Title: "test"})
	require.NoError(t, err)
	addRecallMessage(t, q, "s2", "m1", "user",
		`[{"type":"text","data":{"text":"edit internal/x/y.go"}}]`)

	tool := NewRecallGrepTool(conn, q)
	// A path with slashes/dots would break raw FTS5; quoting + LIKE fallback
	// must still find it.
	input, err := json.Marshal(RecallGrepParams{Pattern: "internal/x/y.go", SessionID: "s2"})
	require.NoError(t, err)
	resp, err := tool.Run(ctx, fantasyToolCall(RecallGrepToolName, string(input)))
	require.NoError(t, err)
	require.Contains(t, resp.Content, "edit internal/x/y.go")
}

func TestRecallDescribe_ReturnsMetadata(t *testing.T) {
	t.Parallel()
	_, q := newRecallTestDB(t)
	ctx := context.Background()
	_, err := q.CreateSession(ctx, db.CreateSessionParams{ID: "s3", Title: "test"})
	require.NoError(t, err)
	_, err = q.CreateCompactionSummary(ctx, db.CreateCompactionSummaryParams{
		ID:                "sumD",
		SessionID:         "s3",
		ParentIds:         "[]",
		Kind:              "leaf",
		SummaryText:       "checkpoint text here",
		Layout:            "{}",
		Checkpoint:        sql.NullString{String: "checkpoint text here", Valid: true},
		CoveredMessageIds: `["m1"]`,
		CreatedAt:         100,
	})
	require.NoError(t, err)

	tool := NewRecallDescribeTool(q)
	input, err := json.Marshal(RecallDescribeParams{ID: "sumD"})
	require.NoError(t, err)
	resp, err := tool.Run(ctx, fantasyToolCall(RecallDescribeToolName, string(input)))
	require.NoError(t, err)
	require.Contains(t, resp.Content, "sumD")
	require.Contains(t, resp.Content, "checkpoint text here")
}
