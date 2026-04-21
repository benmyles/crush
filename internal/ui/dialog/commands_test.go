package dialog

import (
	"testing"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/workspace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type commandsTestWorkspace struct {
	workspace.Workspace
	cfg *config.Config
}

func (w *commandsTestWorkspace) Config() *config.Config {
	return w.cfg
}

func TestCommandsShowsCompactSessionWhenMorphEnabled(t *testing.T) {
	t.Parallel()

	cmds := newCommandsForMorphCompactTest(t, true)
	items := cmds.defaultCommands()

	require.Contains(t, commandIDs(items), "compact")
	for _, item := range items {
		if item.ID() == "compact" {
			_, ok := item.Action().(ActionCompact)
			require.True(t, ok)
			return
		}
	}
	require.Fail(t, "compact command not found")
}

func TestCommandsHidesCompactSessionWhenMorphDisabled(t *testing.T) {
	t.Parallel()

	cmds := newCommandsForMorphCompactTest(t, false)

	assert.NotContains(t, commandIDs(cmds.defaultCommands()), "compact")
}

func newCommandsForMorphCompactTest(t *testing.T, enabled bool) *Commands {
	t.Helper()

	com := common.DefaultCommon(&commandsTestWorkspace{
		cfg: &config.Config{
			Options: &config.Options{
				TUI: &config.TUIOptions{},
				MorphCompact: &config.MorphCompactOptions{
					Enabled: enabled,
					APIKey:  "$MORPH_API_KEY",
				},
			},
			Agents: map[string]config.Agent{},
			Models: map[config.SelectedModelType]config.SelectedModel{},
			MCP:    map[string]config.MCPConfig{},
		},
	})
	cmds, err := NewCommands(com, "session-id", true, false, false, nil, nil)
	require.NoError(t, err)
	return cmds
}

func commandIDs(items []*CommandItem) []string {
	ids := make([]string, len(items))
	for i, item := range items {
		ids[i] = item.ID()
	}
	return ids
}
