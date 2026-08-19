package tools

import (
	"errors"
	"fmt"

	"github.com/charmbracelet/crush/internal/terminal"
)

const (
	TerminalStartToolName  = "terminal_start"
	TerminalInputToolName  = "terminal_input"
	TerminalOutputToolName = "terminal_output"
	TerminalResizeToolName = "terminal_resize"
	TerminalKillToolName   = "terminal_kill"

	terminalDefaultSettleMS = 300
	terminalMaxWaitMS       = 30000
)

// TerminalResponseMetadata is attached to every terminal tool response so
// the UI and the model know which terminal a result belongs to.
type TerminalResponseMetadata struct {
	TerminalID string `json:"terminal_id,omitempty"`
	Command    string `json:"command,omitempty"`
	Cols       int    `json:"cols,omitempty"`
	Rows       int    `json:"rows,omitempty"`
	Running    bool   `json:"running,omitempty"`
	Existing   bool   `json:"existing,omitempty"`
}

// tmuxUnavailable returns the error text shown when tmux is missing. The
// tools fail with an informative message instead of a raw exec error.
func tmuxUnavailable(err error) string {
	if errors.Is(err, terminal.ErrNoTmux) {
		return "tmux is not installed. Interactive terminals require tmux. Install it (e.g. `brew install tmux`, `apt install tmux`) and try again."
	}
	return fmt.Sprintf("interactive terminals require tmux: %v", err)
}

// validateTerminalID ensures a caller-supplied terminal ID is one Crush
// owns. This is defense in depth: the controller re-checks as well.
func validateTerminalID(id string) error {
	if id == "" {
		return errors.New("terminal_id is required")
	}
	if len(id) < 2 || id[:len(terminal.SessionPrefix)] != terminal.SessionPrefix {
		return fmt.Errorf("invalid terminal ID %q", id)
	}
	return nil
}
