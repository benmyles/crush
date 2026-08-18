package tools

import (
	"context"
	"strings"

	"charm.land/fantasy"
)

// CompactContextToolName is the tool name for compact_context.
const CompactContextToolName = "compact_context"

// CompactContextAgent is the minimal agent surface the compact_context tool
// needs, defined here to avoid an import cycle with the agent package.
type CompactContextAgent interface {
	CurrentSessionID() string
	// RequestCompaction sets a per-session flag; compaction runs at the next
	// step boundary instead of synchronously (which would return ErrSessionBusy
	// from inside a running tool call).
	RequestCompaction(sessionID, instructions string)
}

const compactContextDescription = `Compact the conversation context now, at a natural milestone.

Use this when you have finished a sub-task and want to free context for the next phase of work, rather than waiting for the engine to compact at the token threshold. Compaction is lossless: every message is preserved in the session store and recoverable with recall_grep / recall_expand; only the active window is compressed.

When to use it:
- After a sub-task has resolved and you are about to start a distinct new phase.
- When you notice the context is getting long and you just finished a coherent unit of work.
- Do NOT use it mid-derivation or while stuck on an error — compaction works best at closed reasoning boundaries.

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
			sessionID := a.CurrentSessionID()
			if strings.TrimSpace(sessionID) == "" {
				return fantasy.NewTextErrorResponse("no active session to compact"), nil
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
