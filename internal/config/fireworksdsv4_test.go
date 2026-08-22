package config

import (
	"context"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/agent/fireworksdsv4"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/env"
	"github.com/stretchr/testify/require"
)

func TestWithFireworksDSV4Alias(t *testing.T) {
	t.Parallel()

	providers := []catwalk.Provider{
		{ID: "before"},
		{
			ID:          catwalk.InferenceProviderFireworks,
			APIKey:      "$FIREWORKS_API_KEY",
			APIEndpoint: "https://api.fireworks.ai/inference/v1",
			Models: []catwalk.Model{
				{ID: "accounts/fireworks/models/deepseek-v4-pro"},
				{ID: "accounts/fireworks/models/qwen"},
			},
		},
		{ID: "after"},
	}
	result := withFireworksDSV4Alias(providers)
	require.Len(t, result, 4)
	require.Equal(t, catwalk.InferenceProviderFireworks, result[1].ID)
	require.Equal(t, catwalk.InferenceProvider(fireworksdsv4.Name), result[2].ID)
	require.Equal(t, catwalk.InferenceProvider("after"), result[3].ID)
}

func TestConfigureProvidersAcceptsExplicitFireworksDSV4Type(t *testing.T) {
	t.Parallel()

	discover := false
	cfg := &Config{Providers: csync.NewMapFrom(map[string]ProviderConfig{
		"custom-dsv4": {
			Type:               fireworksdsv4.Name,
			BaseURL:            "https://api.fireworks.ai/inference/v1",
			APIKey:             "key",
			AutoDiscoverModels: &discover,
			Models: []catwalk.Model{{
				ID: "accounts/fireworks/models/deepseek-v4-flash",
			}},
		},
	})}
	cfg.setDefaults(t.TempDir(), "")
	values := env.NewFromMap(nil)
	err := cfg.configureProviders(context.Background(), testStore(cfg), values, NewShellVariableResolver(values), nil)
	require.NoError(t, err)
	provider, ok := cfg.Providers.Get("custom-dsv4")
	require.True(t, ok)
	require.Equal(t, catwalk.Type(fireworksdsv4.Name), provider.Type)
}
