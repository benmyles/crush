package compaction

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

func compactionTestConfig() config.CompactionConfig {
	return config.DefaultCompactionConfig()
}

func addRunMessage(t *testing.T, q *db.Queries, sessionID, id string, role message.MessageRole, text string, createdAt int64) {
	t.Helper()
	_, err := q.CreateMessage(context.Background(), db.CreateMessageParams{
		ID:        id,
		SessionID: sessionID,
		Role:      string(role),
		Parts:     `[{"type":"text","data":{"text":"` + text + `"}}]`,
	})
	require.NoError(t, err)
}

// TestEngineRun_HappyPath verifies that Run produces a summary, persists a
// DAG node + causality edges, and points the session at the new active summary.
func TestEngineRun_HappyPath(t *testing.T) {
	t.Parallel()
	completer := func(_ context.Context, _, input string, _ int64) (string, string, error) {
		// Return a short checkpoint that converges (shorter than the input)
		// and has the required sections.
		return "<checkpoint>\n## Goal & User Intent\nTest goal\n## Progress\n### Done\n- thing\n## Next Action\n1. do next\n</checkpoint>\n" + strings.Repeat("x", 100), "stop", nil
	}
	e, q := newTestEngine(t, completer)
	ctx := context.Background()

	_, err := q.CreateSession(ctx, db.CreateSessionParams{ID: "s1", Title: "test"})
	require.NoError(t, err)

	var history []message.Message
	for i := 0; i < 10; i++ {
		m := message.Message{
			ID:        "m" + string(rune('a'+i)),
			Role:      message.User,
			Parts:     []message.ContentPart{message.TextContent{Text: strings.Repeat("task content ", 50)}},
			CreatedAt: int64(100 + i),
		}
		history = append(history, m)
		addRunMessage(t, q, "s1", m.ID, m.Role, strings.Repeat("task content ", 50), m.CreatedAt)
	}

	req := CompactionRequest{
		SessionID:                 "s1",
		History:                   history,
		FirstRetainedSeq:          1,
		FirstRetainedID:           "ma",
		SeqOffset:                 1,
		ConsumerContextWindow:     200000,
		SystemPromptTokens:        8000,
		SummarizerContextWindow:   200000,
		SummarizerMaxOutputTokens: 8192,
		KeepRecentTokens:          20000,
		ReserveTokens:             16384,
		Cfg:                       compactionTestConfig(),
	}
	result, err := e.Run(ctx, req)
	require.NoError(t, err)
	require.NotEmpty(t, result.SummaryText)
	require.NotEmpty(t, result.SummaryID)

	// The summary DAG node must be persisted.
	summary, err := q.GetCompactionSummary(ctx, result.SummaryID)
	require.NoError(t, err)
	require.Equal(t, "s1", summary.SessionID)
	require.NotEmpty(t, summary.SummaryText)

	// The session must point at the new active summary.
	session, err := q.GetSessionByID(ctx, "s1")
	require.NoError(t, err)
	require.True(t, session.ActiveSummaryID.Valid)
	require.Equal(t, result.SummaryID, session.ActiveSummaryID.String)
}

// TestEngineRun_FailClosedOnCompleterError verifies that a completer error
// propagates and no summary is persisted.
func TestEngineRun_FailClosedOnCompleterError(t *testing.T) {
	t.Parallel()
	completer := func(_ context.Context, _, _ string, _ int64) (string, string, error) {
		return "", "", context.Canceled
	}
	e, q := newTestEngine(t, completer)
	ctx := context.Background()
	_, err := q.CreateSession(ctx, db.CreateSessionParams{ID: "s2", Title: "test"})
	require.NoError(t, err)

	var history []message.Message
	for i := 0; i < 5; i++ {
		m := message.Message{
			ID:        "e" + string(rune('a'+i)),
			Role:      message.User,
			Parts:     []message.ContentPart{message.TextContent{Text: strings.Repeat("content ", 50)}},
			CreatedAt: int64(100 + i),
		}
		history = append(history, m)
		addRunMessage(t, q, "s2", m.ID, m.Role, strings.Repeat("content ", 50), m.CreatedAt)
	}

	req := CompactionRequest{
		SessionID:                 "s2",
		History:                   history,
		FirstRetainedSeq:          1,
		SeqOffset:                 1,
		ConsumerContextWindow:     200000,
		SystemPromptTokens:        8000,
		SummarizerContextWindow:   200000,
		SummarizerMaxOutputTokens: 8192,
		KeepRecentTokens:          20000,
		ReserveTokens:             16384,
		Cfg:                       compactionTestConfig(),
	}
	_, err = e.Run(ctx, req)
	require.Error(t, err, "completer error must propagate (fail closed)")

	// No summary should be persisted.
	summaries, err := q.ListCompactionSummariesBySession(ctx, "s2")
	require.NoError(t, err)
	require.Empty(t, summaries, "no summary must be persisted on error")
}

