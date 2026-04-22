package agent

import (
	"context"
	"fmt"
	"log/slog"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/config"
)

func (a *sessionAgent) shouldAutoCompact(steps []fantasy.StepResult, model Model, opts AutoCompactOptions) bool {
	if opts.strategy() == config.PlanCompactStrategyDisabled {
		return false
	}
	threshold, ok := autoCompactTokenThreshold(int64(model.CatwalkCfg.ContextWindow), opts)
	if !ok {
		return false
	}
	return latestStepContextTokens(steps) >= threshold
}

func autoCompactTokenThreshold(contextWindow int64, opts AutoCompactOptions) (int64, bool) {
	if opts.TokenThreshold != nil {
		if *opts.TokenThreshold <= 0 {
			return 0, false
		}
		return *opts.TokenThreshold, true
	}
	if contextWindow <= 0 {
		return 0, false
	}
	if contextWindow > largeContextWindowThreshold {
		return contextWindow - largeContextWindowBuffer, true
	}
	return int64(float64(contextWindow) * (1 - smallContextWindowRatio)), true
}

func latestStepContextTokens(steps []fantasy.StepResult) int64 {
	if len(steps) == 0 {
		return 0
	}
	step := steps[len(steps)-1]
	tokens := usageContextTokens(step.Usage)
	tokens += estimateToolResultTokens(step.Content.ToolResults())
	return tokens
}

func usageContextTokens(usage fantasy.Usage) int64 {
	tokens := usage.InputTokens + usage.OutputTokens + usage.CacheReadTokens
	if tokens == 0 && usage.TotalTokens > 0 {
		return usage.TotalTokens
	}
	return tokens
}

func estimateToolResultTokens(results []fantasy.ToolResultContent) int64 {
	var tokens int64
	for _, result := range results {
		switch output := result.Result.(type) {
		case fantasy.ToolResultOutputContentText:
			tokens += approximateTokenCount(output.Text)
		case fantasy.ToolResultOutputContentError:
			if output.Error != nil {
				tokens += approximateTokenCount(output.Error.Error())
			}
		case fantasy.ToolResultOutputContentMedia:
			tokens += approximateTokenCount(output.Text)
		default:
			tokens += approximateTokenCount(fmt.Sprint(output))
		}
	}
	return tokens
}

func approximateTokenCount(s string) int64 {
	if s == "" {
		return 0
	}
	return int64((len(s) + 3) / 4)
}

func (a *sessionAgent) autoCompact(ctx context.Context, sessionID string, opts fantasy.ProviderOptions, compactOpts AutoCompactOptions) error {
	switch compactOpts.strategy() {
	case config.PlanCompactStrategyDisabled:
		return nil
	case config.PlanCompactStrategyMorph:
		if compactOpts.MorphCompact == nil {
			slog.Warn("Morph auto-compaction is not configured, falling back to summarization")
			return a.Summarize(ctx, sessionID, opts)
		}
		if err := a.Compact(ctx, sessionID, *compactOpts.MorphCompact, opts); err != nil {
			slog.Warn("Morph auto-compaction failed, falling back to summarization", "error", err)
			return a.Summarize(ctx, sessionID, opts)
		}
		return nil
	case config.PlanCompactStrategySummarize, "":
		return a.Summarize(ctx, sessionID, opts)
	default:
		slog.Warn("Unknown auto-compaction strategy, falling back to summarization", "strategy", compactOpts.Strategy)
		return a.Summarize(ctx, sessionID, opts)
	}
}

func (opts AutoCompactOptions) strategy() string {
	if opts.Strategy == "" {
		return config.PlanCompactStrategySummarize
	}
	return opts.Strategy
}
