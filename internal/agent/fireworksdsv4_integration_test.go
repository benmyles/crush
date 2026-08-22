package agent

import (
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/agent/fireworksdsv4"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

func TestGetProviderOptionsFireworksDSV4(t *testing.T) {
	t.Parallel()

	model := Model{
		CatwalkCfg: catwalk.Model{
			ID:                     "accounts/fireworks/models/deepseek-v4-flash",
			CanReason:              true,
			ReasoningLevels:        []string{"none", "low", "high", "max"},
			DefaultReasoningEffort: "high",
			Options: catwalk.ModelOptions{ProviderOptions: map[string]any{
				"extra_body": map[string]any{"catalog": true},
			}},
		},
		ModelCfg: config.SelectedModel{
			Provider:        fireworksdsv4.Name,
			ReasoningEffort: "max",
			ProviderOptions: map[string]any{
				"extra_body": map[string]any{"model": true},
			},
		},
	}
	providerCfg := config.ProviderConfig{
		ID:   fireworksdsv4.Name,
		Type: fireworksdsv4.Name,
		ProviderOptions: map[string]any{
			"extra_body": map[string]any{"provider": true},
		},
	}
	options := getProviderOptions(model, providerCfg, "")
	parsed, ok := options[fireworksdsv4.Name].(*fireworksdsv4.ProviderOptions)
	require.True(t, ok)
	require.Equal(t, "max", parsed.ReasoningEffort)
	require.Equal(t, true, parsed.ExtraBody["catalog"])
	require.Equal(t, true, parsed.ExtraBody["provider"])
	require.Equal(t, true, parsed.ExtraBody["model"])
}
