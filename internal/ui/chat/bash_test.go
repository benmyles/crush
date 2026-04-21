package chat

import (
	"encoding/json"
	"testing"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/stretchr/testify/require"
)

func TestBashToolRendersLiveCommandOutput(t *testing.T) {
	sty := styles.DefaultStyles()
	input, err := json.Marshal(tools.BashParams{
		Command:     "go test ./...",
		Description: "run tests",
	})
	require.NoError(t, err)

	item := NewBashToolMessageItem(&sty, message.ToolCall{
		ID:       "tool-call-id",
		Name:     tools.BashToolName,
		Input:    string(input),
		Finished: true,
	}, nil, false)
	item.(CommandOutputSettable).SetCommandOutput(&tools.CommandOutputEvent{
		ToolCallID: "tool-call-id",
		Command:    "go test ./...",
		Output:     "running tests\nok",
	})

	rendered := item.Render(100)
	require.Contains(t, rendered, "running tests")
	require.Contains(t, rendered, "ok")
	require.NotContains(t, rendered, "Waiting for tool response")
}

func TestBashToolRendersCollapsibleBackgroundPanel(t *testing.T) {
	sty := styles.DefaultStyles()
	input, err := json.Marshal(tools.BashParams{
		Command:         "go test ./...",
		Description:     "run tests",
		RunInBackground: true,
	})
	require.NoError(t, err)

	item := NewBashToolMessageItem(&sty, message.ToolCall{
		ID:       "tool-call-id",
		Name:     tools.BashToolName,
		Input:    string(input),
		Finished: true,
	}, nil, false)
	item.(CommandOutputSettable).SetCommandOutput(&tools.CommandOutputEvent{
		ToolCallID:  "tool-call-id",
		ShellID:     "001",
		Command:     "go test ./...",
		Description: "run tests",
		Output:      "line 1\nline 2",
		Background:  true,
	})

	collapsed := item.Render(100)
	require.Contains(t, collapsed, "PID 001")
	require.Contains(t, collapsed, "Running: line 2")
	require.NotContains(t, collapsed, "line 1")

	require.True(t, item.(Expandable).ToggleExpanded())
	expanded := item.Render(100)
	require.Contains(t, expanded, "line 1")
	require.Contains(t, expanded, "line 2")
}
