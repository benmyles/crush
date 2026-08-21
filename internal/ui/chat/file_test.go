package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestPendingWriteToolTracksStreamedCharacters(t *testing.T) {
	t.Parallel()

	sty := styles.CharmtonePantera()
	item := NewWriteToolMessageItem(&sty, message.ToolCall{
		ID:       "write-1",
		Name:     tools.WriteToolName,
		Input:    strings.Repeat("x", 12_734),
		Finished: false,
	}, nil, false)

	rendered := ansi.Strip(item.Render(80))
	require.Contains(t, rendered, "Write 12k [=======...] 12,734 chars")
	require.NotContains(t, rendered, "$", "the generic scrambled animation must not be rendered")
	require.Nil(t, item.(Animatable).StartAnimation(), "the count-driven meter must not start a timer animation")

	item.SetToolCall(message.ToolCall{
		ID:       "write-1",
		Name:     tools.WriteToolName,
		Input:    strings.Repeat("x", 13_108),
		Finished: false,
	})
	require.Contains(t, ansi.Strip(item.Render(80)), "Write 13k [=.........] 13,108 chars")
}

func TestPendingWriteToolMeterResetsEachThousand(t *testing.T) {
	t.Parallel()

	sty := styles.CharmtonePantera()
	tests := []struct {
		name      string
		charCount int
		want      string
	}{
		{name: "empty", charCount: 0, want: "Write 0k [..........] 0 chars"},
		{name: "initial characters", charCount: 1, want: "Write 0k [=.........] 1 chars"},
		{name: "almost full", charCount: 999, want: "Write 0k [=========.] 999 chars"},
		{name: "reset", charCount: 1000, want: "Write 1k [..........] 1,000 chars"},
		{name: "next chunk", charCount: 1108, want: "Write 1k [=.........] 1,108 chars"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			opts := &ToolRenderOpts{
				ToolCall: message.ToolCall{
					ID:    "write-" + test.name,
					Name:  tools.WriteToolName,
					Input: strings.Repeat("x", test.charCount),
				},
				Status: ToolStatusRunning,
			}
			rendered := ansi.Strip(pendingTool(&sty, "Write", opts, false))
			require.Contains(t, rendered, test.want)
		})
	}
}

func TestPendingWriteToolCountsUnicodeCharacters(t *testing.T) {
	t.Parallel()

	sty := styles.CharmtonePantera()
	item := NewWriteToolMessageItem(&sty, message.ToolCall{
		ID:       "write-unicode",
		Name:     tools.WriteToolName,
		Input:    "💜crush",
		Finished: false,
	}, nil, false)

	require.Contains(t, ansi.Strip(item.Render(80)), "Write 0k [=.........] 6 chars")
}
