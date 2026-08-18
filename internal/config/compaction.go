package config

import (
	"strings"
)

// VerificationMode controls the checkpoint coverage audit.
type VerificationMode string

const (
	// VerificationJudge runs a model audit and appends verbatim ledger facts
	// the judge found missing from the checkpoint.
	VerificationJudge VerificationMode = "judge"
	// VerificationChecks runs structural validation only (no model call).
	VerificationChecks VerificationMode = "checks"
	// VerificationOff disables the coverage audit.
	VerificationOff VerificationMode = "off"
)

// CompactionConfig configures Crush's context compaction engine. The engine
// combines a deterministic session ledger, a structured self-addressed
// checkpoint, exact transcript recovery, and (optionally) labeled extracts and
// dense retrieval, all backed by the append-only message store. Raw messages
// are never mutated; compaction produces derived summary views over them.
//
// When Compaction is nil or Enabled is false, the engine falls back to the
// legacy single-shot summarization path (gated by DisableAutoSummarize for
// back-compat).
type CompactionConfig struct {
	// Enabled controls whether the compaction engine runs. When false,
	// Crush uses the legacy Summarize path. Defaults to true.
	Enabled bool `json:"enabled,omitempty" jsonschema:"description=Enable the context compaction engine,default=true"`

	// ReserveTokens is the hard-context headroom kept free before
	// triggering a blocking compaction. Defaults to 16384.
	ReserveTokens int64 `json:"reserve_tokens,omitempty" jsonschema:"description=Hard-context headroom kept free before a blocking compaction,default=16384"`

	// KeepRecentTokens is the number of recent tokens retained verbatim
	// outside the compacted span. Defaults to 20000.
	KeepRecentTokens int64 `json:"keep_recent_tokens,omitempty" jsonschema:"description=Recent tokens retained verbatim outside the compacted span,default=20000"`

	// SoftThresholdFraction is the fraction of the context window at which
	// an asynchronous compaction is triggered between turns. Defaults to 0.7.
	SoftThresholdFraction float64 `json:"soft_threshold_fraction,omitempty" jsonschema:"description=Fraction of the context window at which async compaction triggers,default=0.7"`

	// BudgetFraction is the fraction of the consumer model's context window
	// the whole compaction entry may use. Defaults to 0.15.
	BudgetFraction float64 `json:"budget_fraction,omitempty" jsonschema:"description=Fraction of the consumer model context window the compaction entry may use,default=0.15"`

	// MaxSummaryTokens is the hard cap on the compaction entry size.
	// Defaults to 48000.
	MaxSummaryTokens int64 `json:"max_summary_tokens,omitempty" jsonschema:"description=Hard cap on the compaction entry size,default=48000"`

	// MinSummaryTokens is the floor for the compaction entry size.
	// Defaults to 6000.
	MinSummaryTokens int64 `json:"min_summary_tokens,omitempty" jsonschema:"description=Floor for the compaction entry size,default=6000"`

	// SummaryModel is the fixed "provider/model-id" used for the checkpoint
	// lane. Empty means follow the active agent model.
	SummaryModel string `json:"summary_model,omitempty" jsonschema:"description=Fixed provider/model-id for the checkpoint lane; empty follows the active model"`

	// SummaryReasoning is the reasoning level for the checkpoint request
	// ("off", "low", "medium", "high", "max"). Defaults to "max".
	SummaryReasoning string `json:"summary_reasoning,omitempty" jsonschema:"description=Reasoning level for the checkpoint request,enum=off,enum=low,enum=medium,enum=high,enum=max,default=max"`

	// Verify selects the checkpoint coverage audit mode.
	// Defaults to "judge".
	Verify string `json:"verify,omitempty" jsonschema:"description=Checkpoint coverage audit mode,enum=judge,enum=checks,enum=off,default=judge"`

	// Ledger enables the deterministic session ledger. Defaults to true.
	Ledger bool `json:"ledger,omitempty" jsonschema:"description=Deterministic session ledger,default=true"`

	// TranscriptMap enables the per-turn transcript map. Defaults to true.
	TranscriptMap bool `json:"transcript_map,omitempty" jsonschema:"description=Per-turn transcript map,default=true"`

	// GitSnapshot records branch/head/status/diff-stat in the ledger.
	// Defaults to true.
	GitSnapshot bool `json:"git_snapshot,omitempty" jsonschema:"description=Record git state in the ledger,default=true"`

	// WorkingSetFiles is the number of recently-modified files snapshotted
	// after compaction. 0 disables the snapshot. Defaults to 3.
	WorkingSetFiles int `json:"working_set_files,omitempty" jsonschema:"description=Recently modified files snapshotted after compaction; 0 disables,default=3"`

	// WorkingSetMaxCharsPerFile is the per-file cap for the working-set
	// snapshot. Defaults to 12000.
	WorkingSetMaxCharsPerFile int `json:"working_set_max_chars_per_file,omitempty" jsonschema:"description=Per-file cap for the working-set snapshot,default=12000"`

	// ExtractsDecay is the ratio multiplier for re-compressing the previous
	// compaction's extracts. 0 disables the older lane. Defaults to 0.5.
	ExtractsDecay float64 `json:"extracts_decay,omitempty" jsonschema:"description=Ratio multiplier for re-compressing prior extracts; 0 disables,default=0.5"`

	// Embeddings enables the optional dense retrieval index. Defaults to
	// false; enabling requires a configured embedding model.
	Embeddings bool `json:"embeddings,omitempty" jsonschema:"description=Enable the optional dense retrieval index,default=false"`

	// LargeFileThreshold is the token count above which a file result is
	// stored as a reference + exploration summary instead of inlined.
	// Defaults to 25000.
	LargeFileThreshold int64 `json:"large_file_threshold,omitempty" jsonschema:"description=Token count above which file results become references + exploration summaries,default=25000"`

	// ParallelBlockThreshold is the span size (in tokens) above which the
	// checkpoint lane splits into parallel block summaries. 0 disables
	// parallel compaction. Defaults to 0 (Phase 3 enables it).
	ParallelBlockThreshold int64 `json:"parallel_block_threshold,omitempty" jsonschema:"description=Span size in tokens above which the checkpoint lane parallelizes; 0 disables,default=0"`
}

