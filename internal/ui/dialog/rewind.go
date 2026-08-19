package dialog

import (
	"fmt"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/ui/common"
	uv "github.com/charmbracelet/ultraviolet"
)

// RewindID is the identifier for the rewind confirmation dialog.
const RewindID = "rewind"

// Rewind represents a confirmation dialog for resuming a session from
// an earlier user message.
type Rewind struct {
	com       *common.Common
	sessionID string
	messageID string
	// deleteCount is the total number of messages that will be removed,
	// including the selected user message.
	deleteCount int

	selectedNo bool
	keyMap     struct {
		LeftRight,
		EnterSpace,
		Yes,
		No,
		Tab,
		Close key.Binding
	}
}

var _ Dialog = (*Rewind)(nil)

// NewRewind creates a new rewind confirmation dialog.
func NewRewind(com *common.Common, sessionID, messageID string, deleteCount int) *Rewind {
	q := &Rewind{
		com:         com,
		sessionID:   sessionID,
		messageID:   messageID,
		deleteCount: deleteCount,
		selectedNo:  true,
	}
	q.keyMap.LeftRight = key.NewBinding(
		key.WithKeys("left", "right"),
		key.WithHelp("←/→", "switch options"),
	)
	q.keyMap.EnterSpace = key.NewBinding(
		key.WithKeys("enter", " "),
		key.WithHelp("enter/space", "confirm"),
	)
	q.keyMap.Yes = key.NewBinding(
		key.WithKeys("y", "Y"),
		key.WithHelp("y/Y", "yes"),
	)
	q.keyMap.No = key.NewBinding(
		key.WithKeys("n", "N"),
		key.WithHelp("n/N", "no"),
	)
	q.keyMap.Tab = key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "switch options"),
	)
	q.keyMap.Close = CloseKey
	return q
}

// ID implements [Dialog].
func (*Rewind) ID() string {
	return RewindID
}

// HandleMsg implements [Dialog].
func (q *Rewind) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, q.keyMap.Close):
			return ActionClose{}
		case key.Matches(msg, q.keyMap.LeftRight, q.keyMap.Tab):
			q.selectedNo = !q.selectedNo
		case key.Matches(msg, q.keyMap.EnterSpace):
			if !q.selectedNo {
				return ActionRewindConfirmed{SessionID: q.sessionID, MessageID: q.messageID}
			}
			return ActionClose{}
		case key.Matches(msg, q.keyMap.Yes):
			return ActionRewindConfirmed{SessionID: q.sessionID, MessageID: q.messageID}
		case key.Matches(msg, q.keyMap.No):
			return ActionClose{}
		}
	}

	return nil
}

// Draw implements [Dialog].
func (q *Rewind) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	after := q.deleteCount - 1
	var hintLineOne string
	if after == 1 {
		hintLineOne = fmt.Sprintf("This removes this message and the %d message after it,", after)
	} else {
		hintLineOne = fmt.Sprintf("This removes this message and the %d messages after it,", after)
	}
	hintLineTwo := "then puts the message text back in the prompt."

	var (
		baseStyle = q.com.Styles.Dialog.Quit.Content
		hintStyle = q.com.Styles.Dialog.Quit.Hint
	)
	buttonOpts := []common.ButtonOpts{
		{Text: "Yep!", Selected: !q.selectedNo, Padding: 3},
		{Text: "Nope", Selected: q.selectedNo, Padding: 3},
	}
	buttons := common.ButtonGroup(q.com.Styles, buttonOpts, " ")
	content := baseStyle.Render(
		lipgloss.JoinVertical(
			lipgloss.Center,
			"Resume from this message?",
			"",
			buttons,
			"",
			hintStyle.Render(hintLineOne),
			hintStyle.Render(hintLineTwo),
		),
	)

	frameStyle := q.com.Styles.Dialog.Quit.Frame
	maxWidth := area.Dx() - frameStyle.GetHorizontalBorderSize()
	if maxWidth < lipgloss.Width(content) {
		frameStyle = frameStyle.Padding(1, 0)
	}
	view := frameStyle.Render(content)
	DrawCenter(scr, area, view)
	return nil
}

// ShortHelp implements [help.KeyMap].
func (q *Rewind) ShortHelp() []key.Binding {
	return []key.Binding{
		q.keyMap.LeftRight,
		q.keyMap.EnterSpace,
	}
}

// FullHelp implements [help.KeyMap].
func (q *Rewind) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{q.keyMap.LeftRight, q.keyMap.EnterSpace, q.keyMap.Yes, q.keyMap.No},
		{q.keyMap.Tab, q.keyMap.Close},
	}
}
