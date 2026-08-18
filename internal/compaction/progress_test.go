package compaction

import (
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

// TestRun_PublishesProgressSnapshots verifies the WithProgress callback gets
// a full snapshot sequence: the span event (nothing composed yet), then
// lane events whose composed estimate never shrinks, then a final "complete"
// event whose output matches the persisted summary exactly.
func TestRun_PublishesProgressSnapshots(t *testing.T) {
	t.Parallel()
	completer := func(_ context.Context, _, _ string, _ int64) (string, string, error) {
		return "<checkpoint>\n## Goal & User Intent\nG\n## Progress\n### Done\n- x\n## Next Action\n1. y\n", "stop", nil
	}
	var events []Progress
	e, q := newTestEngine(t, completer, WithProgress(func(sessionID string, p Progress) {
		require.Equal(t, "sp1", sessionID)
		events = append(events, p)
	}))
	ctx := context.Background()

	_, err := q.CreateSession(ctx, db.CreateSessionParams{ID: "sp1", Title: "test"})
	require.NoError(t, err)

	var history []message.Message
	for i := 0; i < 8; i++ {
		m := message.Message{
			ID:        "p" + string(rune('a'+i)),
			Role:      message.User,
			Parts:     []message.ContentPart{message.TextContent{Text: strings.Repeat("progress content ", 60)}},
			CreatedAt: int64(100 + i),
		}
		history = append(history, m)
		addRunMessage(t, q, "sp1", m.ID, m.Role, strings.Repeat("progress content ", 60), m.CreatedAt)
	}

	req := CompactionRequest{
		SessionID:                 "sp1",
		History:                   history,
		FirstRetainedSeq:          1,
		SeqOffset:                 1,
		ConsumerContextWindow:     200000,
		SystemPromptTokens:        8000,
		SummarizerContextWindow:   200000,
		SummarizerMaxOutputTokens: 8192,
		KeepRecentTokens:          20000,
		ReserveTokens:             16384,
		Cfg:                       config.DefaultCompactionConfig(),
	}
	result, err := e.Run(ctx, req)
	require.NoError(t, err)
	require.NotEmpty(t, events, "a registered progress callback must fire")

	first := events[0]
	require.Equal(t, "span", first.Phase)
	require.Equal(t, int64(0), first.TokensOut)
	require.Greater(t, first.SpanTokens, int64(0))
	require.Equal(t, first.SpanTokens, first.TokensDown, "nothing composed yet: the whole span counts as down")

	for i := 1; i < len(events)-1; i++ {
		require.GreaterOrEqual(t, events[i].TokensOut, events[i-1].TokensOut,
			"lane events must never shrink the composed estimate (event %d)", i)
		require.GreaterOrEqual(t, events[i].TokensDown, int64(0))
	}
	last := events[len(events)-1]
	require.Equal(t, "complete", last.Phase)
	require.Equal(t, int64(EstimateTokens(len(result.SummaryText))), last.TokensOut,
		"the final snapshot must match the persisted summary size")
	require.Equal(t, last.SpanTokens-last.TokensOut, last.TokensDown)
	require.Greater(t, last.TokensDown, int64(0), "compaction must report a positive reduction")
}

// TestRun_NoProgressCallbackIsANoop keeps engine construction optional: with
// no callback registered, Run must behave exactly as before.
func TestRun_NoProgressCallbackIsANoop(t *testing.T) {
	t.Parallel()
	completer := func(_ context.Context, _, _ string, _ int64) (string, string, error) {
		return "## Goal & User Intent\nG\n## Progress\n### Done\n- x\n## Next Action\n1. y\n", "stop", nil
	}
	e, q := newTestEngine(t, completer)
	ctx := context.Background()
	_, err := q.CreateSession(ctx, db.CreateSessionParams{ID: "sp2", Title: "test"})
	require.NoError(t, err)

	var history []message.Message
	for i := 0; i < 8; i++ {
		m := message.Message{
			ID:        "q" + string(rune('a'+i)),
			Role:      message.User,
			Parts:     []message.ContentPart{message.TextContent{Text: strings.Repeat("content ", 60)}},
			CreatedAt: int64(100 + i),
		}
		history = append(history, m)
		addRunMessage(t, q, "sp2", m.ID, m.Role, strings.Repeat("content ", 60), m.CreatedAt)
	}

	req := CompactionRequest{
		SessionID:                 "sp2",
		History:                   history,
		FirstRetainedSeq:          1,
		SeqOffset:                 1,
		ConsumerContextWindow:     200000,
		SystemPromptTokens:        8000,
		SummarizerContextWindow:   200000,
		SummarizerMaxOutputTokens: 8192,
		KeepRecentTokens:          20000,
		ReserveTokens:             16384,
		Cfg:                       config.DefaultCompactionConfig(),
	}
	_, err = e.Run(ctx, req)
	require.NoError(t, err)
}
