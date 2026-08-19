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
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/terminal"
)

//go:embed terminal_input.md
var terminalInputDescription string

type TerminalInputParams struct {
	TerminalID string   `json:"terminal_id" description:"The ID of the terminal to send input to."`
	Text       string   `json:"text,omitempty" description:"Text to type into the terminal, sent literally (e.g. 'ls -la')."`
	Keys       []string `json:"keys,omitempty" description:"Optional control keys to press after the text, e.g. [\"Enter\"], [\"C-c\"], [\"Up\", \"Enter\"]. Supported: enter, esc, tab, backspace, delete, up, down, left, right, home, end, pgup, pgdown, space, f1-f12, ctrl-a..ctrl-z."`
	Enter      bool     `json:"enter,omitempty" description:"If true, press Enter after sending the text (same as adding \"enter\" to keys)."`
	SettleMS   int      `json:"settle_ms,omitempty" description:"How long to wait in milliseconds before reading the screen back, giving the program time to react (default 300, max 30000)."`
	ReadBack   bool     `json:"read_back,omitempty" description:"If true (default), the current screen is returned after sending input so you can see the result. Set to false for rapid multi-step sequences, then read the screen once with terminal_output."`
}

// keyAliases maps friendly lowercase key names to tmux key names.
var keyAliases = map[string]string{
	"enter": "Enter", "return": "Enter",
	"esc": "Escape", "escape": "Escape",
	"tab":       "Tab",
	"backspace": "BSpace", "bspace": "BSpace",
	"delete": "DC", "del": "DC",
	"up": "Up", "down": "Down", "left": "Left", "right": "Right",
	"home": "Home", "end": "End",
	"pgup": "PPage", "pageup": "PPage",
	"pgdown": "NPage", "pagedown": "NPage", "pgdn": "NPage",
	"space": "Space",
	"f1":    "F1", "f2": "F2", "f3": "F3", "f4": "F4", "f5": "F5", "f6": "F6",
	"f7": "F7", "f8": "F8", "f9": "F9", "f10": "F10", "f11": "F11", "f12": "F12",
}

// normalizeKeys converts friendly key names to tmux key names.
func normalizeKeys(keys []string) ([]string, error) {
	names := make([]string, 0, len(keys))
	for _, raw := range keys {
		name := strings.ToLower(strings.TrimSpace(raw))
		mapped, ok := keyAliases[name]
		if !ok && strings.HasPrefix(name, "ctrl-") && len(name) == len("ctrl-x") {
			mapped = "C-" + strings.ToUpper(name[len("ctrl-"):])
			ok = mapped != "C--" // Malformed ctrl- input.
		}
		if !ok {
			return nil, fmt.Errorf("unsupported key %q (supported: enter, esc, tab, backspace, delete, up, down, left, right, home, end, pgup, pgdown, space, f1-f12, ctrl-a..ctrl-z)", raw)
		}
		names = append(names, mapped)
	}
	return names, nil
}

func NewTerminalInputTool(permissions permission.Service, workingDir string, ctrl *terminal.Controller) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		TerminalInputToolName,
		terminalInputDescription,
		func(ctx context.Context, params TerminalInputParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if err := validateTerminalID(params.TerminalID); err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}

			sessionID := GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, errors.New("session ID is required for sending terminal input")
			}

			desc := fmt.Sprintf("Send input to terminal %s", params.TerminalID)
			if params.Text != "" {
				summarized := params.Text
				if len(summarized) > 40 {
					summarized = summarized[:40] + "..."
				}
				desc += fmt.Sprintf(": %s", summarized)
			}

			p, err := permissions.Request(
				ctx,
				permission.CreatePermissionRequest{
					SessionID:   sessionID,
					Path:        workingDir,
					ToolCallID:  call.ID,
					ToolName:    TerminalInputToolName,
					Action:      "input",
					Description: desc,
					Params:      params,
				},
			)
			if err != nil {
				return fantasy.ToolResponse{}, err
			}
			if !p {
				return NewPermissionDeniedResponse(), nil
			}

			if err := ctrl.Available(); err != nil {
				return fantasy.NewTextErrorResponse(tmuxUnavailable(err)), nil
			}

			names, err := normalizeKeys(params.Keys)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}

			if params.SettleMS < 0 || params.SettleMS > terminalMaxWaitMS {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("settle_ms must be between 0 and %d (got %d)", terminalMaxWaitMS, params.SettleMS)), nil
			}

			if params.Text == "" && len(names) == 0 && !params.Enter {
				return fantasy.NewTextErrorResponse("nothing to send: provide text, keys, or enter"), nil
			}

			if err := ctrl.SendKeys(ctx, params.TerminalID, params.Text, names, params.Enter); err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("failed to send input: %v", err)), nil
			}

			readBack := cmp.Or(params.ReadBack, true)
			settleMS := cmp.Or(params.SettleMS, terminalDefaultSettleMS)
			if readBack && settleMS > 0 {
				select {
				case <-ctx.Done():
					return fantasy.ToolResponse{}, ctx.Err()
				case <-time.After(time.Duration(settleMS) * time.Millisecond):
				}
			}

			content := fmt.Sprintf("Input sent to terminal %s", params.TerminalID)
			if readBack {
				if screen, err := ctrl.Capture(ctx, params.TerminalID, false); err == nil && screen != "" {
					content += "\n\n" + TruncateOutput(screen)
				}
			}

			meta := TerminalResponseMetadata{TerminalID: params.TerminalID, Running: true}
			return fantasy.WithResponseMetadata(fantasy.NewTextResponse(content), meta), nil
		},
	)
}
