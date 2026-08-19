package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

// highUsageToolModel reports a hard-threshold-sized context on every step.
// It makes three distinct tool calls before finishing, reproducing a resumed
// turn that used to compact after every tool result.
type highUsageToolModel struct {
	calls atomic.Int64
}

func (*highUsageToolModel) Provider() string { return "test" }
func (*highUsageToolModel) Model() string    { return "high-usage-tool-model" }

func (m *highUsageToolModel) Generate(context.Context, fantasy.Call) (*fantasy.Response, error) {
	return &fantasy.Response{
		Content:      fantasy.ResponseContent{fantasy.TextContent{Text: "done"}},
		FinishReason: fantasy.FinishReasonStop,
	}, nil
}

func (m *highUsageToolModel) Stream(context.Context, fantasy.Call) (fantasy.StreamResponse, error) {
	step := m.calls.Add(1)
	return func(yield func(fantasy.StreamPart) bool) {
		usage := fantasy.Usage{InputTokens: 190000, OutputTokens: 100, TotalTokens: 190100}
		if step <= 3 {
			id := fmt.Sprintf("large-output-%d", step)
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolInputStart, ID: id, ToolCallName: "large_output"}) {
				return
			}
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolInputDelta, ID: id, Delta: `{}`}) {
				return
			}
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolInputEnd, ID: id}) {
				return
			}
			if !yield(fantasy.StreamPart{
				Type:          fantasy.StreamPartTypeToolCall,
				ID:            id,
				ToolCallName:  "large_output",
				ToolCallInput: `{}`,
			}) {
				return
			}
			yield(fantasy.StreamPart{
				Type:         fantasy.StreamPartTypeFinish,
				Usage:        usage,
				FinishReason: fantasy.FinishReasonToolCalls,
			})
			return
		}
		if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: "done"}) {
			return
		}
		if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: "done", Delta: "done"}) {
			return
		}
		if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextEnd, ID: "done"}) {
			return
		}
		yield(fantasy.StreamPart{
			Type:         fantasy.StreamPartTypeFinish,
			Usage:        usage,
			FinishReason: fantasy.FinishReasonStop,
		})
	}, nil
}

func (*highUsageToolModel) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, errors.New("not implemented")
}

func (*highUsageToolModel) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, errors.New("not implemented")
}

// TestRun_AutoCompactionDoesNotReenterSameTurn verifies that a compaction
// continuation gets to finish its logical user turn even when every subsequent
// provider step still reports usage above the hard threshold.
func TestRun_AutoCompactionDoesNotReenterSameTurn(t *testing.T) {
	checkpoint := func(context.Context, string, string, int64) (string, string, error) {
		return "## Goal & User Intent\nFinish the task.\n## Progress\n### Done\n- Preserved context.\n## Next Action\n1. Continue.\n", "stop", nil
	}
	a, _, sessions, messages, _ := newCompactionTestAgent(t, checkpoint)
	ctx := t.Context()
	sess, err := sessions.Create(ctx, "compaction-reentry")
	require.NoError(t, err)

	// Give the first compaction a substantial older span to replace.
	for turn := 0; turn < 12; turn++ {
		body := strings.Repeat(fmt.Sprintf("older-turn-%02d ", turn), 800)
		_, err := messages.Create(ctx, sess.ID, message.CreateMessageParams{
			Role:  message.User,
			Parts: []message.ContentPart{message.TextContent{Text: body}},
		})
		require.NoError(t, err)
		_, err = messages.Create(ctx, sess.ID, message.CreateMessageParams{
			Role: message.Assistant,
			Parts: []message.ContentPart{
				message.TextContent{Text: body},
				message.Finish{Reason: message.FinishReasonEndTurn},
			},
		})
		require.NoError(t, err)
	}

	model := &highUsageToolModel{}
	configured := Model{
		Model: model,
		CatwalkCfg: catwalk.Model{
			ContextWindow:    200000,
			DefaultMaxTokens: 10000,
		},
		ModelCfg: config.SelectedModel{Provider: "test", Model: "high-usage-tool-model"},
	}
	a.SetModels(configured, configured)
	a.SetTools([]fantasy.AgentTool{
		fantasy.NewAgentTool(
			"large_output",
			"Return enough output to leave material in the active turn.",
			func(context.Context, struct{}, fantasy.ToolCall) (fantasy.ToolResponse, error) {
				return fantasy.NewTextResponse(strings.Repeat("tool output ", 4000)), nil
			},
		),
	})

	result, err := a.Run(ctx, SessionAgentCall{
		SessionID:      sess.ID,
		Prompt:         "Complete the multi-step task.",
		NonInteractive: true,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, int64(4), model.calls.Load(), "the resumed agent must reach its final response")

	all, err := messages.List(ctx, sess.ID)
	require.NoError(t, err)
	summaries := 0
	for _, msg := range all {
		if msg.IsSummaryMessage {
			summaries++
		}
	}
	require.Equal(t, 1, summaries, "one logical user turn must not recursively compact after every tool step")

	// The guard rides on the recursive call, not the session, so a fresh user
	// turn can compact normally.
	_, err = a.Run(ctx, SessionAgentCall{
		SessionID:      sess.ID,
		Prompt:         "Start a new turn.",
		NonInteractive: true,
	})
	require.NoError(t, err)
	all, err = messages.List(ctx, sess.ID)
	require.NoError(t, err)
	summaries = 0
	for _, msg := range all {
		if msg.IsSummaryMessage {
			summaries++
		}
	}
	require.Equal(t, 2, summaries, "a fresh user turn must re-enable compaction")
}
