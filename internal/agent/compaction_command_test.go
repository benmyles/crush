package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/compaction"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/stretchr/testify/require"
)

// TestPublishCompactionProgress_ForwardsTokenStats verifies the progress
// publisher forwards live token stats as compaction_progress notifications,
// and skips the initial span event (nothing composed yet) so the pill does
// not show a ↓ count before any work is accounted.
func TestPublishCompactionProgress_ForwardsTokenStats(t *testing.T) {
	t.Parallel()
	broker := pubsub.NewBroker[notify.Notification]()
	a := &sessionAgent{notify: broker}
	notifications := broker.Subscribe(context.Background())

	a.publishCompactionProgress("s1", compaction.Progress{Phase: "checkpoint", SpanTokens: 5000, TokensOut: 800, TokensDown: 4200})
	ev := <-notifications
	require.Equal(t, notify.TypeCompactionProgress, ev.Payload.Type)
	require.Equal(t, "s1", ev.Payload.SessionID)
	require.EqualValues(t, 4200, ev.Payload.TokensDown)
	require.EqualValues(t, 800, ev.Payload.TokensOut)

	// The span event (TokensOut == 0) must not be published.
	a.publishCompactionProgress("s1", compaction.Progress{Phase: "span", SpanTokens: 5000})
	select {
	case ev := <-notifications:
		t.Fatalf("span event must be skipped, got %+v", ev.Payload)
	case <-time.After(50 * time.Millisecond):
	}
}

// TestPublishCompactionStream_ForwardsLaneEvents verifies the stream
// publisher maps engine lane events onto compaction_stream notifications for
// the checkpoint lane and drops anything else.
func TestPublishCompactionStream_ForwardsLaneEvents(t *testing.T) {
	t.Parallel()
	broker := pubsub.NewBroker[notify.Notification]()
	a := &sessionAgent{notify: broker}
	notifications := broker.Subscribe(context.Background())

	a.publishCompactionStream("s1", compaction.StreamEvent{
		Kind: compaction.StreamTextDelta, Lane: compaction.LaneCheckpoint, Text: "## Goal",
	})
	ev := <-notifications
	require.Equal(t, notify.TypeCompactionStream, ev.Payload.Type)
	require.Equal(t, "s1", ev.Payload.SessionID)
	require.NotNil(t, ev.Payload.CompactionStream)
	require.Equal(t, notify.CompactionStreamTextDelta, ev.Payload.CompactionStream.Kind)
	require.Equal(t, compaction.LaneCheckpoint, ev.Payload.CompactionStream.Lane)
	require.Equal(t, "## Goal", ev.Payload.CompactionStream.Text)

	// Non-checkpoint lanes and unknown kinds must not publish.
	a.publishCompactionStream("s1", compaction.StreamEvent{Kind: compaction.StreamTextDelta, Lane: "verification", Text: "x"})
	a.publishCompactionStream("s1", compaction.StreamEvent{Kind: compaction.StreamKind(99), Lane: compaction.LaneCheckpoint})
	select {
	case ev := <-notifications:
		t.Fatalf("unexpected notification: %+v", ev.Payload)
	case <-time.After(50 * time.Millisecond):
	}
}

// TestCompact_EngineDisabledReturnsError verifies the /compact entry point
// fails loudly (no legacy fallback) when the engine is not wired.
func TestCompact_EngineDisabledReturnsError(t *testing.T) {
	a, _, sessions, _, _ := newCompactionTestAgent(t, func(context.Context, string, string, int64) (string, string, error) {
		return "", "", nil
	})
	a.compaction = nil
	sess, err := sessions.Create(context.Background(), "no-engine")
	require.NoError(t, err)

	err = a.Compact(context.Background(), sess.ID, fantasy.ProviderOptions{}, nil)
	require.Error(t, err, "Compact must not fall back to the legacy summarizer")
	require.Contains(t, err.Error(), "compaction")
}

// TestCompact_RunsEngineAndAttachesOverview runs a full engine compaction
// through the /compact entry point and asserts the digest part rides on the
// summary message plus the started/finished notifications publish in order.
func TestCompact_RunsEngineAndAttachesOverview(t *testing.T) {
	completer := func(_ context.Context, _, input string, _ int64) (string, string, error) {
		return `## Goal & User Intent
- Compact the session.

## Constraints
- [C1] Lossless.

## Key Decisions
- [D1] Tree overview.

## Dead Ends
- [X1] None.

## Open Questions
- [Q1] None.

## Progress
### Done
- Store the summary.
- Retain the tail.

### In Progress
- Pulse indicator.

### Blocked

## Next Action
- Ship it.
`, "stop", nil
	}
	a, _, sessions, messages, _ := newCompactionTestAgent(t, completer)
	broker := pubsub.NewBroker[notify.Notification]()
	a.notify = broker
	notifications := broker.Subscribe(context.Background())

	ctx := context.Background()
	sess, err := sessions.Create(ctx, "compact-cmd")
	require.NoError(t, err)

	// ~12 turns of large messages so the engine has material to compact.
	for n := 1; n <= 12; n++ {
		body := strings.Repeat(fmt.Sprintf("MARK-%02d ", n), 2000)
		_, err := messages.Create(ctx, sess.ID, message.CreateMessageParams{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: body}}})
		require.NoError(t, err)
		_, err = messages.Create(ctx, sess.ID, message.CreateMessageParams{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: body}, message.Finish{Reason: message.FinishReasonEndTurn}}})
		require.NoError(t, err)
	}

	require.NoError(t, a.Compact(ctx, sess.ID, fantasy.ProviderOptions{}, nil))

	all, err := messages.List(ctx, sess.ID)
	require.NoError(t, err)
	var summaryMessage *message.Message
	for i := range all {
		if all[i].IsSummaryMessage {
			summaryMessage = &all[i]
		}
	}
	require.NotNil(t, summaryMessage, "the engine must record its summary as a summary message")
	part, ok := summaryMessage.CompactionPart()
	require.True(t, ok, "the engine summary must carry the CompactionContent digest")
	require.NotEmpty(t, part.SummaryID)
	require.Equal(t, 0, part.Level, "completer converged at the first level")
	require.Greater(t, part.TokenCount, int64(0))
	require.Greater(t, part.TokensBefore, int64(0))
	require.Greater(t, part.CompactedMessages, 0)
	require.Equal(t, 1, part.Checkpoint.Goals)
	require.Equal(t, 1, part.Checkpoint.Constraints)
	require.Equal(t, 1, part.Checkpoint.Decisions)
	require.Equal(t, 1, part.Checkpoint.DeadEnds)
	require.Equal(t, 1, part.Checkpoint.Questions)
	require.Equal(t, 2, part.Checkpoint.Done)
	require.Equal(t, 1, part.Checkpoint.InProgress)
	require.Equal(t, 0, part.Checkpoint.Blocked)
	require.Equal(t, 1, part.Checkpoint.NextActions)

	// The pulse pill rides on started/finished; live stream events (lane
	// resets and, with a streaming model, reasoning/text deltas) publish
	// between them.
	var got []notify.Notification
