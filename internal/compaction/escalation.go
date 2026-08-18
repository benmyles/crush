package compaction

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
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

// TruncationRetryNote is appended to the level-1 input when the first attempt
// stopped at the output cap, so the model produces a complete (shorter)
// checkpoint on the retry.
const TruncationRetryNote = "\n\n[retry: the previous attempt stopped at the output token cap before finishing. Produce the complete checkpoint; if you must save space, shorten Critical Context and Environment & How-To rather than omitting Next Action.]"

// RunWithEscalation runs a model-backed lane through the three-level escalation
// protocol. It is fail-closed: a model error (transient or cancelled)
// propagates instead of silently falling back to the deterministic level, so
// a failing model never persists a low-quality summary. The deterministic
// fallback is only reached when both model levels produce output that fails
// to converge (output >= input or > 1.5x target), not on error.
//
// Level 1 acceptance: tokens < InputTokens && tokens <= Target*1.25. On a
// "length" stop (output truncated by max_tokens) it retries once at 1.6x the
// output budget with a retry note appended so the model knows to be more
// concise and to finish.
func RunWithEscalation(ctx context.Context, in EscalationInput, input string, complete EscalationCompleter, ledgerText, recentText string) (EscalationResult, error) {
	tryLevel := func(level EscalationLevel, levelInput string, maxOutput int64) (EscalationResult, error) {
		text, stop, err := completeWithRetry(ctx, complete, level, levelInput, maxOutput)
		if err != nil {
			return EscalationResult{}, err
		}
		tokens := int64(EstimateTokens(len(text)))
		return EscalationResult{Level: level, Text: text, Tokens: tokens, Truncated: stop == "length", Converged: tokens < in.InputTokens}, nil
	}
	accept := func(r EscalationResult, target int64) bool {
		return r.Converged && !r.Truncated && r.Tokens <= target
	}

	// Level 1: preserve_details.
	r1, err := tryLevel(LevelPreserveDetails, input, in.MaxOutputTokens)
	if err != nil {
		return EscalationResult{}, fmt.Errorf("compaction: checkpoint level 1 failed: %w", err)
	}
	if accept(r1, in.TargetTokens*5/4) {
		return r1, nil
	}
	// If level 1 hit the output cap, retry once at 1.6x output with a retry
	// note appended so the model can finish and be more concise.
	if r1.Truncated {
		r1b, err := tryLevel(LevelPreserveDetails, input+TruncationRetryNote, in.MaxOutputTokens*8/5)
		if err != nil {
			return EscalationResult{}, fmt.Errorf("compaction: checkpoint level 1 retry failed: %w", err)
		}
		if accept(r1b, in.TargetTokens*5/4) {
			return r1b, nil
		}
	}

	// Level 2: bullet_points at half the target.
	halfTarget := in.TargetTokens / 2
	if halfTarget <= 0 {
		halfTarget = in.TargetTokens
	}
	r2, err := tryLevel(LevelBulletPoints, input, in.MaxOutputTokens)
	if err != nil {
		return EscalationResult{}, fmt.Errorf("compaction: checkpoint level 2 failed: %w", err)
	}
	if accept(r2, halfTarget*3/2) {
		return r2, nil
	}

	// Level 3: deterministic, no LLM. Only reached when both model levels
	// produced output that failed to converge — never on error.
	fb := DeterministicFallback(ledgerText, recentText, in.TargetTokens)
	return fb, nil
}

// completeWithRetry wraps a single completion call with one transient retry.
// It retries once on a transient error (network/5xx/429) with a short backoff,
// but never retries on context cancellation, 4xx, or auth errors.
func completeWithRetry(ctx context.Context, complete EscalationCompleter, level EscalationLevel, input string, maxOutput int64) (string, string, error) {
	text, stop, err := complete(ctx, level, input, maxOutput)
	if err == nil {
		return text, stop, nil
	}
	// Fail closed on cancellation.
	if ctx.Err() != nil {
		return "", "", err
	}
	// Fail closed on non-transient errors (4xx/auth). Retry only on transient
	// (5xx/429/network) errors.
	if !isTransient(err) {
		return "", "", err
	}
	// One retry with a short backoff.
	select {
	case <-ctx.Done():
		return "", "", ctx.Err()
	case <-time.After(500 * time.Millisecond):
	}
	return complete(ctx, level, input, maxOutput)
}

// isTransient reports whether an error is a transient (retryable) failure.
// Non-transient (4xx, auth, cancelled) errors propagate immediately.
// statusCodeRe matches a bare HTTP status code token from provider errors,
// so incidental digit runs ("40000 tokens exceeded", port numbers) are not
// misclassified as HTTP responses.
var statusCodeRe = regexp.MustCompile(`\b(400|401|403|422|429|500|502|503|504)\b`)

// isTransient reports whether an error is a transient (retryable) failure.
// Non-transient (4xx, auth, cancelled) errors propagate immediately.
func isTransient(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "context canceled") || strings.Contains(msg, "context deadline exceeded") {
		return false
	}
	// 4xx and auth are not transient. Match status codes as standalone
	// tokens so a stray "40000" (token counts, ports) does not read as 400.
	if strings.Contains(msg, "unauthorized") || strings.Contains(msg, "forbidden") {
		return false
	}
	for _, code := range statusCodeRe.FindAllString(msg, -1) {
		switch code {
		case "400", "401", "403", "422":
			return false
		case "429", "500", "502", "503", "504":
			return true
		}
	}
	// Text heuristics for network/proxy failures. msg is lowercased above.
	for _, needle := range []string{"timeout", "connection reset", "eof", "temporary", "overloaded", "rate limit"} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}
