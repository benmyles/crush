package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// TestSummaryMessageItemCollapsedByDefault asserts that a plain summarize
// message (no structured CompactionContent part) collapses to a one-line
// preview instead of flooding the chat, and expands on toggle.
func TestSummaryMessageItemCollapsedByDefault(t *testing.T) {
	t.Parallel()

	sty := styles.CharmtonePantera()
	styPtr := &sty
	msg := &message.Message{
		ID:               "sum-1",
		Role:             message.Assistant,
		IsSummaryMessage: true,
		Parts: []message.ContentPart{
			message.TextContent{Text: "# Session summary\nthis is the body"},
		},
	}
	item := NewSummaryMessageItem(styPtr, msg)

	rendered := ansi.Strip(item.Render(80))
	require.Contains(t, rendered, "Session summary", "collapsed view must show the first line")
	require.NotContains(t, rendered, "this is the body", "collapsed view must hide the body")
	require.Contains(t, rendered, "space to expand", "collapsed view must show the expand hint")
	require.Len(t, strings.Split(strings.TrimRight(rendered, "\n"), "\n"), 1,
		"collapsed view must be a single line")

	require.True(t, item.ToggleExpanded())
	rendered = ansi.Strip(item.Render(80))
	require.Contains(t, rendered, "this is the body", "expanded view must show the body")
	require.NotContains(t, rendered, "space to expand", "expanded view must drop the hint")

	require.False(t, item.ToggleExpanded())
	rendered = ansi.Strip(item.Render(80))
	require.NotContains(t, rendered, "this is the body", "re-collapsed view must hide the body again")
}

// TestSummaryMessageItemStreamsPreview verifies SetMessage keeps the
// collapsed preview current while the summary streams, and that an empty
// summary still renders (the working spinner path).
func TestSummaryMessageItemStreamsPreview(t *testing.T) {
	t.Parallel()

	sty := styles.CharmtonePantera()
	styPtr := &sty
	msg := &message.Message{
		ID:               "sum-2",
		Role:             message.Assistant,
		IsSummaryMessage: true,
	}
	item := NewSummaryMessageItem(styPtr, msg)

	// No content yet: must render without panicking.
	require.NotPanics(t, func() { _ = item.Render(80) })

	msg.AppendContent("part one of the summary")
	_ = item.SetMessage(msg)
	rendered := ansi.Strip(item.Render(80))
	require.Contains(t, rendered, "part one of the summary")

	msg.AppendContent(" and now part two")
	_ = item.SetMessage(msg)
	rendered = ansi.Strip(item.Render(80))
	require.Contains(t, rendered, "and now part two", "preview must stream with the message")
}
