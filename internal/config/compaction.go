package config

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
// checkpoint, budgeted verbatim extracts, a working-set snapshot, and exact
// transcript recovery, all backed by the append-only message store. Raw
// messages are never mutated; compaction produces derived summary views over
// them.
//
// When Compaction is nil the legacy DisableAutoSummarize flag decides; when
// Enabled is false, Crush uses the legacy single-shot summarization path.
// In crushrc these keys are set with `option compaction <key> <value>`.
type CompactionConfig struct {
	// Enabled controls whether the compaction engine runs. When nil/false,
	// Crush uses the legacy Summarize path. Defaults to true. Uses *bool so
	// a partial config block (only reserve_tokens) does not silently disable
	// the engine.
	Enabled *bool `json:"enabled,omitempty" jsonschema:"description=Enable the context compaction engine,default=true"`

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

	// Verify selects the checkpoint coverage audit mode.
	// Defaults to "judge".
	Verify string `json:"verify,omitempty" jsonschema:"description=Checkpoint coverage audit mode,enum=judge,enum=checks,enum=off,default=judge"`

	// Ledger enables the deterministic session ledger. *bool so a partial
	// block does not silently disable it. Defaults to true.
	Ledger *bool `json:"ledger,omitempty" jsonschema:"description=Deterministic session ledger,default=true"`

	// TranscriptMap enables the per-turn transcript map. Defaults to true.
	TranscriptMap *bool `json:"transcript_map,omitempty" jsonschema:"description=Per-turn transcript map,default=true"`

	// WorkingSetFiles is the number of recently-modified files snapshotted
	// after compaction. 0 disables the snapshot. Defaults to 3.
	WorkingSetFiles int `json:"working_set_files,omitempty" jsonschema:"description=Recently modified files snapshotted after compaction; 0 disables,default=3"`

	// WorkingSetMaxCharsPerFile is the per-file cap for the working-set
	// snapshot. Defaults to 12000.
	WorkingSetMaxCharsPerFile int `json:"working_set_max_chars_per_file,omitempty" jsonschema:"description=Per-file cap for the working-set snapshot,default=12000"`

	// ExtractsDecay is the ratio multiplier for re-compressing the previous
	// compaction's extracts. 0 disables the older lane; a negative value
	// disables the extracts lane entirely. *float64 so an unset value (nil)
	// takes the default (0.5) while an explicit 0 is respected.
	ExtractsDecay *float64 `json:"extracts_decay,omitempty" jsonschema:"description=Ratio multiplier for re-compressing prior extracts; 0 disables the older lane; negative disables extracts,default=0.5"`

	// ParallelBlockThreshold is the span size (in tokens) above which the
	// checkpoint lane splits into parallel block summaries. 0 disables
	// parallel compaction. Defaults to 0.
	ParallelBlockThreshold int64 `json:"parallel_block_threshold,omitempty" jsonschema:"description=Span size in tokens above which the checkpoint lane parallelizes; 0 disables,default=0"`
}

// DefaultCompactionConfig returns the production defaults for the compaction
// engine. These match the values documented in docs/compaction-plan.md.
func DefaultCompactionConfig() CompactionConfig {
	return CompactionConfig{
		Enabled:                   ptrBool(true),
		ReserveTokens:             16384,
		KeepRecentTokens:          20000,
		SoftThresholdFraction:     0.7,
		BudgetFraction:            0.15,
		MaxSummaryTokens:          48000,
		MinSummaryTokens:          6000,
		Verify:                    string(VerificationJudge),
		Ledger:                    ptrBool(true),
		TranscriptMap:             ptrBool(true),
		WorkingSetFiles:           3,
		WorkingSetMaxCharsPerFile: 12000,
		ExtractsDecay:             ptrFloat(0.5),
		ParallelBlockThreshold:    0,
	}
}

func ptrBool(v bool) *bool { return &v }

func ptrFloat(v float64) *float64 { return &v }

// ExtractsDecayValue returns the effective extracts decay ratio: the default
// (0.5) when unset, otherwise the configured value (0 disables the older
// lane, negative disables the extracts lane).
func (c CompactionConfig) ExtractsDecayValue() float64 {
	if c.ExtractsDecay == nil {
		return *DefaultCompactionConfig().ExtractsDecay
	}
	return *c.ExtractsDecay
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
			resolved.Enabled = ptrBool(false)
		}
		return resolved
	}
	// A fully-zero Compaction block (all nil/zero) means "use defaults" so
	// an empty options.compaction = {} does not disable everything.
	if isZeroCompactionBlock(opts.Compaction) {
		resolved := DefaultCompactionConfig()
		if opts.DisableAutoSummarize {
			resolved.Enabled = ptrBool(false)
		}
		return resolved
	}
	resolved := *opts.Compaction
	applyCompactionDefaults(&resolved)
	return resolved
}

// isZeroCompactionBlock reports whether every field is nil/zero, meaning the
// user wrote options.compaction = {} or only set fields that are zero-valued.
func isZeroCompactionBlock(c *CompactionConfig) bool {
	return c.Enabled == nil &&
		c.ReserveTokens == 0 &&
		c.KeepRecentTokens == 0 &&
		c.SoftThresholdFraction == 0 &&
		c.BudgetFraction == 0 &&
		c.MaxSummaryTokens == 0 &&
		c.MinSummaryTokens == 0 &&
		c.Verify == "" &&
		c.Ledger == nil &&
		c.TranscriptMap == nil &&
		c.WorkingSetFiles == 0 &&
		c.WorkingSetMaxCharsPerFile == 0 &&
		c.ExtractsDecay == nil &&
		c.ParallelBlockThreshold == 0
}

// applyCompactionDefaults fills zero values with the documented defaults
// without overwriting explicit non-zero settings.
func applyCompactionDefaults(c *CompactionConfig) {
	d := DefaultCompactionConfig()
	// *bool fields: nil means unset -> use the default. Explicit false is
	// respected (the user disabled the feature).
	if c.Enabled == nil {
		c.Enabled = d.Enabled
	}
	if c.Ledger == nil {
		c.Ledger = d.Ledger
	}
	if c.TranscriptMap == nil {
		c.TranscriptMap = d.TranscriptMap
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
	if c.ExtractsDecay == nil {
		c.ExtractsDecay = d.ExtractsDecay
	}
}

// CompactionEnabled reports whether the compaction engine should run for the
// given published Config. It reconciles the legacy DisableAutoSummarize flag.
// Pass the result of ConfigStore.Config().
func CompactionEnabled(cfg *Config) bool {
	if cfg == nil {
		return true
	}
	return ResolveCompactionConfig(cfg).Enabled != nil && *ResolveCompactionConfig(cfg).Enabled
}