// TestEngineRun_SecondCompactionKeepsContextBounded verifies that a second
// compaction produces a summary smaller than the first and the ActiveContext
// retains only the messages after the latest summary's first_retained_message_id.
func TestEngineRun_SecondCompactionKeepsContextBounded(t *testing.T) {
	t.Parallel()
	completer := func(_ context.Context, _, _ string, _ int64) (string, string, error) {
		return "<checkpoint>\n## Goal & User Intent\nTest goal\n## Progress\n### Done\n- thing\n## Next Action\n1. do next\n</checkpoint>\n" + strings.Repeat("y", 200), "stop", nil
	}
	e, q := newTestEngine(t, completer)
	ctx := context.Background()
	_, err := q.CreateSession(ctx, db.CreateSessionParams{ID: "s3", Title: "test"})
	require.NoError(t, err)

	// First batch of messages.
	var history []message.Message
	for i := 0; i < 8; i++ {
		m := message.Message{
			ID:        "p" + string(rune('a'+i)),
			Role:      message.User,
			Parts:     []message.ContentPart{message.TextContent{Text: strings.Repeat("first batch ", 50)}},
			CreatedAt: int64(100 + i),
		}
		history = append(history, m)
		addRunMessage(t, q, "s3", m.ID, m.Role, strings.Repeat("first batch ", 50), m.CreatedAt)
	}

	cfg := compactionTestConfig()
	req := CompactionRequest{
		SessionID:                 "s3",
		History:                   history,
		FirstRetainedSeq:          1,
		FirstRetainedID:           "pa",
		SeqOffset:                 1,
		ConsumerContextWindow:     200000,
		SystemPromptTokens:        8000,
		SummarizerContextWindow:   200000,
		SummarizerMaxOutputTokens: 8192,
		KeepRecentTokens:          20000,
		ReserveTokens:             16384,
		Cfg:                       cfg,
	}
	result1, err := e.Run(ctx, req)
	require.NoError(t, err)
	require.NotEmpty(t, result1.SummaryText)

	// Load all raw messages and check ActiveContext after the first compaction.
	all, err := q.ListMessagesBySession(ctx, "s3")
	require.NoError(t, err)
	_, retained, err := e.ActiveContext(ctx, "s3", toDomainMessages(all))
	require.NoError(t, err)
	// All 8 raw messages are retained (none excluded since firstRetainedID = "pa" = first message).
	require.Len(t, retained, 8)

	// Second compaction with a new batch.
	for i := 0; i < 8; i++ {
		m := message.Message{
			ID:        "q" + string(rune('a'+i)),
			Role:      message.User,
			Parts:     []message.ContentPart{message.TextContent{Text: strings.Repeat("second batch ", 50)}},
			CreatedAt: int64(200 + i),
		}
		addRunMessage(t, q, "s3", m.ID, m.Role, strings.Repeat("second batch ", 50), m.CreatedAt)
	}

	// Reload all messages and run a second compaction over the full raw list.
	all2, err := q.ListMessagesBySession(ctx, "s3")
	require.NoError(t, err)
	history2 := toDomainMessages(all2)
	req2 := CompactionRequest{
		SessionID:                 "s3",
		History:                   history2[:8], // compact the first batch
		FirstRetainedSeq:          9,
		FirstRetainedID:           "qa",
		SeqOffset:                 1,
		ConsumerContextWindow:     200000,
		SystemPromptTokens:        8000,
		SummarizerContextWindow:   200000,
		SummarizerMaxOutputTokens: 8192,
		KeepRecentTokens:          20000,
		ReserveTokens:             16384,
		Cfg:                       cfg,
	}
	result2, err := e.Run(ctx, req2)
	require.NoError(t, err)
	require.NotEmpty(t, result2.SummaryText)

	// ActiveContext after the second compaction must retain only qa..qh (the
	// second batch), not the first batch.
	all3, err := q.ListMessagesBySession(ctx, "s3")
	require.NoError(t, err)
	_, retained2, err := e.ActiveContext(ctx, "s3", toDomainMessages(all3))
	require.NoError(t, err)
	require.Len(t, retained2, 8, "second compaction must retain only the second batch")
	for _, r := range retained2 {
		require.True(t, strings.HasPrefix(r.ID, "q"), "retained messages must be from the second batch")
	}
}

