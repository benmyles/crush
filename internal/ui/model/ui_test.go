package model

import (
	"testing"

	"charm.land/bubbles/v2/textarea"
	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/crush/internal/workspace"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestCurrentModelSupportsImages(t *testing.T) {
	t.Parallel()

	t.Run("returns false when config is nil", func(t *testing.T) {
		t.Parallel()

		ui := newTestUIWithConfig(t, nil)
		require.False(t, ui.currentModelSupportsImages())
	})

	t.Run("returns false when coder agent is missing", func(t *testing.T) {
		t.Parallel()

		cfg := &config.Config{
			Providers: csync.NewMap[string, config.ProviderConfig](),
			Agents:    map[string]config.Agent{},
		}
		ui := newTestUIWithConfig(t, cfg)
		require.False(t, ui.currentModelSupportsImages())
	})

	t.Run("returns false when model is not found", func(t *testing.T) {
		t.Parallel()

		cfg := &config.Config{
			Providers: csync.NewMap[string, config.ProviderConfig](),
			Agents: map[string]config.Agent{
				config.AgentCoder: {Model: config.SelectedModelTypeLarge},
			},
		}
		ui := newTestUIWithConfig(t, cfg)
		require.False(t, ui.currentModelSupportsImages())
	})

	t.Run("returns true when current model supports images", func(t *testing.T) {
		t.Parallel()

		providers := csync.NewMap[string, config.ProviderConfig]()
		providers.Set("test-provider", config.ProviderConfig{
			ID: "test-provider",
			Models: []catwalk.Model{
				{ID: "test-model", SupportsImages: true},
			},
		})

		cfg := &config.Config{
			Models: map[config.SelectedModelType]config.SelectedModel{
				config.SelectedModelTypeLarge: {
					Provider: "test-provider",
					Model:    "test-model",
				},
			},
			Providers: providers,
			Agents: map[string]config.Agent{
				config.AgentCoder: {Model: config.SelectedModelTypeLarge},
			},
		}

		ui := newTestUIWithConfig(t, cfg)
		require.True(t, ui.currentModelSupportsImages())
	})
}

func TestPlanModeUsesDistinctEditorVisuals(t *testing.T) {
	t.Parallel()

	ws := &testWorkspace{}
	sty := styles.DefaultStyles()
	ta := textarea.New()
	ta.SetStyles(sty.TextArea)
	ta.SetWidth(40)
	ta.SetHeight(3)
	ta.Focus()
	ta.SetValue("x")

	ui := &UI{
		com: &common.Common{
			Workspace: ws,
			Styles:    &sty,
		},
		textarea:           ta,
		focus:              uiFocusEditor,
		readyPlaceholder:   "Ready",
		workingPlaceholder: "Working",
	}
	ui.setEditorPrompt(false, false)
	require.NotContains(t, xansi.Strip(ui.textarea.View()), " ! ")
	require.NotContains(t, xansi.Strip(ui.textarea.View()), " ✎ ")

	ui.setPlanModeActive(true)
	ui.refreshEditorPlaceholder()

	require.Equal(t, "Plan mode!", ui.textarea.Placeholder)
	require.NotContains(t, xansi.Strip(ui.textarea.View()), " ! ")
	require.Contains(t, xansi.Strip(ui.textarea.View()), " ✎ ")

	ws.skipRequests = true
	ui.refreshEditorPrompt()
	ui.refreshEditorPlaceholder()

	require.Equal(t, "Plan mode!", ui.textarea.Placeholder)
	require.NotContains(t, xansi.Strip(ui.textarea.View()), " ! ")
	require.Contains(t, xansi.Strip(ui.textarea.View()), " ✎ ")

	ui.setPlanModeActive(false)
	ui.refreshEditorPlaceholder()

	require.Equal(t, "Yolo mode!", ui.textarea.Placeholder)
	require.Contains(t, xansi.Strip(ui.textarea.View()), " ! ")
	require.NotContains(t, xansi.Strip(ui.textarea.View()), " ✎ ")
}

