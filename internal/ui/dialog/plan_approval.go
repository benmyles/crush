package dialog

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/planning"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
)

const PlanApprovalID = "plan_approval"

type PlanApproval struct {
	com            *common.Common
	submission     planning.Submission
	comments       textarea.Model
	focus          planApprovalFocus
	selected       int
	compactHistory bool
	compactLabel   string
	viewport       viewport.Model
	help           help.Model
	keyMap         planApprovalKeyMap
}

type planApprovalFocus uint8

const (
	planApprovalFocusComments planApprovalFocus = iota
	planApprovalFocusCompact
	planApprovalFocusActions
)

type planApprovalKeyMap struct {
	Left            key.Binding
	Right           key.Binding
	Tab             key.Binding
	Comments        key.Binding
	Compact         key.Binding
	Toggle          key.Binding
	Newline         key.Binding
	InputScrollUp   key.Binding
	InputScrollDown key.Binding
	Select          key.Binding
	Approve         key.Binding
	Revise          key.Binding
	Close           key.Binding
	ScrollUp        key.Binding
	ScrollDown      key.Binding
	Scroll          key.Binding
}

func defaultPlanApprovalKeyMap() planApprovalKeyMap {
	return planApprovalKeyMap{
		Left: key.NewBinding(
			key.WithKeys("left", "h"),
			key.WithHelp("←", "previous"),
		),
		Right: key.NewBinding(
			key.WithKeys("right", "l"),
			key.WithHelp("→", "next"),
		),
		Tab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "actions"),
		),
		Comments: key.NewBinding(
			key.WithKeys("up"),
			key.WithHelp("↑", "comments"),
		),
		Compact: key.NewBinding(
			key.WithKeys("down"),
			key.WithHelp("↓", "compact"),
		),
		Toggle: key.NewBinding(
			key.WithKeys("space", "enter"),
			key.WithHelp("space", "toggle"),
		),
		Newline: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "newline"),
		),
		InputScrollUp: key.NewBinding(
			key.WithKeys("shift+up"),
			key.WithHelp("shift+↑", "scroll up"),
		),
		InputScrollDown: key.NewBinding(
			key.WithKeys("shift+down"),
			key.WithHelp("shift+↓", "scroll down"),
		),
		Select: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "confirm"),
		),
		Approve: key.NewBinding(
			key.WithKeys("y", "Y"),
			key.WithHelp("y", "approve"),
		),
		Revise: key.NewBinding(
			key.WithKeys("n", "N"),
			key.WithHelp("n", "revise"),
		),
		Close: CloseKey,
		ScrollUp: key.NewBinding(
			key.WithKeys("shift+up", "K"),
			key.WithHelp("shift+↑", "scroll up"),
		),
		ScrollDown: key.NewBinding(
			key.WithKeys("shift+down", "J"),
			key.WithHelp("shift+↓", "scroll down"),
		),
		Scroll: key.NewBinding(
			key.WithKeys("shift+up", "shift+down"),
			key.WithHelp("shift+↑/↓", "scroll"),
		),
	}
}

var _ Dialog = (*PlanApproval)(nil)

func NewPlanApproval(com *common.Common, submission planning.Submission) *PlanApproval {
	km := defaultPlanApprovalKeyMap()
	comments := textarea.New()
	comments.SetVirtualCursor(false)
	comments.SetStyles(com.Styles.TextArea)
	comments.ShowLineNumbers = false
	comments.CharLimit = -1
	comments.Prompt = "> "
	comments.Placeholder = "Optional comments"
	comments.SetHeight(4)
	comments.Focus()

	vp := viewport.New()
	vp.KeyMap = viewport.KeyMap{
		Up:           km.ScrollUp,
		Down:         km.ScrollDown,
		PageUp:       key.NewBinding(key.WithDisabled()),
		PageDown:     key.NewBinding(key.WithDisabled()),
		HalfPageUp:   key.NewBinding(key.WithDisabled()),
		HalfPageDown: key.NewBinding(key.WithDisabled()),
	}

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()

	compactHistory := true
	compactLabel := "Compact before starting"
	if com != nil && com.Workspace != nil {
		if cfg := com.Config(); cfg != nil && cfg.Options != nil {
			strategy := cfg.Options.EffectivePlanCompactStrategy()
			switch strategy {
			case config.PlanCompactStrategyDisabled:
				compactHistory = false
			case config.PlanCompactStrategyMorph:
				compactLabel = "Compact before starting (Morph)"
			case config.PlanCompactStrategySummarize:
				compactLabel = "Compact before starting (Summarize)"
			}
		}
	}

	return &PlanApproval{
		com:            com,
		submission:     submission,
		comments:       comments,
		focus:          planApprovalFocusComments,
		compactHistory: compactHistory,
		compactLabel:   compactLabel,
		viewport:       vp,
		help:           h,
		keyMap:         km,
	}
}

