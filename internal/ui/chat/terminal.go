package chat

import (
	"cmp"
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
)

// TerminalToolMessageItem renders terminal_start, terminal_input,
// terminal_output, terminal_resize, and terminal_kill tool calls.
type TerminalToolMessageItem struct {
	*baseToolMessageItem
}

var _ ToolMessageItem = (*TerminalToolMessageItem)(nil)

// NewTerminalToolMessageItem creates a new [TerminalToolMessageItem].
func NewTerminalToolMessageItem(
	sty *styles.Styles,
	toolCall message.ToolCall,
	result *message.ToolResult,
	canceled bool,
) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &TerminalToolRenderContext{}, canceled)
}

// TerminalToolRenderContext renders terminal tool messages.
type TerminalToolRenderContext struct{}

// RenderTool implements the [ToolRenderer] interface.
func (t *TerminalToolRenderContext) RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
	cappedWidth := cappedMessageWidth(width)
	if opts.IsPending() {
		return pendingTool(sty, "Terminal", opts, opts.Compact)
	}

	action, terminalID, description := terminalToolHeader(opts.ToolCall)

	header := terminalHeader(sty, opts.Status, action, terminalID, description, cappedWidth)
	if opts.Compact {
		return header
	}

	if earlyState, ok := toolEarlyStateContent(sty, opts, cappedWidth); ok {
		return joinToolParts(header, earlyState)
	}

	if !opts.HasResult() {
		return header
	}

	content := opts.Result.Content
	if content == "" {
		return header
	}

	bodyWidth := cappedWidth - toolBodyLeftPaddingTotal
	body := sty.Tool.Body.Render(toolOutputPlainContent(sty, content, bodyWidth, opts.ExpandedContent))
	return joinToolParts(header, body)
}

// terminalHeader builds the header line:
// "● Terminal (input) ID crush-abc description...".
func terminalHeader(sty *styles.Styles, status ToolStatus, action, terminalID, description string, width int) string {
	icon := toolIcon(sty, status)
	termPart := sty.Tool.JobToolName.Render("Terminal")
	actionPart := sty.Tool.JobAction.Render("(" + action + ")")
	idPart := sty.Tool.JobPID.Render("ID " + terminalID)

	prefix := fmt.Sprintf("%s %s %s %s", icon, termPart, actionPart, idPart)

	if description == "" {
		return prefix
	}

	prefixWidth := lipgloss.Width(prefix)
	availableWidth := width - prefixWidth - 1
	if availableWidth < 10 {
		return prefix
	}

	truncatedDesc := ansi.Truncate(description, availableWidth, "…")
	return prefix + " " + sty.Tool.JobDescription.Render(truncatedDesc)
}

// terminalToolHeader extracts the action verb, terminal ID, and a short
// description from a terminal tool call's input.
func terminalToolHeader(toolCall message.ToolCall) (action, terminalID, description string) {
	switch toolCall.Name {
	case tools.TerminalStartToolName:
		var params tools.TerminalStartParams
		if err := json.Unmarshal([]byte(toolCall.Input), &params); err == nil {
			return "start", cmp.Or(params.Name, "new"), cmp.Or(params.Description, params.Command)
		}
		return "start", "", ""

	case tools.TerminalInputToolName:
		var params tools.TerminalInputParams
		if err := json.Unmarshal([]byte(toolCall.Input), &params); err == nil {
			desc := params.Text
			if len(params.Keys) > 0 {
				desc = strings.Join(params.Keys, ", ")
			}
			return "input", params.TerminalID, desc
		}
		return "input", "", ""

	case tools.TerminalOutputToolName:
		var params tools.TerminalOutputParams
		if err := json.Unmarshal([]byte(toolCall.Input), &params); err == nil {
			if params.TerminalID == "" {
				return "output", "list", "list active terminals"
			}
			desc := params.WaitFor
			if desc == "" && params.History {
				desc = "full history"
			}
			return "output", params.TerminalID, desc
		}
		return "output", "", ""

	case tools.TerminalResizeToolName:
		var params tools.TerminalResizeParams
		if err := json.Unmarshal([]byte(toolCall.Input), &params); err == nil {
			return "resize", params.TerminalID, fmt.Sprintf("%dx%d", params.Cols, params.Rows)
		}
		return "resize", "", ""

	case tools.TerminalKillToolName:
		var params tools.TerminalKillParams
		if err := json.Unmarshal([]byte(toolCall.Input), &params); err == nil {
			if params.All {
				return "kill", "all", "kill all terminals"
			}
			return "kill", params.TerminalID, ""
		}
		return "kill", "", ""
	}

	return "terminal", "", ""
}
