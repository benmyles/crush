package tools

// CommandOutputEvent is a runtime-only update for command output.
//
// These events are used by frontends to render live command output while the
// final persisted tool result remains the source of truth for session history.
type CommandOutputEvent struct {
	SessionID        string `json:"session_id"`
	MessageID        string `json:"message_id"`
	ToolCallID       string `json:"tool_call_id"`
	ShellID          string `json:"shell_id"`
	Command          string `json:"command"`
	Description      string `json:"description"`
	WorkingDirectory string `json:"working_directory"`
	Output           string `json:"output"`
	Background       bool   `json:"background"`
	Done             bool   `json:"done"`
	ExitCode         int    `json:"exit_code"`
	Error            string `json:"error,omitempty"`
	StartTime        int64  `json:"start_time"`
	EndTime          int64  `json:"end_time,omitempty"`
	UpdatedAt        int64  `json:"updated_at"`
}