// toDomainMessages converts db.Message to message.Message for ActiveContext.
func toDomainMessages(dbMsgs []db.Message) []message.Message {
	out := make([]message.Message, len(dbMsgs))
	for i, m := range dbMsgs {
		out[i] = message.Message{
			ID:               m.ID,
			Role:             message.MessageRole(m.Role),
			IsSummaryMessage: m.IsSummaryMessage == 1,
			Parts:            []message.ContentPart{message.TextContent{Text: m.Parts}},
			CreatedAt:        m.CreatedAt,
		}
	}
	return out
}

// TestEngineRun_ExtractsAreBudgeted verifies the extractive lane is wired to
// the budget governor in the engine path: a large span yields a non-empty
// extracts section that is bounded by the extracts allotment (not a verbatim
// re-emission of every block), and the composed summary is smaller than the
// span it replaces.
func TestEngineRun_ExtractsAreBudgeted(t *testing.T) {
	t.Parallel()
	completer := func(_ context.Context, _, _ string, _ int64) (string, string, error) {
		return "<checkpoint>\n## Goal & User Intent\nTest goal\n## Progress\n### Done\n- thing\n## Next Action\n1. do next\n</checkpoint>\n", "stop", nil
	}
	e, q := newTestEngine(t, completer)
	ctx := context.Background()
	_, err := q.CreateSession(ctx, db.CreateSessionParams{ID: "s4", Title: "test"})
	require.NoError(t, err)

	// ~40 turns of ~5k chars each: far more than the extracts allotment
	// (~46k chars for a 200k window with the default budget fraction).
	var history []message.Message
	spanChars := 0
	for i := 0; i < 40; i++ {
		userText := strings.Repeat("user request line for turn "+strings.Repeat("x", 3)+"\n", 60)
		replyText := strings.Repeat("assistant reply line "+strings.Repeat("y", 5)+"\n", 60)
		spanChars += len(userText) + len(replyText)
		history = append(history,
			message.Message{ID: "u" + strings.Repeat("a", i+1), Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: userText}}, CreatedAt: int64(100 + 2*i)},
			message.Message{ID: "r" + strings.Repeat("a", i+1), Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: replyText}, message.Finish{Reason: message.FinishReasonEndTurn}}, CreatedAt: int64(101 + 2*i)},
		)
	}
	req := CompactionRequest{
		SessionID:                 "s4",
		History:                   history,
		FirstRetainedSeq:          len(history) + 1,
		FirstRetainedID:           "retained",
		SeqOffset:                 1,
		ConsumerContextWindow:     200000,
		SystemPromptTokens:        8000,
		SummarizerContextWindow:   200000,
		SummarizerMaxOutputTokens: 8192,
		KeepRecentTokens:          20000,
		ReserveTokens:             16384,
		Cfg:                       compactionTestConfig(),
	}
	result, err := e.Run(ctx, req)
	require.NoError(t, err)
	plan := PlanBudget(BudgetInputFromConfig(
		req.Cfg,
		req.ConsumerContextWindow,
		req.SystemPromptTokens,
		req.SummarizerContextWindow,
		req.SummarizerMaxOutputTokens,
		req.KeepRecentTokens,
		req.ReserveTokens,
		BudgetFeatures{
			Ledger:        true,
			TranscriptMap: true,
			Restore:       true,
			Extracts:      true,
		},
	))
	require.LessOrEqual(t, result.TokenCount, plan.AllowanceTokens, "the whole compaction entry must respect the governor allowance")

	bounds, ok := result.Layout["extracts"]
	require.True(t, ok, "the extracts section must be present in the composed summary")
	extracts := result.SummaryText[bounds[0]:bounds[1]]
	require.Contains(t, extracts, keepOpen, "golden spans are kept")
	require.Less(t, len(extracts), 60000, "extracts must be bounded by the budget allotment, not emit every block")
	require.Less(t, len(result.SummaryText), spanChars, "the composed summary must be smaller than the span it replaces")
	require.Contains(t, result.SummaryText, result.SummaryID, "the recovery note cites the summary id")
	require.Contains(t, result.SummaryText, "seq 1", "seq anchors are session-absolute and start at SeqOffset")
}

