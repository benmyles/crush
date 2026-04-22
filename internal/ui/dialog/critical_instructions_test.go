package dialog

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/stretchr/testify/require"
)

func TestCriticalInstructionsSavesInlineText(t *testing.T) {
	t.Parallel()

	d := NewCriticalInstructions(common.DefaultCommon(nil), config.ScopeWorkspace, "Keep all details.")
	action := d.HandleMsg(tea.PasteMsg{Content: "\n\nAsk before truncating."})
	require.Nil(t, action)

	action = d.HandleMsg(keyCode(tea.KeyTab))
	require.Nil(t, action)
	require.Equal(t, criticalInstructionsFocusActions, d.focus)

	action = d.HandleMsg(keyCode(tea.KeyEnter))
	resp, ok := action.(ActionCriticalInstructionsResponse)
	require.True(t, ok)
	require.Equal(t, config.ScopeWorkspace, resp.Scope)
	require.Equal(t, "Keep all details.\n\nAsk before truncating.", resp.Text)
}

func TestCriticalInstructionsCopiesInlineText(t *testing.T) {
	t.Parallel()

	d := NewCriticalInstructions(common.DefaultCommon(nil), config.ScopeGlobal, "Never truncate.")
	action := d.HandleMsg(tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))
	_, ok := action.(ActionCmd)
	require.True(t, ok)
}
