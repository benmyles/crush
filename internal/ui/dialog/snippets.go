package dialog

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/list"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/sahilm/fuzzy"
)

const (
	SnippetsID      = "snippets"
	SnippetEditorID = "snippet_editor"
)

type ScopedSnippet struct {
	Scope   config.Scope
	Index   int
	Snippet config.Snippet
}

type snippetsKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Select key.Binding
	New    key.Binding
	Edit   key.Binding
	Delete key.Binding
	Close  key.Binding
}

func defaultSnippetsKeyMap() snippetsKeyMap {
	return snippetsKeyMap{
		Up: key.NewBinding(
			key.WithKeys("up"),
			key.WithHelp("↑", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down"),
			key.WithHelp("↓", "down"),
		),
		Select: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "insert"),
		),
		New: key.NewBinding(
			key.WithKeys("ctrl+n"),
			key.WithHelp("ctrl+n", "new"),
		),
		Edit: key.NewBinding(
			key.WithKeys("ctrl+e"),
			key.WithHelp("ctrl+e", "edit"),
		),
		Delete: key.NewBinding(
			key.WithKeys("ctrl+d"),
			key.WithHelp("ctrl+d", "delete"),
		),
		Close: CloseKey,
	}
}

type Snippets struct {
	com      *common.Common
	input    textinput.Model
	list     *list.FilterableList
	snippets []ScopedSnippet
	help     help.Model
	keyMap   snippetsKeyMap
}

var _ Dialog = (*Snippets)(nil)

func NewSnippets(com *common.Common, snippets []ScopedSnippet) *Snippets {
	input := textinput.New()
	input.SetVirtualCursor(false)
	input.SetStyles(com.Styles.TextInput)
	input.Prompt = "> "
	input.Placeholder = "Search snippets"
	input.Focus()

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()

	s := &Snippets{
		com:    com,
		input:  input,
		list:   list.NewFilterableList(),
		help:   h,
		keyMap: defaultSnippetsKeyMap(),
	}
	s.SetSnippets(snippets)
	return s
}

func (*Snippets) ID() string {
	return SnippetsID
}

func (s *Snippets) SetSnippets(snippets []ScopedSnippet) {
	s.snippets = snippets
	items := make([]list.FilterableItem, 0, len(snippets))
	for _, snippet := range snippets {
		items = append(items, &SnippetItem{snippet: snippet, t: s.com.Styles})
	}
	s.list.SetItems(items...)
	s.list.SetFilter(s.input.Value())
	s.list.SetSelected(0)
}

func (s *Snippets) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, s.keyMap.Close):
			return ActionClose{}
		case key.Matches(msg, s.keyMap.Up):
			if s.list.IsSelectedFirst() {
				s.list.SelectLast()
			} else {
				s.list.SelectPrev()
			}
			s.list.ScrollToSelected()
		case key.Matches(msg, s.keyMap.Down):
			if s.list.IsSelectedLast() {
				s.list.SelectFirst()
			} else {
				s.list.SelectNext()
			}
			s.list.ScrollToSelected()
		case key.Matches(msg, s.keyMap.Select):
			if snippet, ok := s.selectedSnippet(); ok {
				return ActionSnippetSelected{Snippet: snippet.Snippet}
			}
		case key.Matches(msg, s.keyMap.New):
			return ActionNewSnippet{Scope: s.defaultScope()}
		case key.Matches(msg, s.keyMap.Edit):
			if snippet, ok := s.selectedSnippet(); ok {
				return ActionEditSnippet{
					Scope:   snippet.Scope,
					Index:   snippet.Index,
					Snippet: snippet.Snippet,
				}
			}
		case key.Matches(msg, s.keyMap.Delete):
			if snippet, ok := s.selectedSnippet(); ok {
				return ActionSnippetDeleted{
					Scope: snippet.Scope,
					Index: snippet.Index,
				}
			}
		default:
			return s.updateInput(msg)
		}
	default:
		return s.updateInput(msg)
	}
	return nil
}

