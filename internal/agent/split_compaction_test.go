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
	// keepRecent is small so the tool result (large) should be in history.
	history, _, firstRetained := splitForCompaction(msgs, 500)
	require.NotEqual(t, -1, firstRetained, "should have something to compact")
	require.NotEmpty(t, history, "history should not be empty")
	// The retained tail starts at the budget boundary. In this case the
	// retained tail is the final assistant message (a2) with no user message
	// after it, so it starts at an assistant message — that is correct; the
	// user prompt + tool result are compacted.
	require.Equal(t, "a2", msgs[firstRetained].ID)
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
	// Keep only the last message. The retained tail must start at u3 (user).
	history, _, firstRetained := splitForCompaction(msgs, 1)
	require.NotEqual(t, -1, firstRetained)
	require.Equal(t, "u3", msgs[firstRetained].ID, "retained tail must start at a user message")
	require.Equal(t, message.User, msgs[firstRetained].Role)
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
