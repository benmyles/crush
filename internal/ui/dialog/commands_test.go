package dialog

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/commands"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/skills"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/crush/internal/workspace"
	"github.com/stretchr/testify/require"
)

// commandsTestWorkspace is a minimal workspace stub exposing a config.
type commandsTestWorkspace struct {
	workspace.Workspace
	cfg *config.Config
}

func (w *commandsTestWorkspace) Config() *config.Config { return w.cfg }

func newTestCommands(t *testing.T, custom []commands.CustomCommand) *Commands {
	t.Helper()
	cfg := &config.Config{
		Models:    map[config.SelectedModelType]config.SelectedModel{},
		Providers: csync.NewMap[string, config.ProviderConfig](),
	}
	s := styles.CharmtonePantera()
	com := &common.Common{Styles: &s, Workspace: &commandsTestWorkspace{cfg: cfg}}
	d, err := NewCommands(com, "", false, false, false, custom, nil)
	require.NoError(t, err)
	return d
}

func testSkillCommand(name, desc, location string) commands.CustomCommand {
	return commands.CustomCommand{
		ID:   name,
		Name: name,
		Skill: &skills.Skill{
			Name:          name,
			Description:   desc,
			SkillFilePath: location,
		},
	}
}

// selectCommandID moves the selection to the command item with the given id.
func (c *Commands) selectCommandID(t *testing.T, id string) {
	t.Helper()
	for i, it := range c.list.FilteredItems() {
		if item, ok := it.(*CommandItem); ok && item.ID() == id {
			c.list.SetSelected(i)
			return
		}
	}
	t.Fatalf("command %q not found", id)
}

func TestCommands_SkillsSection(t *testing.T) {
	t.Parallel()
	skill := testSkillCommand("http-server", "Serves HTTP", "/tmp/skills/http-server/SKILL.md")
	d := newTestCommands(t, []commands.CustomCommand{
		skill,
		{ID: "greet", Name: "greet", Content: "say hello"},
	})

	items := d.list.FilteredItems()
	sectionIdx := -1
	skillIdx := -1
	for i, it := range items {
		switch item := it.(type) {
		case *CommandSection:
			require.Equal(t, "Skills", item.title)
			sectionIdx = i
		case *CommandItem:
			if item.ID() == "skill_http-server" {
				skillIdx = i
				require.Equal(t, "Skill: http-server", item.title)
			}
		}
	}
	require.Greater(t, sectionIdx, -1, "skills section header missing")
	require.Greater(t, skillIdx, -1, "skill item missing")
	require.Greater(t, skillIdx, sectionIdx, "skill item must come after the section header")

	// The divider renders as a section line.
	rendered := items[sectionIdx].Render(40)
	require.Contains(t, rendered, "Skills")

	// Enter on a skill returns a skill reference insertion action.
	d.selectCommandID(t, "skill_http-server")
	action := d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	insert, ok := action.(ActionInsertSkillReference)
	require.True(t, ok, "enter on skill emits an insert action, got %T", action)
	require.Equal(t, "[skill:http-server]", insert.Reference)

	// Skills are not repeated in the user commands tab, and plain custom
	// commands remain there.
	d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyTab})
	for _, it := range d.list.FilteredItems() {
		item, ok := it.(*CommandItem)
		require.True(t, ok, "user tab must contain only commands, got %T", it)
		require.NotEqual(t, "skill_http-server", item.ID())
	}
}

func TestCommands_SkillsFilterBySubstring(t *testing.T) {
	t.Parallel()
	skill := testSkillCommand("http-server", "Serves HTTP", "/tmp/skills/http-server/SKILL.md")
	d := newTestCommands(t, []commands.CustomCommand{skill})

	// A substring that only matches the skill name keeps exactly the skill
	// item, and the section header is hidden while filtering.
	d.list.SetFilter("server")
	items := d.list.FilteredItems()
	require.Len(t, items, 1, "only the matching skill item should remain")
	item, ok := items[0].(*CommandItem)
	require.True(t, ok, "expected a command item, got %T", items[0])
	require.Equal(t, "skill_http-server", item.ID())

	// The description is searchable too.
	d.list.SetFilter("serves")
	for _, it := range d.list.FilteredItems() {
		item, ok := it.(*CommandItem)
		require.True(t, ok, "section header should be hidden while filtering, got %T", it)
		require.Equal(t, "skill_http-server", item.ID())
	}
}

func TestCommands_NoSkillsNoSection(t *testing.T) {
	t.Parallel()
	d := newTestCommands(t, []commands.CustomCommand{
		{ID: "greet", Name: "greet", Content: "say hello"},
	})

	for _, it := range d.list.FilteredItems() {
		_, isSection := it.(*CommandSection)
		require.False(t, isSection, "no skills section when there are no skills")
	}

	// Arrow navigation never lands on a non-command item.
	d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyUp})
	_, ok := d.list.SelectedItem().(*CommandItem)
	require.True(t, ok, "selection must stay on commands")
}
