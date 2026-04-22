package dialog

import (
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/ui/common"
	uv "github.com/charmbracelet/ultraviolet"
)

const CriticalInstructionsID = "critical_instructions"

type CriticalInstructions struct {
	com      *common.Common
	scope    config.Scope
	input    textarea.Model
	focus    criticalInstructionsFocus
	selected int
	help     help.Model
	keyMap   criticalInstructionsKeyMap
}

type criticalInstructionsFocus uint8

const (
	criticalInstructionsFocusInput criticalInstructionsFocus = iota
	criticalInstructionsFocusActions
)

type criticalInstructionsKeyMap struct {
	Left    key.Binding
	Right   key.Binding
	Tab     key.Binding
	Save    key.Binding
	Cancel  key.Binding
	Select  key.Binding
	Newline key.Binding
	Close   key.Binding
}

func defaultCriticalInstructionsKeyMap() criticalInstructionsKeyMap {
	return criticalInstructionsKeyMap{
		Left: key.NewBinding(
			key.WithKeys("left", "h"),
			key.WithHelp("left", "previous"),
		),
		Right: key.NewBinding(
			key.WithKeys("right", "l"),
			key.WithHelp("right", "next"),
		),
		Tab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "actions"),
		),
		Save: key.NewBinding(
			key.WithKeys("s", "S"),
			key.WithHelp("s", "save"),
		),
		Cancel: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "cancel"),
		),
		Select: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "confirm"),
		),
		Newline: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "newline"),
		),
		Close: CloseKey,
	}
}

var _ Dialog = (*CriticalInstructions)(nil)

func NewCriticalInstructions(com *common.Common, scope config.Scope, value string) *CriticalInstructions {
	input := textarea.New()
	input.SetVirtualCursor(false)
	input.SetStyles(com.Styles.TextArea)
	input.ShowLineNumbers = false
	input.CharLimit = -1
	input.Prompt = "> "
	input.Placeholder = "One instruction per paragraph"
	input.SetHeight(10)
	input.SetValue(value)
	input.Focus()
	input.MoveToEnd()

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()

	return &CriticalInstructions{
		com:    com,
		scope:  scope,
		input:  input,
		focus:  criticalInstructionsFocusInput,
		help:   h,
		keyMap: defaultCriticalInstructionsKeyMap(),
	}
}

func (*CriticalInstructions) ID() string {
	return CriticalInstructionsID
}

func (c *CriticalInstructions) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch c.focus {
		case criticalInstructionsFocusInput:
			return c.handleInputKey(msg)
		case criticalInstructionsFocusActions:
			return c.handleActionsKey(msg)
		}
	default:
		if c.focus == criticalInstructionsFocusInput {
			return c.updateInput(msg)
		}
	}
	return nil
}

func (c *CriticalInstructions) handleInputKey(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, c.keyMap.Close):
		return ActionClose{}
	case key.Matches(msg, c.keyMap.Tab):
		c.focusActions()
	case key.Matches(msg, CopyKey):
		return ActionCmd{Cmd: common.CopyToClipboard(c.input.Value(), "Critical instructions copied to clipboard")}
	default:
		return c.updateInput(msg)
	}
	return nil
}

func (c *CriticalInstructions) handleActionsKey(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, c.keyMap.Close), key.Matches(msg, c.keyMap.Cancel):
		return ActionClose{}
	case key.Matches(msg, c.keyMap.Tab):
		c.focusInput()
	case key.Matches(msg, c.keyMap.Left):
		c.selected = (c.selected + 1) % 2
	case key.Matches(msg, c.keyMap.Right):
		c.selected = (c.selected + 1) % 2
	case key.Matches(msg, c.keyMap.Save):
		return c.save()
	case key.Matches(msg, c.keyMap.Select):
		if c.selected == 0 {
			return c.save()
		}
		return ActionClose{}
	}
	return nil
}

func (c *CriticalInstructions) updateInput(msg tea.Msg) Action {
	var cmd tea.Cmd
	c.input, cmd = c.input.Update(msg)
	if cmd != nil {
		return ActionCmd{Cmd: cmd}
	}
	return nil
}

