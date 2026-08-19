package tools

import (
	"context"
	_ "embed"
	"fmt"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/terminal"
)

//go:embed terminal_resize.md
var terminalResizeDescription string

type TerminalResizeParams struct {
	TerminalID string `json:"terminal_id" description:"The ID of the terminal to resize."`
	Cols       int    `json:"cols" description:"New width in columns (1-500)."`
	Rows       int    `json:"rows" description:"New height in rows (1-500)."`
}

func NewTerminalResizeTool(ctrl *terminal.Controller) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		TerminalResizeToolName,
		terminalResizeDescription,
		func(ctx context.Context, params TerminalResizeParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if err := validateTerminalID(params.TerminalID); err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			if params.Cols <= 0 || params.Cols > 500 || params.Rows <= 0 || params.Rows > 500 {
				return fantasy.NewTextErrorResponse("cols and rows must be between 1 and 500"), nil
			}

			if err := ctrl.Resize(ctx, params.TerminalID, params.Cols, params.Rows); err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("failed to resize terminal: %v", err)), nil
			}

			meta := TerminalResponseMetadata{TerminalID: params.TerminalID, Cols: params.Cols, Rows: params.Rows, Running: true}
			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(fmt.Sprintf("Terminal %s resized to %dx%d", params.TerminalID, params.Cols, params.Rows)),
				meta,
			), nil
		},
	)
}
