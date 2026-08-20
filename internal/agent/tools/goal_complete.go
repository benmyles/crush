package tools

import (
	"context"
	_ "embed"
	"fmt"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/goal"
)

//go:embed goal_complete.md
var completeGoalDescription string

const CompleteGoalToolName = "goal_complete"

type CompleteGoalParams struct {
	// Summary is a short description of what was accomplished.
	Summary string `json:"summary" description:"Short summary of what was accomplished, shown to the user"`
}

// NewCompleteGoalTool creates the tool the agent uses to mark the active
// goal complete. Completion is the only way a goal exits the supervision
// loop without user intervention.
func NewCompleteGoalTool(store *goal.Store, notify GoalNotifier) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		CompleteGoalToolName,
		completeGoalDescription,
		func(ctx context.Context, params CompleteGoalParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			sessionID := GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, fmt.Errorf("session ID is required for completing the goal")
			}
			completed, err := store.Complete(ctx, sessionID, params.Summary)
			if err != nil {
				if err == goal.ErrNoGoal {
					return fantasy.NewTextResponse("No goal is active; nothing to complete."), nil
				}
				return fantasy.ToolResponse{}, fmt.Errorf("failed to complete goal: %w", err)
			}
			if notify != nil {
				notify(completed)
			}
			return fantasy.NewTextResponse(fmt.Sprintf(
				"Goal marked complete: %s\n\nSummary: %s\nThe goal supervision loop is now stopped.",
				completed.Text,
				completed.CompleteReason,
			)), nil
		},
	)
}