func (c *CriticalInstructions) InsertText(text string) tea.Cmd {
	c.focusInput()
	c.input.InsertString(text)
	return nil
}

func (c *CriticalInstructions) focusInput() {
	c.focus = criticalInstructionsFocusInput
	c.input.Focus()
}

func (c *CriticalInstructions) focusActions() {
	c.focus = criticalInstructionsFocusActions
	c.input.Blur()
}

func (c *CriticalInstructions) save() Action {
	return ActionCriticalInstructionsResponse{
		Scope: c.scope,
		Text:  strings.TrimSpace(c.input.Value()),
	}
}

func (c *CriticalInstructions) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := c.com.Styles
	width := min(max(64, int(float64(area.Dx())*0.72)), 96)
	if area.Dx() < width {
		width = area.Dx()
	}
	maxHeight := min(max(16, int(float64(area.Dy())*0.68)), area.Dy())
	dialogStyle := t.Dialog.View.Width(width).Padding(0, 1)
	contentWidth := width - dialogStyle.GetHorizontalFrameSize() - 2

	titleText := c.scopeTitle()
	title := common.DialogTitle(t, titleText, contentWidth-t.Dialog.Title.GetHorizontalFrameSize(), t.Primary, t.Secondary)
	title = t.Dialog.Title.Render(title)
	scope := t.Muted.Render(c.scopeDescription())
	buttons := c.renderButtons()
	helpView := c.help.View(c)

	fixedHeight := lipgloss.Height(title) + lipgloss.Height(scope) + lipgloss.Height(buttons) + lipgloss.Height(helpView) + 6
	inputHeight := max(5, maxHeight-fixedHeight)
	c.input.SetWidth(max(10, contentWidth-2))
	c.input.SetHeight(inputHeight)
	inputView := c.input.View()

	view := lipgloss.JoinVertical(lipgloss.Left, title, "", scope, "", inputView, "", buttons, "", helpView)

	var cur *tea.Cursor
	if c.focus == criticalInstructionsFocusInput {
		cur = c.input.Cursor()
		if cur != nil {
			cur.X += dialogStyle.GetBorderLeftSize() +
				dialogStyle.GetPaddingLeft() +
				dialogStyle.GetMarginLeft()
			cur.Y += dialogStyle.GetBorderTopSize() +
				dialogStyle.GetPaddingTop() +
				dialogStyle.GetMarginTop()
			cur.Y += lipgloss.Height(title) + 1 +
				lipgloss.Height(scope) + 1
		}
	}

	DrawCenterCursor(scr, area, dialogStyle.Render(view), cur)
	return cur
}

func (c *CriticalInstructions) scopeTitle() string {
	if c.scope == config.ScopeWorkspace {
		return "Project Critical Instructions"
	}
	return "Global Critical Instructions"
}

func (c *CriticalInstructions) scopeDescription() string {
	if c.scope == config.ScopeWorkspace {
		return "Saved to project config"
	}
	return "Saved to global config"
}

func (c *CriticalInstructions) renderButtons() string {
	return common.ButtonGroup(c.com.Styles, []common.ButtonOpts{
		{Text: "Save", UnderlineIndex: 0, Selected: c.focus == criticalInstructionsFocusActions && c.selected == 0},
		{Text: "Cancel", UnderlineIndex: 0, Selected: c.focus == criticalInstructionsFocusActions && c.selected == 1},
	}, "  ")
}

func (c *CriticalInstructions) ShortHelp() []key.Binding {
	if c.focus == criticalInstructionsFocusInput {
		return []key.Binding{c.keyMap.Newline, c.keyMap.Tab, c.keyMap.Close}
	}
	return []key.Binding{c.keyMap.Left, c.keyMap.Right, c.keyMap.Select, c.keyMap.Save, c.keyMap.Cancel}
}

func (c *CriticalInstructions) FullHelp() [][]key.Binding {
	return [][]key.Binding{c.ShortHelp()}
}