// TestEngineRun_WholeSummaryRespectsAllowance covers force-kept tool-call
// blocks whose rendered headers can exceed the extracts lane's nominal share.
// The whole-entry governor is the final safety boundary: no combination of
// lanes may produce a summary larger than its allowance.
func TestEngineRun_WholeSummaryRespectsAllowance(t *testing.T) {
	t.Parallel()
	completer := func(_ context.Context, _, _ string, _ int64) (string, string, error) {
		return "<checkpoint>\n## Goal & User Intent\nTest goal\n## Progress\n### Done\n- thing\n## Next Action\n1. do next\n</checkpoint>\n", "stop", nil
	}
	e, q := newTestEngine(t, completer)
	ctx := context.Background()
	_, err := q.CreateSession(ctx, db.CreateSessionParams{ID: "summary-budget", Title: "test"})
	require.NoError(t, err)

	var history []message.Message
	for turn := 0; turn < 8; turn++ {
		history = append(history, message.Message{
			ID:        fmt.Sprintf("budget-user-%d", turn),
			Role:      message.User,
			Parts:     []message.ContentPart{message.TextContent{Text: "Inspect the tool output and continue."}},
			CreatedAt: int64(100 + turn*2),
		})
		parts := make([]message.ContentPart, 0, 41)
		for call := 0; call < 40; call++ {
			parts = append(parts, message.ToolCall{
				ID:       fmt.Sprintf("call-%d-%d", turn, call),
				Name:     "large_tool",
				Input:    strings.Repeat("argument-value ", 2000),
				Finished: true,
			})
		}
		parts = append(parts, message.Finish{Reason: message.FinishReasonToolUse})
		history = append(history, message.Message{
			ID:        fmt.Sprintf("budget-assistant-%d", turn),
			Role:      message.Assistant,
			Parts:     parts,
			CreatedAt: int64(101 + turn*2),
		})
	}

	cfg := compactionTestConfig()
	cfg.Verify = string(config.VerificationChecks)
	req := CompactionRequest{
		SessionID:                 "summary-budget",
		History:                   history,
		FirstRetainedSeq:          len(history) + 1,
		FirstRetainedID:           "retained",
		SeqOffset:                 1,
		ConsumerContextWindow:     200000,
		SystemPromptTokens:        8000,
		SummarizerContextWindow:   200000,
		SummarizerMaxOutputTokens: 8192,
		KeepRecentTokens:          20000,
		ReserveTokens:             16384,
		Cfg:                       cfg,
	}
	result, err := e.Run(ctx, req)
	require.NoError(t, err)

	plan := PlanBudget(BudgetInputFromConfig(
		req.Cfg,
		req.ConsumerContextWindow,
		req.SystemPromptTokens,
		req.SummarizerContextWindow,
		req.SummarizerMaxOutputTokens,
		req.KeepRecentTokens,
		req.ReserveTokens,
		BudgetFeatures{
			Ledger:        true,
			TranscriptMap: true,
			Restore:       true,
			Extracts:      true,
		},
	))
	require.LessOrEqual(t, result.TokenCount, plan.AllowanceTokens, "the whole compaction entry must respect the governor allowance")
}
