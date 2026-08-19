package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/compaction"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/stretchr/testify/require"
)

// TestHardCompactionThreshold pins the trigger arithmetic in usage space: a
// 200k window with the default 16384 reserve compacts at >= 183616 tokens
// (not at 16384, which an inverted "remaining <= window - reserve" check
// would produce), and the legacy constants still apply with the engine off.
func TestHardCompactionThreshold(t *testing.T) {
	t.Parallel()
	cfg := config.DefaultCompactionConfig()

	require.Equal(t, int64(200000-16384), hardCompactionThreshold(200000, true, cfg))
	require.Equal(t, int64(1_000_000-16384), hardCompactionThreshold(1_000_000, true, cfg))
	// Small windows clamp the reserve to window/8 so the threshold stays
	// meaningful (32768 - 4096).
	require.Equal(t, int64(32768-4096), hardCompactionThreshold(32768, true, cfg))

	// Legacy: 20% buffer at or below 200k, fixed 20k buffer above.
	require.Equal(t, int64(160000), hardCompactionThreshold(200000, false, cfg))
	require.Equal(t, int64(1_000_000-20000), hardCompactionThreshold(1_000_000, false, cfg))

	// Sanity: a session at 17k tokens of a 200k window must NOT trigger.
	require.False(t, int64(17000) >= hardCompactionThreshold(200000, true, cfg))
	require.True(t, int64(190000) >= hardCompactionThreshold(200000, true, cfg))
}

func TestCompactionLimits_ClampToWindow(t *testing.T) {
	t.Parallel()
	cfg := config.DefaultCompactionConfig()
	keep, reserve := compactionLimits(cfg, 200000)
	require.Equal(t, int64(20000), keep)
	require.Equal(t, int64(16384), reserve)

	keep, reserve = compactionLimits(cfg, 32768)
	require.Equal(t, int64(8192), keep, "keep_recent clamps to window/4")
	require.Equal(t, int64(4096), reserve, "reserve clamps to window/8")
	require.Less(t, keep+reserve, int64(32768))

	cfg.KeepRecentTokens = 0
	cfg.ReserveTokens = 0
	keep, reserve = compactionLimits(cfg, 0)
	require.Equal(t, int64(20000), keep, "unset falls back to the default")
	require.Equal(t, int64(16384), reserve)
}

// TestCompactionStatus_SoftThreshold pins the CompactionStatus contract used
// by the compact_context tool: the soft threshold is the configured fraction
// of the model window (0.7 by default) and the usage is the session's
// accumulated prompt + completion tokens.
func TestCompactionStatus_SoftThreshold(t *testing.T) {
	a, _, sessions, _, _ := newCompactionTestAgent(t, nil)
	ctx := context.Background()
	sess, err := sessions.Create(ctx, "status")
	require.NoError(t, err)
	sess.PromptTokens = 30000
	sess.CompletionTokens = 20000
	sess, err = sessions.Save(ctx, sess)
	require.NoError(t, err)

	status, err := a.CompactionStatus(ctx, sess.ID)
	require.NoError(t, err)
	require.Equal(t, int64(200000), status.ContextWindow)
	require.Equal(t, int64(140000), status.SoftThresholdTokens, "0.7 of the 200k window")
	require.Equal(t, int64(50000), status.UsageTokens)

	// A customized fraction is honored.
	a.cfg.Config().Options.Compaction = &config.CompactionConfig{SoftThresholdFraction: 0.5}
	status, err = a.CompactionStatus(ctx, sess.ID)
	require.NoError(t, err)
	require.Equal(t, int64(100000), status.SoftThresholdTokens)
}

// TestSummarize_ConsumesCompactionRequestOnNoop verifies that an
// agent-initiated request is consumed even when there is nothing to compact,
// so HasCompactionRequest cannot stop every subsequent step.
func TestSummarize_ConsumesCompactionRequestOnNoop(t *testing.T) {
	a, _, sessions, _, _ := newCompactionTestAgent(t, func(context.Context, string, string, int64) (string, string, error) {
		t.Fatal("completer must not be called when there is nothing to compact")
		return "", "", nil
	})
	sess, err := sessions.Create(context.Background(), "empty")
	require.NoError(t, err)

	a.RequestCompaction(sess.ID, "focus on X")
	require.True(t, a.HasCompactionRequest(sess.ID))
	require.NoError(t, a.Summarize(context.Background(), sess.ID, fantasy.ProviderOptions{}, nil))
	require.False(t, a.HasCompactionRequest(sess.ID), "the request must be consumed even on a no-op")
}

