package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveCompactionConfig_PartialBlockDoesNotDisableEngine(t *testing.T) {
	t.Parallel()
	// A partial block with only reserve_tokens set must not silently disable
	// the engine, ledger, or transcript map. This was the core B4 bug: plain
	// bools couldn't distinguish unset from false.
	cfg := &Config{
		Options: &Options{
			Compaction: &CompactionConfig{
				ReserveTokens: 32768,
			},
		},
	}
	resolved := ResolveCompactionConfig(cfg)
	require.NotNil(t, resolved.Enabled)
	require.True(t, *resolved.Enabled, "partial block must not disable the engine")
	require.NotNil(t, resolved.Ledger)
	require.True(t, *resolved.Ledger, "partial block must not disable the ledger")
	require.NotNil(t, resolved.TranscriptMap)
	require.True(t, *resolved.TranscriptMap, "partial block must not disable the transcript map")
	require.Equal(t, int64(32768), resolved.ReserveTokens, "explicit value must be respected")
	require.Equal(t, int64(20000), resolved.KeepRecentTokens, "unset value must get the default")
}

func TestResolveCompactionConfig_ExplicitDisable(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		Options: &Options{
			Compaction: &CompactionConfig{
				Enabled:       ptrBool(false),
				ReserveTokens: 32768,
			},
		},
	}
	resolved := ResolveCompactionConfig(cfg)
	require.NotNil(t, resolved.Enabled)
	require.False(t, *resolved.Enabled, "explicit enabled=false must be respected")
}

func TestResolveCompactionConfig_EmptyBlockUsesDefaults(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		Options: &Options{
			Compaction: &CompactionConfig{},
		},
	}
	resolved := ResolveCompactionConfig(cfg)
	require.NotNil(t, resolved.Enabled)
	require.True(t, *resolved.Enabled)
	require.Equal(t, int64(16384), resolved.ReserveTokens)
}

func TestResolveCompactionConfig_NilCompactionFallsBackToLegacy(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		Options: &Options{
			DisableAutoSummarize: true,
		},
	}
	resolved := ResolveCompactionConfig(cfg)
	require.NotNil(t, resolved.Enabled)
	require.False(t, *resolved.Enabled, "DisableAutoSummarize must disable the engine when Compaction is nil")
}

func TestResolveCompactionConfig_ExtractsDecayZeroDisables(t *testing.T) {
	t.Parallel()
	// ExtractsDecay: 0 is a valid "disable" value, not re-defaulted to 0.5.
	cfg := &Config{
		Options: &Options{
			Compaction: &CompactionConfig{
				ExtractsDecay: 0,
				ReserveTokens: 16384, // non-zero so the block isn't "empty"
			},
		},
	}
	resolved := ResolveCompactionConfig(cfg)
	require.Equal(t, float64(0), resolved.ExtractsDecay, "explicit 0 must disable the older lane, not be re-defaulted to 0.5")
}

func TestCompactionEnabled(t *testing.T) {
	t.Parallel()
	require.True(t, CompactionEnabled(nil), "nil config defaults to enabled")
	cfg := &Config{Options: &Options{}}
	require.True(t, CompactionEnabled(cfg), "nil Compaction defaults to enabled")
	cfg.Options.DisableAutoSummarize = true
	require.False(t, CompactionEnabled(cfg), "DisableAutoSummarize disables when Compaction is nil")
	cfg.Options.DisableAutoSummarize = false
	cfg.Options.Compaction = &CompactionConfig{Enabled: ptrBool(false)}
	require.False(t, CompactionEnabled(cfg), "explicit enabled=false disables")
	cfg.Options.Compaction = &CompactionConfig{Enabled: ptrBool(true)}
	require.True(t, CompactionEnabled(cfg), "explicit enabled=true enables")
}
