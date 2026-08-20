package tools

import (
	"context"
	_ "embed"
	"fmt"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/goal"
)

//go:embed goal_blocked.md
var blockGoalDescription string

const BlockGoalToolName = "goal_blocked"

type BlockGoalParams struct {
	// Reason is the concise, specific reason the goal cannot progress.
	Reason string `json:"reason" description:"Concise, specific reason the goal cannot progress right now"`
}

// NewBlockGoalTool creates the tool the agent uses to halt the goal
// supervision loop because meaningful progress is impossible. The user
// can reactivate with /goal:resume.
func NewBlockGoalTool(store *goal.Store, notify GoalNotifier) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		BlockGoalToolName,
		blockGoalDescription,
		func(ctx context.Context, params BlockGoalParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			sessionID := GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, fmt.Errorf("session ID is required for blocking the goal")
			}
			blocked, err := store.Block(ctx, sessionID, params.Reason)
			if err != nil {
				if err == goal.ErrNoGoal {
					return fantasy.NewTextResponse("No goal is active; nothing to block."), nil
				}
				return fantasy.ToolResponse{}, fmt.Errorf("failed to block goal: %w", err)
			}
			if notify != nil {
				notify(blocked)
			}
			return fantasy.NewTextResponse(fmt.Sprintf(
				"Goal marked blocked: %s\n\nReason: %s\nThe user can reactivate the goal with /goal:resume.",
				blocked.Text,
				blocked.BlockedReason,
			)), nil
		},
	)
}