func (s *Snippets) updateInput(msg tea.Msg) Action {
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	s.list.SetFilter(s.input.Value())
	s.list.SetSelected(0)
	if cmd != nil {
		return ActionCmd{Cmd: cmd}
	}
	return nil
}

func (s *Snippets) selectedSnippet() (ScopedSnippet, bool) {
	item := s.list.SelectedItem()
	if item == nil {
		return ScopedSnippet{}, false
	}
	snippet, ok := item.(*SnippetItem)
	if !ok {
		return ScopedSnippet{}, false
	}
	return snippet.snippet, true
}

func (s *Snippets) defaultScope() config.Scope {
	if snippet, ok := s.selectedSnippet(); ok {
		return snippet.Scope
	}
	return config.ScopeWorkspace
}

func (s *Snippets) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := s.com.Styles
	width := min(max(58, int(float64(area.Dx())*0.62)), 94)
	if area.Dx() < width {
		width = area.Dx()
	}
	height := min(max(16, int(float64(area.Dy())*0.62)), area.Dy())
	dialogStyle := t.Dialog.View.Width(width).Padding(0, 1)
	contentWidth := width - dialogStyle.GetHorizontalFrameSize() - 2

	title := common.DialogTitle(t, "Snippets", contentWidth-t.Dialog.Title.GetHorizontalFrameSize(), t.Primary, t.Secondary)
	title = t.Dialog.Title.Render(title)
	s.input.SetWidth(max(10, contentWidth-2))
	inputView := t.Dialog.InputPrompt.Render(s.input.View())

	listHeight := max(4, height-lipgloss.Height(title)-lipgloss.Height(inputView)-lipgloss.Height(s.help.View(s))-6)
	s.list.SetSize(contentWidth, listHeight)
	listView := s.list.Render()
	if len(s.list.FilteredItems()) == 0 {
		listView = t.Muted.Width(contentWidth).Render("No matching snippets. Press ctrl+n to create one.")
	}
	listView = t.Dialog.List.Height(listHeight).Render(listView)

	helpView := s.help.View(s)
	view := lipgloss.JoinVertical(lipgloss.Left, title, "", inputView, "", listView, "", helpView)

	cur := s.input.Cursor()
	if cur != nil {
		cur.X += dialogStyle.GetBorderLeftSize() +
			dialogStyle.GetPaddingLeft() +
			dialogStyle.GetMarginLeft()
		cur.Y += dialogStyle.GetBorderTopSize() +
			dialogStyle.GetPaddingTop() +
			dialogStyle.GetMarginTop() +
			lipgloss.Height(title) + 1
		offsetCursorByStyle(cur, t.Dialog.InputPrompt)
	}

	DrawCenterCursor(scr, area, dialogStyle.Render(view), cur)
	return cur
}

func (s *Snippets) ShortHelp() []key.Binding {
	return []key.Binding{s.keyMap.Select, s.keyMap.New, s.keyMap.Edit, s.keyMap.Delete, s.keyMap.Close}
}

func (s *Snippets) FullHelp() [][]key.Binding {
	return [][]key.Binding{{
		s.keyMap.Up,
		s.keyMap.Down,
		s.keyMap.Select,
		s.keyMap.New,
		s.keyMap.Edit,
		s.keyMap.Delete,
		s.keyMap.Close,
	}}
}

type SnippetItem struct {
	snippet ScopedSnippet
	t       *styles.Styles
	m       fuzzy.Match
	cache   map[int]string
	focused bool
}

var _ ListItem = (*SnippetItem)(nil)

func (s *SnippetItem) Filter() string {
	return strings.Join([]string{
		snippetTitle(s.snippet.Snippet),
		s.snippet.Snippet.Body,
		configScopeLabel(s.snippet.Scope),
	}, "\n")
}

func (s *SnippetItem) ID() string {
	if s.snippet.Snippet.ID != "" {
		return s.snippet.Snippet.ID
	}
	return fmt.Sprintf("%s-%d", s.snippet.Scope.String(), s.snippet.Index)
}

