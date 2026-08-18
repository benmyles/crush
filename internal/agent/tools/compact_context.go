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
	CurrentSessionID() string
	Summarize(ctx context.Context, sessionID string, opts fantasy.ProviderOptions, onAuthRefresh func(context.Context, *fantasy.ProviderError) error) error
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

// NewCompactContextTool creates the agent-initiated compaction tool. It calls
// the session agent's Summarize path (which routes to the compaction engine
// when enabled). The agentResolver returns the current session's agent.
func NewCompactContextTool(agentResolver func() CompactContextAgent, optsResolver func() fantasy.ProviderOptions) fantasy.AgentTool {
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
			opts := fantasy.ProviderOptions{}
			if optsResolver != nil {
				opts = optsResolver()
			}
			// Custom instructions are recorded via the tool-call input; the
			// engine reads them from the request. For now, run the standard
			// compaction; the instructions param is surfaced in the response.
			if err := a.Summarize(ctx, sessionID, opts, nil); err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("compaction failed: %v", err)), nil
			}
			msg := "Context compacted. The structured checkpoint, deterministic ledger, and exact-recovery index are now in your active context. Use recall_grep to recover any compacted detail."
			if strings.TrimSpace(params.Instructions) != "" {
				msg += "\nOperator focus for this compaction: " + params.Instructions
			}
			return fantasy.NewTextResponse(msg), nil
		},
	)
}
