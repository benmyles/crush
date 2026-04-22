package model

import (
	"context"
	"slices"
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/planning"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/attachments"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/crush/internal/ui/util"
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

func TestClearPromptShortcutClearsFullTextarea(t *testing.T) {
	t.Parallel()

	ui := newTestUI()
	ui.keyMap = DefaultKeyMap()
	ui.dialog = dialog.NewOverlay()
	ui.attachments = attachments.New(nil, attachments.Keymap{
		DeleteMode: ui.keyMap.Editor.AttachmentDeleteMode,
		DeleteAll:  ui.keyMap.Editor.DeleteAllAttachments,
		Escape:     ui.keyMap.Editor.Escape,
	})
	ui.updateLayoutAndSize()

	ui.textarea.SetValue("first line\nsecond line")
	ui.textarea.MoveToEnd()
	ui.promptHistory.index = 0
	ui.promptHistory.draft = "draft message"

	ui.handleKeyPressMsg(tea.KeyPressMsg(tea.Key{
		Code: 'x',
		Mod:  tea.ModCtrl,
	}))

	require.Empty(t, ui.textarea.Value())
	require.Equal(t, -1, ui.promptHistory.index)
	require.Empty(t, ui.promptHistory.draft)
}

func TestInsertSnippetTextMainPrompt(t *testing.T) {
	t.Parallel()

	ui := newTestUI()
	ui.dialog = dialog.NewOverlay()
	ui.textarea.SetValue("Before ")
	ui.textarea.MoveToEnd()

	ui.insertSnippetText("after")

	require.Equal(t, "Before after", ui.textarea.Value())
	require.Equal(t, "Before after", ui.promptHistory.draft)
	require.Equal(t, -1, ui.promptHistory.index)
}

func TestNewSnippetIDProducesUniqueIDs(t *testing.T) {
	t.Parallel()

	ids := make(map[string]struct{}, 100)
	for i := 0; i < 100; i++ {
		id := newSnippetID()
		require.NotEmpty(t, id)
		_, exists := ids[id]
		require.False(t, exists, "duplicate snippet ID: %s", id)
		ids[id] = struct{}{}
	}
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

func TestRevisePlanPromptRequiresCompleteResubmission(t *testing.T) {
	t.Parallel()

	prompt := revisePlanPrompt(planning.Submission{
		Markdown: "## Plan\n\n- Do the thing.",
	}, "Handle edge cases first.")

	require.Contains(t, prompt, "You must revise it before doing any implementation.")
	require.Contains(t, prompt, "Rework the plan based on the user's feedback below.")
	require.Contains(t, prompt, "ask the user follow-up questions using the relevant tool")
	require.Contains(t, prompt, "call submit_plan with a complete updated plan and structured todos")
	require.Contains(t, prompt, "so the user can re-review the revised plan")
	require.Contains(t, prompt, "Handle edge cases first.")
}

func TestPlanApprovalRevisionRespondsToBlockedSubmission(t *testing.T) {
	t.Parallel()

	ws := &testWorkspace{}
	sty := styles.DefaultStyles()
	ta := textarea.New()
	ta.SetStyles(sty.TextArea)
	ui := &UI{
		com: &common.Common{
			Workspace: ws,
			Styles:    &sty,
		},
		textarea:           ta,
		readyPlaceholder:   "Ready",
		workingPlaceholder: "Working",
	}

	cmd := ui.handlePlanApprovalResponse(dialog.ActionPlanApprovalResponse{
		Submission: planning.Submission{
			ID:        "plan-1",
			SessionID: "session-1",
		},
		Approved: false,
		Comment:  "Handle edge cases first.",
	})

	require.NotNil(t, cmd)
	require.True(t, ui.planModeActive)
	require.Len(t, ws.planResponses, 1)
	require.Equal(t, planning.Response{
		SubmissionID: "plan-1",
		Approved:     false,
		Comment:      "Handle edge cases first.",
	}, ws.planResponses[0])
}

func TestPlanApprovalApprovalStopsPlanModeBeforeImplementation(t *testing.T) {
	t.Parallel()

	ws := &testWorkspace{
		session: session.Session{ID: "session-1"},
	}
	sty := styles.DefaultStyles()
	ta := textarea.New()
	ta.SetStyles(sty.TextArea)
	ui := &UI{
		com: &common.Common{
			Workspace: ws,
			Styles:    &sty,
		},
		session:            &session.Session{ID: "session-1"},
		textarea:           ta,
		planModeActive:     true,
		readyPlaceholder:   "Ready",
		workingPlaceholder: "Working",
	}

	cmd := ui.handlePlanApprovalResponse(dialog.ActionPlanApprovalResponse{
		Submission: planning.Submission{
			ID:        "plan-1",
			SessionID: "session-1",
			Todos: []session.Todo{{
				Content:    "Do the thing",
				Status:     session.TodoStatusPending,
				ActiveForm: "Doing the thing",
			}},
		},
		Approved: true,
		Comment:  "Looks good.",
	})

	require.NotNil(t, cmd)
	require.False(t, ui.planModeActive)

	msg := cmd()
	require.IsType(t, util.InfoMsg{}, msg)
	require.Len(t, ws.planResponses, 1)
	require.True(t, ws.planResponses[0].Approved)
	require.Equal(t, []string{"busy-check", "plan-respond", "get-session", "save-session", "agent-run"}, ws.events)
	require.Equal(t, []session.Todo{{
		Content:    "Do the thing",
		Status:     session.TodoStatusPending,
		ActiveForm: "Doing the thing",
	}}, ws.session.Todos)
	require.Len(t, ws.agentRunOptions, 1)
	require.False(t, ws.agentRunOptions[0].PlanMode)
	require.Contains(t, ws.agentRunPrompts[0], "Looks good.")
}

func TestPlanApprovalPlanRespondCalledAfterSessionIdle(t *testing.T) {
	t.Parallel()

	ws := &testWorkspace{
		session: session.Session{ID: "session-1"},
	}
	sty := styles.DefaultStyles()
	ta := textarea.New()
	ta.SetStyles(sty.TextArea)
	ui := &UI{
		com: &common.Common{
			Workspace: ws,
			Styles:    &sty,
		},
		session:            &session.Session{ID: "session-1"},
		textarea:           ta,
		planModeActive:     true,
		readyPlaceholder:   "Ready",
		workingPlaceholder: "Working",
	}

	cmd := ui.handlePlanApprovalResponse(dialog.ActionPlanApprovalResponse{
		Submission: planning.Submission{
			ID:        "plan-1",
			SessionID: "session-1",
			Todos: []session.Todo{{
				Content:    "Do the thing",
				Status:     session.TodoStatusPending,
				ActiveForm: "Doing the thing",
			}},
		},
		Approved: true,
		Comment:  "Looks good.",
	})

	_ = cmd()

	busyIdx := slices.Index(ws.events, "busy-check")
	respondIdx := slices.Index(ws.events, "plan-respond")
	require.GreaterOrEqual(t, busyIdx, 0, "expected busy-check event")
	require.GreaterOrEqual(t, respondIdx, 0, "expected plan-respond event")
	require.Less(t, busyIdx, respondIdx, "plan-respond should come after busy-check")
}

func TestSaveCriticalInstructionsWritesSelectedScope(t *testing.T) {
	t.Parallel()

	ws := &testWorkspace{}
	ui := newTestUIWithConfig(t, &config.Config{Options: &config.Options{TUI: &config.TUIOptions{}}})
	ui.com.Workspace = ws

	msg := ui.saveCriticalInstructions(config.ScopeWorkspace, "Do not truncate.")()
	require.IsType(t, util.InfoMsg{}, msg)
	require.Equal(t, config.ScopeWorkspace, ws.setConfigScope)
	require.Equal(t, "options.critical_instructions", ws.setConfigKey)
	require.Equal(t, "Do not truncate.", ws.setConfigValue)
	require.Equal(t, 1, ws.updateAgentModelCalls)
}

func TestSaveCriticalInstructionsRemovesEmptyScope(t *testing.T) {
	t.Parallel()

	ws := &testWorkspace{}
	ui := newTestUIWithConfig(t, &config.Config{Options: &config.Options{TUI: &config.TUIOptions{}}})
	ui.com.Workspace = ws

	msg := ui.saveCriticalInstructions(config.ScopeGlobal, "")()
	require.IsType(t, util.InfoMsg{}, msg)
	require.Equal(t, config.ScopeGlobal, ws.removeConfigScope)
	require.Equal(t, "options.critical_instructions", ws.removeConfigKey)
	require.Equal(t, 1, ws.updateAgentModelCalls)
}

func TestSaveSnippetWritesSelectedScope(t *testing.T) {
	t.Parallel()

	ws := &testWorkspace{snippets: map[config.Scope][]config.Snippet{}}
	ui := newTestUIWithConfig(t, &config.Config{Options: &config.Options{TUI: &config.TUIOptions{}}})
	ui.com.Workspace = ws

	msg := ui.saveSnippet(config.ScopeWorkspace, -1, config.ScopeWorkspace, config.Snippet{
		Title: "Review",
		Body:  "Check correctness and missing tests.",
	})()

	saved, ok := msg.(snippetsSavedMsg)
	require.True(t, ok)
	require.Equal(t, "Snippet saved (project)", saved.Message)
	require.Equal(t, config.ScopeWorkspace, ws.setConfigScope)
	require.Equal(t, "options.snippets", ws.setConfigKey)
	snippets, ok := ws.setConfigValue.([]config.Snippet)
	require.True(t, ok)
	require.Len(t, snippets, 1)
	require.NotEmpty(t, snippets[0].ID)
	require.Equal(t, "Review", snippets[0].Title)
	require.Equal(t, "Check correctness and missing tests.", snippets[0].Body)
	require.Len(t, saved.Snippets, 1)
	require.Equal(t, config.ScopeWorkspace, saved.Snippets[0].Scope)
}

func TestLoadCriticalInstructionsOpensInlineDialog(t *testing.T) {
	t.Parallel()

	sty := styles.DefaultStyles()
	ws := &testWorkspace{
		criticalInstructions: "Global rule\n\nProject rule",
	}
	ui := &UI{
		com: &common.Common{
			Workspace: ws,
			Styles:    &sty,
		},
		dialog: dialog.NewOverlay(),
	}

	msg := ui.loadCriticalInstructions(config.ScopeWorkspace)()
	loaded, ok := msg.(criticalInstructionsLoadedMsg)
	require.True(t, ok)
	require.Equal(t, "Global rule\n\nProject rule", loaded.Text)

	ui.openCriticalInstructionsDialog(loaded.Scope, loaded.Text)
	require.True(t, ui.dialog.ContainsDialog(dialog.CriticalInstructionsID))
}

func TestSelectedForkMessageIDUsesParentAssistantForToolItem(t *testing.T) {
	t.Parallel()

	sty := styles.DefaultStyles()
	com := &common.Common{
		Workspace: &testWorkspace{},
		Styles:    &sty,
	}
	ui := &UI{
		com:  com,
		chat: NewChat(com),
	}

	ui.setSessionMessages([]message.Message{
		{
			ID:   "assistant-1",
			Role: message.Assistant,
			Parts: []message.ContentPart{
				message.ToolCall{ID: "tool-1", Name: "unknown_tool", Input: "{}"},
			},
		},
	})
	ui.chat.SetSelected(0)

	messageID, err := ui.selectedForkMessageID()
	require.NoError(t, err)
	require.Equal(t, "assistant-1", messageID)
}

func TestHandleSessionForkedPrefillsPrompt(t *testing.T) {
	t.Parallel()

	sty := styles.DefaultStyles()
	com := &common.Common{
		Workspace: &testWorkspace{},
		Styles:    &sty,
	}
	ta := textarea.New()
	ta.SetStyles(sty.TextArea)
	ta.SetWidth(80)
	ta.SetHeight(3)
	ui := &UI{
		com:      com,
		chat:     NewChat(com),
		status:   NewStatus(com, nil),
		textarea: ta,
		focus:    uiFocusMain,
		state:    uiChat,
		width:    80,
		height:   24,
	}

	cmd := ui.handleSessionForked(session.ForkResult{
		Session: session.Session{ID: "forked"},
		Prefill: "draft this message",
	})

	require.NotNil(t, cmd)
	require.Equal(t, "forked", ui.session.ID)
	require.Equal(t, "draft this message", ui.textarea.Value())
	require.Equal(t, uiFocusEditor, ui.focus)
}

func TestPlanModeChangeRequestQueuesEnteredPlanModePrompt(t *testing.T) {
	t.Parallel()

	sty := styles.DefaultStyles()
	ws := &testWorkspace{agentReady: true}
	com := &common.Common{
		Workspace: ws,
		Styles:    &sty,
	}
	ta := textarea.New()
	ta.SetStyles(sty.TextArea)
	ui := &UI{
		com:                com,
		session:            &session.Session{ID: "session-1"},
		chat:               NewChat(com),
		status:             NewStatus(com, nil),
		textarea:           ta,
		readyPlaceholder:   "Ready",
		workingPlaceholder: "Working",
		state:              uiChat,
	}

	cmd := ui.handlePlanModeChangeRequest(planning.ModeChangeRequest{
		SessionID: "session-1",
		Mode:      planning.ModeEnter,
		Prompt:    "Plan a large migration",
	})

	require.True(t, ui.planModeActive)
	require.NotNil(t, cmd)
	runBatchCommand(t, cmd)
	require.Equal(t, []string{"Plan a large migration"}, ws.agentRunPrompts)
	require.Len(t, ws.agentRunOptions, 1)
	require.True(t, ws.agentRunOptions[0].PlanMode)
}

func TestPlanModeChangeRequestQueuesExitedPlanModePrompt(t *testing.T) {
	t.Parallel()

	sty := styles.DefaultStyles()
	ws := &testWorkspace{agentReady: true}
	com := &common.Common{
		Workspace: ws,
		Styles:    &sty,
	}
	ta := textarea.New()
	ta.SetStyles(sty.TextArea)
	ui := &UI{
		com:                com,
		session:            &session.Session{ID: "session-1"},
		chat:               NewChat(com),
		status:             NewStatus(com, nil),
		textarea:           ta,
		readyPlaceholder:   "Ready",
		workingPlaceholder: "Working",
		planModeActive:     true,
		state:              uiChat,
	}

	cmd := ui.handlePlanModeChangeRequest(planning.ModeChangeRequest{
		SessionID: "session-1",
		Mode:      planning.ModeExit,
		Prompt:    "Implement the approved plan",
	})

	require.False(t, ui.planModeActive)
	require.NotNil(t, cmd)
	runBatchCommand(t, cmd)
	require.Equal(t, []string{"Implement the approved plan"}, ws.agentRunPrompts)
	require.Len(t, ws.agentRunOptions, 1)
	require.False(t, ws.agentRunOptions[0].PlanMode)
}

func runBatchCommand(t *testing.T, cmd tea.Cmd) {
	t.Helper()

	runCommand(t, cmd)
}

func runCommand(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msg := cmd()
	switch msg := msg.(type) {
	case tea.BatchMsg:
		for _, batched := range msg {
			runCommand(t, batched)
		}
	}
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
	cfg                   *config.Config
	skipRequests          bool
	planResponses         []planning.Response
	setConfigScope        config.Scope
	setConfigKey          string
	setConfigValue        any
	removeConfigScope     config.Scope
	removeConfigKey       string
	updateAgentModelCalls int
	criticalInstructions  string
	snippets              map[config.Scope][]config.Snippet
	session               session.Session
	events                []string
	agentRunOptions       []workspace.AgentRunOptions
	agentRunPrompts       []string
	agentReady            bool
}

func (w *testWorkspace) Config() *config.Config {
	return w.cfg
}

func (w *testWorkspace) PermissionSkipRequests() bool {
	return w.skipRequests
}

func (w *testWorkspace) PlanRespond(resp planning.Response) {
	w.events = append(w.events, "plan-respond")
	w.planResponses = append(w.planResponses, resp)
}

func (w *testWorkspace) GetSession(ctx context.Context, sessionID string) (session.Session, error) {
	w.events = append(w.events, "get-session")
	if w.session.ID == "" {
		w.session.ID = sessionID
	}
	return w.session, nil
}

func (w *testWorkspace) SaveSession(ctx context.Context, current session.Session) (session.Session, error) {
	w.events = append(w.events, "save-session")
	w.session = current
	return current, nil
}

func (w *testWorkspace) AgentRunWithOptions(ctx context.Context, sessionID, prompt string, options workspace.AgentRunOptions, attachments ...message.Attachment) error {
	w.events = append(w.events, "agent-run")
	w.agentRunOptions = append(w.agentRunOptions, options)
	w.agentRunPrompts = append(w.agentRunPrompts, prompt)
	return nil
}

func (w *testWorkspace) AgentIsSessionBusy(sessionID string) bool {
	w.events = append(w.events, "busy-check")
	return false
}

func (w *testWorkspace) CriticalInstructions(scope config.Scope) (string, error) {
	return w.criticalInstructions, nil
}

func (w *testWorkspace) Snippets(scope config.Scope) ([]config.Snippet, error) {
	if w.snippets == nil {
		return nil, nil
	}
	return slices.Clone(w.snippets[scope]), nil
}

func (w *testWorkspace) SetConfigField(scope config.Scope, key string, value any) error {
	w.setConfigScope = scope
	w.setConfigKey = key
	w.setConfigValue = value
	if key == "options.snippets" {
		if w.snippets == nil {
			w.snippets = make(map[config.Scope][]config.Snippet)
		}
		if snippets, ok := value.([]config.Snippet); ok {
			w.snippets[scope] = slices.Clone(snippets)
		}
	}
	return nil
}

func (w *testWorkspace) RemoveConfigField(scope config.Scope, key string) error {
	w.removeConfigScope = scope
	w.removeConfigKey = key
	if key == "options.snippets" && w.snippets != nil {
		delete(w.snippets, scope)
	}
	return nil
}

func (w *testWorkspace) UpdateAgentModel(ctx context.Context) error {
	w.updateAgentModelCalls++
	return nil
}

func (w *testWorkspace) AgentIsReady() bool {
	return w.agentReady
}

func (w *testWorkspace) AgentIsBusy() bool {
	return false
}
