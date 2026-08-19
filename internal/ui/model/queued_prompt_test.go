package model

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/completions"
)

// newQueueUI builds a chat UI with the queue pill expanded and focused on
// the queue section, holding three queued prompts.
func newQueueUI(t *testing.T) (*UI, *countingWorkspace) {
	t.Helper()
	pinTTLs(t)
	ws := &countingWorkspace{ready: true, queued: []agent.QueuedPromptItem{
		{QueueID: 1, Prompt: "first"},
		{QueueID: 2, Prompt: "second"},
		{QueueID: 3, Prompt: "third"},
	}}
	m := newBusyUI(ws)
	m.completions = completions.New(
		m.com.Styles.Completions.Normal,
		m.com.Styles.Completions.Focused,
		m.com.Styles.Completions.Match,
	)
	warmCaches(m, true)
	m.focus = uiFocusMain
	m.pillsExpanded = true
	m.focusedPillSection = pillSectionQueue
	m.promptQueue = len(ws.queued)
	m.promptQueueItems = append([]agent.QueuedPromptItem{}, ws.queued...)
	m.promptQueueCheckedAt = time.Now()
	return m, ws
}

// TestQueueCursorNavigation pins that up/down move the selection through
// the expanded queue list, clamped at both ends, without probing the
// workspace.
func TestQueueCursorNavigation(t *testing.T) {
	m, ws := newQueueUI(t)

	for range 4 {
		_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	require.Equal(t, 2, m.queueCursor, "cursor must clamp at the last item")

	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	require.Equal(t, 1, m.queueCursor, "up must move the cursor toward the top")

	require.Zero(t, ws.removeQueueCalls, "navigation must not remove anything")
	require.Zero(t, ws.syncProbes(), "navigation must not probe the workspace")
}

// TestQueueRemoveSelectedItem pins that x removes the cursor item
// optimistically, fires the workspace removal off-thread, and re-fetches
// the authoritative queue when the item is already gone.
func TestQueueRemoveSelectedItem(t *testing.T) {
	m, ws := newQueueUI(t)

	next, cmd := m.Update(tea.KeyPressMsg{Code: rune('x'), Text: "x"})
	require.NotNil(t, cmd, "x on a queue item must dispatch the removal")
	m = next.(*UI)

	require.Equal(t, 2, m.promptQueue, "the removed item must drop out of the pill")
	require.Len(t, m.promptQueueItems, 2)
	require.Equal(t, uint64(2), m.promptQueueItems[0].QueueID,
		"the following items must shift into place")

	runCmds(m, cmd)
	require.Equal(t, 1, ws.removeQueueCalls, "the removal must hit the workspace")
	require.Equal(t, uint64(1), ws.removeQueueID, "the cursor item's queue ID must be sent")
	require.Equal(t, 1, ws.queueListCalls,
		"an unsuccessful removal must re-fetch the authoritative queue")
	require.Equal(t, 3, m.promptQueue,
		"the authoritative fetch must reconcile the optimistic removal")
}

// TestQueueRecallPullsPromptIntoComposer pins that enter pulls the cursor
// item back into the composer (prepended to an existing draft), focuses
// the editor, and removes the item from the queue.
func TestQueueRecallPullsPromptIntoComposer(t *testing.T) {
	m, ws := newQueueUI(t)
	m.textarea.SetValue("old draft")

	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, cmd, "enter on a queue item must dispatch the removal")
	m = next.(*UI)

	require.Equal(t, "first\nold draft", m.textarea.Value(),
		"the queued prompt must be prepended to the existing draft")
	require.Equal(t, uiFocusEditor, m.focus, "recall must focus the editor")
	require.Equal(t, 2, m.promptQueue, "the recalled item must leave the queue")

	runCmds(m, cmd)
	require.Equal(t, 1, ws.removeQueueCalls)
	require.Equal(t, uint64(1), ws.removeQueueID)
}

// TestQueueKeysIgnoredWhenQueueNotFocused pins that the queue keys are
// inert when the queue section is not focused, so they fall through to
// their normal chat behaviors.
func TestQueueKeysIgnoredWhenQueueNotFocused(t *testing.T) {
	m, ws := newQueueUI(t)
	m.focusedPillSection = pillSectionTodos
	m.session.Todos = []session.Todo{{Content: "task", Status: session.TodoStatusPending}}

	_, cmd := m.Update(tea.KeyPressMsg{Code: rune('x'), Text: "x"})
	require.Nil(t, cmd, "x without queue focus must not dispatch a removal")
	require.Zero(t, ws.removeQueueCalls)
	require.Equal(t, 3, m.promptQueue, "queue must be untouched")
}
