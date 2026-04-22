package session

import (
	"testing"

	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

func setupForkTest(t *testing.T) (Service, message.Service) {
	t.Helper()

	conn, err := db.Connect(t.Context(), t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })

	q := db.New(conn)
	return NewService(q, conn), message.NewService(q)
}

func TestForkIncludesSelectedAssistantMessage(t *testing.T) {
	t.Parallel()

	sessions, messages := setupForkTest(t)
	source, err := sessions.Create(t.Context(), "Original")
	require.NoError(t, err)

	_, err = messages.Create(t.Context(), source.ID, message.CreateMessageParams{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: "one"}},
	})
	require.NoError(t, err)
	assistant, err := messages.Create(t.Context(), source.ID, message.CreateMessageParams{
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: "two"},
			message.Finish{Reason: message.FinishReasonEndTurn},
		},
		Model:    "model-a",
		Provider: "provider-a",
	})
	require.NoError(t, err)
	_, err = messages.Create(t.Context(), source.ID, message.CreateMessageParams{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: "three"}},
	})
	require.NoError(t, err)

	result, err := Fork(t.Context(), sessions, messages, source.ID, assistant.ID)
	require.NoError(t, err)
	require.Empty(t, result.Prefill)
	require.Equal(t, "Forked: Original", result.Session.Title)

	forkedMessages, err := messages.List(t.Context(), result.Session.ID)
	require.NoError(t, err)
	require.Len(t, forkedMessages, 2)
	require.Equal(t, message.User, forkedMessages[0].Role)
	require.Equal(t, "one", forkedMessages[0].Content().Text)
	require.Equal(t, message.Assistant, forkedMessages[1].Role)
	require.Equal(t, "two", forkedMessages[1].Content().Text)
	require.Equal(t, "model-a", forkedMessages[1].Model)
	require.Equal(t, "provider-a", forkedMessages[1].Provider)
	require.NotNil(t, forkedMessages[1].FinishPart())
}

func TestForkPrefillsSelectedUserMessage(t *testing.T) {
	t.Parallel()

	sessions, messages := setupForkTest(t)
	source, err := sessions.Create(t.Context(), "New Session")
	require.NoError(t, err)

	_, err = messages.Create(t.Context(), source.ID, message.CreateMessageParams{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: "one"}},
	})
	require.NoError(t, err)
	selected, err := messages.Create(t.Context(), source.ID, message.CreateMessageParams{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: "draft this"}},
	})
	require.NoError(t, err)
	_, err = messages.Create(t.Context(), source.ID, message.CreateMessageParams{
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: "after"},
			message.Finish{Reason: message.FinishReasonEndTurn},
		},
	})
	require.NoError(t, err)

	result, err := Fork(t.Context(), sessions, messages, source.ID, selected.ID)
	require.NoError(t, err)
	require.Equal(t, "Forked Session", result.Session.Title)
	require.Equal(t, "draft this", result.Prefill)

	forkedMessages, err := messages.List(t.Context(), result.Session.ID)
	require.NoError(t, err)
	require.Len(t, forkedMessages, 1)
	require.Equal(t, "one", forkedMessages[0].Content().Text)
}

func TestForkAssistantSelectionIncludesAdjacentToolResults(t *testing.T) {
	t.Parallel()

	sessions, messages := setupForkTest(t)
	source, err := sessions.Create(t.Context(), "Tool Session")
	require.NoError(t, err)

	assistant, err := messages.Create(t.Context(), source.ID, message.CreateMessageParams{
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.ToolCall{ID: "tool-1", Name: "bash", Input: `{"command":"echo hi"}`},
			message.Finish{Reason: message.FinishReasonToolUse},
		},
	})
	require.NoError(t, err)
	_, err = messages.Create(t.Context(), source.ID, message.CreateMessageParams{
		Role: message.Tool,
		Parts: []message.ContentPart{
			message.ToolResult{ToolCallID: "tool-1", Name: "bash", Content: "hi"},
		},
	})
	require.NoError(t, err)
	_, err = messages.Create(t.Context(), source.ID, message.CreateMessageParams{
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: "after"},
			message.Finish{Reason: message.FinishReasonEndTurn},
		},
	})
	require.NoError(t, err)

	result, err := Fork(t.Context(), sessions, messages, source.ID, assistant.ID)
	require.NoError(t, err)

	forkedMessages, err := messages.List(t.Context(), result.Session.ID)
	require.NoError(t, err)
	require.Len(t, forkedMessages, 2)
	require.Equal(t, message.Assistant, forkedMessages[0].Role)
	require.Equal(t, message.Tool, forkedMessages[1].Role)
	require.Equal(t, "hi", forkedMessages[1].ToolResults()[0].Content)
}

func TestForkWithStandaloneToolResultIncludesIt(t *testing.T) {
	t.Parallel()

	sessions, messages := setupForkTest(t)
	source, err := sessions.Create(t.Context(), "Tool Session")
	require.NoError(t, err)

	toolMsg, err := messages.Create(t.Context(), source.ID, message.CreateMessageParams{
		Role: message.Tool,
		Parts: []message.ContentPart{
			message.ToolResult{ToolCallID: "orphan-1", Name: "bash", Content: "orphaned output"},
		},
	})
	require.NoError(t, err)

	result, err := Fork(t.Context(), sessions, messages, source.ID, toolMsg.ID)
	require.NoError(t, err)

	forkedMessages, err := messages.List(t.Context(), result.Session.ID)
	require.NoError(t, err)
	require.Len(t, forkedMessages, 1)
	require.Equal(t, message.Tool, forkedMessages[0].Role)
	require.Equal(t, "orphaned output", forkedMessages[0].ToolResults()[0].Content)
}
