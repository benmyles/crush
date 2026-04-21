package tools

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/shell"
)

const (
	JobInputToolName = "job_input"
	inputPreviewMax  = 120
)

//go:embed job_input.md
var jobInputDescription []byte

type JobInputParams struct {
	ShellID    string `json:"shell_id" description:"The ID of the interactive background shell to send input to"`
	Input      string `json:"input" description:"The exact text to send to the running shell"`
	PressEnter bool   `json:"press_enter,omitempty" description:"If true, press Enter after sending input"`
}

type JobInputPermissionsParams struct {
	ShellID      string `json:"shell_id"`
	InputPreview string `json:"input_preview"`
	PressEnter   bool   `json:"press_enter"`
}

type JobInputResponseMetadata struct {
	ShellID          string `json:"shell_id"`
	Command          string `json:"command"`
	Description      string `json:"description"`
	Done             bool   `json:"done"`
	SupportsInput    bool   `json:"supports_input"`
	WorkingDirectory string `json:"working_directory"`
}

func NewJobInputTool(permissions permission.Service) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		JobInputToolName,
		FirstLineDescription(jobInputDescription),
		func(ctx context.Context, params JobInputParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.ShellID == "" {
				return fantasy.NewTextErrorResponse("missing shell_id"), nil
			}
			if params.Input == "" && !params.PressEnter {
				return fantasy.NewTextErrorResponse("missing input"), nil
			}

			bgManager := shell.GetBackgroundShellManager()
			bgShell, ok := bgManager.Get(params.ShellID)
			if !ok {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("background shell not found: %s", params.ShellID)), nil
			}

			sessionID := GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, fmt.Errorf("session ID is required for sending shell input")
			}
			granted, err := permissions.Request(ctx, permission.CreatePermissionRequest{
				SessionID:   sessionID,
				Path:        bgShell.WorkingDir,
				ToolCallID:  call.ID,
				ToolName:    JobInputToolName,
				Action:      "input",
				Description: fmt.Sprintf("Send input to job %s: %s", params.ShellID, previewJobInput(params.Input, params.PressEnter)),
				Params: JobInputPermissionsParams{
					ShellID:      params.ShellID,
					InputPreview: previewJobInput(params.Input, params.PressEnter),
					PressEnter:   params.PressEnter,
				},
			})
			if err != nil {
				return fantasy.ToolResponse{}, err
			}
			if !granted {
				return fantasy.ToolResponse{}, permission.ErrorPermissionDenied
			}

			input := params.Input
			if params.PressEnter {
				input += "\r"
			}
			if err := bgShell.WriteInput(input); err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}

			_, _, done, _ := bgShell.GetOutput()
			metadata := JobInputResponseMetadata{
				ShellID:          params.ShellID,
				Command:          bgShell.Command,
				Description:      bgShell.Description,
				Done:             done,
				SupportsInput:    bgShell.SupportsInput(),
				WorkingDirectory: bgShell.WorkingDir,
			}
			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(fmt.Sprintf("Input sent to background shell %s", params.ShellID)),
				metadata,
			), nil
		})
}

func previewJobInput(input string, pressEnter bool) string {
	preview := strings.ReplaceAll(input, "\n", "\\n")
	preview = strings.ReplaceAll(preview, "\r", "\\r")
	if len(preview) > inputPreviewMax {
		preview = preview[:inputPreviewMax] + "..."
	}
	if pressEnter {
		if preview == "" {
			return "<enter>"
		}
		return preview + "\\r"
	}
	return preview
}
