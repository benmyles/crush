package model

import (
	"testing"

	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestSkillMentionRangesExactMatches(t *testing.T) {
	t.Parallel()

	names := map[string]struct{}{
		"my-skill": {},
	}

	require.Equal(t, [][2]int{{4, 13}}, skillMentionRanges("run $my-skill now", names))
	require.Empty(t, skillMentionRanges("run $my now", names))
	require.Empty(t, skillMentionRanges("run $my-skill-extra now", names))
	require.Empty(t, skillMentionRanges("run $my-skill_extra now", names))
}

func TestRenderRainbowSkillMentionsStylesOnlyKnownSkills(t *testing.T) {
	t.Parallel()

	names := map[string]struct{}{
		"my-skill": {},
	}
	palette := []lipgloss.Style{
		lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
	}

	input := "use $my-skill and $other"
	got := renderRainbowSkillMentions(input, names, palette)

	require.Equal(t, input, xansi.Strip(got))
	require.Contains(t, got, "\x1b[")
	require.Equal(t, "$other", renderRainbowSkillMentions("$other", names, palette))
}

func TestRenderRainbowSkillMentionsPreservesExistingANSI(t *testing.T) {
	t.Parallel()

	names := map[string]struct{}{
		"my-skill": {},
	}
	palette := []lipgloss.Style{
		lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
	}
	line := lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Render("::: use $my-skill")

	got := renderRainbowSkillMentions(line, names, palette)

	require.Equal(t, "::: use $my-skill", xansi.Strip(got))
	require.Contains(t, got, "\x1b[")
}