// TestCompactWithEngine_IncrementalSpan runs two compactions through the real
// caller and checks that the second one covers only what the first left in
// context (previous retained tail + new messages), that the previous
// checkpoint is fed to the model, and that the active context stays bounded
// with no duplicated summary message.
func TestCompactWithEngine_IncrementalSpan(t *testing.T) {
	var prompts []string
	completer := func(_ context.Context, _, input string, _ int64) (string, string, error) {
		prompts = append(prompts, input)
		return "## Goal & User Intent\nGoal marker CKPT\n## Progress\n### Done\n- x\n## Next Action\n1. y\n", "stop", nil
	}
	a, q, sessions, messages, conn := newCompactionTestAgent(t, completer)
	ctx := context.Background()
	sess, err := sessions.Create(ctx, "incremental")
	require.NoError(t, err)

	// Each turn is ~4k tokens so keep_recent (20k) retains only the last few.
	addTurn := func(n int) {
		body := strings.Repeat(fmt.Sprintf("MARK-%02d ", n), 2000)
		_, err := messages.Create(ctx, sess.ID, message.CreateMessageParams{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "turn " + fmt.Sprint(n) + " " + body}}})
		require.NoError(t, err)
		_, err = messages.Create(ctx, sess.ID, message.CreateMessageParams{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "reply " + fmt.Sprint(n) + " " + body}, message.Finish{Reason: message.FinishReasonEndTurn}}})
		require.NoError(t, err)
	}
	for n := 1; n <= 12; n++ {
		addTurn(n)
	}

	require.NoError(t, a.compactWithEngine(ctx, sess.ID, "", fantasy.ProviderOptions{}, nil))
	require.NotEmpty(t, prompts)
	first := prompts[0]
	require.Contains(t, first, "MARK-01", "the first compaction covers the session start")
	require.NotContains(t, first, "MARK-12", "the most recent turn is retained, not compacted")
	require.NotContains(t, first, "<previous-checkpoint>")

	sum1, err := q.GetActiveCompactionSummary(ctx, sess.ID)
	require.NoError(t, err)
	require.True(t, sum1.FirstRetainedMessageID.Valid)
	require.Equal(t, int64(1), sum1.CoveredStart.Int64, "the first span starts at seq 1")

	// The active context is summary + retained tail only, with no summary
	// message duplicated inside the tail.
	all, err := messages.List(ctx, sess.ID)
	require.NoError(t, err)
	summaryText, retained, err := a.compaction.ActiveContext(ctx, sess.ID, all)
	require.NoError(t, err)
	require.Contains(t, summaryText, "CKPT")
	require.Less(t, len(retained), 24)
	for _, m := range retained {
		require.False(t, m.IsSummaryMessage)
	}
	firstRetainedIdx := -1
	for i, m := range all {
		if m.ID == sum1.FirstRetainedMessageID.String {
			firstRetainedIdx = i
		}
	}
	require.GreaterOrEqual(t, firstRetainedIdx, 0)
	require.Equal(t, message.User, all[firstRetainedIdx].Role, "the retained tail starts at a turn boundary")

	// Second round: more turns, then compact again.
	for n := 13; n <= 20; n++ {
		addTurn(n)
	}
	prompts = nil
	require.NoError(t, a.compactWithEngine(ctx, sess.ID, "", fantasy.ProviderOptions{}, nil))
	require.NotEmpty(t, prompts)
	second := prompts[0]
	require.Contains(t, second, "<previous-checkpoint>", "the previous checkpoint is fed to the model for the monotonic merge")
	require.NotContains(t, second, "MARK-01", "already-compacted messages are not re-summarized")
	require.NotContains(t, second, "MARK-05", "already-compacted messages are not re-summarized")
	require.Contains(t, second, "MARK-13", "new messages are covered")
	require.NotContains(t, second, "MARK-20", "the newest turn is retained")

	sum2, err := q.GetActiveCompactionSummary(ctx, sess.ID)
	require.NoError(t, err)
	require.NotEqual(t, sum1.ID, sum2.ID)
	require.Contains(t, sum2.ParentIds, sum1.ID, "the DAG links the new node to the previous one")
	require.Greater(t, sum2.CoveredStart.Int64, sum1.CoveredEnd.Int64, "the second span starts after the first span ends")
	require.Greater(t, sum2.CoveredEnd.Int64, sum1.CoveredEnd.Int64)

	all, err = messages.List(ctx, sess.ID)
	require.NoError(t, err)
	_, retained, err = a.compaction.ActiveContext(ctx, sess.ID, all)
	require.NoError(t, err)
	require.Less(t, len(retained), 16, "the retained tail stays bounded after the second compaction")
	for _, m := range retained {
		require.False(t, m.IsSummaryMessage, "summary messages never appear in the retained tail")
	}
	// Two summary messages exist for the UI, and both are excluded above.
	summaryMessages := 0
	for _, m := range all {
		if m.IsSummaryMessage {
			summaryMessages++
		}
	}
	require.Equal(t, 2, summaryMessages)
	_ = conn
}

