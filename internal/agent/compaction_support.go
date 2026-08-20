package agent

import (
	"github.com/charmbracelet/crush/internal/compaction"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
)

// compactionLimits returns the effective keep-recent and reserve token
// budgets for a context window. The configured values are clamped so small
// windows (local models with 32k contexts) cannot end up with
// keep_recent + reserve >= window, which would make every compaction a no-op
// while the trigger keeps firing.
func compactionLimits(cfg config.CompactionConfig, window int64) (keepRecent, reserve int64) {
	keepRecent = cfg.KeepRecentTokens
	if keepRecent <= 0 {
		keepRecent = config.DefaultCompactionConfig().KeepRecentTokens
	}
	reserve = cfg.ReserveTokens
	if reserve <= 0 {
		reserve = config.DefaultCompactionConfig().ReserveTokens
	}
	if window > 0 {
		if maxKeep := window / 4; keepRecent > maxKeep {
			keepRecent = maxKeep
		}
		if maxReserve := window / 8; reserve > maxReserve {
			reserve = maxReserve
		}
	}
	return keepRecent, reserve
}

// hardCompactionThreshold returns the usage (in tokens) at or above which a
// blocking compaction must run for a context window of cw tokens. With the
// compaction engine enabled it is window - reserve (the LCM τ_hard); otherwise
// the legacy constants apply: a fixed 20k buffer for windows above 200k and a
// 20% buffer below.
func hardCompactionThreshold(cw int64, engineEnabled bool, cfg config.CompactionConfig) int64 {
	if engineEnabled {
		_, reserve := compactionLimits(cfg, cw)
		return cw - reserve
	}
	if cw > largeContextWindowThreshold {
		return cw - largeContextWindowBuffer
	}
	return cw - int64(float64(cw)*smallContextWindowRatio)
}

// effectiveHardCompactionThreshold returns the hard threshold with an
// optional model-level override applied. Models whose usable context is
// smaller than their declared window (e.g. codex models that auto-compact
// before their 500k API window) declare a compaction_trigger_tokens value
// that caps the threshold.
func effectiveHardCompactionThreshold(cw, override int64, engineEnabled bool, cfg config.CompactionConfig) int64 {
	threshold := hardCompactionThreshold(cw, engineEnabled, cfg)
	if override > 0 && override < threshold {
		return override
	}
	return threshold
}

// estimateStoredMessageTokens sums the approximate token cost of all parts of
// a message (text, reasoning, tool-call input, tool-result content, shell
// command + output), not just the first text part. Without this, tool-heavy
// turns cost 0 and the retained tail can be far larger than keepRecentTokens.
func estimateStoredMessageTokens(msg message.Message) int64 {
	var total int64
	for _, part := range msg.Parts {
		switch p := part.(type) {
		case message.TextContent:
			total += approxTokenCount(p.Text)
		case message.ReasoningContent:
			total += approxTokenCount(p.String())
		case message.ToolCall:
			total += approxTokenCount(p.Name + " " + p.Input)
		case message.ToolResult:
			total += approxTokenCount(p.Content + " " + p.Data)
		case message.ShellCommand:
			total += approxTokenCount(p.Command + "\n" + p.Output)
		case message.BinaryContent:
			total += approxTokenCount(p.Path + " " + p.MIMEType)
		}
	}
	return total
}

// splitTurnFactor is how much larger than keepRecent the last turn may be
// before it is split mid-turn instead of being retained whole.
const splitTurnFactor = 2

