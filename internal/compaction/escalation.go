package compaction

import (
	"context"
	"fmt"
	"strings"
)

// EscalationLevel enumerates the three-level summarization escalation protocol
// (from LCM). If a level fails to reduce token count, the engine escalates to a
// more aggressive strategy, culminating in a deterministic fallback that
// requires no LLM inference.
type EscalationLevel int

const (
	// LevelPreserveDetails is the first, most faithful summarization level.
	LevelPreserveDetails EscalationLevel = 0
	// LevelBulletPoints is the aggressive level, targeting half the budget.
	LevelBulletPoints EscalationLevel = 1
	// LevelDeterministic is the no-LLM fallback: deterministic truncation.
	LevelDeterministic EscalationLevel = 2
)

// EscalationInput is the input to the escalation guard.
type EscalationInput struct {
	// TargetTokens is the max tokens the output may occupy.
	TargetTokens int64
	// InputTokens is the token count of the material being summarized.
	InputTokens int64
	// MaxOutputTokens is the summarizer model's output cap.
	MaxOutputTokens int64
}

// EscalationResult is the outcome of one escalation attempt.
type EscalationResult struct {
	Level     EscalationLevel
	Text      string
	Tokens    int64
	Truncated bool
	// Converged is true when the output is strictly smaller than the input.
	Converged bool
}

// DeterministicFallback produces a no-LLM summary by truncating the ledger and
// recent turn text to a small budget. It guarantees the engine never gets stuck
// on a model that won't compress.
func DeterministicFallback(ledgerText, recentText string, targetTokens int64) EscalationResult {
	budget := int(targetTokens) * CharsPerToken
	if budget < 2048 {
		budget = 2048
	}
	// Reserve ~40% for recent text (the actionable tail), the rest for ledger.
	recentBudget := budget * 2 / 5
	ledgerBudget := budget - recentBudget
	recentCut, _, _ := TruncateHeadTail(recentText, recentBudget, 0.3, func(omitted int) string {
		return fmt.Sprintf("[… %s characters omitted (deterministic fallback)]", formatCount(omitted))
	})
	ledgerCut, _, _ := TruncateHeadTail(ledgerText, ledgerBudget, 1, func(omitted int) string {
		return fmt.Sprintf("[… %s characters of ledger omitted (deterministic fallback)]", formatCount(omitted))
	})
	var sb strings.Builder
	sb.WriteString("# Context Compaction (deterministic fallback)\n\n")
	sb.WriteString("The model-backed checkpoint did not converge; this is a deterministic extract of the ledger and recent turns. Recover full detail with recall_grep / recall_expand.\n\n")
	if ledgerCut != "" {
		sb.WriteString(ledgerCut)
		sb.WriteString("\n\n")
	}
	if recentCut != "" {
		sb.WriteString("## Recent turns (truncated)\n\n")
		sb.WriteString(recentCut)
	}
	text := strings.TrimSpace(sb.String())
	tokens := int64(EstimateTokens(len(text)))
	return EscalationResult{
		Level:     LevelDeterministic,
		Text:      text,
		Tokens:    tokens,
		Truncated: true,
		Converged: tokens < targetTokens,
	}
}

// EscalationCompleter is the model-backed completion function the guard wraps.
// It receives the level and input text and returns the summary text and stop
// reason. Implementations map the level onto the appropriate prompt mode.
type EscalationCompleter func(ctx context.Context, level EscalationLevel, input string, maxOutputTokens int64) (text string, stopReason string, err error)

// RunWithEscalation runs a model-backed lane through the three-level escalation
// protocol. It returns as soon as a level produces tokens(out) < tokens(in), or
// falls back to the deterministic level if both model levels fail to converge.
func RunWithEscalation(ctx context.Context, in EscalationInput, input string, complete EscalationCompleter, ledgerText, recentText string) (EscalationResult, error) {
	// Level 1: preserve_details.
	text, stop, err := complete(ctx, LevelPreserveDetails, input, in.MaxOutputTokens)
	if err != nil {
		// Fall through to level 2; a transient error at level 1 should not
		// abort the whole engine when a deterministic fallback exists.
		text = ""
		stop = "error"
	}
	tokens := int64(EstimateTokens(len(text)))
	if tokens < in.InputTokens && tokens < in.TargetTokens && stop != "error" {
		return EscalationResult{Level: LevelPreserveDetails, Text: text, Tokens: tokens, Truncated: stop == "length", Converged: true}, nil
	}

	// Level 2: bullet_points at half the target.
	text2, stop2, err := complete(ctx, LevelBulletPoints, input, in.MaxOutputTokens)
	if err != nil {
		text2 = ""
		stop2 = "error"
	}
	tokens2 := int64(EstimateTokens(len(text2)))
	halfTarget := in.TargetTokens / 2
	if halfTarget <= 0 {
		halfTarget = in.TargetTokens
	}
	if tokens2 < in.InputTokens && tokens2 < halfTarget && stop2 != "error" {
		return EscalationResult{Level: LevelBulletPoints, Text: text2, Tokens: tokens2, Truncated: stop2 == "length", Converged: true}, nil
	}

	// Level 3: deterministic, no LLM.
	fb := DeterministicFallback(ledgerText, recentText, in.TargetTokens)
	return fb, nil
}
