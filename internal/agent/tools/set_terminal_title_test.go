package tools

import (
	"context"
	"encoding/json"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"
)

// runTitleTool invokes the set_terminal_title tool directly with a
// session-scoped context, mirroring the agent's tool dispatch.
func runTitleTool(t *testing.T, tool fantasy.AgentTool, sessionID string, params any) (fantasy.ToolResponse, error) {
	t.Helper()
	input, err := json.Marshal(params)
	require.NoError(t, err)
	ctx := context.WithValue(context.Background(), SessionIDContextKey, sessionID)
	return tool.Run(ctx, fantasy.ToolCall{ID: "t1", Name: "set_terminal_title", Input: string(input)})
}

func TestSetTerminalTitleTool(t *testing.T) {
	t.Parallel()

	newTool := func() (fantasy.AgentTool, *[]titleEvent) {
		var events []titleEvent
		tool := NewSetTerminalTitleTool(func(sessionID, title string) {
			events = append(events, titleEvent{sessionID: sessionID, title: title})
		})
		return tool, &events
	}

	t.Run("sets a curated title", func(t *testing.T) {
		t.Parallel()
		tool, events := newTool()
		resp, err := runTitleTool(t, tool, "sess-1", SetTerminalTitleParams{Title: "Migrating auth queries"})
		require.NoError(t, err)
		require.Contains(t, resp.Content, "Migrating auth queries")
		require.Equal(t, []titleEvent{{sessionID: "sess-1", title: "Migrating auth queries"}}, *events)
	})

	t.Run("flattens whitespace and control characters", func(t *testing.T) {
		t.Parallel()
		tool, events := newTool()
		_, err := runTitleTool(t, tool, "sess-1", SetTerminalTitleParams{Title: "  fixing\tdeploy\x1b[31m\npipeline  "})
		require.NoError(t, err)
		require.Equal(t, "fixing deploy pipeline", (*events)[0].title)
	})

	t.Run("empty title clears", func(t *testing.T) {
		t.Parallel()
		tool, events := newTool()
		resp, err := runTitleTool(t, tool, "sess-1", SetTerminalTitleParams{})
		require.NoError(t, err)
		require.Contains(t, resp.Content, "cleared")
		require.Equal(t, []titleEvent{{sessionID: "sess-1", title: ""}}, *events)
	})

	t.Run("more than four words errors", func(t *testing.T) {
		t.Parallel()
		tool, events := newTool()
		_, err := runTitleTool(t, tool, "sess-1", SetTerminalTitleParams{Title: "one two three four five"})
		require.Error(t, err)
		require.Empty(t, *events)
	})

	t.Run("missing session ID errors", func(t *testing.T) {
		t.Parallel()
		tool, _ := newTool()
		_, err := tool.Run(context.Background(), fantasy.ToolCall{
			ID:    "t2",
			Name:  "set_terminal_title",
			Input: `{"title":"x"}`,
		})
		require.Error(t, err)
	})
}

type titleEvent struct {
	sessionID string
	title     string
}