// compactionOverviewPart builds the structured CompactionContent part that
// rides on the engine's summary message. The TUI renders it as the
// "Compaction complete" tree; it is metadata only and never reaches the
// model-facing prompt (prompt assembly reads TextContent parts).
func compactionOverviewPart(result *compaction.CompactionResult, req compaction.CompactionRequest) message.CompactionContent {
	part := message.CompactionContent{
		SummaryID:         result.SummaryID,
		Level:             int(result.Level),
		TokenCount:        result.TokenCount,
		TokensBefore:      req.TokensBefore,
		ModelProvider:     req.ModelProvider,
		ModelID:           req.ModelID,
		CompactedMessages: len(result.CoveredMessageIDs),
		SeqStart:          result.Transcript.CompactedStartSeq,
		SeqEnd:            result.Transcript.CompactedEndSeq,
		FirstRetainedSeq:  req.FirstRetainedSeq,
	}
	part.Checkpoint.Goals = result.Overview.Goals
	part.Checkpoint.Constraints = result.Overview.Constraints
	part.Checkpoint.Decisions = result.Overview.Decisions
	part.Checkpoint.DeadEnds = result.Overview.DeadEnds
	part.Checkpoint.Questions = result.Overview.Questions
	part.Checkpoint.Done = result.Overview.Done
	part.Checkpoint.InProgress = result.Overview.InProgress
	part.Checkpoint.Blocked = result.Overview.Blocked
	part.Checkpoint.NextActions = result.Overview.NextActions
	part.Ledger.Instructions = len(result.Ledger.UserInstructions)
	part.Ledger.Errors = len(result.Ledger.Errors)
	part.Ledger.Files = len(result.Ledger.Files)
	part.Ledger.Commands = len(result.Ledger.Commands)
	part.ExtractsKeptBlocks = result.ExtractsKeptBlocks
	part.ExtractsTotalBlocks = result.ExtractsTotalBlocks
	part.OlderLaneCompressed = result.OlderLaneCompressed
	part.WorkingSetFiles = result.WorkingSetFiles
	return part
}

// splitForCompaction divides messages into the history to compact, an optional
// turn prefix (the beginning of an in-flight turn that is compacted while its
// suffix stays in context), and the index of the first retained message.
//
// The retained tail is the most recent messages whose estimated stored token
// count fits keepRecentTokens, aligned to a turn boundary (a user message) so
// the model resumes with a whole turn. When the last turn alone is much larger
// than the budget (a long tool loop), it is split: the older part of the turn
// becomes turnPrefix and the retained suffix starts at an assistant message so
// tool calls and their results stay paired.
//
// firstRetainedIdx is -1 when everything fits in the budget (nothing to
// compact).
func splitForCompaction(msgs []message.Message, keepRecentTokens int64) (history, turnPrefix []message.Message, firstRetainedIdx int) {
	if len(msgs) == 0 {
		return nil, nil, -1
	}
	// budgetIdx: first message of the tail that fits the budget (always keeps
	// at least the last message).
	var budget int64
	budgetIdx := len(msgs)
	for i := len(msgs) - 1; i >= 0; i-- {
		cost := estimateStoredMessageTokens(msgs[i])
		if budget+cost > keepRecentTokens && i < len(msgs)-1 {
			break
		}
		budget += cost
		budgetIdx = i
	}
	if budgetIdx <= 0 {
		return nil, nil, -1
	}

	// turnStart: the user message that opens the turn containing budgetIdx.
	turnStart := budgetIdx
	for turnStart > 0 && msgs[turnStart].Role != message.User {
		turnStart--
	}
	if msgs[turnStart].Role != message.User {
		// No user message at all before budgetIdx (continuation-only
		// history): fall back to a plain budget split.
		turnStart = budgetIdx
	}

	// Retain the whole turn when it is reasonably sized.
	var turnTokens int64
	for _, m := range msgs[turnStart:] {
		turnTokens += estimateStoredMessageTokens(m)
	}
	if turnStart > 0 && turnTokens <= keepRecentTokens*splitTurnFactor {
		return msgs[:turnStart], nil, turnStart
	}

	// Split the turn: keep the budgeted suffix, aligned back so it does not
	// begin with orphaned tool results.
	idx := budgetIdx
	for idx > turnStart && msgs[idx].Role == message.Tool {
		idx--
	}
	if idx <= 0 {
		return nil, nil, -1
	}
	if idx == turnStart {
		return msgs[:turnStart], nil, turnStart
	}
	return msgs[:turnStart], msgs[turnStart:idx], idx
}