func (s *SnippetItem) SetFocused(focused bool) {
	if s.focused != focused {
		s.cache = nil
	}
	s.focused = focused
}

func (s *SnippetItem) SetMatch(m fuzzy.Match) {
	s.cache = nil
	s.m = m
}

func (s *SnippetItem) Render(width int) string {
	styles := ListItemStyles{
		ItemBlurred:     s.t.Dialog.NormalItem,
		ItemFocused:     s.t.Dialog.SelectedItem,
		InfoTextBlurred: s.t.Subtle,
		InfoTextFocused: s.t.Base,
	}
	return renderItem(styles, snippetTitle(s.snippet.Snippet), configScopeLabel(s.snippet.Scope), s.focused, width, s.cache, &s.m)
}

type snippetEditorFocus uint8

const (
	snippetEditorFocusTitle snippetEditorFocus = iota
	snippetEditorFocusBody
	snippetEditorFocusScope
	snippetEditorFocusActions
)

type snippetEditorKeyMap struct {
	Left    key.Binding
	Right   key.Binding
	Up      key.Binding
	Down    key.Binding
	Tab     key.Binding
	Toggle  key.Binding
	Select  key.Binding
	Save    key.Binding
	Delete  key.Binding
	Cancel  key.Binding
	Newline key.Binding
	Close   key.Binding
}

func defaultSnippetEditorKeyMap() snippetEditorKeyMap {
	return snippetEditorKeyMap{
		Left: key.NewBinding(
			key.WithKeys("left", "h"),
			key.WithHelp("←", "previous"),
		),
		Right: key.NewBinding(
			key.WithKeys("right", "l"),
			key.WithHelp("→", "next"),
		),
		Up: key.NewBinding(
			key.WithKeys("up"),
			key.WithHelp("↑", "previous field"),
		),
		Down: key.NewBinding(
			key.WithKeys("down"),
			key.WithHelp("↓", "next field"),
		),
		Tab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "next field"),
		),
		Toggle: key.NewBinding(
			key.WithKeys("space", "enter"),
			key.WithHelp("space", "toggle scope"),
		),
		Select: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "confirm"),
		),
		Save: key.NewBinding(
			key.WithKeys("ctrl+s"),
			key.WithHelp("ctrl+s", "save"),
		),
		Delete: key.NewBinding(
			key.WithKeys("ctrl+d"),
			key.WithHelp("ctrl+d", "delete"),
		),
		Cancel: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "cancel"),
		),
		Newline: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "newline"),
		),
		Close: CloseKey,
	}
}

type SnippetEditor struct {
	com           *common.Common
	originalScope config.Scope
	originalIndex int
	id            string
	scope         config.Scope
	title         textinput.Model
	body          textarea.Model
	focus         snippetEditorFocus
	selected      int
	help          help.Model
	keyMap        snippetEditorKeyMap
}

var _ Dialog = (*SnippetEditor)(nil)
var _ TextInserter = (*SnippetEditor)(nil)

func NewSnippetEditor(com *common.Common, scope config.Scope, index int, snippet config.Snippet) *SnippetEditor {
	title := textinput.New()
	title.SetVirtualCursor(false)
	title.SetStyles(com.Styles.TextInput)
	title.Prompt = "> "
	title.Placeholder = "Snippet title"
	title.SetValue(snippet.Title)
	title.Focus()
	title.CursorEnd()

	body := textarea.New()
	body.SetVirtualCursor(false)
	body.SetStyles(com.Styles.TextArea)
	body.ShowLineNumbers = false
	body.CharLimit = -1
	body.Prompt = "> "
	body.Placeholder = "Snippet body"
	body.SetHeight(8)
	body.SetValue(snippet.Body)

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()

	return &SnippetEditor{
		com:           com,
		originalScope: scope,
		originalIndex: index,
		id:            snippet.ID,
		scope:         scope,
		title:         title,
		body:          body,
		focus:         snippetEditorFocusTitle,
		help:          h,
		keyMap:        defaultSnippetEditorKeyMap(),
	}
}