func TestPlanModeToggleActionTogglesVisualState(t *testing.T) {
	t.Parallel()

	ws := &testWorkspace{}
	sty := styles.DefaultStyles()
	ta := textarea.New()
	ta.SetStyles(sty.TextArea)
	ta.SetWidth(40)
	ta.SetHeight(3)
	ta.Focus()
	ta.SetValue("x")

	ui := &UI{
		com: &common.Common{
			Workspace: ws,
			Styles:    &sty,
		},
		textarea:           ta,
		focus:              uiFocusEditor,
		readyPlaceholder:   "Ready",
		workingPlaceholder: "Working",
	}
	ui.setEditorPrompt(false, false)
	require.NotContains(t, xansi.Strip(ui.textarea.View()), " ! ")
	require.NotContains(t, xansi.Strip(ui.textarea.View()), " ✎ ")

	ui.togglePlanMode()
	require.True(t, ui.planModeActive)
	require.Equal(t, "Plan mode!", ui.textarea.Placeholder)
	require.NotContains(t, xansi.Strip(ui.textarea.View()), " ! ")
	require.Contains(t, xansi.Strip(ui.textarea.View()), " ✎ ")

	ui.togglePlanMode()
	require.False(t, ui.planModeActive)
	require.Equal(t, "Ready", ui.textarea.Placeholder)
	require.NotContains(t, xansi.Strip(ui.textarea.View()), " ! ")
	require.NotContains(t, xansi.Strip(ui.textarea.View()), " ✎ ")
}

func TestCompactionNotificationStartsAndStopsLoader(t *testing.T) {
	t.Parallel()

	ui := newCompactionTestUI("session-1")

	cmd := ui.handleAgentNotification(notify.Notification{
		SessionID: "session-1",
		Type:      notify.TypeCompactionStarted,
	})

	require.NotNil(t, cmd)
	require.True(t, ui.compactionActive)
	require.Equal(t, "session-1", ui.compactionSessionID)
	require.True(t, ui.shouldDrawCompactionLoader(false))

	ui.handleAgentNotification(notify.Notification{
		SessionID: "session-1",
		Type:      notify.TypeCompactionFinished,
	})

	require.False(t, ui.compactionActive)
	require.Empty(t, ui.compactionSessionID)
	require.False(t, ui.shouldDrawCompactionLoader(false))
}

func TestCompactionNotificationIgnoresOtherSessions(t *testing.T) {
	t.Parallel()

	ui := newCompactionTestUI("session-1")

	cmd := ui.handleAgentNotification(notify.Notification{
		SessionID: "session-2",
		Type:      notify.TypeCompactionStarted,
	})

	require.Nil(t, cmd)
	require.False(t, ui.compactionActive)
	require.False(t, ui.shouldDrawCompactionLoader(false))
}

func newTestUIWithConfig(t *testing.T, cfg *config.Config) *UI {
	t.Helper()

	return &UI{
		com: &common.Common{
			Workspace: &testWorkspace{cfg: cfg},
		},
	}
}

func newCompactionTestUI(sessionID string) *UI {
	sty := styles.DefaultStyles()
	com := &common.Common{
		Workspace: &testWorkspace{},
		Styles:    &sty,
	}
	return &UI{
		com:     com,
		session: &session.Session{ID: sessionID},
		status:  NewStatus(com, nil),
	}
}

// testWorkspace is a minimal [workspace.Workspace] stub for unit tests.
type testWorkspace struct {
	workspace.Workspace
	cfg          *config.Config
	skipRequests bool
}

func (w *testWorkspace) Config() *config.Config {
	return w.cfg
}

func (w *testWorkspace) PermissionSkipRequests() bool {
	return w.skipRequests
}

func (w *testWorkspace) AgentIsReady() bool {
	return false
}

func (w *testWorkspace) AgentIsBusy() bool {
	return false
}
