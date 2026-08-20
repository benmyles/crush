package model

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/goal"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/util"
)

// goalFetchedMsg delivers an off-thread goal fetch to the Update loop.
type goalFetchedMsg struct {
	goal goal.Goal
	show bool
	err  error
}

// goalSetSuccessMsg is emitted after GoalSet succeeded and carries the
// goal text to submit as the next user prompt.
type goalSetSuccessMsg struct {
	sessionID string
	text      string
}

// dispatchGoalFetch loads the session's goal off-thread. When show is
// true the result opens the goal status dialog.
func (m *UI) dispatchGoalFetch(sessionID string, show bool) tea.Cmd {
	return func() tea.Msg {
		g, err := m.com.Workspace.GoalGet(context.Background(), sessionID)
		return goalFetchedMsg{goal: g, show: show, err: err}
	}
}

// handleGoalFetched applies an off-thread goal fetch.
func (m *UI) handleGoalFetched(msg goalFetchedMsg) tea.Cmd {
	if msg.err != nil {
		return util.ReportError(msg.err)
	}
	m.goal = msg.goal
	m.renderPills()
	if !msg.show {
		return nil
	}
	dlg, _ := dialog.NewGoalShow(m.com, msg.goal)
	m.dialog.OpenDialog(dlg)
	return nil
}

// handleGoalSetSuccess submits the goal text as the next user prompt so
// the agent starts working on the goal immediately.
func (m *UI) handleGoalSetSuccess(msg goalSetSuccessMsg) tea.Cmd {
	m.goal = goal.Goal{SessionID: msg.sessionID, Text: msg.text, Status: goal.StatusActive}
	m.renderPills()
	return tea.Batch(m.sendMessage(msg.text), m.loadPromptHistory())
}

// goalCommand sets the goal and submits a continuation that triggers
// the real prompt send once the goal is persisted, so the first turn
// always observes the goal.
func (m *UI) goalCommand(sessionID, text string) tea.Cmd {
	return func() tea.Msg {
		if err := m.com.Workspace.GoalSet(context.Background(), sessionID, text); err != nil {
			return util.ReportError(err)()
		}
		return goalSetSuccessMsg{sessionID: sessionID, text: text}
	}
}

// trySlashCommand intercepts goal and pause/resume slash commands typed
// in the chat. It returns the command to run and true when the input was
// a recognized slash command; otherwise it returns nil, false and the
// caller falls through to the normal prompt send.
func (m *UI) trySlashCommand(value string) (tea.Cmd, bool) {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "/") {
		return nil, false
	}

	switch {
	case trimmed == "/goal":
		if !m.hasSession() {
			return util.ReportWarn("No active session; send a message first"), true
		}
		dlg, _ := dialog.NewGoalInput(m.com, m.session.ID)
		m.dialog.OpenDialog(dlg)
		return nil, true

	case strings.HasPrefix(trimmed, "/goal "):
		text := strings.TrimSpace(strings.TrimPrefix(trimmed, "/goal "))
		if text == "" {
			return nil, true
		}
		if m.isAgentBusy() {
			return util.ReportWarn("Agent is busy; wait for the current turn before setting a goal"), true
		}
		if !m.hasSession() {
			return util.ReportWarn("No active session; send a message first"), true
		}
		return m.goalCommand(m.session.ID, text), true

	case trimmed == "/goal:show":
		if !m.hasSession() {
			return util.ReportWarn("No active session"), true
		}
		return m.dispatchGoalFetch(m.session.ID, true), true

	case trimmed == "/goal:resume":
		if !m.hasSession() {
			return util.ReportWarn("No active session"), true
		}
		sessionID := m.session.ID
		return func() tea.Msg {
			if err := m.com.Workspace.GoalResume(context.Background(), sessionID); err != nil {
				return util.ReportError(err)()
			}
			return util.NewInfoMsg("Goal reactivated; the agent will continue")
		}, true

	case trimmed == "/goal:clear":
		if !m.hasSession() {
			return util.ReportWarn("No active session"), true
		}
		sessionID := m.session.ID
		return func() tea.Msg {
			if err := m.com.Workspace.GoalClear(context.Background(), sessionID); err != nil {
				return util.ReportError(err)()
			}
			return goalClearedMsg{sessionID: sessionID}
		}, true

	case trimmed == "/pause":
		m.pausedActive = true
		m.renderPills()
		return func() tea.Msg {
			if !m.com.Workspace.AgentPause() {
				m.pausedActive = false
				return util.NewWarnMsg("Pause requested; the agent stops at its next step")
			}
			return util.NewInfoMsg("Agent paused; it stops at its next step")
		}, true

	case trimmed == "/resume":
		m.pausedActive = false
		m.renderPills()
		return func() tea.Msg {
			m.com.Workspace.AgentResume()
			return util.NewInfoMsg("Agent resumed")
		}, true
	}

	return nil, false
}

// goalClearedMsg marks a successful /goal:clear.
type goalClearedMsg struct {
	sessionID string
}
