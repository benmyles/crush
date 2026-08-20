package compaction

import (
	"github.com/charmbracelet/crush/internal/message"
)

// TriggerReason is why a compaction was triggered.
type TriggerReason string

const (
	TriggerNone      TriggerReason = "none"
	TriggerSoft      TriggerReason = "soft"
	TriggerHard      TriggerReason = "hard"
	TriggerRubric    TriggerReason = "rubric"
	TriggerAgentInit TriggerReason = "agent-initiated"
)

// TriggerInput is the input to the trigger decision.
type TriggerInput struct {
	// UsageTokens is the current context usage.
	UsageTokens int64
	// ContextWindow is the active model's context window.
	ContextWindow int64
	// ReserveTokens is the hard headroom.
	ReserveTokens int64
	// SoftThresholdFraction is the fraction of the window at which async
	// compaction triggers (default 0.7).
	SoftThresholdFraction float64
	// HardThresholdTokens optionally overrides the hard compaction
	// threshold (normally ContextWindow - ReserveTokens). When set and
	// below the computed threshold, it becomes the model's blocking
	// auto-compaction point.
	HardThresholdTokens int64
	// Messages is the recent message list, for the structure-aware rubric.
	Messages []message.Message
}

// TriggerDecision is the outcome of the trigger check.
type TriggerDecision struct {
	Reason   TriggerReason
	Blocking bool
	// Score is the rubric's 0-1 confidence that now is a good time to compact.
	Score float64
}

// DecideTrigger applies the LCM soft/hard thresholds plus a deterministic,
// training-free structure-aware rubric (from SelfCompact). The rubric fires
// compaction at closed reasoning units (a tool batch that completed without
// errors, or an assistant turn that ended with a final answer) and suppresses
// it mid-derivation (a multi-step tool sequence mid-flight) or when stuck.
func DecideTrigger(in TriggerInput) TriggerDecision {
	if in.ContextWindow <= 0 {
		return TriggerDecision{Reason: TriggerNone}
	}
	hardThreshold := in.ContextWindow - in.ReserveTokens
	if in.HardThresholdTokens > 0 && in.HardThresholdTokens < hardThreshold {
		hardThreshold = in.HardThresholdTokens
	}
	softThreshold := int64(float64(in.ContextWindow) * in.SoftThresholdFraction)
	if in.SoftThresholdFraction <= 0 {
		softThreshold = int64(float64(in.ContextWindow) * 0.7)
	}

	// Hard threshold: always block to compact.
	if in.UsageTokens >= hardThreshold {
		return TriggerDecision{Reason: TriggerHard, Blocking: true, Score: 1.0}
	}

	// Below soft: no compaction (zero-cost continuity).
	if in.UsageTokens < softThreshold {
		return TriggerDecision{Reason: TriggerNone}
	}

	// Above soft but below hard: run the structure-aware rubric. The rubric
	// decides whether now is a good time (closed reasoning unit) or a bad
	// time (mid-derivation, stuck).
	score := rubricScore(in.Messages)
	if score >= 0.5 {
		return TriggerDecision{Reason: TriggerRubric, Blocking: false, Score: score}
	}
	// Even if the rubric says "not now", if we're close to the hard threshold
	// (within 10% of the window), compact anyway to avoid a blocking event.
	if in.UsageTokens >= hardThreshold-int64(float64(in.ContextWindow)*0.1) {
		return TriggerDecision{Reason: TriggerSoft, Blocking: false, Score: score}
	}
	return TriggerDecision{Reason: TriggerNone, Score: score}
}

// rubricScore returns a 0-1 confidence that now is a good time to compact,
// computed deterministically from the recent message structure (training-free,
// per SelfCompact). High score = closed reasoning unit; low score = mid-
// derivation or stuck.
func rubricScore(messages []message.Message) float64 {
	if len(messages) == 0 {
		return 0
	}
	// Look at the last few messages to detect structure.
	var lastAssistant *message.Message
	var lastTool *message.Message
	for i := len(messages) - 1; i >= 0 && i >= len(messages)-6; i-- {
		m := messages[i]
		switch m.Role {
		case message.Assistant:
			if lastAssistant == nil {
				mc := m
				lastAssistant = &mc
			}
		case message.Tool:
			if lastTool == nil {
				mc := m
				lastTool = &mc
			}
		}
	}

	var score float64

	// A finished assistant turn with a final answer (not mid-tool-use) is a
	// closed reasoning unit -> good time to compact.
	if lastAssistant != nil {
		finish := lastAssistant.FinishPart()
		if finish != nil && (finish.Reason == message.FinishReasonEndTurn || finish.Reason == message.FinishReasonMaxTokens) {
			// Ended with text (not just tool calls) = a natural boundary.
			if lastAssistant.Content().Text != "" && len(lastAssistant.ToolCalls()) == 0 {
				score += 0.6
			}
		}
	}

	// A tool batch that completed without errors is a closed unit.
	if lastTool != nil {
		hasError := false
		for _, tr := range lastTool.ToolResults() {
			if tr.IsError {
				hasError = true
				break
			}
		}
		if !hasError {
			score += 0.2
		} else {
			// Errors suggest we're mid-debugging; suppress.
			score -= 0.3
		}
	}

	// An in-flight assistant turn (no finish) means mid-derivation -> suppress.
	if lastAssistant != nil && !lastAssistant.IsFinished() {
		score -= 0.4
	}

	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return score
}
