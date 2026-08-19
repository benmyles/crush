package model

import (
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/charmbracelet/x/ansi"
)

// streamNotification builds a compaction_stream notification for a session.
func streamNotification(sessionID string, kind notify.CompactionStreamKind, text string) notify.Notification {
	return notify.Notification{
		SessionID: sessionID,
		Type:      notify.TypeCompactionStream,
		CompactionStream: &notify.CompactionStreamEvent{
			Kind: kind,
			Lane: "checkpoint",
			Text: text,
		},
	}
}

// TestLiveCompactionMessageLifecycle covers the transient chat item that
// streams a compaction's checkpoint generation: it appears on
// TypeCompactionStarted (only for the displayed session), accumulates the
// reasoning/text deltas, restarts on a reset, and disappears on
// TypeCompactionFinished.
func TestLiveCompactionMessageLifecycle(t *testing.T) {
	u := newTestUI()
	u.session = &session.Session{ID: "s1"}
	u.updateLayoutAndSize()

	// A compaction of another session must not open the live item.
	_ = u.handleAgentNotification(notify.Notification{SessionID: "other", Type: notify.TypeCompactionStarted})
	if u.chat.MessageItem(chat.LiveCompactionMessageID) != nil {
		t.Fatalf("live item must not open for other sessions")
	}

	_ = u.handleAgentNotification(notify.Notification{SessionID: "s1", Type: notify.TypeCompactionStarted})
	item := u.chat.MessageItem(chat.LiveCompactionMessageID)
	if item == nil {
		t.Fatalf("expected the live compaction item in the chat")
	}
	live, ok := item.(*chat.CompactionLiveItem)
	if !ok {
		t.Fatalf("expected *chat.CompactionLiveItem, got %T", item)
	}

	// Reasoning and text deltas stream into the rendered item.
	_ = u.handleAgentNotification(streamNotification("s1", notify.CompactionStreamReasoningDelta, "thinking about the summary..."))
	_ = u.handleAgentNotification(streamNotification("s1", notify.CompactionStreamReasoningEnd, ""))
	_ = u.handleAgentNotification(streamNotification("s1", notify.CompactionStreamTextDelta, "## Goal"))
	rendered := ansi.Strip(live.Render(u.width))
	if !strings.Contains(rendered, "thinking about the summary...") {
		t.Fatalf("expected reasoning delta in render:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Goal") {
		t.Fatalf("expected text delta in render:\n%s", rendered)
	}

	// A reset (escalation/retry) starts a fresh slate.
	_ = u.handleAgentNotification(streamNotification("s1", notify.CompactionStreamReset, ""))
	_ = u.handleAgentNotification(streamNotification("s1", notify.CompactionStreamTextDelta, "fresh checkpoint"))
	rendered = ansi.Strip(live.Render(u.width))
	if strings.Contains(rendered, "Goal") {
		t.Fatalf("reset must drop previous output:\n%s", rendered)
	}
	if !strings.Contains(rendered, "fresh checkpoint") {
		t.Fatalf("expected post-reset text in render:\n%s", rendered)
	}

	// Stream events for other sessions are ignored.
	_ = u.handleAgentNotification(streamNotification("other", notify.CompactionStreamTextDelta, "intruder"))
	rendered = ansi.Strip(live.Render(u.width))
	if strings.Contains(rendered, "intruder") {
		t.Fatalf("stream events for other sessions must be ignored:\n%s", rendered)
	}

	// Finished removes the transient item.
	_ = u.handleAgentNotification(notify.Notification{SessionID: "s1", Type: notify.TypeCompactionFinished})
	if u.chat.MessageItem(chat.LiveCompactionMessageID) != nil {
		t.Fatalf("live item must be removed when compaction finishes")
	}
	if u.compactLive != nil || u.compactMsg != nil {
		t.Fatalf("live compaction state must clear on finish")
	}

	// clearCompactionState (also used as the TypeAgentFinished failsafe)
	// clears pill state and the live item.
	u.compacting = true
	u.compactTokensDown = 123
	u.clearCompactionState()
	if u.compacting {
		t.Fatalf("clearCompactionState must clear the compaction pulse")
	}
	if u.compactTokensDown != 0 {
		t.Fatalf("clearCompactionState must clear live token stats")
	}
}

// TestCompactionPillClearsOnSummaryArrival covers the failsafe in
// setSessionMessages: when the persisted compaction summary message arrives
// (even if the finished notification is lost or late), the pulsing
// "Compacting" pill must clear.
func TestCompactionPillClearsOnSummaryArrival(t *testing.T) {
	ws := &countingWorkspace{ready: true, agentBusy: false}
	u := newBusyUI(ws)

	u.compacting = true
	u.compactTokensDown = 123

	summaryMsg := message.Message{
		ID:               "sum-1",
		Role:             message.Assistant,
		SessionID:        "s1",
		IsSummaryMessage: true,
		Parts: []message.ContentPart{
			message.TextContent{Text: "summary text"},
			message.CompactionContent{SummaryID: "sum-1"},
		},
	}
	_ = u.setSessionMessages([]message.Message{summaryMsg})

	if u.compacting {
		t.Fatalf("the Compacting pill must clear when the summary message arrives")
	}
	if u.compactTokensDown != 0 {
		t.Fatalf("live token stats must clear with the pill")
	}
	if u.chat.MessageItem(chat.LiveCompactionMessageID) != nil {
		t.Fatalf("the live stream item must not survive the summary rebuild")
	}

	// The live arrival path (appendSessionMessage) must clear it too.
	u.compacting = true
	u.compactTokensDown = 42
	_ = u.appendSessionMessage(message.Message{
		ID:               "sum-2",
		Role:             message.Assistant,
		SessionID:        "s1",
		IsSummaryMessage: true,
		Parts: []message.ContentPart{
			message.TextContent{Text: "summary text"},
			message.CompactionContent{SummaryID: "sum-2"},
		},
	})
	if u.compacting {
		t.Fatalf("the live compaction summary arrival must clear the pill")
	}
	if u.compactTokensDown != 0 {
		t.Fatalf("live token stats must clear on live arrival")
	}
}

// TestLiveCompactionItemFrameRenders verifies the live item wraps its render
// in the CompactionLiveBox frame.
func TestLiveCompactionItemFrameRenders(t *testing.T) {
	u := newTestUI()
	u.session = &session.Session{ID: "s1"}
	u.updateLayoutAndSize()

	_ = u.handleAgentNotification(notify.Notification{SessionID: "s1", Type: notify.TypeCompactionStarted})
	_ = u.handleAgentNotification(streamNotification("s1", notify.CompactionStreamTextDelta, "body"))
	item := u.chat.MessageItem(chat.LiveCompactionMessageID)
	rendered := strings.TrimSpace(ansi.Strip(item.Render(u.width)))
	if !strings.HasPrefix(rendered, "╭") || !strings.HasSuffix(rendered, "╯") {
		t.Fatalf("live item must render inside a rounded frame:\n%s", rendered)
	}
}