func (*PlanApproval) ID() string {
	return PlanApprovalID
}

func (p *PlanApproval) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch p.focus {
		case planApprovalFocusComments:
			return p.handleCommentsKey(msg)
		case planApprovalFocusCompact:
			return p.handleCompactKey(msg)
		case planApprovalFocusActions:
			return p.handleActionsKey(msg)
		}
	case tea.MouseWheelMsg:
		p.viewport, _ = p.viewport.Update(msg)
	}
	return nil
}

func (p *PlanApproval) handleCommentsKey(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, p.keyMap.Close):
		return p.respond(false)
	case key.Matches(msg, p.keyMap.Tab):
		p.focusCompact()
	case key.Matches(msg, p.keyMap.InputScrollUp):
		p.viewport, _ = p.viewport.Update(msg)
	case key.Matches(msg, p.keyMap.InputScrollDown):
		p.viewport, _ = p.viewport.Update(msg)
	default:
		var cmd tea.Cmd
		p.comments, cmd = p.comments.Update(msg)
		if cmd != nil {
			return ActionCmd{Cmd: cmd}
		}
	}
	return nil
}

func (p *PlanApproval) handleCompactKey(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, p.keyMap.Close):
		return p.respond(false)
	case key.Matches(msg, p.keyMap.Toggle):
		p.compactHistory = !p.compactHistory
	case key.Matches(msg, p.keyMap.Tab):
		p.focusActions()
	case key.Matches(msg, p.keyMap.Comments):
		p.focusComments()
	case key.Matches(msg, p.keyMap.ScrollUp), key.Matches(msg, p.keyMap.ScrollDown):
		p.viewport, _ = p.viewport.Update(msg)
	}
	return nil
}

func (p *PlanApproval) handleActionsKey(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, p.keyMap.Close):
		return p.respond(false)
	case key.Matches(msg, p.keyMap.Tab), key.Matches(msg, p.keyMap.Compact):
		p.focusCompact()
	case key.Matches(msg, p.keyMap.Comments):
		p.focusComments()
	case key.Matches(msg, p.keyMap.Left):
		p.selected = (p.selected + 1) % 2
	case key.Matches(msg, p.keyMap.Right):
		p.selected = (p.selected + 1) % 2
	case key.Matches(msg, p.keyMap.Approve):
		return p.respond(true)
	case key.Matches(msg, p.keyMap.Revise):
		return p.respond(false)
	case key.Matches(msg, p.keyMap.Select):
		return p.respond(p.selected == 0)
	case key.Matches(msg, p.keyMap.ScrollUp), key.Matches(msg, p.keyMap.ScrollDown):
		p.viewport, _ = p.viewport.Update(msg)
	}
	return nil
}

func (p *PlanApproval) focusComments() {
	p.focus = planApprovalFocusComments
	p.comments.Focus()
}

func (p *PlanApproval) focusCompact() {
	p.focus = planApprovalFocusCompact
	p.comments.Blur()
}

func (p *PlanApproval) focusActions() {
	p.focus = planApprovalFocusActions
	p.comments.Blur()
}

func (p *PlanApproval) respond(approved bool) Action {
	return ActionPlanApprovalResponse{
		Submission:     p.submission,
		Approved:       approved,
		Comment:        strings.TrimSpace(p.comments.Value()),
		CompactHistory: p.compactHistory && approved,
	}
}

