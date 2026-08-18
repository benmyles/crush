package dialog

import (
	"image"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/crush/internal/workspace"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/stretchr/testify/require"
)

// settingsTestWorkspace is a minimal workspace stub exposing a config.
type settingsTestWorkspace struct {
	workspace.Workspace
	cfg *config.Config
}

func (w *settingsTestWorkspace) Config() *config.Config { return w.cfg }

func newTestCompactionSettings(t *testing.T, cfg *config.Config) *CompactionSettings {
	t.Helper()
	if cfg.Providers == nil {
		cfg.Providers = csync.NewMap[string, config.ProviderConfig]()
	}
	s := styles.CharmtonePantera()
	com := &common.Common{Styles: &s, Workspace: &settingsTestWorkspace{cfg: cfg}}
	return NewCompactionSettings(com)
}

func (c *CompactionSettings) selectRow(t *testing.T, id string) *CompactionSettingItem {
	t.Helper()
	c.list.SetFilter("")
	for i, it := range c.list.FilteredItems() {
		item := it.(*CompactionSettingItem)
		if item.ID() == id {
			c.list.SetSelected(i)
			return item
		}
	}
	t.Fatalf("row %q not found", id)
	return nil
}

func TestCompactionSettings_RowsReflectConfig(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{Options: &config.Options{Compaction: &config.CompactionConfig{ReserveTokens: 32768}}}
	d := newTestCompactionSettings(t, cfg)

	reserve := d.selectRow(t, "reserve_tokens")
	require.Equal(t, "32768", reserve.value)
	require.False(t, reserve.isDefault, "an explicit value is not marked default")

	keep := d.selectRow(t, "keep_recent_tokens")
	require.Equal(t, "20000", keep.value)
	require.True(t, keep.isDefault, "unset values show the default and are marked as such")

	model := d.selectRow(t, "kind-5") // settingKindModel row
	require.Contains(t, model.value, "same as large model")
}

func TestCompactionSettings_ToggleAndCycle(t *testing.T) {
	t.Parallel()
	d := newTestCompactionSettings(t, &config.Config{Options: &config.Options{}})

	d.selectRow(t, "enabled")
	action := d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	set, ok := action.(ActionSetCompactionOption)
	require.True(t, ok, "enter on a bool row emits a set action, got %T", action)
	require.Equal(t, "enabled", set.Key)
	require.Equal(t, false, set.Value, "enabled defaults to on, so the toggle turns it off")

	d.selectRow(t, "verify")
	action = d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	set, ok = action.(ActionSetCompactionOption)
	require.True(t, ok)
	require.Equal(t, "verify", set.Key)
	require.Equal(t, "checks", set.Value, "judge cycles to checks")
}

func TestCompactionSettings_NumericEditFlow(t *testing.T) {
	t.Parallel()
	d := newTestCompactionSettings(t, &config.Config{Options: &config.Options{}})

	d.selectRow(t, "reserve_tokens")
	require.Nil(t, d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter}), "enter opens the editor")
	require.NotNil(t, d.editing)
	require.Equal(t, "16384", d.input.Value(), "the editor starts from the current value")

	// Replace the value and save.
	d.input.SetValue("")
	for _, r := range "30000" {
		d.HandleMsg(keyMsg(r))
	}
	action := d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	set, ok := action.(ActionSetCompactionOption)
	require.True(t, ok, "saving emits a set action, got %T", action)
	require.Equal(t, "reserve_tokens", set.Key)
	require.Equal(t, int64(30000), set.Value)
	require.Nil(t, d.editing, "saving leaves edit mode")

	// Invalid input reports an error and does not write.
	d.selectRow(t, "budget_fraction")
	d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	d.input.SetValue("")
	for _, r := range "1.5" {
		d.HandleMsg(keyMsg(r))
	}
	action = d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, isSet := action.(ActionSetCompactionOption)
	require.False(t, isSet, "out-of-range fraction must not be written")
	require.NotNil(t, action, "an error report is returned")

	// Escape cancels editing without closing the dialog.
	d.selectRow(t, "keep_recent_tokens")
	d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, d.editing)
	require.Nil(t, d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEscape}))
	require.Nil(t, d.editing)
}

func TestCompactionSettings_ModelRows(t *testing.T) {
	t.Parallel()
	providers := csync.NewMap[string, config.ProviderConfig]()
	providers.Set("openai", config.ProviderConfig{
		ID:     "openai",
		Models: []catwalk.Model{{ID: "gpt-x-mini", Name: "GPT X Mini", CanReason: true, ReasoningLevels: []string{"low", "medium", "high"}}},
	})
	cfg := &config.Config{
		Options:   &config.Options{},
		Providers: providers,
		Models: map[config.SelectedModelType]config.SelectedModel{
			config.SelectedModelTypeCompaction: {Provider: "openai", Model: "gpt-x-mini", ReasoningEffort: "low"},
		},
	}
	d := newTestCompactionSettings(t, cfg)

	model := d.selectRow(t, "kind-5")
	require.Contains(t, model.value, "GPT X Mini")

	// A dedicated reasoning model gets a reasoning-effort row that cycles.
	reasoning := d.selectRow(t, "kind-7")
	require.Contains(t, reasoning.value, "Low")
	action := d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	upd, ok := action.(ActionUpdateCompactionModel)
	require.True(t, ok, "enter on the reasoning row emits an update action, got %T", action)
	require.Equal(t, "medium", upd.Model.ReasoningEffort)
	require.Equal(t, "gpt-x-mini", upd.Model.Model)

	d.selectRow(t, "kind-5")
	action = d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, ok = action.(ActionOpenCompactionModel)
	require.True(t, ok, "enter on the model row opens the model picker, got %T", action)

	d.selectRow(t, "kind-6") // reset row
	action = d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, ok = action.(ActionResetCompactionOptions)
	require.True(t, ok, "enter on the reset row emits the reset action, got %T", action)
}

func TestModelType_CycleIncludesCompaction(t *testing.T) {
	t.Parallel()
	require.Equal(t, ModelTypeSmall, ModelTypeLarge.next())
	require.Equal(t, ModelTypeCompaction, ModelTypeSmall.next())
	require.Equal(t, ModelTypeLarge, ModelTypeCompaction.next())
	require.Equal(t, config.SelectedModelTypeCompaction, ModelTypeCompaction.Config())

	s := styles.CharmtonePantera()
	item := NewFollowLargeModelItem(&s)
	require.True(t, item.followLarge)
	require.Equal(t, followLargeItemID, item.ID())
	require.Equal(t, config.SelectedModelTypeCompaction, item.SelectedModelType())
}

// TestCompactionSettings_DrawRendersRows draws the dialog into a screen
// buffer and checks that the title and setting rows are visible.
func TestCompactionSettings_DrawRendersRows(t *testing.T) {
	t.Parallel()
	d := newTestCompactionSettings(t, &config.Config{Options: &config.Options{}})
	scr := uv.NewScreenBuffer(100, 40)
	d.Draw(scr, image.Rect(0, 0, 100, 40))

	var text strings.Builder
	for y := 0; y < 40; y++ {
		for x := 0; x < 100; x++ {
			if cell := scr.CellAt(x, y); cell != nil && cell.Content != "" {
				text.WriteString(cell.Content)
			}
		}
		text.WriteString("\n")
	}
	out := text.String()
	t.Log("\n" + out)
	require.Contains(t, out, "Compaction Settings")
	require.Contains(t, out, "Compaction model")
	require.Contains(t, out, "Reserve tokens")
	require.Contains(t, out, "16384")
	require.Contains(t, out, "Reset all to defaults")
}
