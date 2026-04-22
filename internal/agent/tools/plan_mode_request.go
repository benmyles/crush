package tools

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/planning"
)

//go:embed request_enter_plan_mode.md
var requestEnterPlanModeDescription []byte

//go:embed request_exit_plan_mode.md
var requestExitPlanModeDescription []byte

const (
	RequestEnterPlanModeToolName = "request_enter_plan_mode"
	RequestExitPlanModeToolName  = "request_exit_plan_mode"
)

type PlanModeRequestParams struct {
	Prompt string `json:"prompt,omitempty" description:"Optional exact user task or continuation prompt to run again after switching plan mode"`
	Reason string `json:"reason,omitempty" description:"Brief explanation for why the mode switch is needed"`
}

type PlanModeRequestMetadata struct {
	ID     string        `json:"id"`
	Mode   planning.Mode `json:"mode"`
	Prompt string        `json:"prompt,omitempty"`
	Reason string        `json:"reason,omitempty"`
}

func NewRequestEnterPlanModeTool(service planning.Service) fantasy.AgentTool {
	return newPlanModeRequestTool(
		RequestEnterPlanModeToolName,
		requestEnterPlanModeDescription,
		service,
		planning.ModeEnter,
	)
}

func NewRequestExitPlanModeTool(service planning.Service) fantasy.AgentTool {
	return newPlanModeRequestTool(
		RequestExitPlanModeToolName,
		requestExitPlanModeDescription,
		service,
		planning.ModeExit,
	)
}

func newPlanModeRequestTool(name string, description []byte, service planning.Service, mode planning.Mode) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		name,
		FirstLineDescription(description),
		func(ctx context.Context, params PlanModeRequestParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if service == nil {
				return fantasy.NewTextErrorResponse(name + " service is unavailable"), nil
			}
			sessionID := GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, fmt.Errorf("session ID is required for requesting plan mode changes")
			}

			request, err := service.RequestModeChange(ctx, planning.ModeChangeRequest{
				SessionID:  sessionID,
				ToolCallID: call.ID,
				Mode:       mode,
				Prompt:     strings.TrimSpace(params.Prompt),
				Reason:     strings.TrimSpace(params.Reason),
			})
			if err != nil {
				return fantasy.ToolResponse{}, err
			}

			response := fantasy.NewTextResponse(planModeRequestResponseText(mode, request.Prompt))
			response.StopTurn = true
			return fantasy.WithResponseMetadata(response, PlanModeRequestMetadata{
				ID:     request.ID,
				Mode:   request.Mode,
				Prompt: request.Prompt,
				Reason: request.Reason,
			}), nil
		},
	)
}

func planModeRequestResponseText(mode planning.Mode, prompt string) string {
	switch mode {
	case planning.ModeEnter:
		if strings.TrimSpace(prompt) != "" {
			return "Plan mode requested. Stop this turn now; the UI will restart the task in plan mode."
		}
		return "Plan mode requested. Stop this turn now; the UI will enable plan mode for the next user message."
	case planning.ModeExit:
		if strings.TrimSpace(prompt) != "" {
			return "Plan mode exit requested. Stop this turn now; the UI will continue the task outside plan mode."
		}
		return "Plan mode exit requested. Stop this turn now; the UI will leave plan mode for the next user message."
	default:
		return "Plan mode change requested. Stop this turn now."
	}
}
