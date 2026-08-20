package compaction

import (
	"context"
	"testing"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

func TestDecideTrigger_NoneBelowSoft(t *testing.T) {
	t.Parallel()
	d := DecideTrigger(TriggerInput{
		UsageTokens:           1000,
		ContextWindow:         200000,
		ReserveTokens:         16384,
		SoftThresholdFraction: 0.7,
	})
	require.Equal(t, TriggerNone, d.Reason)
}

func TestDecideTrigger_HardAtWindow(t *testing.T) {
	t.Parallel()
	d := DecideTrigger(TriggerInput{
		UsageTokens:           195000,
		ContextWindow:         200000,
		ReserveTokens:         16384,
		SoftThresholdFraction: 0.7,
	})
	require.Equal(t, TriggerHard, d.Reason)
	require.True(t, d.Blocking)
}

func TestDecideTrigger_HardThresholdOverride(t *testing.T) {
	t.Parallel()
	// A model with a 500k window but a 372k auto-compaction point must
	// block at 372k, not at window - reserve.
	d := DecideTrigger(TriggerInput{
		UsageTokens:           372000,
		ContextWindow:         500000,
		ReserveTokens:         16384,
		SoftThresholdFraction: 0.7,
		HardThresholdTokens:   372000,
	})
	require.Equal(t, TriggerHard, d.Reason)
	require.True(t, d.Blocking)
}

func TestDecideTrigger_HardThresholdOverrideNotHit(t *testing.T) {
	t.Parallel()
	// Below the override, the rubric (soft) path governs.
	d := DecideTrigger(TriggerInput{
		UsageTokens:           371000,
		ContextWindow:         500000,
		ReserveTokens:         16384,
		SoftThresholdFraction: 0.7,
		HardThresholdTokens:   372000,
	})
	require.NotEqual(t, TriggerHard, d.Reason)
}

func TestDecideTrigger_RubricFiresAtClosedUnit(t *testing.T) {
	t.Parallel()
	// A finished assistant turn with a final text answer (no tool calls) is a
	// closed reasoning unit -> the rubric should fire.
	msgs := []message.Message{
		{ID: "u1", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "do it"}}, CreatedAt: 100},
		{ID: "a1", Role: message.Assistant, Parts: []message.ContentPart{
			message.TextContent{Text: "Done."},
			message.Finish{Reason: message.FinishReasonEndTurn},
		}, CreatedAt: 101},
	}
	d := DecideTrigger(TriggerInput{
		UsageTokens:           150000, // above soft (140k), below hard
		ContextWindow:         200000,
		ReserveTokens:         16384,
		SoftThresholdFraction: 0.7,
		Messages:              msgs,
	})
	require.Equal(t, TriggerRubric, d.Reason)
	require.GreaterOrEqual(t, d.Score, 0.5)
}

func TestDecideTrigger_SuppressesMidDerivation(t *testing.T) {
	t.Parallel()
	// An in-flight assistant turn (no finish) = mid-derivation -> suppress.
	msgs := []message.Message{
		{ID: "u1", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "do it"}}, CreatedAt: 100},
		{ID: "a1", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "thinking"}}, CreatedAt: 101},
	}
	d := DecideTrigger(TriggerInput{
		UsageTokens:           150000,
		ContextWindow:         200000,
		ReserveTokens:         16384,
		SoftThresholdFraction: 0.7,
		Messages:              msgs,
	})
	// Score should be low; the rubric should not fire (but close-to-hard may
	// still trigger a soft).
	require.Less(t, d.Score, 0.5)
}

func TestRunParallelBlockCompaction_SingleBlock(t *testing.T) {
	t.Parallel()
	span := BuildSpanModel(SpanInput{History: []message.Message{
		{ID: "u1", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "hi"}}, CreatedAt: 100},
	}})
	res := RunParallelBlockCompaction(context.Background(), ParallelBlockInput{
		Span:       span,
		BlockCount: 1,
		Budget:     CheckpointRenderBudget,
		Summarize: func(_ context.Context, blockText string) (string, error) {
			return "summary of: " + blockText, nil
		},
	})
	require.Equal(t, 1, res.BlockCount)
	require.Contains(t, res.Summary, "summary of:")
}

func TestRunParallelBlockCompaction_MultipleBlocks(t *testing.T) {
	t.Parallel()
	var history []message.Message
	for i := 0; i < 8; i++ {
		history = append(history, message.Message{
			ID:        "u" + string(rune('1'+i)),
			Role:      message.User,
			Parts:     []message.ContentPart{message.TextContent{Text: "task"}},
			CreatedAt: int64(100 + i),
		})
		history = append(history, message.Message{
			ID:        "a" + string(rune('1'+i)),
			Role:      message.Assistant,
			Parts:     []message.ContentPart{message.TextContent{Text: "done"}, message.Finish{Reason: message.FinishReasonEndTurn}},
			CreatedAt: int64(101 + i),
		})
	}
	span := BuildSpanModel(SpanInput{History: history})
	res := RunParallelBlockCompaction(context.Background(), ParallelBlockInput{
		Span:       span,
		BlockCount: 4,
		Budget:     CheckpointRenderBudget,
		Summarize: func(_ context.Context, _ string) (string, error) {
			return "block-summary", nil
		},
	})
	require.GreaterOrEqual(t, res.BlockCount, 1)
	require.Contains(t, res.Summary, "block-summary")
}

func TestSplitBlocksByTurns(t *testing.T) {
	t.Parallel()
	var history []message.Message
	for i := 0; i < 6; i++ {
		history = append(history, message.Message{ID: "u", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "x"}}, CreatedAt: int64(i)})
	}
	span := BuildSpanModel(SpanInput{History: history})
	chunks := splitBlocksByTurns(span, 3)
	require.GreaterOrEqual(t, len(chunks), 1)
	// Every block should appear in exactly one chunk.
	total := 0
	for _, c := range chunks {
		total += len(c)
	}
	require.Equal(t, len(span.Blocks), total)
}
