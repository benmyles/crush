package model

import (
	"fmt"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/goal"
	"github.com/charmbracelet/crush/internal/ui/common"
)

// goalInfo renders the session goal section for the sidebar: status badge,
// goal text (truncated), and terminal-state detail (summary or reason).
func (m *UI) goalInfo(width int, isSection bool) string {
	t := m.com.Styles

	title := t.Resource.Heading.Render("Goal")
	if isSection {
		title = common.Section(t, title, width)
	}

	g := m.goal
	if !g.Exists() {
		body := t.Resource.AdditionalText.Render("None.")
		return lipgloss.NewStyle().Width(width).Render(fmt.Sprintf("%s\n\n%s", title, body))
	}

	var status string
	statusStyle := t.Resource.AdditionalText
	switch g.Status {
	case goal.StatusActive:
		status = "Active"
		statusStyle = t.Resource.Heading
	case goal.StatusComplete:
		status = "Complete"
	case goal.StatusBlocked:
		status = "Blocked"
	case goal.StatusStalled:
		status = "Stalled"
	default:
		status = string(g.Status)
	}

	goalText := g.Text
	const maxGoalTextLen = 72
	if len(goalText) > maxGoalTextLen {
		goalText = goalText[:maxGoalTextLen-1] + "…"
	}

	lines := []string{
		statusStyle.Render(status) + " · " + t.Resource.AdditionalText.Render(fmt.Sprintf("%d checks", g.TotalProds)),
		t.Resource.AdditionalText.Render(goalText),
	}
	switch g.Status {
	case goal.StatusComplete:
		lines = append(lines, t.Resource.AdditionalText.Render("✓ "+g.CompleteReason))
	case goal.StatusBlocked:
		lines = append(lines, t.Resource.AdditionalText.Render("⛔ "+g.BlockedReason))
	case goal.StatusStalled:
		lines = append(lines, t.Resource.AdditionalText.Render("Stalled; /goal:resume to continue"))
	}

	body := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return lipgloss.NewStyle().Width(width).Render(fmt.Sprintf("%s\n\n%s", title, body))
}
