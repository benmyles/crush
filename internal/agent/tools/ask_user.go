package tools

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/userquestion"
)

//go:embed ask_user.md
var askUserDescription []byte

const AskUserToolName = "ask_user"

type AskUserChoice struct {
	ID          string `json:"id" description:"Stable identifier for this answer option"`
	Label       string `json:"label" description:"Short user-facing label for this option"`
	Description string `json:"description,omitempty" description:"One sentence explaining the impact of this option"`
}

type AskUserParams struct {
	Question    string          `json:"question" description:"The specific question the user should answer"`
	Description string          `json:"description,omitempty" description:"Optional concise context for why the answer matters"`
	Choices     []AskUserChoice `json:"choices" description:"Two or more mutually exclusive answer options"`
}

func NewAskUserTool(service userquestion.Service) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		AskUserToolName,
		FirstLineDescription(askUserDescription),
		func(ctx context.Context, params AskUserParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if service == nil {
				return fantasy.NewTextErrorResponse("ask_user service is unavailable"), nil
			}
			if strings.TrimSpace(params.Question) == "" {
				return fantasy.NewTextErrorResponse("question is required"), nil
			}
			sessionID := GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, fmt.Errorf("session ID is required for asking the user a question")
			}

			choices := make([]userquestion.Choice, len(params.Choices))
			for i, choice := range params.Choices {
				choices[i] = userquestion.Choice{
					ID:          strings.TrimSpace(choice.ID),
					Label:       strings.TrimSpace(choice.Label),
					Description: strings.TrimSpace(choice.Description),
				}
			}
			if err := userquestion.ValidateChoices(choices); err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}

			resp, err := service.Request(ctx, userquestion.CreateRequest{
				SessionID:   sessionID,
				ToolCallID:  call.ID,
				Question:    strings.TrimSpace(params.Question),
				Description: strings.TrimSpace(params.Description),
				Choices:     choices,
			})
			if err != nil {
				return fantasy.ToolResponse{}, err
			}
			if resp.Dismissed {
				return fantasy.NewTextErrorResponse("user dismissed the question"), nil
			}

			var parts []string
			if resp.ChoiceID != "" || resp.ChoiceLabel != "" {
				choice := resp.ChoiceLabel
				if resp.ChoiceID != "" {
					choice = fmt.Sprintf("%s (%s)", resp.ChoiceLabel, resp.ChoiceID)
				}
				parts = append(parts, "Selected answer: "+choice)
			}
			if strings.TrimSpace(resp.Comment) != "" {
				parts = append(parts, "User comment: "+strings.TrimSpace(resp.Comment))
			}
			if len(parts) == 0 {
				parts = append(parts, "The user provided no answer.")
			}
			return fantasy.NewTextResponse(strings.Join(parts, "\n")), nil
		},
	)
}