collect:
	for {
		select {
		case ev := <-notifications:
			n := ev.Payload
			got = append(got, n)
			if n.Type == notify.TypeCompactionFinished {
				break collect
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for compaction notifications, got %v", got)
		}
	}
	require.NotEmpty(t, got)
	require.Equal(t, notify.TypeCompactionStarted, got[0].Type)
	require.Equal(t, notify.TypeCompactionFinished, got[len(got)-1].Type)
	streamed := false
	for _, n := range got[1 : len(got)-1] {
		require.Equal(t, notify.TypeCompactionStream, n.Type, "only stream events publish between started and finished")
		if n.CompactionStream != nil && n.CompactionStream.Kind == notify.CompactionStreamReset && n.CompactionStream.Lane == compaction.LaneCheckpoint {
			streamed = true
		}
	}
	require.True(t, streamed, "the checkpoint lane must publish live stream events")
}

// TestCompact_PublishesFinishedIndependentlyOfQueueDrain verifies the
// finished notification is emitted the moment the engine completes, not
// deferred behind a queued prompt: the finished event appears exactly once
// even when the drained prompt fails to run, and the drain error propagates
// without suppressing the finished event.
func TestCompact_PublishesFinishedIndependentlyOfQueueDrain(t *testing.T) {
	completer := func(_ context.Context, _, _ string, _ int64) (string, string, error) {
		return "## Goal & User Intent\nG\n## Progress\n### Done\n- x\n## Next Action\n1. y", "stop", nil
	}
	a, _, sessions, messages, _ := newCompactionTestAgent(t, completer)
	broker := pubsub.NewBroker[notify.Notification]()
	a.notify = broker
	notifications := broker.Subscribe(context.Background())

	ctx := context.Background()
	sess, err := sessions.Create(ctx, "drain-order")
	require.NoError(t, err)
	for n := 1; n <= 12; n++ {
		body := strings.Repeat(fmt.Sprintf("MARK-%02d ", n), 2000)
		_, err := messages.Create(ctx, sess.ID, message.CreateMessageParams{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: body}}})
		require.NoError(t, err)
		_, err = messages.Create(ctx, sess.ID, message.CreateMessageParams{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: body}, message.Finish{Reason: message.FinishReasonEndTurn}}})
		require.NoError(t, err)
	}

	// Two queued prompts that fail Run validation instantly (no prompt, no
	// attachment): the drain dequeues them and the error surfaces.
	a.messageQueue.Set(sess.ID, []SessionAgentCall{
		{SessionID: sess.ID},
		{SessionID: sess.ID},
	})

	err = a.Compact(ctx, sess.ID, fantasy.ProviderOptions{}, nil)
	require.Error(t, err, "the queued prompt fails validation, proving the drain ran after the engine finished")

	// The drain consumed exactly the first queued prompt.
	remaining, ok := a.messageQueue.Get(sess.ID)
	require.True(t, ok)
	require.Len(t, remaining, 1)

	// Collected notifications must show started ... finished, with finished
	// published exactly once despite the drain error.
	var got []notify.Notification
	drain := notifications
collect:
	for {
		select {
		case ev := <-drain:
			got = append(got, ev.Payload)
			if ev.Payload.Type == notify.TypeCompactionFinished {
				break collect
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for compaction notifications, got %v", got)
		}
	}
	require.Equal(t, notify.TypeCompactionStarted, got[0].Type)
	finishedCount := 0
	for _, n := range got {
		if n.Type == notify.TypeCompactionFinished {
			finishedCount++
		}
	}
	require.Equal(t, 1, finishedCount, "finished must publish exactly once")
	require.Equal(t, notify.TypeCompactionFinished, got[len(got)-1].Type)
}

// TestSummarize_FallsBackToLegacyWhenEngineMissing keeps the legacy path
// reachable through the original entry point when the engine is absent.
func TestSummarize_FallsBackToLegacyWhenEngineMissing(t *testing.T) {
	a, _, sessions, _, _ := newCompactionTestAgent(t, func(context.Context, string, string, int64) (string, string, error) {
		return "", "", nil
	})
	a.compaction = nil

	sess, err := sessions.Create(context.Background(), "legacy")
	require.NoError(t, err)

	// With no messages the legacy path is a no-op success, not an error.
	require.NoError(t, a.Summarize(context.Background(), sess.ID, fantasy.ProviderOptions{}, nil))
}
