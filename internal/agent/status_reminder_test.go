package agent

import (
	"context"
	"strings"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
	statuspkg "github.com/charmbracelet/crush/internal/status"
	"github.com/stretchr/testify/require"
)

// TestMaybeAppendStatusReminder covers the reminder cadence: baseline for
// fresh sessions, interval gating against the latest update, spacing
// between reminders, and the disable/non-interactive paths.
func TestMaybeAppendStatusReminder(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dataDir := t.TempDir()
	conn, err := db.Connect(ctx, dataDir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Release(dataDir) })
	q := db.New(conn)

	workingDir := t.TempDir()
	cfg, err := config.Init(workingDir, dataDir, false)
	require.NoError(t, err)

	store := statuspkg.NewStore(conn)

	sa := NewSessionAgent(SessionAgentOptions{
		LargeModel: Model{
			CatwalkCfg: catwalk.Model{ContextWindow: 200000, DefaultMaxTokens: 10000},
			ModelCfg:   config.SelectedModel{Provider: "test", Model: "test-model"},
		},
		SmallModel: Model{
			CatwalkCfg: catwalk.Model{ContextWindow: 200000, DefaultMaxTokens: 10000},
			ModelCfg:   config.SelectedModel{Provider: "test", Model: "test-model"},
		},
		Sessions:      session.NewService(q, conn),
		Messages:      message.NewService(q),
		Querier:       q,
		DB:            conn,
		Config:        cfg,
		StatusUpdates: store,
	}).(*sessionAgent)

	call := SessionAgentCall{SessionID: "sess-1", Prompt: "work"}
	base := []fantasy.Message{fantasy.NewSystemMessage("sys")}
	baseCopy := func() []fantasy.Message { return append([]fantasy.Message(nil), base...) }

	lastMessageIsReminder := func(msgs []fantasy.Message) bool {
		if len(msgs) == 0 {
			return false
		}
		msg := msgs[len(msgs)-1]
		if msg.Role != fantasy.MessageRoleUser {
			return false
		}
		for _, part := range msg.Content {
			if tp, ok := fantasy.AsContentType[fantasy.TextPart](part); ok && strings.Contains(tp.Text, "[Status update]") {
				return true
			}
		}
		return false
	}

	t.Run("disabled leaves messages untouched", func(t *testing.T) {
		got := sa.maybeAppendStatusReminder(ctx, call, baseCopy())
		require.Equal(t, base, got)
	})

	// Enable the feature and record none: the first observation stamps a
	// baseline, so no reminder yet.
	cfg.Config().Options.StatusUpdates = true
	t.Run("first step only stamps baseline", func(t *testing.T) {
		got := sa.maybeAppendStatusReminder(ctx, call, baseCopy())
		require.Equal(t, base, got)
	})

	// Aging the baseline past the interval arms the reminder.
	baseline, _ := sa.statusBaseline.Get("sess-1")
	sa.statusBaseline.Set("sess-1", baseline-int64(statuspkg.ReminderInterval.Seconds())-10)

	t.Run("expired baseline injects reminder once", func(t *testing.T) {
		got := sa.maybeAppendStatusReminder(ctx, call, baseCopy())
		require.Len(t, got, len(base)+1)
		require.True(t, lastMessageIsReminder(got))

		// Consecutive steps within the interval are not re-reminded.
		again := sa.maybeAppendStatusReminder(ctx, call, baseCopy())
		require.Equal(t, base, again)
	})

	t.Run("recent update suppresses the reminder", func(t *testing.T) {
		sess, err := sa.sessions.Create(ctx, "sess-2")
		require.NoError(t, err)
		_, err = store.Upsert(ctx, sess.ID, "did", "doing", "next")
		require.NoError(t, err)
		call2 := SessionAgentCall{SessionID: sess.ID, Prompt: "work"}
		got := sa.maybeAppendStatusReminder(ctx, call2, baseCopy())
		require.Equal(t, base, got)
	})

	t.Run("old update injects immediately", func(t *testing.T) {
		sess, err := sa.sessions.Create(ctx, "sess-3")
		require.NoError(t, err)
		_, err = store.Upsert(ctx, sess.ID, "did", "doing", "next")
		require.NoError(t, err)
		_, err = conn.Exec(`UPDATE status_updates SET updated_at = updated_at - 300 WHERE session_id = ?`, sess.ID)
		require.NoError(t, err)
		call3 := SessionAgentCall{SessionID: sess.ID, Prompt: "work"}
		got := sa.maybeAppendStatusReminder(ctx, call3, baseCopy())
		require.Len(t, got, len(base)+1)
		require.True(t, lastMessageIsReminder(got))
	})

	t.Run("non-interactive runs never remind", func(t *testing.T) {
		ni := call
		ni.NonInteractive = true
		got := sa.maybeAppendStatusReminder(ctx, ni, baseCopy())
		require.Equal(t, base, got)
	})

	t.Run("disabled again leaves messages untouched", func(t *testing.T) {
		cfg.Config().Options.StatusUpdates = false
		got := sa.maybeAppendStatusReminder(ctx, call, baseCopy())
		require.Equal(t, base, got)
	})
}
