package model

import (
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/commands"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/skills"
	"github.com/charmbracelet/crush/internal/ui/common"
	uistyles "github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// TestInsertSkillReferenceInsertsIntoComposer verifies the reference chip
// lands in the composer at the cursor.
func TestInsertSkillReferenceInsertsIntoComposer(t *testing.T) {
	t.Parallel()

	u := newTestUI()
	_ = u.insertSkillReference("[skill:foo]")
	require.Equal(t, "[skill:foo] ", u.textarea.Value())
}

// TestChipifySkillReferences verifies skill chips are styled inline without
// changing the rendered width or text.
func TestChipifySkillReferences(t *testing.T) {
	t.Parallel()

	u := newTestUI()
	u.textarea.SetValue("use [skill:foo] now")
	u.textarea.SetPromptFunc(4, func(info textarea.PromptInfo) string { return "> " })
	u.textarea, _ = u.textarea.Update(tea.WindowSizeMsg{Width: 60, Height: 5})

	view := u.textarea.View()
	chipped := u.chipifySkillReferences(view)

	require.NotEqual(t, view, chipped, "chip should restyle the token")
	require.Equal(t, lipgloss.Width(view), lipgloss.Width(chipped), "chip styling must not change width")
	require.Equal(t, ansi.Strip(view), ansi.Strip(chipped), "chip styling must not change text")

	// A token with no known styling context still renders.
	require.Equal(t, "[skill:foo]", ansi.Strip(u.chipifySkillReferences("[skill:foo]")))
}

// TestSkillLocationMap verifies skill name to location resolution for
// prompt expansion.
func TestSkillLocationMap(t *testing.T) {
	t.Parallel()

	u := newTestUI()
	u.customCommands = []commands.CustomCommand{
		{
			ID:   "skill-a",
			Name: "skill-a",
			Skill: &skills.Skill{
				Name:          "skill-a",
				SkillFilePath: "/skills/a/SKILL.md",
			},
		},
		{ID: "greet", Name: "greet", Content: "hello"},
		{
			ID:   "dup",
			Name: "dup",
			Skill: &skills.Skill{
				Name:          "skill-a",
				SkillFilePath: "/skills/a/dup/SKILL.md",
			},
		},
	}

	locations := u.skillLocationMap()
	require.Len(t, locations, 1)
	require.Equal(t, "/skills/a/SKILL.md", locations["skill-a"])
	require.Equal(t, "hello [use skill /skills/a/SKILL.md]",
		skills.ExpandReferences("hello [skill:skill-a]", locations))
}

// TestSkillStatusItemsIncludesBuiltinSkills verifies sidebar skills include
// both runtime-discovered skill states and builtin skills that may not have
// emitted a SkillState event yet.
func TestSkillStatusItemsIncludesBuiltinSkills(t *testing.T) {
	t.Parallel()

	st := uistyles.CharmtonePantera()
	ui := &UI{
		com: &common.Common{Styles: &st},
		skillStates: []*skills.SkillState{
			{Name: "go-doc", Path: "/tmp/go-doc/SKILL.md", State: skills.StateNormal},
		},
	}

	items := ui.skillStatusItems()
	require.NotEmpty(t, items)

	var hasGoDoc bool
	for _, item := range items {
		if item.title == st.Resource.Name.Render("go-doc") {
			hasGoDoc = true
			break
		}
	}
	require.True(t, hasGoDoc)

	builtinSkills := skills.DiscoverBuiltin()
	require.NotEmpty(t, builtinSkills)

	var hasBuiltin bool
	for _, skill := range builtinSkills {
		if skill.Name == "go-doc" {
			continue
		}
		expected := st.Resource.Name.Render(skill.Name)
		for _, item := range items {
			if item.title == expected {
				hasBuiltin = true
				break
			}
		}
		if hasBuiltin {
			break
		}
	}
	require.True(t, hasBuiltin)
}

func TestSkillStatusItemsExcludesDisabledSkills(t *testing.T) {
	t.Parallel()

	st := uistyles.CharmtonePantera()
	ui := &UI{
		com: &common.Common{
			Styles:    &st,
			Workspace: &testWorkspace{cfg: &config.Config{Options: &config.Options{DisabledSkills: []string{"go-doc", "crush-config"}}}},
		},
		skillStates: []*skills.SkillState{
			{Name: "go-doc", Path: "/tmp/go-doc/SKILL.md", State: skills.StateNormal},
		},
	}

	items := ui.skillStatusItems()

	for _, item := range items {
		require.NotEqual(t, "go-doc", item.name)
		require.NotEqual(t, "crush-config", item.name)
	}
}
