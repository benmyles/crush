// Package goal implements session goals: a user- or agent-set objective
// that the agent supervises. While a goal is active, the agent loop prods
// the model after every completed turn until the model marks the goal
// complete or blocked (see the goal tools in internal/agent/tools) or the
// user clears it.
package goal

import (
	"fmt"
	"strings"
)

// MaxConsecutiveProds caps how many consecutive goal checks may run
// without a fresh user prompt in between. Reaching the cap stalls the
// goal loop instead of letting a stuck or evasive model loop forever.
const MaxConsecutiveProds = 3

// Status describes the lifecycle of a goal.
type Status string

// Possible goal statuses. The zero value StatusNone describes a session
// without a goal.
const (
	StatusNone     Status = ""
	StatusActive   Status = "active"
	StatusComplete Status = "complete"
	StatusBlocked  Status = "blocked"
	StatusStalled  Status = "stalled"
)

// Goal holds the state of a single session goal.
type Goal struct {
	SessionID string `json:"session_id"`
	Text      string `json:"text"`
	Status    Status `json:"status"`
	// CreatedAt and UpdatedAt are unix seconds.
	CreatedAt int64 `json:"created_at"`
	UpdatedAt int64 `json:"updated_at"`
	// CompleteReason carries the summary the agent supplied when it
	// completed the goal.
	CompleteReason string `json:"complete_reason,omitempty"`
	// BlockedReason carries the reason the agent supplied when it
	// blocked the goal.
	BlockedReason string `json:"blocked_reason,omitempty"`
	// ConsecutiveProds counts goal checks issued without a fresh user
	// prompt in between. A fresh prompt or /goal:resume resets it.
	ConsecutiveProds int `json:"consecutive_prods"`
	// TotalProds counts every goal check issued for the goal.
	TotalProds int `json:"total_prods"`
}

// Active reports whether the goal exists and should keep supervising.
func (g Goal) Active() bool {
	return g.SessionID != "" && g.Status == StatusActive
}

// Exists reports whether a goal row is present, regardless of status.
func (g Goal) Exists() bool {
	return g.SessionID != ""
}

// IsTerminal reports whether the goal has reached a stopping status.
func (g Goal) IsTerminal() bool {
	return g.Status == StatusComplete || g.Status == StatusBlocked || g.Status == StatusStalled
}

// CheckPrompt returns the user message sent to the model when the agent
// loop prods it to verify progress against the goal. attempt is the
// 1-based number of this check.
func CheckPrompt(text string, attempt int) string {
	var b strings.Builder
	b.WriteString("[Goal check] The previous turn stopped without ")
	b.WriteString("marking the current goal complete.\n\n")
	fmt.Fprintf(&b, "Goal: %s\n\n", text)
	b.WriteString("Review the progress so far against the goal above. Then act:\n")
	b.WriteString("- If every part of the goal is truly finished, call goal_complete with a short summary of what was accomplished instead of continuing.\n")
	b.WriteString("- If you cannot make meaningful progress (missing access, need the user, repeated failures), call goal_blocked with the reason instead of spinning.\n")
	b.WriteString("- Otherwise continue working on the goal. Do not ask the user whether to continue.\n")
	if attempt > 1 {
		fmt.Fprintf(&b, "\n(Goal check #%d; consecutive checks without a fresh user prompt.)\n", attempt)
	} else {
		b.WriteString("\n(Goal check #1.)\n")
	}
	return b.String()
}
