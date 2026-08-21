package dialog

import (
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/list"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/sahilm/fuzzy"
)

const (
	// WebBackendID is the identifier for the search/fetch backend picker
	// dialog.
	WebBackendID              = "web_backend"
	webBackendDialogMaxWidth  = 60
	webBackendDialogMaxHeight = 12
)

// WebBackendOption represents a search/fetch backend option.
type WebBackendOption struct {
	ID          string
	Title       string
	Description string
}

// AllWebBackendOptions lists all available search/fetch backends in order.
var AllWebBackendOptions = []WebBackendOption{
	{ID: "default", Title: "Default", Description: "Use Firecrawl when FIRECRAWL_API_KEY is set, then Exa when EXA_API_KEY is set, otherwise DuckDuckGo search and direct HTTP fetching"},
	{ID: "firecrawl", Title: "Firecrawl", Description: "Search and fetch through the Firecrawl API (requires FIRECRAWL_API_KEY)"},
	{ID: "exa", Title: "Exa", Description: "Search and fetch through the Exa API (requires EXA_API_KEY)"},
}

// WebBackend represents a dialog for selecting the search/fetch backend.
type WebBackend struct {
	com   *common.Common
	help  help.Model
	list  *list.FilterableList
	input textinput.Model

	keyMap struct {
		Select   key.Binding
		Next     key.Binding
		Previous key.Binding
		UpDown   key.Binding
		Close    key.Binding
	}
}

// WebBackendItem represents a search/fetch backend list item.
type WebBackendItem struct {
	*list.Versioned
	option    WebBackendOption
	isCurrent bool
	t         *styles.Styles
	m         fuzzy.Match
	cache     map[int]string
	focused   bool
}

// Finished implements list.Item. Backend items are render-stable outside
// of explicit SetFocused / SetMatch.
func (w *WebBackendItem) Finished() bool {
	return true
}

var (
	_ Dialog   = (*WebBackend)(nil)
	_ ListItem = (*WebBackendItem)(nil)
)

// NewWebBackend creates a new search/fetch backend picker dialog.
func NewWebBackend(com *common.Common) *WebBackend {
	w := &WebBackend{com: com}

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()
	w.help = h

	w.list = list.NewFilterableList()
	w.list.Focus()

	w.input = textinput.New()
	w.input.SetVirtualCursor(false)
	w.input.Placeholder = "Type to filter"
	w.input.SetStyles(com.Styles.TextInput)
	w.input.Focus()

	w.keyMap.Select = key.NewBinding(
		key.WithKeys("enter", "ctrl+y"),
		key.WithHelp("enter", "confirm"),
	)
	w.keyMap.Next = key.NewBinding(
		key.WithKeys("down", "ctrl+n"),
		key.WithHelp("↓", "next item"),
	)
	w.keyMap.Previous = key.NewBinding(
		key.WithKeys("up", "ctrl+p"),
		key.WithHelp("↑", "previous item"),
	)
	w.keyMap.UpDown = key.NewBinding(
		key.WithKeys("up", "down"),
		key.WithHelp("↑/↓", "choose"),
	)
	w.keyMap.Close = CloseKey

	w.setItems()
	return w
}

// ID implements Dialog.
func (w *WebBackend) ID() string {
	return WebBackendID
}

// HandleMsg implements [Dialog].
func (w *WebBackend) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, w.keyMap.Close):
			return ActionClose{}
		case key.Matches(msg, w.keyMap.Previous):
			w.list.Focus()
			if w.list.IsSelectedFirst() {
				w.list.SelectLast()
				w.list.ScrollToBottom()
				break
			}
			w.list.SelectPrev()
			w.list.ScrollToSelected()
		case key.Matches(msg, w.keyMap.Next):
			w.list.Focus()
			if w.list.IsSelectedLast() {
				w.list.SelectFirst()
				w.list.ScrollToTop()
				break
			}
			w.list.SelectNext()
			w.list.ScrollToSelected()
		case key.Matches(msg, w.keyMap.Select):
			selectedItem := w.list.SelectedItem()
			if selectedItem == nil {
				break
			}
			backendItem, ok := selectedItem.(*WebBackendItem)
			if !ok {
				break
			}
			return ActionSelectWebBackend{Backend: backendItem.option.ID}
		default:
			var cmd tea.Cmd
			w.input, cmd = w.input.Update(msg)
			value := w.input.Value()
			w.list.SetFilter(value)
			w.list.ScrollToTop()
			w.list.SetSelected(0)
			return ActionCmd{cmd}
		}
	}
	return nil
}

