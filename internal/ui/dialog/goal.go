package dialog

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/goal"
	"github.com/charmbracelet/crush/internal/ui/common"
	uv "github.com/charmbracelet/ultraviolet"
)

const (
	// GoalInputID is the identifier for the goal input dialog.
	GoalInputID = "goal_input"
	// GoalShowID is the identifier for the goal status dialog.
	GoalShowID          = "goal_show"
	goalDialogMaxWidth  = 70
	goalDialogMaxHeight = 14
)

// GoalInput is a small prompt dialog for entering (or replacing) the
// session goal. Submitting sends ActionSetGoal with the entered text.
type GoalInput struct {
	com *common.Common

	sessionID string
	width     int

	keyMap struct {
		Submit key.Binding
		Close  key.Binding
	}
	input textinput.Model
	help  help.Model
}

var _ Dialog = (*GoalInput)(nil)

// NewGoalInput creates a new goal input dialog.
func NewGoalInput(com *common.Common, sessionID string) (*GoalInput, tea.Cmd) {
	m := &GoalInput{com: com, sessionID: sessionID}

	m.input = textinput.New()
	m.input.SetVirtualCursor(false)
	m.input.Prompt = "> "
	m.input.Placeholder = "Keep working until all tests pass"
	m.input.SetStyles(com.Styles.TextInput)
	m.input.Focus()

	m.help = help.New()
	m.help.Styles = com.Styles.DialogHelpStyles()

	m.keyMap.Submit = key.NewBinding(
		key.WithKeys("enter", "ctrl+y"),
		key.WithHelp("enter", "set goal"),
	)
	m.keyMap.Close = CloseKey

	return m, nil
}

// ID implements Dialog.
func (m *GoalInput) ID() string {
	return GoalInputID
}

// HandleMsg implements [Dialog].
func (m *GoalInput) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, m.keyMap.Close):
			return ActionClose{}
		case key.Matches(msg, m.keyMap.Submit):
			text := strings.TrimSpace(m.input.Value())
			if text == "" {
				return nil
			}
			return ActionSetGoal{SessionID: m.sessionID, Text: text}
		default:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			if cmd != nil {
				return ActionCmd{cmd}
			}
		}
	case tea.PasteMsg:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		if cmd != nil {
			return ActionCmd{cmd}
		}
	}
	return nil
}

// Draw implements [Dialog].
func (m *GoalInput) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := m.com.Styles

	m.width = max(0, min(goalDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	innerWidth := m.width - t.Dialog.View.GetHorizontalFrameSize() - 2
	m.input.SetWidth(max(0, innerWidth-t.Dialog.InputPrompt.GetHorizontalFrameSize()-1)) // (1) cursor padding

	dialogStyle := t.Dialog.View.Width(m.width)
	textStyle := t.Dialog.SecondaryText
	inputStyle := t.Dialog.InputPrompt
	helpView := renderDialogHelp(t, &m.help, m, m.width-dialogStyle.GetHorizontalFrameSize())

	headerOffset := t.Dialog.Title.GetHorizontalFrameSize() + dialogStyle.GetHorizontalFrameSize()
	content := strings.Join([]string{
		common.DialogTitle(t, t.Dialog.Title.Render("Set Goal"), m.width-headerOffset, m.com.Styles.Dialog.TitleGradFromColor, m.com.Styles.Dialog.TitleGradToColor),
		inputStyle.Render(m.input.View()),
		textStyle.Render("The agent works toward the goal autonomously and is"),
		textStyle.Render("prompted to verify completion after every turn."),
		"",
		helpView,
	}, "\n")

	cur := m.Cursor()
	view := dialogStyle.Render(content)
	DrawCenterCursor(scr, area, view, cur)
	return cur
}

// Cursor returns the cursor position relative to the dialog.
func (m *GoalInput) Cursor() *tea.Cursor {
	return InputCursor(m.com.Styles, m.input.Cursor())
}

// ShortHelp implements [help.KeyMap].
func (m *GoalInput) ShortHelp() []key.Binding {
	return []key.Binding{m.keyMap.Submit, m.keyMap.Close}
}

// FullHelp implements [help.KeyMap].
func (m *GoalInput) FullHelp() [][]key.Binding {
	return [][]key.Binding{{m.keyMap.Submit, m.keyMap.Close}}
}

// GoalShow displays the current goal state: text, status, attempt
// counts, and the completion summary or block reason.
type GoalShow struct {
	com  *common.Common
	goal goal.Goal
	help help.Model

	keyMap struct {
		Close key.Binding
	}
}

var _ Dialog = (*GoalShow)(nil)

// NewGoalShow creates a new goal status dialog. The caller fetches the
// goal; a StatusNone goal renders as "no goal set".
func NewGoalShow(com *common.Common, g goal.Goal) (*GoalShow, tea.Cmd) {
	m := &GoalShow{com: com, goal: g}
	m.help = help.New()
	m.help.Styles = com.Styles.DialogHelpStyles()
	m.keyMap.Close = CloseKey
	return m, nil
}

// ID implements Dialog.
func (m *GoalShow) ID() string {
	return GoalShowID
}

// HandleMsg implements [Dialog].
func (m *GoalShow) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if key.Matches(msg, m.keyMap.Close) {
			return ActionClose{}
		}
	}
	return nil
}

