package tools

import (
	"context"
	_ "embed"
	"errors"
	"fmt"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/terminal"
)

//go:embed terminal_kill.md
var terminalKillDescription string

type TerminalKillParams struct {
	TerminalID string `json:"terminal_id,omitempty" description:"The ID of the terminal to kill."`
	All        bool   `json:"all,omitempty" description:"If true, kill every active Crush terminal at once. Set when you know all sessions are done (e.g. at the end of a task)."`
}

func NewTerminalKillTool(permissions permission.Service, workingDir string, ctrl *terminal.Controller) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		TerminalKillToolName,
		terminalKillDescription,
		func(ctx context.Context, params TerminalKillParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.TerminalID != "" {
				if err := validateTerminalID(params.TerminalID); err != nil {
					return fantasy.NewTextErrorResponse(err.Error()), nil
				}
			} else if !params.All {
				return fantasy.NewTextErrorResponse("provide terminal_id or set all=true"), nil
			}

			sessionID := GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, errors.New("session ID is required for killing a terminal")
			}

			desc := fmt.Sprintf("Kill terminal %s", params.TerminalID)
			if params.All {
				desc = "Kill all interactive terminals"
			}

			p, err := permissions.Request(
				ctx,
				permission.CreatePermissionRequest{
					SessionID:   sessionID,
					Path:        workingDir,
					ToolCallID:  call.ID,
					ToolName:    TerminalKillToolName,
					Action:      "kill",
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

			if params.All {
				if err := ctrl.KillAll(ctx); err != nil {
					return fantasy.NewTextErrorResponse(fmt.Sprintf("failed to kill terminals: %v", err)), nil
				}
				return fantasy.NewTextResponse("All interactive terminals terminated."), nil
			}

			if err := ctrl.Kill(ctx, params.TerminalID); err != nil {
				if errors.Is(err, terminal.ErrNotFound) {
					return fantasy.NewTextErrorResponse(fmt.Sprintf("terminal %q is not running (already exited?)", params.TerminalID)), nil
				}
				return fantasy.NewTextErrorResponse(fmt.Sprintf("failed to kill terminal: %v", err)), nil
			}

			meta := TerminalResponseMetadata{TerminalID: params.TerminalID, Running: false}
			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(fmt.Sprintf("Terminal %s terminated.", params.TerminalID)),
				meta,
			), nil
		},
	)
}