func (p *PlanApproval) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := p.com.Styles
	width := min(max(60, int(float64(area.Dx())*0.76)), 120)
	if area.Dx() < width {
		width = area.Dx()
	}
	maxHeight := min(max(18, int(float64(area.Dy())*0.78)), area.Dy())
	dialogStyle := t.Dialog.View.Width(width).Padding(0, 1)
	contentWidth := width - dialogStyle.GetHorizontalFrameSize() - 2

	title := common.DialogTitle(t, "Approve Plan", contentWidth-t.Dialog.Title.GetHorizontalFrameSize(), t.Primary, t.Secondary)
	title = t.Dialog.Title.Render(title)

	body := p.renderBody(contentWidth)
	buttons := p.renderButtons()
	comment := p.renderComments(contentWidth)
	compact := p.renderCompact(contentWidth)
	helpView := p.help.View(p)

	fixedHeight := lipgloss.Height(title) + lipgloss.Height(buttons) + lipgloss.Height(comment) + lipgloss.Height(compact) + lipgloss.Height(helpView) + 8
	availableHeight := max(5, maxHeight-fixedHeight)
	p.viewport.SetWidth(contentWidth)
	p.viewport.SetHeight(availableHeight)
	p.viewport.SetContent(body)

	content := p.viewport.View()
	if p.viewport.TotalLineCount() > availableHeight {
		scrollbar := common.Scrollbar(t, availableHeight, p.viewport.TotalLineCount(), availableHeight, p.viewport.YOffset())
		content = lipgloss.JoinHorizontal(lipgloss.Top, content, scrollbar)
	}

	view := lipgloss.JoinVertical(lipgloss.Left, title, "", content, "", comment, "", compact, "", buttons, "", helpView)

	var cur *tea.Cursor
	if p.focus == planApprovalFocusComments {
		cur = p.comments.Cursor()
		if cur != nil {
			// Offset for dialog frame.
			cur.X += dialogStyle.GetBorderLeftSize() +
				dialogStyle.GetPaddingLeft() +
				dialogStyle.GetMarginLeft()
			cur.Y += dialogStyle.GetBorderTopSize() +
				dialogStyle.GetPaddingTop() +
				dialogStyle.GetMarginTop()
			// Offset for everything above the comments textarea in the view.
			cur.Y += lipgloss.Height(title) + 1 + // title + blank line
				availableHeight + 1 + // viewport + blank line
				1 // "Comments" label line
		}
	}

	DrawCenterCursor(scr, area, dialogStyle.Render(view), cur)
	return cur
}

func (p *PlanApproval) renderBody(width int) string {
	renderer := common.MarkdownRenderer(p.com.Styles, width)
	markdown, err := renderer.Render(p.submission.Markdown)
	if err != nil {
		markdown = p.submission.Markdown
	}

	var parts []string
	parts = append(parts, strings.TrimSpace(markdown))
	if todos := chat.FormatTodosList(p.com.Styles, p.submission.Todos, styles.ArrowRightIcon, width); todos != "" {
		parts = append(parts, p.com.Styles.Muted.Render("Tasks"), todos)
	}
	return strings.Join(parts, "\n\n")
}

func (p *PlanApproval) renderComments(width int) string {
	t := p.com.Styles
	p.comments.SetWidth(max(10, width-2))
	label := t.Muted.Render("Comments")
	if p.focus == planApprovalFocusComments {
		label = t.Base.Render("Comments")
	}
	return lipgloss.JoinVertical(lipgloss.Left, label, p.comments.View())
}

func (p *PlanApproval) renderCompact(_ int) string {
	t := p.com.Styles
	checkbox := "○"
	if p.compactHistory {
		checkbox = "●"
	}
	label := fmt.Sprintf("%s %s", checkbox, p.compactLabel)
	if p.focus == planApprovalFocusCompact {
		label = t.Base.Render(label)
	} else {
		label = t.Muted.Render(label)
	}
	return label
}

func (p *PlanApproval) renderButtons() string {
	t := p.com.Styles
	return common.ButtonGroup(t, []common.ButtonOpts{
		{Text: "Approve", UnderlineIndex: 0, Selected: p.focus == planApprovalFocusActions && p.selected == 0},
		{Text: "Revise", UnderlineIndex: 0, Selected: p.focus == planApprovalFocusActions && p.selected == 1},
	}, "  ")
}

func (p *PlanApproval) ShortHelp() []key.Binding {
	var bindings []key.Binding
	switch p.focus {
	case planApprovalFocusComments:
		bindings = []key.Binding{
			p.keyMap.Newline,
			p.keyMap.Tab,
			p.keyMap.Close,
		}
	case planApprovalFocusCompact:
		bindings = []key.Binding{
			p.keyMap.Toggle,
			p.keyMap.Tab,
			p.keyMap.Comments,
			p.keyMap.Close,
		}
	case planApprovalFocusActions:
		bindings = []key.Binding{
			p.keyMap.Left,
			p.keyMap.Right,
			p.keyMap.Select,
			p.keyMap.Approve,
			p.keyMap.Revise,
			p.keyMap.Compact,
			p.keyMap.Close,
		}
	}
	if !p.viewport.AtTop() || !p.viewport.AtBottom() {
		if p.focus == planApprovalFocusComments {
			bindings = append(bindings, key.NewBinding(
				key.WithKeys("shift+up", "shift+down"),
				key.WithHelp("shift+↑/↓", "scroll"),
			))
		} else {
			bindings = append(bindings, p.keyMap.Scroll)
		}
	}
	return bindings
}

func (p *PlanApproval) FullHelp() [][]key.Binding {
	return [][]key.Binding{p.ShortHelp()}
}
