package agent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRemoveQueuedPromptRemovesOnlyTargetedItem verifies QueueID
// assignment on enqueue and that removing one item leaves the others in
// order.
func TestRemoveQueuedPromptRemovesOnlyTargetedItem(t *testing.T) {
	t.Parallel()
	sa, env := newStreamTestAgent(t)

	sess, err := env.sessions.Create(t.Context(), "session")
	require.NoError(t, err)

	sa.enqueueCall(SessionAgentCall{SessionID: sess.ID, Prompt: "first"})
	sa.enqueueCall(SessionAgentCall{SessionID: sess.ID, Prompt: "second"})
	sa.enqueueCall(SessionAgentCall{SessionID: sess.ID, Prompt: "third"})

	items := sa.QueuedPromptsList(sess.ID)
	require.Len(t, items, 3)
	require.Equal(t, uint64(1), items[0].QueueID)
	require.Equal(t, "first", items[0].Prompt)
	require.Equal(t, uint64(3), items[2].QueueID)

	require.True(t, sa.RemoveQueuedPrompt(sess.ID, items[1].QueueID))

	remaining := sa.QueuedPromptsList(sess.ID)
	require.Len(t, remaining, 2)
	assert.Equal(t, "first", remaining[0].Prompt)
	assert.Equal(t, "third", remaining[1].Prompt)
}

// TestRemoveQueuedPromptUnknownIDReturnsFalse verifies a stale queue ID
// (already drained, or from another session) is a no-op that leaves the
// queue intact.
func TestRemoveQueuedPromptUnknownIDReturnsFalse(t *testing.T) {
	t.Parallel()
	sa, env := newStreamTestAgent(t)

	sess, err := env.sessions.Create(t.Context(), "session")
	require.NoError(t, err)

	sa.enqueueCall(SessionAgentCall{SessionID: sess.ID, Prompt: "only"})

	require.False(t, sa.RemoveQueuedPrompt(sess.ID, 999))
	require.False(t, sa.RemoveQueuedPrompt("other-session", 1))
	require.Equal(t, 1, sa.QueuedPrompts(sess.ID))
}

// TestRemoveQueuedPromptPublishesCanceledRunCompleteForRunID verifies a
// caller waiting on the removed prompt's RunID (e.g. `crush run`) is not
// left hanging: removal publishes a terminal cancelled RunComplete.
func TestRemoveQueuedPromptPublishesCanceledRunCompleteForRunID(t *testing.T) {
	t.Parallel()
	sa, env, broker := newCancelTestAgentWithRunComplete(t)

	sess, err := env.sessions.Create(t.Context(), "session")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ch := broker.Subscribe(ctx)

	sa.enqueueCall(SessionAgentCall{SessionID: sess.ID, RunID: "run-queued", Prompt: "queued"})
	items := sa.QueuedPromptsList(sess.ID)
	require.Len(t, items, 1)

	require.True(t, sa.RemoveQueuedPrompt(sess.ID, items[0].QueueID))

	select {
	case got := <-ch:
		assert.Equal(t, "run-queued", got.Payload.RunID,
			"RunComplete must echo the removed prompt's RunID")
		assert.Equal(t, sess.ID, got.Payload.SessionID)
		assert.True(t, got.Payload.Cancelled,
			"a removed queued prompt must publish a cancelled RunComplete")
	case <-time.After(2 * time.Second):
		t.Fatal("removing a RunID-bearing queued prompt must publish its cancelled RunComplete; a RunID caller would hang otherwise")
	}
	require.Equal(t, 0, sa.QueuedPrompts(sess.ID))
}
