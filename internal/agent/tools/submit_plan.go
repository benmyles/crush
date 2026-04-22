package tools

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/planning"
	"github.com/charmbracelet/crush/internal/session"
)

//go:embed submit_plan.md
var submitPlanDescription []byte

const SubmitPlanToolName = "submit_plan"

type SubmitPlanParams struct {
	Markdown string     `json:"markdown" description:"Markdown plan to show the user for approval"`
	Todos    []TodoItem `json:"todos" description:"Structured task list to activate after approval"`
}

type SubmitPlanResponseMetadata struct {
	ID       string         `json:"id"`
	Todos    []session.Todo `json:"todos"`
	Approved bool           `json:"approved"`
	Comment  string         `json:"comment,omitempty"`
}

func NewSubmitPlanTool(service planning.Service) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		SubmitPlanToolName,
		FirstLineDescription(submitPlanDescription),
		func(ctx context.Context, params SubmitPlanParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if service == nil {
				return fantasy.NewTextErrorResponse("submit_plan service is unavailable"), nil
			}
			if !GetPlanModeFromContext(ctx) {
				return fantasy.NewTextErrorResponse("submit_plan can only be used in plan mode"), nil
			}
			sessionID := GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, fmt.Errorf("session ID is required for submitting a plan")
			}
			if strings.TrimSpace(params.Markdown) == "" {
				return fantasy.NewTextErrorResponse("markdown is required"), nil
			}
			todos, err := todoItemsToSessionTodos(params.Todos)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}

			submission, response, err := service.Submit(ctx, planning.Submission{
				SessionID:  sessionID,
				ToolCallID: call.ID,
				Markdown:   strings.TrimSpace(params.Markdown),
				Todos:      todos,
			})
			if err != nil {
				return fantasy.ToolResponse{}, err
			}

			metadata := SubmitPlanResponseMetadata{
				ID:       submission.ID,
				Todos:    submission.Todos,
				Approved: response.Approved,
				Comment:  strings.TrimSpace(response.Comment),
			}
			content := planSubmissionResponseText(submission, response)
			toolResponse := fantasy.NewTextResponse(content)
			if response.Approved {
				toolResponse.StopTurn = true
			}
			return fantasy.WithResponseMetadata(
				toolResponse,
				metadata,
			), nil
		},
	)
}

func planSubmissionResponseText(submission planning.Submission, response planning.Response) string {
	comment := strings.TrimSpace(response.Comment)
	if response.Approved {
		var parts []string
		parts = append(parts, "Plan approved by the user.")
		if comment != "" {
			parts = append(parts, "User comment: "+comment)
		}
		parts = append(parts, "Stop planning now. Implementation will begin after this plan-mode turn completes.")
		return strings.Join(parts, "\n")
	}

	var parts []string
	parts = append(parts, "Plan was not approved. Revise the plan based on the user's feedback before doing any implementation.")
	parts = append(parts, "You must ask follow-up questions with ask_user if the feedback is unclear or if any remaining choice requires user judgement.")
	parts = append(parts, "After resolving any ambiguity, call submit_plan again with a complete updated plan and structured todos so the user can re-review the revised plan.")
	if comment != "" {
		parts = append(parts, "User feedback: "+comment)
	}
	if markdown := strings.TrimSpace(submission.Markdown); markdown != "" {
		parts = append(parts, "Previous plan:\n"+markdown)
	}
	return strings.Join(parts, "\n\n")
}

func todoItemsToSessionTodos(items []TodoItem) ([]session.Todo, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("at least one structured todo is required")
	}

	todos := make([]session.Todo, len(items))
	inProgress := 0
	for i, item := range items {
		content := strings.TrimSpace(item.Content)
		activeForm := strings.TrimSpace(item.ActiveForm)
		if content == "" {
			return nil, fmt.Errorf("todo %d content is required", i+1)
		}
		if activeForm == "" {
			return nil, fmt.Errorf("todo %q active_form is required", content)
		}

		status := session.TodoStatus(item.Status)
		switch status {
		case session.TodoStatusPending, session.TodoStatusInProgress, session.TodoStatusCompleted:
		default:
			return nil, fmt.Errorf("invalid status %q for todo %q", item.Status, content)
		}
		if status == session.TodoStatusInProgress {
			inProgress++
		}

		todos[i] = session.Todo{
			Content:    content,
			Status:     status,
			ActiveForm: activeForm,
		}
	}
	if inProgress > 1 {
		return nil, fmt.Errorf("only one todo can be in_progress")
	}
	return todos, nil
}
