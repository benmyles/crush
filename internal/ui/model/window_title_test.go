package model

import (
	"testing"

	"github.com/charmbracelet/crush/internal/goal"
	"github.com/charmbracelet/crush/internal/message"
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
		require.Equal(t, "⏳ Refactor the config layer", u.windowTitle())
	})

	t.Run("busy without text thinks", func(t *testing.T) {
		t.Parallel()
		u := newGoalUI()
		u.agentBusyCache.set(true)
		require.Equal(t, "💭 Refactor the config layer", u.windowTitle())
	})

	t.Run("busy streaming text responds", func(t *testing.T) {
		t.Parallel()
		u := newGoalUI()
		u.agentBusyCache.set(true)
		u.chat.AppendMessages(chat.NewAssistantMessageItem(
			u.com.Styles, assistantMessage("a1", "Here is the plan...", false),
		))
		require.Equal(t, "💬 Refactor the config layer", u.windowTitle())
	})

	t.Run("finished assistant text stops responding", func(t *testing.T) {
		t.Parallel()
		u := newGoalUI()
		u.agentBusyCache.set(true)
		u.chat.AppendMessages(chat.NewAssistantMessageItem(
			u.com.Styles, assistantMessage("a1", "Done.", true),
		))
		require.Equal(t, "💭 Refactor the config layer", u.windowTitle())
	})

	t.Run("question pending", func(t *testing.T) {
		t.Parallel()
		u := newGoalUI()
		u.activeInline = &dialog.QuestionForm{}
		require.Equal(t, "❔ Refactor the config layer", u.windowTitle())
	})

	t.Run("blocked goal", func(t *testing.T) {
		t.Parallel()
		u := newGoalUI()
		u.goal.Status = goal.StatusBlocked
		require.Equal(t, "⛔ Refactor the config layer", u.windowTitle())
	})

	t.Run("stalled goal", func(t *testing.T) {
		t.Parallel()
		u := newGoalUI()
		u.goal.Status = goal.StatusStalled
		require.Equal(t, "💤 Refactor the config layer", u.windowTitle())
	})

	t.Run("complete goal", func(t *testing.T) {
		t.Parallel()
		u := newGoalUI()
		u.goal.Status = goal.StatusComplete
		require.Equal(t, "✅ Refactor the config layer", u.windowTitle())
	})

	t.Run("paused trumps busy", func(t *testing.T) {
		t.Parallel()
		u := newGoalUI()
		u.agentBusyCache.set(true)
		u.pausedActive = true
		require.Equal(t, "⏸️ Refactor the config layer", u.windowTitle())
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
