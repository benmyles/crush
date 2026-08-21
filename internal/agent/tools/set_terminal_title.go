package tools

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"charm.land/fantasy"
)

//go:embed set_terminal_title.md
var setTerminalTitleDescription string

// SetTerminalTitleToolName is the tool name the agent calls to curate
// the terminal window title.
const SetTerminalTitleToolName = "set_terminal_title"

// SetTerminalTitleParams carries the curated title text.
type SetTerminalTitleParams struct {
	// Title is the curated terminal title: a terse 2-4 word phrase
	// describing the current task. Empty clears the custom title.
	Title string `json:"title" description:"A terse 2-4 word phrase describing the current task, e.g. \"Migrating auth queries\". Empty string clears the custom title."`
}

// TerminalTitleNotifier delivers a terminal title change
// (sessionID-scoped) to the host. It may be nil.
type TerminalTitleNotifier func(sessionID, title string)

// NewSetTerminalTitleTool creates the tool the agent uses to keep the
// terminal window title in sync with the current task.
func NewSetTerminalTitleTool(notify TerminalTitleNotifier) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		SetTerminalTitleToolName,
		setTerminalTitleDescription,
		func(ctx context.Context, params SetTerminalTitleParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			sessionID := GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, fmt.Errorf("session ID is required for setting the terminal title")
			}
			title := sanitizeTitle(params.Title)
			if title != "" {
				if words := strings.Fields(title); len(words) > 4 {
					return fantasy.ToolResponse{}, fmt.Errorf(
						"title must be at most 4 words, got %d; condense %q to a terse phrase",
						len(words), title,
					)
				}
			}
			if notify != nil {
				notify(sessionID, title)
			}
			if title == "" {
				return fantasy.NewTextResponse("Terminal title cleared."), nil
			}
			return fantasy.NewTextResponse(fmt.Sprintf("Terminal title set to %q.", title)), nil
		},
	)
}

// sanitizeTitle flattens whitespace and drops control and escape
// sequences so the title renders as a single clean line.
func sanitizeTitle(s string) string {
	runes := []rune(s)
	var b strings.Builder
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == 0x1b {
			// Skip the whole escape sequence: ESC followed by parameter
			// bytes and a final byte in 0x40..0x7E. The CSI introducer
			// '[' (0x5B) falls in that range, so skip it explicitly
			// before scanning for the final byte.
			if i+1 < len(runes) && runes[i+1] == '[' {
				i++
			}
			for i+1 < len(runes) {
				i++
				if runes[i] >= 0x40 && runes[i] <= 0x7e {
					break
				}
			}
			continue
		}
		if r < 0x20 || r == 0x7f {
			b.WriteRune(' ')
			continue
		}
		b.WriteRune(r)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