// newCompactionTestAgent builds a sessionAgent over a real SQLite store with
// the compaction engine wired to a stub completer (no provider).
func newCompactionTestAgent(t *testing.T, completer compaction.Completer) (*sessionAgent, *db.Queries, session.Service, message.Service, interface{ Close() error }) {
	t.Helper()
	ctx := context.Background()
	dataDir := t.TempDir()
	conn, err := db.Connect(ctx, dataDir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Release(dataDir) })
	q := db.New(conn)
	sessions := session.NewService(q, conn)
	messages := message.NewService(q, message.WithDebounce(0))

	workingDir := t.TempDir()
	cfg, err := config.Init(workingDir, dataDir, false)
	require.NoError(t, err)

	large := Model{
		CatwalkCfg: catwalk.Model{ContextWindow: 200000, DefaultMaxTokens: 10000},
		ModelCfg:   config.SelectedModel{Provider: "test", Model: "test-model"},
	}
	agent := NewSessionAgent(SessionAgentOptions{
		LargeModel: large,
		SmallModel: large,
		Sessions:   sessions,
		Messages:   messages,
		Querier:    q,
		DB:         conn,
		Config:     cfg,
	}).(*sessionAgent)
	agent.compaction = compaction.NewEngine(q, completer, compaction.WithTxDB(conn))
	return agent, q, sessions, messages, conn
}

// TestCompactWithEngine_UsesDedicatedCompactionModel verifies that when
// models.compaction is set, the summarizer identity recorded on the summary
// node is the compaction model (not the large model), and that clearing the
// slot falls back to the large model.
func TestCompactWithEngine_UsesDedicatedCompactionModel(t *testing.T) {
	completer := func(_ context.Context, _, _ string, _ int64) (string, string, error) {
		return "## Goal & User Intent\nG\n## Progress\n### Done\n- x\n## Next Action\n1. y\n", "stop", nil
	}
	a, q, sessions, messages, _ := newCompactionTestAgent(t, completer)
	ctx := context.Background()

	addTurns := func(sessID string, from, to int) {
		for n := from; n <= to; n++ {
			body := strings.Repeat(fmt.Sprintf("BODY-%02d ", n), 2000)
			_, err := messages.Create(ctx, sessID, message.CreateMessageParams{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: body}}})
			require.NoError(t, err)
			_, err = messages.Create(ctx, sessID, message.CreateMessageParams{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: body}, message.Finish{Reason: message.FinishReasonEndTurn}}})
			require.NoError(t, err)
		}
	}

	// Dedicated compaction model on another provider.
	a.SetCompactionModel(&Model{
		Model:      stubLanguageModel{},
		CatwalkCfg: catwalk.Model{ID: "cheap-summarizer", ContextWindow: 128000, DefaultMaxTokens: 4096},
		ModelCfg:   config.SelectedModel{Provider: "otherprov", Model: "cheap-summarizer"},
	})
	sess, err := sessions.Create(ctx, "dedicated")
	require.NoError(t, err)
	addTurns(sess.ID, 1, 10)
	require.NoError(t, a.compactWithEngine(ctx, sess.ID, "", fantasy.ProviderOptions{}, nil))
	sum, err := q.GetActiveCompactionSummary(ctx, sess.ID)
	require.NoError(t, err)
	require.Equal(t, "otherprov", sum.ModelProvider.String)
	require.Equal(t, "cheap-summarizer", sum.ModelID.String)

	// Clearing the slot falls back to the large model.
	a.SetCompactionModel(nil)
	sess2, err := sessions.Create(ctx, "fallback")
	require.NoError(t, err)
	addTurns(sess2.ID, 1, 10)
	require.NoError(t, a.compactWithEngine(ctx, sess2.ID, "", fantasy.ProviderOptions{}, nil))
	sum2, err := q.GetActiveCompactionSummary(ctx, sess2.ID)
	require.NoError(t, err)
	require.Equal(t, "test", sum2.ModelProvider.String)
	require.Equal(t, "test-model", sum2.ModelID.String)
}

// stubLanguageModel is a non-nil fantasy.LanguageModel so the dedicated
// compaction model is considered configured; the engine's completer is
// stubbed in these tests, so it is never invoked.
type stubLanguageModel struct{ fantasy.LanguageModel }

func (stubLanguageModel) Provider() string { return "otherprov" }
func (stubLanguageModel) Model() string    { return "cheap-summarizer" }
