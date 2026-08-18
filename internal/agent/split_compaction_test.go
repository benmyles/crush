package agent

import (
	"testing"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

func TestSplitForCompaction_TokenAccountingIncludesToolResults(t *testing.T) {
	t.Parallel()
	// Tool results have large Content but Content().Text returns the first
	// TextContent (empty here). The estimator must count the tool result body.
	msgs := []message.Message{
		{ID: "u1", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "run the tests"}}, CreatedAt: 1},
		{ID: "a1", Role: message.Assistant, Parts: []message.ContentPart{
			message.ToolCall{ID: "tc1", Name: "bash", Input: `{"cmd":"go test"}`},
		}, CreatedAt: 2},
		{ID: "r1", Role: message.Tool, Parts: []message.ContentPart{
			message.ToolResult{ToolCallID: "tc1", Name: "bash", Content: "ok\n" + repeatStr("test output line\n", 2000)},
		}, CreatedAt: 3},
		{ID: "a2", Role: message.Assistant, Parts: []message.ContentPart{
			message.TextContent{Text: "All tests passed."},
			message.Finish{Reason: message.FinishReasonEndTurn},
		}, CreatedAt: 4},
	}
	// keepRecent is small so the tool result (large) must not be retained.
	// The whole session is one turn far above the budget, so it is split
	// mid-turn: the user prompt + tool call + result become the turn prefix
	// and the retained suffix starts at the final assistant message.
	history, turnPrefix, firstRetained := splitForCompaction(msgs, 500)
	require.NotEqual(t, -1, firstRetained, "should have something to compact")
	require.Empty(t, history, "no complete earlier turn to compact")
	require.Len(t, turnPrefix, 3, "the in-flight turn's prefix is compacted")
	require.Equal(t, "u1", turnPrefix[0].ID)
	require.Equal(t, "a2", msgs[firstRetained].ID)
}

func TestSplitForCompaction_SplitsOversizedLastTurnWithoutOrphanResults(t *testing.T) {
	t.Parallel()
	// Turn 1 is small and complete; turn 2 is a long tool loop far above the
	// budget. The split must compact turn 1 as history, the older part of
	// turn 2 as the turn prefix, and the retained suffix must not start with
	// an orphaned tool result.
	msgs := []message.Message{
		{ID: "u1", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "first task"}}, CreatedAt: 1},
		{ID: "a1", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "done first"}}, CreatedAt: 2},
		{ID: "u2", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "second task, long"}}, CreatedAt: 3},
	}
	for i := 0; i < 20; i++ {
		msgs = append(msgs,
			message.Message{ID: "a2-" + string(rune('a'+i)), Role: message.Assistant, Parts: []message.ContentPart{
				message.ToolCall{ID: "tc" + string(rune('a'+i)), Name: "bash", Input: `{"cmd":"go test ./..."}`},
			}, CreatedAt: int64(10 + 2*i)},
			message.Message{ID: "r2-" + string(rune('a'+i)), Role: message.Tool, Parts: []message.ContentPart{
				message.ToolResult{ToolCallID: "tc" + string(rune('a'+i)), Name: "bash", Content: repeatStr("output line\n", 200)},
			}, CreatedAt: int64(11 + 2*i)},
		)
	}
	history, turnPrefix, firstRetained := splitForCompaction(msgs, 2000)
	require.NotEqual(t, -1, firstRetained)
	require.Len(t, history, 2, "the complete first turn is compacted as history")
	require.NotEmpty(t, turnPrefix, "the oversized last turn is split")
	require.Equal(t, "u2", turnPrefix[0].ID, "the turn prefix starts at the turn's user message")
	require.NotEqual(t, message.Tool, msgs[firstRetained].Role, "retained suffix must not start with an orphaned tool result")
	require.Less(t, firstRetained, len(msgs)-1)
	// history + prefix + retained must partition the list contiguously.
	require.Equal(t, len(history)+len(turnPrefix), firstRetained)
}

func TestSplitForCompaction_RetainedTailStartsAtUser(t *testing.T) {
	t.Parallel()
	msgs := []message.Message{
		{ID: "u1", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "do thing 1"}}, CreatedAt: 1},
		{ID: "a1", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "done 1"}}, CreatedAt: 2},
		{ID: "u2", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "do thing 2"}}, CreatedAt: 3},
		{ID: "a2", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "done 2"}}, CreatedAt: 4},
		{ID: "u3", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "do thing 3"}}, CreatedAt: 5},
		{ID: "a3", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "done 3"}}, CreatedAt: 6},
	}
	// The budget covers only the last message; the last turn (u3+a3) is
	// small, so the retained tail is aligned back to start at u3 (user).
	history, turnPrefix, firstRetained := splitForCompaction(msgs, 3)
	require.NotEqual(t, -1, firstRetained)
	require.Equal(t, "u3", msgs[firstRetained].ID, "retained tail must start at a user message")
	require.Equal(t, message.User, msgs[firstRetained].Role)
	require.Nil(t, turnPrefix, "a reasonably sized last turn is retained whole")
	require.Contains(t, history[0].ID, "u1")
}

func TestSplitForCompaction_NothingToCompact(t *testing.T) {
	t.Parallel()
	msgs := []message.Message{
		{ID: "u1", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "hi"}}, CreatedAt: 1},
		{ID: "a1", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "hello"}}, CreatedAt: 2},
	}
	// keepRecent is huge -> everything fits -> nothing to compact.
	history, _, firstRetained := splitForCompaction(msgs, 100000)
	require.Equal(t, -1, firstRetained, "should signal nothing to compact")
	require.Empty(t, history)
}

func TestEstimateStoredMessageTokens_CountsAllParts(t *testing.T) {
	t.Parallel()
	msg := message.Message{
		Parts: []message.ContentPart{
			message.TextContent{Text: "some text"},
			message.ReasoningContent{Thinking: "some reasoning"},
			message.ToolCall{ID: "tc1", Name: "bash", Input: `{"cmd":"ls"}`},
			message.ToolResult{ToolCallID: "tc1", Name: "bash", Content: "file1\nfile2\n"},
			message.ShellCommand{Command: "echo hi", Output: "hi\n"},
		},
	}
	cost := estimateStoredMessageTokens(msg)
	require.Greater(t, cost, int64(0))
	// Must be larger than just the text part alone.
	textOnly := approxTokenCount("some text")
	require.Greater(t, cost, textOnly, "must count reasoning, tool calls, and tool results, not just text")
}

func repeatStr(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}
