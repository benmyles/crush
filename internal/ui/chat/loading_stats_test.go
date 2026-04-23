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

func TestForegroundCommandResultOverridesStaleLiveOutput(t *testing.T) {
	t.Parallel()

	sty := styles.DefaultStyles()
	input := `{"command":"printf final"}`
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
		Command:    "printf final",
		Output:     "partial output",
	})

	result := message.ToolResult{
		ToolCallID: "tool-1",
		Name:       agenttools.BashToolName,
		Content:    "final output",
	}
	item.SetResult(&result)

	rendered := ansi.Strip(item.Render(80))
	require.Contains(t, rendered, "final output")
	require.NotContains(t, rendered, "partial output")
	require.NotContains(t, rendered, "Waiting for tool response")
	require.NotContains(t, rendered, "down 0")

	settable.SetCommandOutput(&agenttools.CommandOutputEvent{
		SessionID:  "session-1",
		MessageID:  "assistant-1",
		ToolCallID: "tool-1",
		Command:    "printf final",
		Output:     "late partial output",
	})

	rendered = ansi.Strip(item.Render(80))
	require.Contains(t, rendered, "final output")
	require.NotContains(t, rendered, "late partial output")
}

func TestBackgroundCommandOutputSurvivesInitialResult(t *testing.T) {
	t.Parallel()

	sty := styles.DefaultStyles()
	input := `{"command":"npm run dev","run_in_background":true}`
	item := NewToolMessageItem(&sty, "assistant-1", message.ToolCall{
		ID:       "tool-1",
		Name:     agenttools.BashToolName,
		Input:    input,
		Finished: true,
	}, nil, false)

	item.(CommandOutputSettable).SetCommandOutput(&agenttools.CommandOutputEvent{
		SessionID:  "session-1",
		MessageID:  "assistant-1",
		ToolCallID: "tool-1",
		ShellID:    "123",
		Command:    "npm run dev",
		Output:     "server ready",
		Background: true,
	})
	result := message.ToolResult{
		ToolCallID: "tool-1",
		Name:       agenttools.BashToolName,
		Content:    "Command is running in the background.",
	}
	item.SetResult(&result)

	rendered := ansi.Strip(item.Render(80))
	require.Contains(t, rendered, "PID 123")
	require.Contains(t, rendered, "Running: server ready")
	require.NotContains(t, rendered, "Command is running in the background")
}

func TestTerminalParentFinishStopsNoResultToolSpinner(t *testing.T) {
	t.Parallel()

	sty := styles.DefaultStyles()
	item := NewToolMessageItem(&sty, "assistant-1", message.ToolCall{
		ID:       "tool-1",
		Name:     "provider_tool",
		Input:    `{"query":"done"}`,
		Finished: true,
	}, nil, false)
	item.SetParentFinishReason(message.FinishReasonEndTurn)

	rendered := ansi.Strip(item.Render(80))
	require.Contains(t, rendered, "Provider Tool")
	require.NotContains(t, rendered, "Waiting for tool response")
	require.NotContains(t, rendered, "up ")
	require.NotContains(t, rendered, "down ")
}

func TestToolUseParentKeepsNoResultToolSpinner(t *testing.T) {
	t.Parallel()

	sty := styles.DefaultStyles()
	input := `{"query":"still waiting"}`
	item := NewToolMessageItem(&sty, "assistant-1", message.ToolCall{
		ID:       "tool-1",
		Name:     "provider_tool",
		Input:    input,
		Finished: true,
	}, nil, false)
	item.SetParentFinishReason(message.FinishReasonToolUse)

	rendered := ansi.Strip(item.Render(80))
	require.Contains(t, rendered, "Waiting for tool response")
	require.Contains(t, rendered, fmt.Sprintf("up %d", utf8.RuneCountInString(input)))
	require.Contains(t, rendered, "down 0")
}