// DefaultCompactionConfig returns the production defaults for the compaction
// engine. These match the values documented in docs/compaction-plan.md.
func DefaultCompactionConfig() CompactionConfig {
	return CompactionConfig{
		Enabled:                   true,
		ReserveTokens:             16384,
		KeepRecentTokens:          20000,
		SoftThresholdFraction:     0.7,
		BudgetFraction:            0.15,
		MaxSummaryTokens:          48000,
		MinSummaryTokens:          6000,
		SummaryReasoning:          "max",
		Verify:                    string(VerificationJudge),
		Ledger:                    true,
		TranscriptMap:             true,
		GitSnapshot:               true,
		WorkingSetFiles:           3,
		WorkingSetMaxCharsPerFile: 12000,
		ExtractsDecay:             0.5,
		Embeddings:                false,
		LargeFileThreshold:        25000,
		ParallelBlockThreshold:    0,
	}
}

// ResolveCompactionConfig returns the effective compaction configuration for a
// published Config, applying defaults and reconciling the legacy
// DisableAutoSummarize flag. The legacy flag wins only when Compaction is nil
// (back-compat): an explicit Compaction.Enabled takes precedence.
func ResolveCompactionConfig(cfg *Config) CompactionConfig {
	if cfg == nil {
		return DefaultCompactionConfig()
	}
	opts := cfg.Options
	if opts == nil || opts.Compaction == nil {
		resolved := DefaultCompactionConfig()
		if opts != nil && opts.DisableAutoSummarize {
			resolved.Enabled = false
		}
		return resolved
	}
	resolved := *opts.Compaction
	applyCompactionDefaults(&resolved)
	// An explicit Compaction block is authoritative; DisableAutoSummarize is
	// ignored once the user has opted into the new config shape.
	return resolved
}

