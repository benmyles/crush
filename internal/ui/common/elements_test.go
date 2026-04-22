package common

import (
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContextUsagePercentage(t *testing.T) {
	t.Parallel()

	_, ok := ContextUsagePercentage(100, 0)
	require.False(t, ok)

	percentage, ok := ContextUsagePercentage(160_000, 200_000)
	require.True(t, ok)
	assert.Equal(t, int64(80), percentage)

	percentage, ok = ContextUsagePercentage(2_000_000, 100_000)
	require.True(t, ok)
	assert.Equal(t, int64(999), percentage)
}

func TestFormatTokensAndCostUnknownContextWindow(t *testing.T) {
	t.Parallel()

	sty := styles.DefaultStyles()
	rendered := ansi.Strip(formatTokensAndCost(&sty, 12_345, 0, 1.23))

	assert.Contains(t, rendered, "(12.3K)")
	assert.Contains(t, rendered, "$1.23")
	assert.NotContains(t, rendered, "%")
}

func TestFormatTokensAndCostCapsPercentage(t *testing.T) {
	t.Parallel()

	sty := styles.DefaultStyles()
	rendered := ansi.Strip(formatTokensAndCost(&sty, 2_000_000, 100_000, 0))

	assert.Contains(t, rendered, "999%")
	assert.False(t, strings.Contains(rendered, "2000%"))
}
