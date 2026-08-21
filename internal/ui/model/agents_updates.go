package model

import (
	"encoding/json"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/charmbracelet/crush/internal/ui/util"
)

// resetAgents clears the dock. Session switches and landing calls it so
// the panel never shows sub-agents from a session the user is not
// viewing.
func (m *UI) resetAgents() {
	m.agents = NewAgentsPanel(m.com.Styles)
	m.agentsTickActive = false
	if m.focus == uiFocusAgents {
		m.focus = uiFocusMain
		m.chat.Focus()
	}
}

// agentsTickCmd issues the per-second dock refresh.
func (m *UI) agentsTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return agentsTickMsg{} })
}

// agentsEnsureTicking arms the per-second refresh when the dock is
// visible and no ticker is already running.
func (m *UI) agentsEnsureTicking() tea.Cmd {
	if m.agents == nil || !m.agents.Visible() {
		return nil
	}
	if m.agentsTickActive {
		return nil
	}
	m.agentsTickActive = true
	return m.agentsTickCmd()
}

// handleAgentsTick prunes lingered rows, falls focus back when the dock
// empties, and re-arms the ticker while anything remains visible.
func (m *UI) handleAgentsTick() tea.Cmd {
	if m.agents == nil {
		m.agentsTickActive = false
		return nil
	}
	if m.agents.Prune(time.Now()) && !m.agents.Visible() {
		if m.focus == uiFocusAgents {
			m.focus = uiFocusMain
			m.chat.Focus()
		}
		m.updateLayoutAndSize()
	}
	if !m.agents.Visible() {
		m.agentsTickActive = false
		return nil
	}
	return m.agentsTickCmd()
}

// sendSubAgentMessage delivers a dock-composed message to the selected
// sub-agent. Called via the subAgentSendMsg handler so the workspace
// round-trip never blocks the Update loop.
func (m *UI) handleSubAgentSend(msg subAgentSendMsg) tea.Cmd {
	if err := m.com.Workspace.SubAgentMessage(msg.sessionID, msg.text); err != nil {
		return util.ReportWarn("Failed to message sub-agent: " + err.Error())
	}
	return nil
}

// registerAgentCall adds a running agent/agentic_fetch tool call to the
// dock. It is a no-op for any other tool. The prompt is parsed from the
// streamed tool input; entries may start out without one and get
// enriched by the subagent_started notification.
func (m *UI) registerAgentCall(parentMessageID string, tc message.ToolCall) {
	var kind, prompt string
	switch tc.Name {
	case agent.AgentToolName:
		var params agent.AgentParams
		_ = json.Unmarshal([]byte(tc.Input), &params)
		kind, prompt = tc.Name, params.Prompt
	case tools.AgenticFetchToolName:
		var params tools.AgenticFetchParams
		_ = json.Unmarshal([]byte(tc.Input), &params)
		kind, prompt = tc.Name, params.Prompt
	default:
		return
	}
	if m.agents == nil {
		return
	}
	m.agents.Register(tc.ID, m.com.Workspace.CreateAgentToolSessionID(parentMessageID, tc.ID), kind, prompt)
}

// registerAgentItems registers every agent/agentic_fetch tool item in
// items under parentMessageID.
func (m *UI) registerAgentItems(parentMessageID string, items []chat.MessageItem) {
	for _, item := range items {
		toolItem, ok := item.(chat.ToolMessageItem)
		if !ok {
			continue
		}
		m.registerAgentCall(parentMessageID, toolItem.ToolCall())
	}
}

// noteAgentActivity feeds live nested-tool progress and the doing text
// into the dock for the parent agent call identified by toolCallID.
func (m *UI) noteAgentActivity(toolCallID string, nestedTools []chat.ToolMessageItem, doing string) tea.Cmd {
	if m.agents == nil || !m.agents.Visible() {
		// The dock can only be fed entries the model registered; if it
		// is not visible there is nothing to update and no ticker to arm.
		return nil
	}
	var toolName string
	if len(nestedTools) > 0 {
		toolName = nestedTools[len(nestedTools)-1].ToolCall().Name
	}
	runes := 0
	for _, nt := range nestedTools {
		runes += utf8.RuneCountInString(nt.ToolCall().Input)
	}
	m.agents.SetActivity(toolCallID, toolName, runes)
	m.agents.SetDoing(toolCallID, doing)
	return m.agentsEnsureTicking()
}

// noteAgentSessionUsage feeds a child agent-tool session's latest token
// usage into the dock, keeping the per-agent token label live as each
// generation completes.
func (m *UI) noteAgentSessionUsage(sesh *session.Session) tea.Cmd {
	if m.agents == nil || !m.agents.Visible() || sesh == nil {
		return nil
	}
	_, toolCallID, ok := m.com.Workspace.ParseAgentToolSessionID(sesh.ID)
	if !ok {
		return nil
	}
	m.agents.SetUsage(toolCallID, sesh.PromptTokens, sesh.CompletionTokens)
	return m.agentsEnsureTicking()
}

// noteAgentFinished retires the dock row for the parent agent call.
func (m *UI) noteAgentFinished(toolCallID string) tea.Cmd {
	if m.agents == nil || !m.agents.Visible() {
		return nil
	}
	m.agents.MarkDone(toolCallID)
	return m.agentsEnsureTicking()
}
