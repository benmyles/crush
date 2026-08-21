package model

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/status"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/crush/internal/ui/util"
)

// statusFetchedMsg delivers an off-thread status fetch to the Update loop.
type statusFetchedMsg struct {
	update status.Update
	err    error
}

// dispatchStatusFetch loads the session's latest status update off-thread.
func (m *UI) dispatchStatusFetch(sessionID string) tea.Cmd {
	return func() tea.Msg {
		u, err := m.com.Workspace.StatusGet(context.Background(), sessionID)
		return statusFetchedMsg{update: u, err: err}
	}
}

// handleStatusFetched applies an off-thread status fetch.
func (m *UI) handleStatusFetched(msg statusFetchedMsg) tea.Cmd {
	if msg.err != nil {
		return util.ReportError(msg.err)
	}
	m.statusUpdate = msg.update
	return nil
}

// statusInfo renders the latest agent status update for the sidebar: a
// relative timestamp and the done/doing/next/blockers standup rows.
func (m *UI) statusInfo(width int, isSection bool) string {
	t := m.com.Styles

	title := t.Resource.Heading.Render("Status")
	if isSection {
		title = common.Section(t, title, width)
	}

	u := m.statusUpdate
	if !u.Exists() {
		body := t.Resource.AdditionalText.Render("None.")
		return lipgloss.NewStyle().Width(width).Render(fmt.Sprintf("%s\n\n%s", title, body))
	}

	header := fmt.Sprintf("%s · %s",
		t.Resource.Heading.Render("Agent update"),
		t.Resource.AdditionalText.Render(formatSince(u.UpdatedAt)))

	rows := []string{
		statusRow(t, "✓", t.Sidebar.StatusDone, u.Done),
		statusRow(t, "→", t.Sidebar.StatusDoing, u.Doing),
		statusRow(t, "▸", t.Sidebar.StatusNext, u.Next),
	}
	if u.Blockers != "" {
		rows = append(rows, statusRow(t, "⛔", t.Sidebar.StatusBlockers, u.Blockers))
	}

	body := lipgloss.JoinVertical(lipgloss.Left, append([]string{header}, rows...)...)
	return lipgloss.NewStyle().Width(width).Render(fmt.Sprintf("%s\n\n%s", title, body))
}

// statusRow renders one labeled standup row.
func statusRow(t *styles.Styles, icon string, valueStyle lipgloss.Style, value string) string {
	return t.Sidebar.StatusLabel.Render(icon) + " " + valueStyle.Render(value)
}

// formatSince renders a unix timestamp as a short relative age.
func formatSince(unix int64) string {
	d := time.Since(time.Unix(unix, 0))
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
