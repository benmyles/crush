// Package status implements agent status updates: mini standup reports
// (recently done, now doing, next, blockers) that the agent emits through
// the status_update tool. The reminder loop prods the model when no update
// arrived within ReminderInterval while the feature is enabled.
package status

import (
	"strings"
	"time"
)

// ReminderInterval is how long Crush waits after the latest status update
// before reminding a working agent to emit a new one.
const ReminderInterval = 120 * time.Second

// Update is a single structured agent status update.
type Update struct {
	SessionID string `json:"session_id"`
	// Done summarizes what the agent recently finished.
	Done string `json:"done"`
	// Doing summarizes what the agent is working on right now.
	Doing string `json:"doing"`
	// Next summarizes what the agent will work on after the current task.
	Next string `json:"next"`
	// Blockers carries anything blocking progress, empty when none.
	Blockers string `json:"blockers,omitempty"`
	// UpdatedAt is the unix timestamp of the latest update.
	UpdatedAt int64 `json:"updated_at"`
}

// Exists reports whether an update row is present for the session.
func (u Update) Exists() bool {
	return u.SessionID != ""
}

// ReminderPrompt returns the user message the agent loop injects when the
// reminder interval elapsed without a status update. The model is expected
// to report via the status_update tool, reconcile its todo list, and then
// continue its work.
func ReminderPrompt() string {
	var b strings.Builder
	b.WriteString("[Status update] More than two minutes passed since your last ")
	b.WriteString("status update. Call the status_update tool now with what you ")
	b.WriteString("recently did, what you are doing now, what you will do next, ")
	b.WriteString("and any blockers (omit the blockers field when nothing is ")
	b.WriteString("blocking you). Then review your todo list and make sure every ")
	b.WriteString("todo is up-to-date and accurate: mark finished work completed, ")
	b.WriteString("split or update stale items, and keep exactly the work that ")
	b.WriteString("remains. Then continue working; do not ask the user ")
	b.WriteString("whether to continue.")
	return b.String()
}
