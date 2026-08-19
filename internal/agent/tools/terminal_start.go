package tools

import (
	"cmp"
	"context"
	_ "embed"
	"errors"
	"fmt"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/terminal"
)

//go:embed terminal_start.md
var terminalStartDescription string

type TerminalStartParams struct {
	Command     string `json:"command" description:"The command to run inside the terminal, e.g. 'ssh user@host' or 'vim main.go'. Runs through the default shell, so pipes and environment assignments work. The program gets a real TTY, so interactive programs work."`
	Description string `json:"description" description:"A brief description of what this terminal is for, e.g. 'ssh to staging'. Shown in permission prompts and terminal listings. Keep it under 60 characters."`
	WorkingDir  string `json:"working_dir,omitempty" description:"Starting directory for the terminal session (defaults to the project directory)."`
	Name        string `json:"name,omitempty" description:"Optional stable name (letters, digits, dots, dashes, underscores). Reusing a name reconnects to the existing terminal rather than starting a new one, so you can resume terminals from earlier turns or after a restart."`
	Cols        int    `json:"cols,omitempty" description:"Terminal width in columns (default 120)."`
	Rows        int    `json:"rows,omitempty" description:"Terminal height in rows (default 40)."`
}

func NewTerminalStartTool(permissions permission.Service, workingDir string, ctrl *terminal.Controller) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		TerminalStartToolName,
		terminalStartDescription,
		func(ctx context.Context, params TerminalStartParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.Command == "" {
				return fantasy.NewTextErrorResponse("command is required"), nil
			}

			sessionID := GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, errors.New("session ID is required for starting a terminal")
			}

			execWorkingDir := cmp.Or(params.WorkingDir, workingDir)

			p, err := permissions.Request(
				ctx,
				permission.CreatePermissionRequest{
					SessionID:   sessionID,
					Path:        execWorkingDir,
					ToolCallID:  call.ID,
					ToolName:    TerminalStartToolName,
					Action:      "start",
					Description: fmt.Sprintf("Start interactive terminal: %s", params.Command),
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

			sess, err := ctrl.Start(ctx, params.Name, execWorkingDir, params.Command, params.Cols, params.Rows)
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("failed to start terminal: %v", err)), nil
			}

			status := "started"
			if sess.Existing {
				status = "reconnected to existing terminal"
			}

			meta := TerminalResponseMetadata{
				TerminalID: sess.ID,
				Command:    params.Command,
				Cols:       sess.Cols,
				Rows:       sess.Rows,
				Running:    true,
				Existing:   sess.Existing,
			}

			content := fmt.Sprintf("Terminal %s (%dx%d): %s\n\n%s", sess.ID, sess.Cols, sess.Rows, status, params.Command)
			if screen, err := ctrl.Capture(ctx, sess.ID, false); err == nil && screen != "" {
				content += "\n\n" + TruncateOutput(screen)
			}

			return fantasy.WithResponseMetadata(fantasy.NewTextResponse(content), meta), nil
		},
	)
}
