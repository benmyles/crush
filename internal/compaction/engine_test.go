package compaction

import (
	"context"
	"database/sql"
	"testing"

	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite" //nolint:revive
)

// newTestEngine opens a fresh migrated SQLite database and returns an Engine
// backed by the real db.Queries, plus the Queries for direct row insertion.
func newTestEngine(t *testing.T, completer Completer, opts ...EngineOption) (*Engine, *db.Queries) {
	t.Helper()
	dataDir := t.TempDir()
	conn, err := db.Connect(context.Background(), dataDir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Release(dataDir) })
	q := db.New(conn)
	return NewEngine(q, completer, opts...), q
}

func mkMsg(id string, role message.MessageRole, isSummary bool) message.Message {
	return message.Message{
		ID:               id,
		Role:             role,
		Parts:            []message.ContentPart{message.TextContent{Text: id + " body"}},
		IsSummaryMessage: isSummary,
	}
}

// TestActiveContext_SecondCompactionAnchorsByID reproduces review finding B2:
// after the second compaction, ActiveContext must retain only the tail after
// the latest summary's first_retained_message_id, not re-include the whole
// session (which caused immediate re-trigger loops). It must also exclude
// summary messages so the checkpoint is never duplicated in the prompt.
func TestActiveContext_SecondCompactionAnchorsByID(t *testing.T) {
	t.Parallel()
	e, q := newTestEngine(t, nil)
	ctx := context.Background()

	_, err := q.CreateSession(ctx, db.CreateSessionParams{ID: "sess1", Title: "test"})
	require.NoError(t, err)

	// Persist two summary DAG nodes. The second compaction retained m4..m6
	// (first_retained_message_id = m4), so ActiveContext must return only
	// the non-summary messages from m4 onward.
	_, err = q.CreateCompactionSummary(ctx, db.CreateCompactionSummaryParams{
		ID:                     "sum1",
		SessionID:              "sess1",
		ParentIds:              "[]",
		CoveredStart:           sql.NullInt64{Int64: 1, Valid: true},
		CoveredEnd:             sql.NullInt64{Int64: 3, Valid: true},
		FirstRetainedMessageID: sql.NullString{String: "m4", Valid: true},
		Kind:                   "leaf",
		SummaryText:            "first summary",
		Layout:                 "{}",
		CoveredMessageIds:      `["m1","m2","m3"]`,
		CreatedAt:              100,
	})
	require.NoError(t, err)
	_, err = q.CreateCompactionSummary(ctx, db.CreateCompactionSummaryParams{
		ID:                     "sum2",
		SessionID:              "sess1",
		ParentIds:              `["sum1"]`,
		CoveredStart:           sql.NullInt64{Int64: 1, Valid: true},
		CoveredEnd:             sql.NullInt64{Int64: 3, Valid: true},
		FirstRetainedMessageID: sql.NullString{String: "m4", Valid: true},
		Kind:                   "leaf",
		SummaryText:            "second summary",
		Layout:                 "{}",
		CoveredMessageIds:      `["m1","m2","m3"]`,
		CreatedAt:              200,
	})
	require.NoError(t, err)
	require.NoError(t, q.UpdateSessionCompaction(ctx, db.UpdateSessionCompactionParams{
		ActiveSummaryID: sql.NullString{String: "sum2", Valid: true},
		ID:              "sess1",
	}))

	// All raw messages in created_at order, as the agent loads them. Includes
	// two summary messages that must be excluded from the retained tail.
	all := []message.Message{
		mkMsg("m1", message.User, false),
		mkMsg("m2", message.User, false),
		mkMsg("m3", message.User, false),
		mkMsg("m4", message.User, false),
		mkMsg("m5", message.User, false),
		mkMsg("m6", message.User, false),
		mkMsg("s1", message.Assistant, true),
		mkMsg("s2", message.Assistant, true),
	}
	summaryText, retained, err := e.ActiveContext(ctx, "sess1", all)
	require.NoError(t, err)
	require.Equal(t, "second summary", summaryText)

	// Retained must be exactly m4, m5, m6 — NOT the summary messages and
	// NOT the earlier m1..m3.
	require.Len(t, retained, 3)
	ids := []string{retained[0].ID, retained[1].ID, retained[2].ID}
	require.Equal(t, []string{"m4", "m5", "m6"}, ids)
	for _, r := range retained {
		require.False(t, r.IsSummaryMessage, "summary messages must not appear in the retained tail")
	}
}

// TestActiveContext_NoCompaction retains everything.
func TestActiveContext_NoCompaction(t *testing.T) {
	t.Parallel()
	e, _ := newTestEngine(t, nil)
	ctx := context.Background()
	all := []message.Message{
		mkMsg("a", message.User, false),
		mkMsg("b", message.User, false),
	}
	summaryText, retained, err := e.ActiveContext(ctx, "no-such-session", all)
	require.NoError(t, err)
	require.Empty(t, summaryText)
	require.Len(t, retained, 2)
}

// TestActiveContext_FallbackToCoveredEndWhenNoID verifies the ordinal fallback
// when a summary row predates the first_retained_message_id column (older rows).
func TestActiveContext_FallbackToCoveredEndWhenNoID(t *testing.T) {
	t.Parallel()
	e, q := newTestEngine(t, nil)
	ctx := context.Background()
	_, err := q.CreateSession(ctx, db.CreateSessionParams{ID: "sess2", Title: "test"})
	require.NoError(t, err)
	_, err = q.CreateCompactionSummary(ctx, db.CreateCompactionSummaryParams{
		ID:                "sumA",
		SessionID:         "sess2",
		ParentIds:         "[]",
		CoveredStart:      sql.NullInt64{Int64: 1, Valid: true},
		CoveredEnd:        sql.NullInt64{Int64: 3, Valid: true},
		Kind:              "leaf",
		SummaryText:       "summary A",
		Layout:            "{}",
		CoveredMessageIds: `["m1","m2","m3"]`,
		CreatedAt:         100,
	})
	require.NoError(t, err)
	require.NoError(t, q.UpdateSessionCompaction(ctx, db.UpdateSessionCompactionParams{
		ActiveSummaryID: sql.NullString{String: "sumA", Valid: true},
		ID:              "sess2",
	}))
	all := []message.Message{
		mkMsg("m1", message.User, false),
		mkMsg("m2", message.User, false),
		mkMsg("m3", message.User, false),
		mkMsg("m4", message.User, false),
		mkMsg("m5", message.User, false),
	}
	summaryText, retained, err := e.ActiveContext(ctx, "sess2", all)
	require.NoError(t, err)
	require.Equal(t, "summary A", summaryText)
	// covered_end=3 (1-based) → cut at index 3 → m4, m5.
	require.Len(t, retained, 2)
	require.Equal(t, "m4", retained[0].ID)
	require.Equal(t, "m5", retained[1].ID)
}
