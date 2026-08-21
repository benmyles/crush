package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingPublisher collects notifications for assertions without a
// subscription loop.
type recordingPublisher struct {
	mu      sync.Mutex
	events  []notify.Notification
	publish pubsub.EventType
}

func (r *recordingPublisher) Publish(t pubsub.EventType, payload notify.Notification) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.publish = t
	r.events = append(r.events, payload)
}

func (r *recordingPublisher) PublishMustDeliver(_ context.Context, t pubsub.EventType, payload notify.Notification) {
	r.Publish(t, payload)
}

func (r *recordingPublisher) snapshot() (pubsub.EventType, []notify.Notification) {
	r.mu.Lock()
	defer r.mu.Unlock()
	events := make([]notify.Notification, len(r.events))
	copy(events, r.events)
	return r.publish, events
}

// TestCoordinatorSubAgentRegistry verifies the live sub-agent registry:
// child sessions are visible to IsSessionBusy, Cancel, and
// SendSubAgentMessage for the duration of the run, and the started and
// finished notifications bracket it.
func TestCoordinatorSubAgentRegistry(t *testing.T) {
	const providerID = "test-provider"
	env := testEnv(t)
	coord := newTestCoordinator(t, env, providerID, config.ProviderConfig{ID: providerID})
	rec := &recordingPublisher{}
	coord.notify = rec

	parentSession, err := env.sessions.Create(t.Context(), "Parent")
	require.NoError(t, err)

	// Once the registry entry is gone, IsSessionBusy degrades to the
	// coordinator's primary agent, which this coordinator normally
	// never leaves nil.
	coord.currentAgent = newMockAgent(providerID, 4096, func(_ context.Context, _ SessionAgentCall) (*fantasy.AgentResult, error) {
		return agentResultWithText("fallback"), nil
	})

	started := make(chan struct{})
	release := make(chan struct{})
	agent := newMockAgent(providerID, 4096, func(ctx context.Context, _ SessionAgentCall) (*fantasy.AgentResult, error) {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return agentResultWithText("done"), nil
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := coord.runSubAgent(t.Context(), subAgentParams{
			Agent:          agent,
			SessionID:      parentSession.ID,
			AgentMessageID: "msg-1",
			ToolCallID:     "call-1",
			Prompt:         "do something",
			SessionTitle:   "Test Session",
			Kind:           AgentToolName,
		})
		assert.NoError(t, err)
	}()

	<-started

	// The run is registered under its child session ID and visible to the
	// coordinator's busy probes.
	var childSessionID string
	require.Eventually(t, func() bool {
		for sessionID := range coord.subAgents.Seq2() {
			childSessionID = sessionID
			return true
		}
		return false
	}, 2*time.Second, 5*time.Millisecond)
	require.NotEmpty(t, childSessionID)
	assert.True(t, coord.IsSessionBusy(childSessionID))

	// Interim messages route to the child run's agent.
	require.NoError(t, coord.SendSubAgentMessage(t.Context(), childSessionID, "heads up"))
	assert.Equal(t, []interimMessage{{sessionID: childSessionID, text: "heads up"}}, agent.interim)

	// Cancel reaches the child session through the registry.
	coord.Cancel(childSessionID)
	assert.Equal(t, []string{childSessionID}, agent.cancelled)

	close(release)
	<-done

	// Registration is lifted after the run returns.
	assert.False(t, coord.IsSessionBusy(childSessionID))
	_, ok := coord.subAgents.Get(childSessionID)
	assert.False(t, ok)

	// The lifecycle notifications bracket the run.
	_, events := rec.snapshot()
	require.Len(t, events, 2)
	assert.Equal(t, notify.TypeSubAgentStarted, events[0].Type)
	assert.Equal(t, AgentToolName, events[0].SubAgentKind)
	assert.Equal(t, "do something", events[0].SubAgentPrompt)
	assert.Equal(t, childSessionID, events[0].SessionID)
	assert.Equal(t, notify.TypeSubAgentFinished, events[1].Type)
	assert.Equal(t, childSessionID, events[1].SessionID)
}

// TestCoordinatorSendSubAgentMessageUnknownSession errors for sessions
// without a live registry entry.
func TestCoordinatorSendSubAgentMessageUnknownSession(t *testing.T) {
	env := testEnv(t)
	coord := newTestCoordinator(t, env, "test-provider", config.ProviderConfig{ID: "test-provider"})
	err := coord.SendSubAgentMessage(t.Context(), "ghost-session", "nope")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no running sub-agent")
}
