package chat

import (
	"fmt"
	"testing"
	"unicode/utf8"

	agenttools "github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestFormatLoadingCount(t *testing.T) {
	t.Parallel()

	require.Equal(t, "0", formatLoadingCount(0))
	require.Equal(t, "999", formatLoadingCount(999))
	require.Equal(t, "1k", formatLoadingCount(1_000))
	require.Equal(t, "1.5k", formatLoadingCount(1_500))
	require.Equal(t, "12k", formatLoadingCount(12_000))
	require.Equal(t, "1.2m", formatLoadingCount(1_250_000))
}

func TestAssistantStreamingRenderShowsLoadingStats(t *testing.T) {
	t.Parallel()

	sty := styles.DefaultStyles()
	msg := &message.Message{
		ID:   "assistant-1",
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: "hello"},
		},
	}
	item := NewAssistantMessageItem(&sty, msg).(*AssistantMessageItem)
	item.SetLoadingUpChars(42)

	rendered := ansi.Strip(item.Render(80))
	require.Contains(t, rendered, "hello")
	require.Contains(t, rendered, "up 42")
	require.Contains(t, rendered, "down 5")
}

func TestToolLoadingRenderShowsUpDownStats(t *testing.T) {
	t.Parallel()

	sty := styles.DefaultStyles()
	input := `{"command":"printf hello"}`
	item := NewToolMessageItem(&sty, "assistant-1", message.ToolCall{
		ID:       "tool-1",
		Name:     agenttools.BashToolName,
		Input:    input,
		Finished: true,
	}, nil, false)

	rendered := ansi.Strip(item.Render(80))
	require.Contains(t, rendered, "Waiting for tool response")
	require.Contains(t, rendered, fmt.Sprintf("up %d", utf8.RuneCountInString(input)))
	require.Contains(t, rendered, "down 0")
}

func TestToolLoadingRenderCountsLiveOutput(t *testing.T) {
	t.Parallel()

	sty := styles.DefaultStyles()
	input := `{"command":"printf hello"}`
	item := NewToolMessageItem(&sty, "assistant-1", message.ToolCall{
		ID:       "tool-1",
		Name:     agenttools.BashToolName,
		Input:    input,
		Finished: true,
	}, nil, false)
	settable := item.(CommandOutputSettable)
	settable.SetCommandOutput(&agenttools.CommandOutputEvent{
		SessionID:  "session-1",
		MessageID:  "assistant-1",
		ToolCallID: "tool-1",
		Command:    "printf hello",
		Output:     "hello",
	})

	rendered := ansi.Strip(item.Render(80))
	require.Contains(t, rendered, "hello")
	require.Contains(t, rendered, fmt.Sprintf("up %d", utf8.RuneCountInString(input)))
	require.Contains(t, rendered, "down 5")
}
