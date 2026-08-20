package tools

import (
	"context"
	_ "embed"
	"fmt"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/goal"
)

//go:embed update_goal.md
var updateGoalDescription string

const UpdateGoalToolName = "update_goal"

type UpdateGoalParams struct {
	// Text is the new or replacement goal text.
	Text string `json:"text" description:"The goal text: an outcome-focused objective describing what done means"`
}

// GoalNotifier is invoked after a goal tool changes goal state so the
// host (coordinator) can publish a state-change notification. It may be
// nil.
type GoalNotifier func(goal.Goal)

// NewUpdateGoalTool creates the agent-initiated goal tool. The agent
// uses it to set or replace the active goal when the user asks it to.
func NewUpdateGoalTool(store *goal.Store, notify GoalNotifier) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		UpdateGoalToolName,
		updateGoalDescription,
		func(ctx context.Context, params UpdateGoalParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			sessionID := GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, fmt.Errorf("session ID is required for updating the goal")
			}
			current, err := store.Get(ctx, sessionID)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("failed to read goal: %w", err)
			}
			var updated goal.Goal
			if current.Exists() {
				updated, err = store.Update(ctx, sessionID, params.Text)
			} else {
				updated, err = store.Set(ctx, sessionID, params.Text)
			}
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("failed to set goal: %w", err)
			}
			if notify != nil {
				notify(updated)
			}
			return fantasy.NewTextResponse(fmt.Sprintf(
				"Goal updated. The supervision loop now expects: %s\n\nKeep working until the goal is met, then call goal_complete. If you become blocked, call goal_blocked with the reason.",
				updated.Text,
			)), nil
		},
	)
}
