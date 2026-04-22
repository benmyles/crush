package dialog

import (
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/userquestion"
	uv "github.com/charmbracelet/ultraviolet"
)

const UserQuestionID = "user_question"

type UserQuestion struct {
	com      *common.Common
	request  userquestion.Request
	input    textinput.Model
	selected int
	help     help.Model
	keyMap   userQuestionKeyMap
}

type userQuestionKeyMap struct {
	Next     key.Binding
	Previous key.Binding
	Select   key.Binding
	Close    key.Binding
}

func defaultUserQuestionKeyMap() userQuestionKeyMap {
	return userQuestionKeyMap{
		Next: key.NewBinding(
			key.WithKeys("down", "tab"),
			key.WithHelp("↓/tab", "next"),
		),
		Previous: key.NewBinding(
			key.WithKeys("up", "shift+tab"),
			key.WithHelp("↑", "previous"),
		),
		Select: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "answer"),
		),
		Close: CloseKey,
	}
}

var _ Dialog = (*UserQuestion)(nil)

func NewUserQuestion(com *common.Common, request userquestion.Request) *UserQuestion {
	input := textinput.New()
	input.SetVirtualCursor(false)
	input.SetStyles(com.Styles.TextInput)
	input.Prompt = "> "
	input.Placeholder = "Optional comment or alternate answer"
	input.Focus()

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()

	return &UserQuestion{
		com:     com,
		request: request,
		input:   input,
		help:    h,
		keyMap:  defaultUserQuestionKeyMap(),
	}
}

func (*UserQuestion) ID() string {
	return UserQuestionID
}

func (q *UserQuestion) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, q.keyMap.Close):
			return q.dismiss()
		case key.Matches(msg, q.keyMap.Next):
			q.selected = (q.selected + 1) % q.choiceCount()
		case key.Matches(msg, q.keyMap.Previous):
			q.selected = (q.selected + q.choiceCount() - 1) % q.choiceCount()
		case key.Matches(msg, q.keyMap.Select):
			return q.respond()
		case key.Matches(msg, CopyKey):
			return ActionCmd{Cmd: common.CopyToClipboard(q.input.Value(), "Question comment copied to clipboard")}
		default:
			return q.updateInput(msg)
		}
	default:
		return q.updateInput(msg)
	}
	return nil
}

func (q *UserQuestion) updateInput(msg tea.Msg) Action {
	var cmd tea.Cmd
	q.input, cmd = q.input.Update(msg)
	if cmd != nil {
		return ActionCmd{Cmd: cmd}
	}
	return nil
}

func (q *UserQuestion) InsertText(text string) tea.Cmd {
	q.input.Focus()
	insertIntoTextInput(&q.input, text)
	return nil
}

func (q *UserQuestion) choiceCount() int {
	return len(q.request.Choices) + 1
}

func (q *UserQuestion) dismiss() Action {
	return ActionUserQuestionResponse{
		Response: userquestion.Response{
			RequestID: q.request.ID,
			Dismissed: true,
		},
	}
}

func (q *UserQuestion) respond() Action {
	resp := userquestion.Response{
		RequestID: q.request.ID,
		Comment:   strings.TrimSpace(q.input.Value()),
	}
	if q.selected < len(q.request.Choices) {
		choice := q.request.Choices[q.selected]
		resp.ChoiceID = choice.ID
		resp.ChoiceLabel = choice.Label
	}
	return ActionUserQuestionResponse{Response: resp}
}

func (q *UserQuestion) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := q.com.Styles
	width := min(max(52, int(float64(area.Dx())*0.55)), 88)
	if area.Dx() < width {
		width = area.Dx()
	}
	dialogStyle := t.Dialog.View.Width(width).Padding(0, 1)
	contentWidth := width - dialogStyle.GetHorizontalFrameSize() - 2
	q.input.SetWidth(max(10, contentWidth-2))

	title := common.DialogTitle(t, "Question", contentWidth-t.Dialog.Title.GetHorizontalFrameSize(), t.Primary, t.Secondary)
	title = t.Dialog.Title.Render(title)

	var parts []string
	parts = append(parts, title)
	parts = append(parts, "")
	parts = append(parts, t.Base.Width(contentWidth).Render(q.request.Question))
	if q.request.Description != "" {
		parts = append(parts, t.Muted.Width(contentWidth).Render(q.request.Description))
	}
	parts = append(parts, "")
	parts = append(parts, q.renderChoices(contentWidth))
	parts = append(parts, "")
	parts = append(parts, t.Muted.Render("Comment"))
	inputIndex := len(parts)
	parts = append(parts, q.input.View())
	parts = append(parts, "")
	parts = append(parts, q.help.View(q))

	cur := q.inputCursor(dialogStyle, parts[:inputIndex])
	DrawCenterCursor(scr, area, dialogStyle.Render(lipgloss.JoinVertical(lipgloss.Left, parts...)), cur)
	return cur
}

func (q *UserQuestion) inputCursor(dialogStyle lipgloss.Style, partsBeforeInput []string) *tea.Cursor {
	cur := q.input.Cursor()
	if cur == nil {
		return nil
	}
	cur.X += dialogStyle.GetBorderLeftSize() +
		dialogStyle.GetMarginLeft() +
		dialogStyle.GetPaddingLeft()
	cur.Y += dialogStyle.GetBorderTopSize() +
		dialogStyle.GetMarginTop() +
		dialogStyle.GetPaddingTop()
	for _, part := range partsBeforeInput {
		cur.Y += max(1, lipgloss.Height(part))
	}
	return cur
}

func (q *UserQuestion) renderChoices(width int) string {
	t := q.com.Styles
	var lines []string
	for i, choice := range q.request.Choices {
		prefix := "  "
		style := t.Base
		if q.selected == i {
			prefix = "> "
			style = t.Base.Bold(true)
		}
		line := prefix + choice.Label
		if choice.Description != "" {
			line += "\n" + t.Muted.Width(max(1, width-2)).Render("  "+choice.Description)
		}
		lines = append(lines, style.Width(width).Render(line))
	}

	otherIndex := len(q.request.Choices)
	prefix := "  "
	style := t.Base
	if q.selected == otherIndex {
		prefix = "> "
		style = t.Base.Bold(true)
	}
	lines = append(lines, style.Width(width).Render(prefix+"Other / comment only"))
	return strings.Join(lines, "\n")
}

func (q *UserQuestion) ShortHelp() []key.Binding {
	return []key.Binding{
		q.keyMap.Next,
		q.keyMap.Previous,
		q.keyMap.Select,
		q.keyMap.Close,
	}
}

func (q *UserQuestion) FullHelp() [][]key.Binding {
	return [][]key.Binding{q.ShortHelp()}
}