// applyCompactionDefaults fills zero values with the documented defaults
// without overwriting explicit non-zero settings.
func applyCompactionDefaults(c *CompactionConfig) {
	d := DefaultCompactionConfig()
	// Enabled defaults to true only when the struct was allocated (non-nil
	// pointer) but left zero. A user setting enabled=false explicitly also
	// leaves the bool false, so we cannot distinguish "unset" from "false"
	// on a plain bool. To preserve the ability to disable, we treat a
	// fully-zero CompactionConfig (all fields zero) as "use defaults" and
	// any non-zero field as "respect the explicit Enabled value".
	if isZeroCompaction(c) {
		*c = d
		return
	}
	if c.ReserveTokens == 0 {
		c.ReserveTokens = d.ReserveTokens
	}
	if c.KeepRecentTokens == 0 {
		c.KeepRecentTokens = d.KeepRecentTokens
	}
	if c.SoftThresholdFraction == 0 {
		c.SoftThresholdFraction = d.SoftThresholdFraction
	}
	if c.BudgetFraction == 0 {
		c.BudgetFraction = d.BudgetFraction
	}
	if c.MaxSummaryTokens == 0 {
		c.MaxSummaryTokens = d.MaxSummaryTokens
	}
	if c.MinSummaryTokens == 0 {
		c.MinSummaryTokens = d.MinSummaryTokens
	}
	if c.SummaryReasoning == "" {
		c.SummaryReasoning = d.SummaryReasoning
	}
	if c.Verify == "" {
		c.Verify = d.Verify
	}
	if c.WorkingSetFiles == 0 && c.WorkingSetMaxCharsPerFile == 0 {
		c.WorkingSetFiles = d.WorkingSetFiles
		c.WorkingSetMaxCharsPerFile = d.WorkingSetMaxCharsPerFile
	}
	if c.WorkingSetMaxCharsPerFile == 0 {
		c.WorkingSetMaxCharsPerFile = d.WorkingSetMaxCharsPerFile
	}
	if c.ExtractsDecay == 0 {
		c.ExtractsDecay = d.ExtractsDecay
	}
	if c.LargeFileThreshold == 0 {
		c.LargeFileThreshold = d.LargeFileThreshold
	}
}

func isZeroCompaction(c *CompactionConfig) bool {
	return c.Enabled == false &&
		c.ReserveTokens == 0 &&
		c.KeepRecentTokens == 0 &&
		c.SoftThresholdFraction == 0 &&
		c.BudgetFraction == 0 &&
		c.MaxSummaryTokens == 0 &&
		c.MinSummaryTokens == 0 &&
		c.SummaryModel == "" &&
		c.SummaryReasoning == "" &&
		c.Verify == "" &&
		c.Ledger == false &&
		c.TranscriptMap == false &&
		c.GitSnapshot == false &&
		c.WorkingSetFiles == 0 &&
		c.WorkingSetMaxCharsPerFile == 0 &&
		c.ExtractsDecay == 0 &&
		c.Embeddings == false &&
		c.LargeFileThreshold == 0 &&
		c.ParallelBlockThreshold == 0
}

// ParseSummaryModel splits a "provider/model-id" string into its parts. The
// provider prefix is mandatory. A model id may itself contain slashes.
func ParseSummaryModel(s string) (provider, modelID string, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", false
	}
	idx := strings.IndexByte(s, '/')
	if idx <= 0 || idx == len(s)-1 {
		return "", "", false
	}
	return s[:idx], s[idx+1:], true
}

// CompactionEnabled reports whether the compaction engine should run for the
// given published Config. It reconciles the legacy DisableAutoSummarize flag.
// Pass the result of ConfigStore.Config().
func CompactionEnabled(cfg *Config) bool {
	if cfg == nil {
		return true
	}
	return ResolveCompactionConfig(cfg).Enabled
}