// Cursor returns the cursor position relative to the dialog.
func (w *WebBackend) Cursor() *tea.Cursor {
	return InputCursor(w.com.Styles, w.input.Cursor())
}

// Draw implements [Dialog].
func (w *WebBackend) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := w.com.Styles
	width := max(0, min(webBackendDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	height := max(0, min(webBackendDialogMaxHeight, area.Dy()-t.Dialog.View.GetVerticalBorderSize()))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()
	heightOffset := t.Dialog.Title.GetVerticalFrameSize() + titleContentHeight +
		t.Dialog.InputPrompt.GetVerticalFrameSize() + inputContentHeight +
		t.Dialog.HelpView.GetVerticalFrameSize() +
		t.Dialog.View.GetVerticalFrameSize()

	w.input.SetWidth(dialogInputTextWidth(t, w.input, innerWidth))
	w.list.SetSize(innerWidth, max(0, height-heightOffset))

	rc := NewRenderContext(t, width)
	rc.Title = "Search & Fetch Backend"
	inputView := t.Dialog.InputPrompt.Render(w.input.View())
	rc.AddPart(inputView)

	visibleCount := len(w.list.FilteredItems())
	if w.list.Height() >= visibleCount {
		w.list.ScrollToTop()
	} else {
		w.list.ScrollToSelected()
	}

	listView := t.Dialog.List.Height(w.list.Height()).Render(w.list.Render())
	rc.AddPart(listView)
	rc.Help = renderDialogHelp(t, &w.help, w, innerWidth)

	view := rc.Render()

	cur := w.Cursor()
	DrawCenterCursor(scr, area, view, cur)
	return cur
}

// ShortHelp implements [help.KeyMap].
func (w *WebBackend) ShortHelp() []key.Binding {
	return []key.Binding{
		w.keyMap.UpDown,
		w.keyMap.Select,
		w.keyMap.Close,
	}
}

// FullHelp implements [help.KeyMap].
func (w *WebBackend) FullHelp() [][]key.Binding {
	m := [][]key.Binding{}
	slice := []key.Binding{
		w.keyMap.Select,
		w.keyMap.Next,
		w.keyMap.Previous,
		w.keyMap.Close,
	}
	for i := 0; i < len(slice); i += 4 {
		end := min(i+4, len(slice))
		m = append(m, slice[i:end])
	}
	return m
}

func (w *WebBackend) setItems() {
	cfg := w.com.Config()
	currentBackend := "default"
	if cfg != nil && cfg.Options != nil && cfg.Options.WebBackend != "" {
		currentBackend = cfg.Options.WebBackend
	}

	items := make([]list.FilterableItem, 0, len(AllWebBackendOptions))
	selectedIndex := 0
	for _, option := range AllWebBackendOptions {
		item := &WebBackendItem{
			Versioned: list.NewVersioned(),
			option:    option,
			isCurrent: option.ID == currentBackend,
			t:         w.com.Styles,
		}
		if option.ID == currentBackend {
			selectedIndex = len(items)
		}
		items = append(items, item)
	}

	w.list.SetItems(items...)
	w.list.SetSelected(selectedIndex)
	w.list.ScrollToSelected()
}

// Filter returns the filter value for the backend item.
func (w *WebBackendItem) Filter() string {
	return w.option.Title
}

// ID returns the unique identifier for the backend option.
func (w *WebBackendItem) ID() string {
	return w.option.ID
}

// SetFocused sets the focus state of the backend item.
func (w *WebBackendItem) SetFocused(focused bool) {
	if w.focused == focused {
		return
	}
	w.cache = nil
	w.focused = focused
	if w.Versioned != nil {
		w.Bump()
	}
}

// SetMatch sets the fuzzy match for the backend item.
func (w *WebBackendItem) SetMatch(m fuzzy.Match) {
	if sameFuzzyMatch(w.m, m) {
		return
	}
	w.cache = nil
	w.m = m
	if w.Versioned != nil {
		w.Bump()
	}
}

// Render returns the string representation of the backend item.
func (w *WebBackendItem) Render(width int) string {
	info := ""
	if w.isCurrent {
		info = "current"
	}
	st := ListItemStyles{
		ItemBlurred:     w.t.Dialog.NormalItem,
		ItemFocused:     w.t.Dialog.SelectedItem,
		InfoTextBlurred: w.t.Dialog.ListItem.InfoBlurred,
		InfoTextFocused: w.t.Dialog.ListItem.InfoFocused,
	}
	return renderItem(st, w.option.Title, info, w.focused, width, w.cache, &w.m)
}
