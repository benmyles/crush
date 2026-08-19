package tools

import (
	"context"
	"fmt"
	"strings"

	"charm.land/fantasy"
)

// CompactContextToolName is the tool name for compact_context.
const CompactContextToolName = "compact_context"

// CompactContextAgent is the minimal agent surface the compact_context tool
// needs, defined here to avoid an import cycle with the agent package.
type CompactContextAgent interface {
	// RequestCompaction sets a per-session flag; compaction runs at the next
	// step boundary instead of synchronously (which would return
	// ErrSessionBusy from inside a running tool call).
	RequestCompaction(sessionID, instructions string)
	// CompactionStatus reports current context usage and the soft compaction
	// threshold used to gate explicit requests. Honors requests
	// unconditionally when CompactContextStatus.ContextWindow is zero
	// (unknown window).
	CompactionStatus(ctx context.Context, sessionID string) (CompactContextStatus, error)
}

// CompactContextStatus is the compaction-relevant session state the
// compact_context tool reads before scheduling a compaction.
type CompactContextStatus struct {
	// UsageTokens is the current context usage in tokens.
	UsageTokens int64
	// ContextWindow is the active model's context window in tokens. Zero
	// means the window is unknown, in which case SoftThresholdTokens is
	// also zero and the caller should honor the request unconditionally.
	ContextWindow int64
	// SoftThresholdTokens is the soft compaction threshold in tokens. A
	// request with UsageTokens below it is declined with a status message.
	SoftThresholdTokens int64
}

const compactContextDescription = `Compact the conversation context now, at a natural milestone.

Use this when you have finished a sub-task and want to free context for the next phase of work, rather than waiting for the engine to compact at the token threshold. Compaction is lossless: every message is preserved in the session store and recoverable with recall_grep / recall_expand; only the active window is compressed.

When to use it:
- After a sub-task has resolved and you are about to start a distinct new phase.
- When you notice the context is getting long and you just finished a coherent unit of work.
- Do NOT use it mid-derivation or while stuck on an error — compaction works best at closed reasoning boundaries.

Requests made while context usage is below the engine's soft threshold are declined with a status message instead of scheduling a compaction; the engine still compacts automatically once usage crosses the threshold.

Optional instructions let you steer what the checkpoint should emphasize.`

// CompactContextParams are the params for compact_context.
type CompactContextParams struct {
	Instructions string `json:"instructions,omitempty" description:"Optional focus for what the checkpoint should emphasize"`
}

// NewCompactContextTool creates the agent-initiated compaction tool. It sets
// a per-session request flag (with optional instructions) instead of calling
// Summarize directly; the engine runs the compaction at the next step boundary
// so the tool call does not collide with the in-flight run (ErrSessionBusy).
func NewCompactContextTool(agentResolver func() CompactContextAgent) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		CompactContextToolName,
		compactContextDescription,
		func(ctx context.Context, params CompactContextParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			a := agentResolver()
			if a == nil {
				return fantasy.NewTextErrorResponse("no active agent available to compact"), nil
			}
			sessionID := GetSessionFromContext(ctx)
			if strings.TrimSpace(sessionID) == "" {
				return fantasy.NewTextErrorResponse("no active session to compact"), nil
			}
			// Decline explicit requests that have nothing useful to compress yet:
			// mirror the automatic trigger's soft-threshold gate. Fail open when
			// the status is unavailable so an explicit request is never silently
			// dropped.
			status, err := a.CompactionStatus(ctx, sessionID)
			if err == nil && status.ContextWindow > 0 && status.UsageTokens < status.SoftThresholdTokens {
				return fantasy.NewTextResponse(fmt.Sprintf(
					"Compaction not needed yet: current context usage is %d tokens, which is %.0f%% of the %d-token context window and below the soft threshold of %d tokens (%.0f%%). Nothing is scheduled. Compaction will run automatically at the next step boundary once usage crosses the soft threshold, or you can call this tool again later if needed.",
					status.UsageTokens,
					float64(status.UsageTokens)/float64(status.ContextWindow)*100,
					status.ContextWindow,
					status.SoftThresholdTokens,
					float64(status.SoftThresholdTokens)/float64(status.ContextWindow)*100,
				)), nil
			}
			a.RequestCompaction(sessionID, params.Instructions)
			msg := "Compaction scheduled. The compaction engine will run at the next step boundary, compressing the older context into a structured checkpoint + deterministic ledger + exact-recovery index. Use recall_grep / recall_expand to recover any compacted detail."
			if strings.TrimSpace(params.Instructions) != "" {
				msg += "\nOperator focus for this compaction: " + params.Instructions
			}
			return fantasy.NewTextResponse(msg), nil
		},
	)
}
