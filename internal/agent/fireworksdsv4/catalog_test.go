package fireworksdsv4

import (
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/stretchr/testify/require"
)

func TestCatalogAlias(t *testing.T) {
	t.Parallel()

	source := catwalk.Provider{
		ID:                  catwalk.InferenceProviderFireworks,
		Name:                "Fireworks",
		APIKey:              "$FIREWORKS_API_KEY",
		APIEndpoint:         "https://api.fireworks.ai/inference/v1",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "accounts/fireworks/models/deepseek-v4-pro",
		DefaultSmallModelID: "accounts/fireworks/models/deepseek-v4-flash",
		Models: []catwalk.Model{
			{ID: "accounts/fireworks/models/deepseek-v4-pro", CostPer1MIn: 1.74, SupportsImages: true},
			{ID: "accounts/fireworks/models/deepseek-v4-flash", CostPer1MIn: 0.14},
			{ID: "accounts/fireworks/models/qwen", CostPer1MIn: 0.1},
		},
	}
	alias, ok := CatalogAlias(source)
	require.True(t, ok)
	require.Equal(t, catwalk.InferenceProvider(Name), alias.ID)
	require.Equal(t, catwalk.Type(Name), alias.Type)
	require.Equal(t, source.APIKey, alias.APIKey)
	require.Equal(t, source.APIEndpoint, alias.APIEndpoint)
	require.Len(t, alias.Models, 2)
	require.Equal(t, []string{"none", "low", "high", "max"}, alias.Models[0].ReasoningLevels)
	require.Equal(t, "high", alias.Models[0].DefaultReasoningEffort)
	require.False(t, alias.Models[0].SupportsImages)
	require.Equal(t, 1.74, alias.Models[0].CostPer1MIn)
	require.Equal(t, source.DefaultLargeModelID, alias.DefaultLargeModelID)
	require.Equal(t, source.DefaultSmallModelID, alias.DefaultSmallModelID)
}

func TestCatalogAliasRejectsOtherProvidersAndEmptyCatalogs(t *testing.T) {
	t.Parallel()

	_, ok := CatalogAlias(catwalk.Provider{ID: "other"})
	require.False(t, ok)
	_, ok = CatalogAlias(catwalk.Provider{ID: catwalk.InferenceProviderFireworks, Models: []catwalk.Model{{ID: "qwen"}}})
	require.False(t, ok)
}