func (*SnippetEditor) ID() string {
	return SnippetEditorID
}

func (e *SnippetEditor) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch e.focus {
		case snippetEditorFocusTitle:
			return e.handleTitleKey(msg)
		case snippetEditorFocusBody:
			return e.handleBodyKey(msg)
		case snippetEditorFocusScope:
			return e.handleScopeKey(msg)
		case snippetEditorFocusActions:
			return e.handleActionsKey(msg)
		}
	default:
		if e.focus == snippetEditorFocusTitle {
			return e.updateTitle(msg)
		}
		if e.focus == snippetEditorFocusBody {
			return e.updateBody(msg)
		}
	}
	return nil
}

func (e *SnippetEditor) handleTitleKey(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, e.keyMap.Close):
		return ActionClose{}
	case key.Matches(msg, e.keyMap.Tab), key.Matches(msg, e.keyMap.Down), key.Matches(msg, e.keyMap.Select):
		e.focusBody()
	default:
		return e.updateTitle(msg)
	}
	return nil
}

func (e *SnippetEditor) handleBodyKey(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, e.keyMap.Close):
		return ActionClose{}
	case key.Matches(msg, e.keyMap.Tab):
		e.focusScope()
	case key.Matches(msg, e.keyMap.Up) && e.body.Line() == 0:
		e.focusTitle()
	case key.Matches(msg, CopyKey):
		return ActionCmd{Cmd: common.CopyToClipboard(e.body.Value(), "Snippet body copied to clipboard")}
	default:
		return e.updateBody(msg)
	}
	return nil
}

func (e *SnippetEditor) handleScopeKey(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, e.keyMap.Close), key.Matches(msg, e.keyMap.Cancel):
		return ActionClose{}
	case key.Matches(msg, e.keyMap.Tab), key.Matches(msg, e.keyMap.Down):
		e.focusActions()
	case key.Matches(msg, e.keyMap.Up):
		e.focusBody()
	case key.Matches(msg, e.keyMap.Left), key.Matches(msg, e.keyMap.Right), key.Matches(msg, e.keyMap.Toggle):
		e.toggleScope()
	}
	return nil
}

func (e *SnippetEditor) handleActionsKey(msg tea.KeyPressMsg) Action {
	count := e.actionCount()
	switch {
	case key.Matches(msg, e.keyMap.Close), key.Matches(msg, e.keyMap.Cancel):
		return ActionClose{}
	case key.Matches(msg, e.keyMap.Tab):
		e.focusTitle()
	case key.Matches(msg, e.keyMap.Up):
		e.focusScope()
	case key.Matches(msg, e.keyMap.Left):
		e.selected = (e.selected + count - 1) % count
	case key.Matches(msg, e.keyMap.Right):
		e.selected = (e.selected + 1) % count
	case key.Matches(msg, e.keyMap.Save):
		return e.save()
	case key.Matches(msg, e.keyMap.Delete) && e.isEditing():
		return e.delete()
	case key.Matches(msg, e.keyMap.Select):
		if e.selected == 0 {
			return e.save()
		}
		if e.isEditing() && e.selected == 1 {
			return e.delete()
		}
		return ActionClose{}
	}
	return nil
}

func (e *SnippetEditor) updateTitle(msg tea.Msg) Action {
	var cmd tea.Cmd
	e.title, cmd = e.title.Update(msg)
	if cmd != nil {
		return ActionCmd{Cmd: cmd}
	}
	return nil
}

func (e *SnippetEditor) updateBody(msg tea.Msg) Action {
	var cmd tea.Cmd
	e.body, cmd = e.body.Update(msg)
	if cmd != nil {
		return ActionCmd{Cmd: cmd}
	}
	return nil
}

func (e *SnippetEditor) InsertText(text string) tea.Cmd {
	e.focusBody()
	e.body.InsertString(text)
	return nil
}

