package chat

import (
	"encoding/json"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
)

// StatusUpdateToolMessageItem is a message item that represents a
// status_update tool call: the agent's mini standup report.
type StatusUpdateToolMessageItem struct {
	*baseToolMessageItem
}

var _ ToolMessageItem = (*StatusUpdateToolMessageItem)(nil)

// NewStatusUpdateToolMessageItem creates a new [StatusUpdateToolMessageItem].
func NewStatusUpdateToolMessageItem(
	sty *styles.Styles,
	toolCall message.ToolCall,
	result *message.ToolResult,
	canceled bool,
) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &StatusUpdateRenderContext{}, canceled)
}

// StatusUpdateRenderContext renders status_update tool messages.
type StatusUpdateRenderContext struct{}

// RenderTool implements the [ToolRenderer] interface.
func (s *StatusUpdateRenderContext) RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
	cappedWidth := cappedMessageWidth(width)
	if opts.IsPending() {
		return pendingTool(sty, "Status", opts.Anim, opts.Compact)
	}

	var params tools.StatusUpdateParams
	summary := ""
	if err := json.Unmarshal([]byte(opts.ToolCall.Input), &params); err == nil && params.Doing != "" {
		summary = sty.Tool.TodoJustStarted.Render(params.Doing) + " · "
	}

	toolParams := []string{summary}
	header := toolHeader(sty, opts.Status, "Status", cappedWidth, opts, toolParams...)
	if opts.Compact {
		return header
	}

	if earlyState, ok := toolEarlyStateContent(sty, opts, cappedWidth); ok {
		return joinToolParts(header, earlyState)
	}

	if err := json.Unmarshal([]byte(opts.ToolCall.Input), &params); err != nil {
		return header
	}

	rows := []string{
		statusRow(sty.Sidebar.StatusDone, "✓", params.Done),
		statusRow(sty.Sidebar.StatusDoing, "→", params.Doing),
		statusRow(sty.Sidebar.StatusNext, "▸", params.Next),
	}
	if params.Blockers != "" {
		rows = append(rows, statusRow(sty.Sidebar.StatusBlockers, "⛔", params.Blockers))
	}
	body := sty.Tool.Body.Render(strings.Join(rows, "\n"))
	return joinToolParts(header, body)
}

func statusRow(valueStyle lipgloss.Style, icon, value string) string {
	if value == "" {
		return ""
	}
	return icon + " " + valueStyle.Render(value)
}
