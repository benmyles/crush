package model

import (
	"testing"

	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/goal"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/stretchr/testify/require"
)

func userMessage(id, text string) *message.Message {
	return &message.Message{
		ID:    id,
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: text}},
	}
}

func assistantMessage(id, text string, finished bool) *message.Message {
	parts := []message.ContentPart{message.TextContent{Text: text}}
	if finished {
		parts = append(parts, message.Finish{Reason: message.FinishReasonEndTurn})
	}
	return &message.Message{ID: id, Role: message.Assistant, Parts: parts}
}

func TestWindowTitle(t *testing.T) {
	t.Parallel()

	t.Run("no session and no messages", func(t *testing.T) {
		t.Parallel()
		u := newTestUI()
		require.Equal(t, "crush", u.windowTitle())
	})

	t.Run("no goal uses first user message", func(t *testing.T) {
		t.Parallel()
		u := newTestUI()
		u.session = &session.Session{ID: "s1", Title: "Fix the deploy"}
		u.chat.SetMessages(
			chat.NewUserMessageItem(u.com.Styles, userMessage("m1", "Fix the deploy"), nil),
			chat.NewUserMessageItem(u.com.Styles, userMessage("m2", "Also run the tests"), nil),
		)
		require.Equal(t, "Fix the deploy", u.windowTitle())
	})

	t.Run("no goal and busy keeps first user message", func(t *testing.T) {
		t.Parallel()
		u := newTestUI()
		u.session = &session.Session{ID: "s1"}
		u.chat.SetMessages(
			chat.NewUserMessageItem(u.com.Styles, userMessage("m1", "Fix the deploy"), nil),
		)
		u.agentBusyCache.set(true)
		require.Equal(t, "Fix the deploy", u.windowTitle())
	})
}

func TestWindowTitleWithGoal(t *testing.T) {
	t.Parallel()

	newGoalUI := func() *UI {
		u := newTestUI()
		u.session = &session.Session{ID: "s1", Title: "ignored"}
		u.goal = goal.Goal{SessionID: "s1", Text: "Refactor the config layer", Status: goal.StatusActive}
		return u
	}

	t.Run("active idle waits", func(t *testing.T) {
		t.Parallel()
		u := newGoalUI()
		require.Equal(t, "○ Refactor the config layer", u.windowTitle())
	})

	t.Run("busy without text thinks", func(t *testing.T) {
		t.Parallel()
		u := newGoalUI()
		u.agentBusyCache.set(true)
		require.Equal(t, "◐ Refactor the config layer", u.windowTitle())
	})

	t.Run("busy streaming text responds", func(t *testing.T) {
		t.Parallel()
		u := newGoalUI()
		u.agentBusyCache.set(true)
		u.chat.AppendMessages(chat.NewAssistantMessageItem(
			u.com.Styles, assistantMessage("a1", "Here is the plan...", false),
		))
		require.Equal(t, "● Refactor the config layer", u.windowTitle())
	})

	t.Run("finished assistant text stops responding", func(t *testing.T) {
		t.Parallel()
		u := newGoalUI()
		u.agentBusyCache.set(true)
		u.chat.AppendMessages(chat.NewAssistantMessageItem(
			u.com.Styles, assistantMessage("a1", "Done.", true),
		))
		require.Equal(t, "◐ Refactor the config layer", u.windowTitle())
	})

	t.Run("question pending", func(t *testing.T) {
		t.Parallel()
		u := newGoalUI()
		u.activeInline = &dialog.QuestionForm{}
		require.Equal(t, "? Refactor the config layer", u.windowTitle())
	})

	t.Run("blocked goal", func(t *testing.T) {
		t.Parallel()
		u := newGoalUI()
		u.goal.Status = goal.StatusBlocked
		require.Equal(t, "! Refactor the config layer", u.windowTitle())
	})

	t.Run("stalled goal", func(t *testing.T) {
		t.Parallel()
		u := newGoalUI()
		u.goal.Status = goal.StatusStalled
		require.Equal(t, "… Refactor the config layer", u.windowTitle())
	})

	t.Run("complete goal", func(t *testing.T) {
		t.Parallel()
		u := newGoalUI()
		u.goal.Status = goal.StatusComplete
		require.Equal(t, "✓ Refactor the config layer", u.windowTitle())
	})

	t.Run("paused trumps busy", func(t *testing.T) {
		t.Parallel()
		u := newGoalUI()
		u.agentBusyCache.set(true)
		u.pausedActive = true
		require.Equal(t, "‖ Refactor the config layer", u.windowTitle())
	})

	t.Run("empty goal text falls back to first user message", func(t *testing.T) {
		t.Parallel()
		u := newGoalUI()
		u.goal.Text = "   "
		u.chat.SetMessages(chat.NewUserMessageItem(u.com.Styles, userMessage("m1", "Fallback prompt"), nil))
		require.Equal(t, "Fallback prompt", u.windowTitle())
	})

	t.Run("long goal text is truncated", func(t *testing.T) {
		t.Parallel()
		u := newGoalUI()
		long := make([]rune, 0, maxTitleWidth+10)
		for range maxTitleWidth + 10 {
			long = append(long, 'x')
		}
		u.goal.Text = string(long)
		got := []rune(u.windowTitle())
		// Icon, space, truncated text, and ellipsis.
		require.Less(t, len(got), maxTitleWidth+4)
		require.Equal(t, "…", string(got[len(got)-1:]))
	})
}