func (e *SnippetEditor) focusTitle() {
	e.focus = snippetEditorFocusTitle
	e.title.Focus()
	e.body.Blur()
}

func (e *SnippetEditor) focusBody() {
	e.focus = snippetEditorFocusBody
	e.title.Blur()
	e.body.Focus()
}

func (e *SnippetEditor) focusScope() {
	e.focus = snippetEditorFocusScope
	e.title.Blur()
	e.body.Blur()
}

func (e *SnippetEditor) focusActions() {
	e.focus = snippetEditorFocusActions
	e.title.Blur()
	e.body.Blur()
	if e.selected >= e.actionCount() {
		e.selected = 0
	}
}

func (e *SnippetEditor) toggleScope() {
	if e.scope == config.ScopeWorkspace {
		e.scope = config.ScopeGlobal
		return
	}
	e.scope = config.ScopeWorkspace
}

func (e *SnippetEditor) isEditing() bool {
	return e.originalIndex >= 0
}

func (e *SnippetEditor) actionCount() int {
	if e.isEditing() {
		return 3
	}
	return 2
}

func (e *SnippetEditor) save() Action {
	body := strings.TrimSpace(e.body.Value())
	title := strings.TrimSpace(e.title.Value())
	if title == "" {
		title = snippetTitle(config.Snippet{Body: body})
	}
	return ActionSnippetSaved{
		OriginalScope: e.originalScope,
		OriginalIndex: e.originalIndex,
		Scope:         e.scope,
		Snippet: config.Snippet{
			ID:    e.id,
			Title: title,
			Body:  body,
		},
	}
}

func (e *SnippetEditor) delete() Action {
	return ActionSnippetDeleted{
		Scope: e.originalScope,
		Index: e.originalIndex,
	}
}

func (e *SnippetEditor) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := e.com.Styles
	width := min(max(64, int(float64(area.Dx())*0.72)), 98)
	if area.Dx() < width {
		width = area.Dx()
	}
	maxHeight := min(max(20, int(float64(area.Dy())*0.72)), area.Dy())
	dialogStyle := t.Dialog.View.Width(width).Padding(0, 1)
	contentWidth := width - dialogStyle.GetHorizontalFrameSize() - 2

	title := common.DialogTitle(t, e.dialogTitle(), contentWidth-t.Dialog.Title.GetHorizontalFrameSize(), t.Primary, t.Secondary)
	title = t.Dialog.Title.Render(title)

	e.title.SetWidth(max(10, contentWidth-2))
	titleLabel := e.fieldLabel("Title", e.focus == snippetEditorFocusTitle)
	titleInput := t.Dialog.InputPrompt.Render(e.title.View())

	scope := e.renderScope(contentWidth)
	buttons := e.renderButtons()
	helpView := e.help.View(e)

	fixedHeight := lipgloss.Height(title) + lipgloss.Height(titleLabel) + lipgloss.Height(titleInput) + lipgloss.Height(scope) + lipgloss.Height(buttons) + lipgloss.Height(helpView) + 9
	bodyHeight := max(5, maxHeight-fixedHeight)
	e.body.SetWidth(max(10, contentWidth-2))
	e.body.SetHeight(bodyHeight)
	bodyLabel := e.fieldLabel("Body", e.focus == snippetEditorFocusBody)
	bodyView := e.body.View()

	parts := []string{
		title,
		"",
		titleLabel,
		titleInput,
		"",
		bodyLabel,
		bodyView,
		"",
		scope,
		"",
		buttons,
		"",
		helpView,
	}

	var cur *tea.Cursor
	switch e.focus {
	case snippetEditorFocusTitle:
		cur = e.title.Cursor()
		if cur != nil {
			e.offsetCursor(cur, dialogStyle, parts[:3])
			offsetCursorByStyle(cur, t.Dialog.InputPrompt)
		}
	case snippetEditorFocusBody:
		cur = e.body.Cursor()
		if cur != nil {
			e.offsetCursor(cur, dialogStyle, parts[:6])
		}
	}

	DrawCenterCursor(scr, area, dialogStyle.Render(lipgloss.JoinVertical(lipgloss.Left, parts...)), cur)
	return cur
}

