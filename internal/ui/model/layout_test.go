package model

import (
	"strconv"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/ui/attachments"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/dialog"
)

// testMessageItem is a minimal chat item used to populate the chat list
// without pulling in full message rendering machinery.
type testMessageItem struct {
	id   string
	text string
}

func (m testMessageItem) ID() string           { return m.id }
func (m testMessageItem) Render(int) string    { return m.text }
func (m testMessageItem) RawRender(int) string { return m.text }
func (m testMessageItem) SetFocused(bool)      {}

var _ chat.MessageItem = testMessageItem{}

// newTestUI builds a focused uiChat model with dynamic textarea sizing enabled.
// It intentionally keeps dependencies minimal so layout behavior can be tested
// in isolation.
func newTestUI() *UI {
	cfg := &config.Config{
		Options: &config.Options{
			TUI: &config.TUIOptions{},
		},
	}
	com := common.DefaultCommon(&testWorkspace{cfg: cfg})

	ta := textarea.New()
	ta.SetStyles(com.Styles.TextArea)
	ta.ShowLineNumbers = false
	ta.CharLimit = -1
	ta.SetVirtualCursor(false)
	ta.DynamicHeight = true
	ta.MinHeight = TextareaMinHeight
	ta.MaxHeight = TextareaMaxHeight
	ta.Focus()

	keyMap := DefaultKeyMap()
	u := &UI{
		com:      com,
		dialog:   dialog.NewOverlay(),
		keyMap:   keyMap,
		status:   NewStatus(com, nil),
		chat:     NewChat(com),
		textarea: ta,
		attachments: attachments.New(nil, attachments.Keymap{
			DeleteMode: keyMap.Editor.AttachmentDeleteMode,
			DeleteAll:  keyMap.Editor.DeleteAllAttachments,
			Escape:     keyMap.Editor.Escape,
		}),
		state:  uiChat,
		focus:  uiFocusEditor,
		width:  140,
		height: 45,
	}

	return u
}

func TestUpdateLayoutAndSize_EditorGrowthShrinksChat(t *testing.T) {
	t.Parallel()

	// Baseline layout at min textarea height.
	u := newTestUI()
	u.updateLayoutAndSize()

	initialEditorHeight := u.layout.editor.Dy()
	initialChatHeight := u.layout.main.Dy()

	// Increase textarea content enough to trigger growth, then run the
	// same resize hook used in the real update path.
	prevHeight := u.textarea.Height()
	u.textarea.SetValue(strings.Repeat("line\n", 8))
	u.textarea.MoveToEnd()
	_ = u.handleTextareaHeightChange(prevHeight)

	if got := u.layout.editor.Dy(); got <= initialEditorHeight {
		t.Fatalf("expected editor to grow: got %d, want > %d", got, initialEditorHeight)
	}

	if got := u.layout.main.Dy(); got >= initialChatHeight {
		t.Fatalf("expected chat to shrink: got %d, want < %d", got, initialChatHeight)
	}
}

func TestHandleTextareaHeightChange_FollowModeStaysAtBottom(t *testing.T) {
	t.Parallel()

	// Use enough messages to make the chat scrollable so AtBottom/Follow
	// assertions are meaningful.
	u := newTestUI()

	msgs := make([]chat.MessageItem, 0, 60)
	for i := range 60 {
		msgs = append(msgs, testMessageItem{
			id:   "m-" + strconv.Itoa(i),
			text: "message " + strconv.Itoa(i),
		})
	}
	u.chat.SetMessages(msgs...)
	u.updateLayoutAndSize()

	// Enter follow mode and verify we're anchored at the bottom first.
	u.chat.ScrollToBottom()
	if !u.chat.AtBottom() {
		t.Fatal("expected chat to start at bottom")
	}

	// Grow the editor; follow mode should keep the chat pinned to the end
	// even as the chat viewport shrinks.
	prevHeight := u.textarea.Height()
	u.textarea.SetValue(strings.Repeat("line\n", 10))
	u.textarea.MoveToEnd()
	_ = u.handleTextareaHeightChange(prevHeight)

	if !u.chat.Follow() {
		t.Fatal("expected follow mode to remain enabled")
	}
	if !u.chat.AtBottom() {
		t.Fatal("expected chat to remain at bottom after editor resize in follow mode")
	}
}

