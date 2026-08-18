package compaction

import "github.com/charmbracelet/crush/internal/config"

// BudgetFeatures flags which composed parts are present, so the governor can
// allocate token budget across them.
type BudgetFeatures struct {
	Ledger        bool
	TranscriptMap bool
	Restore       bool
	Extracts      bool
	OlderLane     bool
}

// BudgetInput is the input to PlanBudget.
type BudgetInput struct {
	ConsumerContextWindow     int64
	KeepRecentTokens          int64
	ReserveTokens             int64
	SystemPromptTokens        int64
	BudgetFraction            float64
	MaxSummaryTokens          int64
	MinSummaryTokens          int64
	SummarizerContextWindow   int64
	SummarizerMaxOutputTokens int64
	Features                  BudgetFeatures
}

// BudgetPlan is the per-part token/char allocation derived from the consumer
// model's context window. No fixed ratios; no default output cap.
type BudgetPlan struct {
	ConsumerContextWindow int64
	AllowanceTokens       int64
	FixedTokens           struct {
		Preamble     int64
		RecoveryNote int64
	}
	Checkpoint struct {
		TargetTokens    int64
		MaxOutputTokens int64
		InputCharBudget int
	}
	Extracts struct {
		TargetTokens    int64
		OlderLaneTokens int64
	}
	Ledger struct {
		MaxChars int
	}
	Map struct {
		MaxChars int
	}
	Restore struct {
		MaxChars int
	}
}

const (
	preambleTokens            int64 = 220
	recoveryNoteTokens        int64 = 1200
	defaultSystemPromptTokens int64 = 8000
	defaultConsumerWindow     int64 = 200000
	reasoningHeadroomTokens   int64 = 16000
)

// PlanBudget sizes every part of the compaction entry from the consumer model's
// context window instead of fixed ratios. It mirrors the ShiftUp budget
// governor, adapted to Go and Crush's config types.
func PlanBudget(in BudgetInput) BudgetPlan {
	window := in.ConsumerContextWindow
	if window <= 0 {
		window = defaultConsumerWindow
	}
	systemPrompt := in.SystemPromptTokens
	if systemPrompt <= 0 {
		systemPrompt = defaultSystemPromptTokens
	}
	headroom := window - in.ReserveTokens - in.KeepRecentTokens - systemPrompt
	if headroom < 0 {
		headroom = 0
	}
	fractional := int64(float64(window) * in.BudgetFraction)
	halfHeadroom := headroom / 2
	// Allowance is the floor of the upper bounds (fractional, MaxSummaryTokens,
	// halfHeadroom), then raised to MinSummaryTokens so a tiny window still
	// gets a usable budget. The previous formula took min(MinSummaryTokens,
	// ...) which collapsed everything to the 6000-token floor.
	upper := min64(min64(fractional, in.MaxSummaryTokens), halfHeadroom)
	allowance := max64(in.MinSummaryTokens, upper)
	if allowance < 2000 {
		allowance = 2000
	}

	var plan BudgetPlan
	plan.ConsumerContextWindow = window
	plan.AllowanceTokens = allowance
	plan.FixedTokens.Preamble = preambleTokens
	plan.FixedTokens.RecoveryNote = recoveryNoteTokens

	remaining := allowance - plan.FixedTokens.Preamble - plan.FixedTokens.RecoveryNote
	if remaining < 2000 {
		remaining = 2000
	}

	ledgerShare := 0.0
	mapShare := 0.0
	restoreShare := 0.0
	extractsShare := 0.0
	if in.Features.Ledger {
		ledgerShare = 0.12
	}
	if in.Features.TranscriptMap {
		mapShare = 0.06
	}
	if in.Features.Restore {
		restoreShare = 0.12
	}
	if in.Features.Extracts {
		extractsShare = 0.4
	}
	checkpointShare := 1 - ledgerShare - mapShare - restoreShare - extractsShare
	if checkpointShare < 0.3 {
		checkpointShare = 0.3
	}

	plan.Checkpoint.TargetTokens = int64(float64(remaining) * checkpointShare)
	if plan.Checkpoint.TargetTokens < 3000 {
		plan.Checkpoint.TargetTokens = 3000
	}
	summarizerMax := in.SummarizerMaxOutputTokens
	if summarizerMax <= 0 {
		summarizerMax = 65536
	}
	// Clamp the checkpoint target to 60% of the summarizer's max output so a
	// low-DefaultMaxTokens model is never asked for an impossible length.
	if cap := summarizerMax * 3 / 5; cap > 0 && plan.Checkpoint.TargetTokens > cap {
		plan.Checkpoint.TargetTokens = cap
	}
	plan.Checkpoint.MaxOutputTokens = min64(summarizerMax, max64(plan.Checkpoint.TargetTokens*3, plan.Checkpoint.TargetTokens+reasoningHeadroomTokens))
	summarizerWindow := in.SummarizerContextWindow
	if summarizerWindow <= 0 {
		summarizerWindow = 128000
	}
	plan.Checkpoint.InputCharBudget = maxInt(40000, int(float64(summarizerWindow-plan.Checkpoint.MaxOutputTokens-6000)*CharsPerToken*0.7))

	plan.Extracts.TargetTokens = int64(float64(remaining) * extractsShare)
	if in.Features.OlderLane {
		plan.Extracts.OlderLaneTokens = plan.Extracts.TargetTokens / 4
	}

	plan.Ledger.MaxChars = int(float64(remaining)*ledgerShare) * CharsPerToken
	plan.Map.MaxChars = int(float64(remaining)*mapShare) * CharsPerToken
	plan.Restore.MaxChars = int(float64(remaining)*restoreShare) * CharsPerToken
	return plan
}

// ExtractsRatioFor derives the extractive lane ratio from the target and input
// token counts, capped at maxRatio.
func ExtractsRatioFor(targetTokens, inputTokens int64, maxRatio float64) float64 {
	if inputTokens <= 0 {
		if maxRatio > 1 {
			return 1
		}
		return maxRatio
	}
	wanted := float64(targetTokens) / float64(inputTokens)
	clamped := wanted
	if clamped < 0.05 {
		clamped = 0.05
	}
	if clamped > maxRatio {
		clamped = maxRatio
	}
	return clampRatio(clamped)
}

func clampRatio(r float64) float64 {
	// Round to 3 decimals like the ShiftUp governor.
	return float64(int(r*1000)) / 1000
}

// BudgetInputFromConfig builds a BudgetInput from a resolved CompactionConfig
// plus runtime values from the active model and host settings.
func BudgetInputFromConfig(cfg config.CompactionConfig, consumerContextWindow, systemPromptTokens, summarizerContextWindow, summarizerMaxOutput int64, keepRecent, reserve int64, features BudgetFeatures) BudgetInput {
	return BudgetInput{
		ConsumerContextWindow:     consumerContextWindow,
		KeepRecentTokens:          keepRecent,
		ReserveTokens:             reserve,
		SystemPromptTokens:        systemPromptTokens,
		BudgetFraction:            cfg.BudgetFraction,
		MaxSummaryTokens:          cfg.MaxSummaryTokens,
		MinSummaryTokens:          cfg.MinSummaryTokens,
		SummarizerContextWindow:   summarizerContextWindow,
		SummarizerMaxOutputTokens: summarizerMaxOutput,
		Features:                  features,
	}
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