func (e *SnippetEditor) offsetCursor(cur *tea.Cursor, dialogStyle lipgloss.Style, partsBeforeInput []string) {
	cur.X += dialogStyle.GetBorderLeftSize() +
		dialogStyle.GetMarginLeft() +
		dialogStyle.GetPaddingLeft()
	cur.Y += dialogStyle.GetBorderTopSize() +
		dialogStyle.GetMarginTop() +
		dialogStyle.GetPaddingTop()
	for _, part := range partsBeforeInput {
		cur.Y += max(1, lipgloss.Height(part))
	}
}

func offsetCursorByStyle(cur *tea.Cursor, style lipgloss.Style) {
	cur.X += style.GetBorderLeftSize() +
		style.GetMarginLeft() +
		style.GetPaddingLeft()
	cur.Y += style.GetBorderTopSize() +
		style.GetMarginTop() +
		style.GetPaddingTop()
}

func (e *SnippetEditor) dialogTitle() string {
	if e.isEditing() {
		return "Edit Snippet"
	}
	return "New Snippet"
}

func (e *SnippetEditor) fieldLabel(label string, focused bool) string {
	if focused {
		return e.com.Styles.Base.Render(label)
	}
	return e.com.Styles.Muted.Render(label)
}

func (e *SnippetEditor) renderScope(width int) string {
	t := e.com.Styles
	global := "○ Global"
	project := "○ Project"
	if e.scope == config.ScopeGlobal {
		global = "● Global"
	} else {
		project = "● Project"
	}
	line := global + "  " + project
	if e.focus == snippetEditorFocusScope {
		return t.Base.Width(width).Render(line)
	}
	return t.Muted.Width(width).Render(line)
}

func (e *SnippetEditor) renderButtons() string {
	buttons := []common.ButtonOpts{
		{Text: "Save", UnderlineIndex: 0, Selected: e.focus == snippetEditorFocusActions && e.selected == 0},
	}
	if e.isEditing() {
		buttons = append(buttons, common.ButtonOpts{Text: "Delete", UnderlineIndex: 0, Selected: e.focus == snippetEditorFocusActions && e.selected == 1})
	}
	cancelIndex := len(buttons)
	buttons = append(buttons, common.ButtonOpts{Text: "Cancel", UnderlineIndex: 0, Selected: e.focus == snippetEditorFocusActions && e.selected == cancelIndex})
	return common.ButtonGroup(e.com.Styles, buttons, "  ")
}

func (e *SnippetEditor) ShortHelp() []key.Binding {
	switch e.focus {
	case snippetEditorFocusTitle:
		return []key.Binding{e.keyMap.Tab, e.keyMap.Select, e.keyMap.Close}
	case snippetEditorFocusBody:
		return []key.Binding{e.keyMap.Newline, e.keyMap.Tab, e.keyMap.Close}
	case snippetEditorFocusScope:
		return []key.Binding{e.keyMap.Toggle, e.keyMap.Tab, e.keyMap.Up, e.keyMap.Close}
	default:
		bindings := []key.Binding{e.keyMap.Left, e.keyMap.Right, e.keyMap.Select, e.keyMap.Save}
		if e.isEditing() {
			bindings = append(bindings, e.keyMap.Delete)
		}
		return append(bindings, e.keyMap.Cancel)
	}
}

func (e *SnippetEditor) FullHelp() [][]key.Binding {
	return [][]key.Binding{e.ShortHelp()}
}

func snippetTitle(snippet config.Snippet) string {
	if title := strings.TrimSpace(snippet.Title); title != "" {
		return title
	}
	for _, line := range strings.Split(snippet.Body, "\n") {
		if line := strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return "Untitled snippet"
}

func configScopeLabel(scope config.Scope) string {
	if scope == config.ScopeWorkspace {
		return "project"
	}
	return "global"
}