func TestMouseWheelOutsideChatDoesNotScroll(t *testing.T) {
	t.Parallel()

	u := newTestUI()
	u.setScrollableChatMessages(80)
	u.chat.ScrollToBottom()
	u.updateLayoutAndSize()

	_, cmd := u.Update(tea.MouseWheelMsg{
		X:      u.layout.editor.Min.X,
		Y:      u.layout.editor.Min.Y,
		Button: tea.MouseWheelUp,
	})
	runCommand(t, cmd)

	if !u.chat.Follow() {
		t.Fatal("expected follow mode to stay enabled")
	}
	if !u.chat.AtBottom() {
		t.Fatal("expected chat to stay at bottom")
	}
}

func TestMouseWheelScrollDoesNotSnapToSelectedItem(t *testing.T) {
	t.Parallel()

	u := newTestUI()
	u.setScrollableChatMessages(100)
	u.chat.SelectLast()
	u.chat.ScrollToBottom()
	u.updateLayoutAndSize()

	for range 12 {
		_, cmd := u.Update(tea.MouseWheelMsg{
			X:      u.layout.main.Min.X,
			Y:      u.layout.main.Min.Y,
			Button: tea.MouseWheelUp,
		})
		runCommand(t, cmd)
	}

	if u.chat.AtBottom() {
		t.Fatal("expected mouse wheel to move chat away from bottom")
	}
	if u.chat.Follow() {
		t.Fatal("expected mouse wheel up to disable follow mode")
	}
	if u.chat.SelectedMessageItem().ID() != "m-99" {
		t.Fatal("expected mouse wheel to leave selected item unchanged")
	}
	if u.chat.SelectedItemInView() {
		t.Fatal("expected mouse wheel not to snap the selected item into view")
	}
}

func TestMouseFocusEditorDoesNotAutoFollow(t *testing.T) {
	t.Parallel()

	u := newTestUI()
	u.setScrollableChatMessages(80)
	u.chat.ScrollToBottom()
	u.updateLayoutAndSize()
	u.chat.ScrollBy(-MouseScrollThreshold)
	u.focus = uiFocusMain
	u.textarea.Blur()
	u.chat.Focus()

	cmd := u.handleClickFocus(tea.MouseClickMsg{
		X:      u.layout.editor.Min.X,
		Y:      u.layout.editor.Min.Y,
		Button: tea.MouseLeft,
	})
	runCommand(t, cmd)

	if u.focus != uiFocusEditor {
		t.Fatal("expected editor to receive focus")
	}
	if u.chat.Follow() {
		t.Fatal("expected mouse focus to leave follow mode unchanged")
	}
	if u.chat.AtBottom() {
		t.Fatal("expected mouse focus not to scroll chat to bottom")
	}
}

func TestFocusedEditorClickAutoFollows(t *testing.T) {
	t.Parallel()

	u := newTestUI()
	u.setScrollableChatMessages(80)
	u.chat.ScrollToBottom()
	u.updateLayoutAndSize()
	u.chat.ScrollBy(-MouseScrollThreshold)
	u.focus = uiFocusEditor
	u.textarea.Focus()
	u.chat.Blur()

	cmd := u.handleClickFocus(tea.MouseClickMsg{
		X:      u.layout.editor.Min.X,
		Y:      u.layout.editor.Min.Y,
		Button: tea.MouseLeft,
	})
	runCommand(t, cmd)

	if !u.chat.Follow() {
		t.Fatal("expected focused editor click to enable follow mode")
	}
	if !u.chat.AtBottom() {
		t.Fatal("expected focused editor click to scroll chat to bottom")
	}
}

func TestEditorTypingAutoFollows(t *testing.T) {
	t.Parallel()

	u := newTestUI()
	u.setScrollableChatMessages(80)
	u.chat.ScrollToBottom()
	u.updateLayoutAndSize()
	u.chat.ScrollBy(-MouseScrollThreshold)
	u.focus = uiFocusEditor
	u.textarea.Focus()
	u.chat.Blur()

	cmd := u.handleKeyPressMsg(tea.KeyPressMsg{
		Text: "x",
		Code: 'x',
	})
	runCommand(t, cmd)

	if u.textarea.Value() != "x" {
		t.Fatal("expected typed text to update textarea")
	}
	if !u.chat.Follow() {
		t.Fatal("expected editor typing to enable follow mode")
	}
	if !u.chat.AtBottom() {
		t.Fatal("expected editor typing to scroll chat to bottom")
	}
}

func (m *UI) setScrollableChatMessages(count int) {
	msgs := make([]chat.MessageItem, 0, count)
	for i := range count {
		msgs = append(msgs, testMessageItem{
			id:   "m-" + strconv.Itoa(i),
			text: "message " + strconv.Itoa(i),
		})
	}
	m.chat.SetMessages(msgs...)
}
