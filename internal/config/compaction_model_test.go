package config

import (
	"context"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/env"
	"github.com/stretchr/testify/require"
)

func compactionModelTestProviders() []catwalk.Provider {
	return []catwalk.Provider{
		{
			ID:                  "openai",
			APIKey:              "abc",
			DefaultLargeModelID: "large-model",
			DefaultSmallModelID: "small-model",
			Models: []catwalk.Model{
				{ID: "large-model", DefaultMaxTokens: 1000},
				{ID: "small-model", DefaultMaxTokens: 500},
				{ID: "cheap-model", DefaultMaxTokens: 700, DefaultReasoningEffort: "low"},
			},
		},
	}
}

func resolveWith(t *testing.T, models map[SelectedModelType]SelectedModel) (*Config, resolvedModels) {
	t.Helper()
	knownProviders := compactionModelTestProviders()
	cfg := &Config{Models: models}
	cfg.setDefaults(t.TempDir(), "")
	e := env.NewFromMap(map[string]string{})
	resolver := NewShellVariableResolver(e)
	require.NoError(t, cfg.configureProviders(context.Background(), testStore(cfg), e, resolver, knownProviders))
	resolved, err := resolveSelectedModels(cfg, knownProviders)
	require.NoError(t, err)
	resolved.apply(cfg)
	return cfg, resolved
}

func TestResolveSelectedModels_CompactionSlot(t *testing.T) {
	t.Run("unset slot follows the large model", func(t *testing.T) {
		cfg, resolved := resolveWith(t, map[SelectedModelType]SelectedModel{})
		require.Nil(t, resolved.Compaction)
		_, ok := cfg.Models[SelectedModelTypeCompaction]
		require.False(t, ok, "no compaction entry is materialized when unset")
		require.Nil(t, cfg.CompactionModel())
	})

	t.Run("valid slot is kept and filled with catalog defaults", func(t *testing.T) {
		cfg, resolved := resolveWith(t, map[SelectedModelType]SelectedModel{
			SelectedModelTypeCompaction: {Provider: "openai", Model: "cheap-model"},
		})
		require.NotNil(t, resolved.Compaction)
		sel := cfg.Models[SelectedModelTypeCompaction]
		require.Equal(t, "openai", sel.Provider)
		require.Equal(t, "cheap-model", sel.Model)
		require.Equal(t, int64(700), sel.MaxTokens, "max tokens default from the catalog")
		require.Equal(t, "low", sel.ReasoningEffort, "reasoning effort default from the catalog")
		require.NotNil(t, cfg.CompactionModel())
		require.Equal(t, "cheap-model", cfg.CompactionModel().ID)
	})

	t.Run("explicit fields win over catalog defaults", func(t *testing.T) {
		cfg, _ := resolveWith(t, map[SelectedModelType]SelectedModel{
			SelectedModelTypeCompaction: {Provider: "openai", Model: "cheap-model", MaxTokens: 123, ReasoningEffort: "high"},
		})
		sel := cfg.Models[SelectedModelTypeCompaction]
		require.Equal(t, int64(123), sel.MaxTokens)
		require.Equal(t, "high", sel.ReasoningEffort)
	})

	t.Run("unknown model is dropped so compaction follows large", func(t *testing.T) {
		cfg, resolved := resolveWith(t, map[SelectedModelType]SelectedModel{
			SelectedModelTypeCompaction: {Provider: "openai", Model: "does-not-exist"},
		})
		require.Nil(t, resolved.Compaction)
		_, ok := cfg.Models[SelectedModelTypeCompaction]
		require.False(t, ok)
	})

	t.Run("unknown provider is dropped so compaction follows large", func(t *testing.T) {
		cfg, resolved := resolveWith(t, map[SelectedModelType]SelectedModel{
			SelectedModelTypeCompaction: {Provider: "ghost", Model: "cheap-model"},
		})
		require.Nil(t, resolved.Compaction)
		_, ok := cfg.Models[SelectedModelTypeCompaction]
		require.False(t, ok)
	})

	t.Run("large and small are unaffected by the compaction slot", func(t *testing.T) {
		cfg, _ := resolveWith(t, map[SelectedModelType]SelectedModel{
			SelectedModelTypeCompaction: {Provider: "openai", Model: "cheap-model"},
		})
		require.Equal(t, "large-model", cfg.Models[SelectedModelTypeLarge].Model)
		require.Equal(t, "small-model", cfg.Models[SelectedModelTypeSmall].Model)
	})
}
