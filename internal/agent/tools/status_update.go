package tools

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/status"
)

//go:embed status_update.md
var statusUpdateDescription string

// StatusUpdateToolName is the tool name the agent calls to record a
// structured status update.
const StatusUpdateToolName = "status_update"

// StatusUpdateParams is the structured standup payload the agent fills
// in.
type StatusUpdateParams struct {
	// Done summarizes what the agent recently finished.
	Done string `json:"done" description:"What you recently finished (past tense, concise)"`
	// Doing summarizes what the agent is working on right now.
	Doing string `json:"doing" description:"What you are working on right now (present tense, concise)"`
	// Next summarizes what the agent will work on after the current task.
	Next string `json:"next" description:"What you will do next (short intent)"`
	// Blockers carries anything blocking progress. Optional: omit this
	// field entirely when nothing blocks you.
	Blockers string `json:"blockers,omitempty" description:"Anything blocking progress. Omit this field entirely when nothing blocks you"`
}

// StatusNotifier is invoked after a status update is recorded so the
// host (coordinator) can publish the update to the UI. It may be nil.
type StatusNotifier func(status.Update)

// NewStatusUpdateTool creates the tool the agent uses to report its
// progress. The update persists under the session and is surfaced in the
// UI sidebar.
func NewStatusUpdateTool(store *status.Store, notify StatusNotifier) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		StatusUpdateToolName,
		statusUpdateDescription,
		func(ctx context.Context, params StatusUpdateParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			sessionID := GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, fmt.Errorf("session ID is required for recording a status update")
			}
			// Trim stray whitespace so fields like blockers render cleanly
			// or drop out of the UI entirely when effectively empty.
			blockers := strings.TrimSpace(params.Blockers)
			update, err := store.Upsert(ctx, sessionID, strings.TrimSpace(params.Done), strings.TrimSpace(params.Doing), strings.TrimSpace(params.Next), blockers)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("failed to record status update: %w", err)
			}
			if notify != nil {
				notify(update)
			}
			return fantasy.NewTextResponse("Status update recorded. Continue working."), nil
		},
	)
}