// Draw implements [Dialog].
func (m *GoalShow) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := m.com.Styles

	width := max(0, min(goalDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	dialogStyle := t.Dialog.View.Width(width)
	textStyle := t.Dialog.PrimaryText
	dimStyle := t.Dialog.SecondaryText

	var statusLine string
	switch m.goal.Status {
	case goal.StatusActive:
		statusLine = t.Dialog.Title.Render("Active") + dimStyle.Render(" · "+fmt.Sprintf("%d checks", m.goal.TotalProds))
	case goal.StatusComplete:
		statusLine = t.Dialog.Title.Render("Complete")
	case goal.StatusBlocked:
		statusLine = t.Dialog.Title.Render("Blocked")
	case goal.StatusStalled:
		statusLine = t.Dialog.Title.Render("Stalled")
	default:
		statusLine = t.Dialog.Title.Render("None")
	}

	lines := []string{
		statusLine,
		"",
	}
	if m.goal.Status != goal.StatusNone {
		lines = append(lines, textStyle.Render(m.goal.Text))
	} else {
		lines = append(lines, textStyle.Render("Set one with /goal: describe the objective, then the agent keeps working until it is done."))
	}

	switch m.goal.Status {
	case goal.StatusComplete:
		lines = append(lines, "", dimStyle.Render("Completed: "+m.goal.CompleteReason))
	case goal.StatusBlocked:
		lines = append(lines, "", dimStyle.Render("Blocked: "+m.goal.BlockedReason))
	case goal.StatusStalled:
		lines = append(lines, "", dimStyle.Render("Checks paused after repeated turns without progress."), dimStyle.Render("Use /goal:resume to continue."))
	}
	if m.goal.Status != goal.StatusNone && m.goal.CreatedAt > 0 {
		lines = append(lines, "", dimStyle.Render("Set at "+time.Unix(m.goal.CreatedAt, 0).Format("2006-01-02 15:04")))
	}

	helpView := renderDialogHelp(t, &m.help, m, width-dialogStyle.GetHorizontalFrameSize())
	lines = append(lines, "", helpView)

	view := dialogStyle.Render(strings.Join(lines, "\n"))
	DrawCenterCursor(scr, area, view, nil)
	return nil
}

// ShortHelp implements [help.KeyMap].
func (m *GoalShow) ShortHelp() []key.Binding {
	return []key.Binding{m.keyMap.Close}
}

// FullHelp implements [help.KeyMap].
func (m *GoalShow) FullHelp() [][]key.Binding {
	return [][]key.Binding{{m.keyMap.Close}}
}
