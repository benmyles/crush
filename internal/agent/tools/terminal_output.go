package tools

import (
	"cmp"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/terminal"
)

//go:embed terminal_output.md
var terminalOutputDescription string

type TerminalOutputParams struct {
	TerminalID string `json:"terminal_id,omitempty" description:"The terminal to read. Omit to list all active terminals and their IDs (useful for reconnecting)."`
	History    bool   `json:"history,omitempty" description:"If true, include the full scrollback history instead of only the current visible screen. Use when long output scrolled past the bottom."`
	WaitFor    string `json:"wait_for,omitempty" description:"Optional text to wait for. Polls the terminal until this text appears anywhere in its output, then returns the full history. Essential for ssh: wait for prompts like 'password:' or a shell prompt such as '$ ' before sending the next input."`
	TimeoutMs  int    `json:"timeout_ms,omitempty" description:"How long to wait for wait_for in milliseconds (default 10000, max 60000)."`
}

func NewTerminalOutputTool(ctrl *terminal.Controller) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		TerminalOutputToolName,
		terminalOutputDescription,
		func(ctx context.Context, params TerminalOutputParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.TerminalID == "" {
				return listTerminalsResponse(ctx, ctrl)
			}

			if err := validateTerminalID(params.TerminalID); err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			if err := ctrl.Available(); err != nil {
				return fantasy.NewTextErrorResponse(tmuxUnavailable(err)), nil
			}

			var (
				screen    string
				matched   bool
				err       error
				waitedFor bool
			)
			switch {
			case params.WaitFor != "":
				timeoutMS := cmp.Or(params.TimeoutMs, terminalDefaultWaitMS)
				timeoutMS = min(timeoutMS, terminalMaxWaitMS)
				waitedFor = true
				screen, matched, err = ctrl.WaitFor(ctx, params.TerminalID, params.WaitFor, time.Duration(timeoutMS)*time.Millisecond)
			default:
				screen, err = ctrl.Capture(ctx, params.TerminalID, params.History)
			}
			if err != nil {
				if errors.Is(err, terminal.ErrNotFound) {
					return fantasy.NewTextErrorResponse(fmt.Sprintf("terminal %q is not running; use terminal_output (no terminal_id) to list active terminals", params.TerminalID)), nil
				}
				return fantasy.NewTextErrorResponse(fmt.Sprintf("failed to read terminal: %v", err)), nil
			}

			var b strings.Builder
			fmt.Fprintf(&b, "Terminal %s", params.TerminalID)
			if params.History {
				b.WriteString(" (full history)")
			} else {
				b.WriteString(" (current screen)")
			}
			if waitedFor {
				if matched {
					fmt.Fprintf(&b, ", wait_for %q matched", params.WaitFor)
				} else {
					fmt.Fprintf(&b, ", wait_for %q not found before timeout", params.WaitFor)
				}
			}
			b.WriteString("\n\n")
			b.WriteString(TruncateOutput(screen))

			meta := TerminalResponseMetadata{TerminalID: params.TerminalID, Running: matched || err == nil}
			return fantasy.WithResponseMetadata(fantasy.NewTextResponse(b.String()), meta), nil
		},
	)
}

// listTerminalsResponse reports all live Crush terminals.
func listTerminalsResponse(ctx context.Context, ctrl *terminal.Controller) (fantasy.ToolResponse, error) {
	if err := ctrl.Available(); err != nil {
		return fantasy.NewTextErrorResponse(tmuxUnavailable(err)), nil
	}
	sessions, err := ctrl.List(ctx)
	if err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("failed to list terminals: %v", err)), nil
	}
	if len(sessions) == 0 {
		return fantasy.NewTextResponse("No active terminals. Start one with terminal_start."), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d active terminal(s):\n", len(sessions))
	for _, s := range sessions {
		fmt.Fprintf(&b, "- %s (%s, %dx%d)\n", s.ID, s.Command, s.Cols, s.Rows)
	}
	return fantasy.NewTextResponse(b.String()), nil
}