func TestWindowTitleCustomTitle(t *testing.T) {
	t.Parallel()

	newUI := func() *UI {
		u := newTestUI()
		u.session = &session.Session{ID: "s1", Title: "ignored"}
		u.goal = goal.Goal{SessionID: "s1", Text: "Refactor the config layer", Status: goal.StatusActive}
		return u
	}

	t.Run("custom title replaces goal text", func(t *testing.T) {
		t.Parallel()
		u := newUI()
		u.terminalTitle = "Rewriting auth middleware"
		require.Equal(t, "○ Rewriting auth middleware", u.windowTitle())
	})

	t.Run("custom title keeps state glyph", func(t *testing.T) {
		t.Parallel()
		u := newUI()
		u.agentBusyCache.set(true)
		u.terminalTitle = "Rewriting auth middleware"
		require.Equal(t, "◐ Rewriting auth middleware", u.windowTitle())
	})

	t.Run("empty custom title falls back to goal text", func(t *testing.T) {
		t.Parallel()
		u := newUI()
		u.terminalTitle = "   "
		require.Equal(t, "○ Refactor the config layer", u.windowTitle())
	})

	t.Run("long custom title is truncated", func(t *testing.T) {
		t.Parallel()
		u := newUI()
		u.goal = goal.Goal{}
		long := make([]rune, 0, maxTitleWidth+10)
		for range maxTitleWidth + 10 {
			long = append(long, 'x')
		}
		u.terminalTitle = string(long)
		got := []rune(u.windowTitle())
		require.Less(t, len(got), maxTitleWidth+4)
		require.Equal(t, "…", string(got[len(got)-1:]))
	})
}

func TestTerminalTitleNotification(t *testing.T) {
	t.Parallel()

	t.Run("sets title for current session", func(t *testing.T) {
		t.Parallel()
		u := newTestUI()
		u.session = &session.Session{ID: "s1"}
		u.handleAgentNotification(notify.Notification{
			SessionID:     "s1",
			Type:          notify.TypeTerminalTitleChanged,
			TerminalTitle: "Fixing deploy pipeline",
		})
		require.Equal(t, "Fixing deploy pipeline", u.terminalTitle)
	})

	t.Run("ignores other sessions", func(t *testing.T) {
		t.Parallel()
		u := newTestUI()
		u.session = &session.Session{ID: "s1"}
		u.handleAgentNotification(notify.Notification{
			SessionID:     "s2",
			Type:          notify.TypeTerminalTitleChanged,
			TerminalTitle: "Fixing deploy pipeline",
		})
		require.Empty(t, u.terminalTitle)
	})

	t.Run("empty title clears", func(t *testing.T) {
		t.Parallel()
		u := newTestUI()
		u.session = &session.Session{ID: "s1"}
		u.terminalTitle = "Stale title"
		u.handleAgentNotification(notify.Notification{
			SessionID: "s1",
			Type:      notify.TypeTerminalTitleChanged,
		})
		require.Empty(t, u.terminalTitle)
	})
}

func TestUserMessageClearsCustomTitle(t *testing.T) {
	ws := &countingWorkspace{}
	u := newBusyUI(ws)
	u.terminalTitle = "Stale title"

	_, cmd := u.Update(pubsub.Event[message.Message]{
		Type:    pubsub.CreatedEvent,
		Payload: message.Message{ID: "m1", SessionID: "s1", Role: message.User},
	})
	require.Empty(t, u.terminalTitle, "a new user message must clear the custom title")
	runCmds(u, cmd)
}
