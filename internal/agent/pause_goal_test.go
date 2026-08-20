package agent

import (
	"context"
	"database/sql"
	"sync/atomic"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/notify"
	agenttools "github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/goal"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/stretchr/testify/require"
)

// pauseStreamModel yields one tool step on its first Stream call (which
// the blocking noop tool can hold open) and a plain final text turn on
// every later call. This reproduces a mid-run pause boundary.
type pauseStreamModel struct {
	calls atomic.Int64
}

func (m *pauseStreamModel) Provider() string { return "fake" }
func (m *pauseStreamModel) Model() string    { return "fake-model" }

func (m *pauseStreamModel) Generate(ctx context.Context, call fantasy.Call) (*fantasy.Response, error) {
	return &fantasy.Response{
		Content:      fantasy.ResponseContent{fantasy.TextContent{Text: "all done"}},
		FinishReason: fantasy.FinishReasonStop,
	}, nil
}

func (m *pauseStreamModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	n := m.calls.Add(1)
	return func(yield func(fantasy.StreamPart) bool) {
		if n == 1 {
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolInputStart, ID: "t1", ToolCallName: "blocked_noop"}) {
				return
			}
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolInputDelta, ID: "t1", Delta: `{}`}) {
				return
			}
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolInputEnd, ID: "t1"}) {
				return
			}
			if !yield(fantasy.StreamPart{
				Type:          fantasy.StreamPartTypeToolCall,
				ID:            "t1",
				ToolCallName:  "blocked_noop",
				ToolCallInput: `{}`,
			}) {
				return
			}
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonToolCalls})
			return
		}
		if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: "1"}) {
			return
		}
		if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: "1", Delta: "all done"}) {
			return
		}
		if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextEnd, ID: "1"}) {
			return
		}
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop})
	}, nil
}

func (m *pauseStreamModel) GenerateObject(ctx context.Context, call fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, nil
}

func (m *pauseStreamModel) StreamObject(ctx context.Context, call fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}

// goalStore helper attaches the goals table to a test SQLite connection.
func goalStore(t *testing.T, conn *sql.DB) *goal.Store {
	t.Helper()
	_, err := conn.ExecContext(context.Background(), `CREATE TABLE IF NOT EXISTS goals (
		session_id TEXT PRIMARY KEY,
		text TEXT NOT NULL,
		status TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		complete_reason TEXT,
		blocked_reason TEXT,
		consecutive_prods INTEGER NOT NULL DEFAULT 0,
		total_prods INTEGER NOT NULL DEFAULT 0
	)`)
	require.NoError(t, err)
	return goal.NewStore(conn)
}

// goalTestEnv builds a test environment whose session database also hosts
// the goals table (same connection), so goal rows reference real sessions.
func goalTestEnv(t *testing.T) (fakeEnv, *goal.Store) {
	t.Helper()
	env := testEnv(t)
	workingDir := t.TempDir()
	conn, err := db.Connect(t.Context(), workingDir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	q := db.New(conn)
	env.sessions = session.NewService(q, conn)
	env.messages = message.NewService(q)

	return env, goalStore(t, conn)
}

// TestRunPauseStopsAtStepBoundaryAndResumeContinues proves the pause
// contract end to end: a latched pause stops the active run right after
// its current tool step returns, holds a silent continuation, and Resume
// replays the turn without a duplicate user message.
func TestRunPauseStopsAtStepBoundaryAndResumeContinues(t *testing.T) {
	t.Parallel()

	env := testEnv(t)
	notifyBroker := pubsub.NewBroker[notify.Notification]()
	t.Cleanup(notifyBroker.Shutdown)
	runBroker := pubsub.NewBroker[notify.RunComplete]()
	t.Cleanup(runBroker.Shutdown)

	notifyCh := notifyBroker.Subscribe(t.Context())
	runCh := runBroker.Subscribe(t.Context())

	toolEntered := make(chan struct{})
	toolGate := make(chan struct{})
	noop := fantasy.NewAgentTool(
		"blocked_noop",
		"A tool that blocks until released, so tests can pause mid-step.",
		func(ctx context.Context, params struct{}, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			close(toolEntered)
			select {
			case <-toolGate:
			case <-ctx.Done():
			}
			return fantasy.NewTextResponse("ok"), nil
		},
	)

	sa := NewSessionAgent(SessionAgentOptions{
		LargeModel:  Model{Model: &pauseStreamModel{}, CatwalkCfg: catwalk.Model{ContextWindow: 200000, DefaultMaxTokens: 10000}},
		SmallModel:  Model{Model: &finishStreamModel{text: "title"}, CatwalkCfg: catwalk.Model{ContextWindow: 200000, DefaultMaxTokens: 10000}},
		IsYolo:      true,
		Sessions:    env.sessions,
		Messages:    env.messages,
		Tools:       []fantasy.AgentTool{noop},
		Notify:      notifyBroker,
		RunComplete: runBroker,
	}).(*sessionAgent)

	sess, err := env.sessions.Create(t.Context(), "pause session")
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() {
		_, runErr := sa.Run(t.Context(), SessionAgentCall{
			SessionID: sess.ID,
			RunID:     "run-1",
			Prompt:    "keep going",
		})
		done <- runErr
	}()

	// Wait until the tool step is executing, then latch the pause. The
	// step still completes (tools that started are never cut off), but
	// the stream must stop at the step boundary.
	select {
	case <-toolEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("tool never entered")
	}
	sa.Pause(sess.ID)
	close(toolGate)

	require.NoError(t, <-done)
	require.True(t, sa.IsPaused(sess.ID), "pause latch must survive the stopped turn")
	require.False(t, sa.IsSessionBusy(sess.ID), "a paused turn must not hold the session busy")
	require.Equal(t, 1, sa.QueuedPrompts(sess.ID), "the silent continuation must wait at the queue front")

	// The turn must not have published its terminal RunComplete yet: it
	// is suspended, not finished.
	select {
	case ev := <-runCh:
		t.Fatalf("paused turn published a premature RunComplete: %+v", ev.Payload)
	default:
	}

	// The pause event reaches observers.
	eventually := func() notify.Type {
		deadline := time.After(3 * time.Second)
		for {
			select {
			case ev := <-notifyCh:
				if ev.Payload.SessionID == sess.ID && ev.Payload.Type == notify.TypeAgentPaused {
					return ev.Payload.Type
				}
			case <-deadline:
				return ""
			}
		}
	}
	require.Equal(t, notify.TypeAgentPaused, eventually())

	// While paused, new prompts hold (they queue behind the
	// continuation), and drains are suppressed.
	res, err := sa.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "later"})
	require.NoError(t, err)
	require.Nil(t, res)
	require.Equal(t, 2, sa.QueuedPrompts(sess.ID))

	// Resume drains the continuation first, then the queued prompt,
	// without creating a second user message for the original turn.
	sa.Resume(sess.ID)
	require.False(t, sa.IsPaused(sess.ID))

	deadline := time.After(5 * time.Second)
	gotRunIDs := map[string]bool{}
	for len(gotRunIDs) < 1 {
		select {
		case ev := <-runCh:
			gotRunIDs[ev.Payload.RunID] = !ev.Payload.Cancelled && ev.Payload.Error == ""
		case <-deadline:
			t.Fatalf("timed out waiting for RunComplete; got %v", gotRunIDs)
		}
	}
	require.True(t, gotRunIDs["run-1"], "the resumed turn must publish the original RunID as complete")

	// The queued follow-up must eventually run after the continuation
	// hands off the session.
	require.Eventually(t, func() bool {
		return !sa.IsSessionBusy(sess.ID) && sa.QueuedPrompts(sess.ID) == 0
	}, 5*time.Second, 10*time.Millisecond)

	msgs, err := env.messages.List(t.Context(), sess.ID)
	require.NoError(t, err)
	userCount := 0
	var users []string
	for _, m := range msgs {
		if m.Role == message.User {
			userCount++
			users = append(users, m.Content().String())
		}
	}
	require.Equal(t, 2, userCount, "pause must not duplicate the original user message; got %v", users)
}

// TestPauseWithNoActiveRunQueuesUntilResume proves the latch also gates
// sessions without an active run: new prompts hold until Resume drains.
func TestPauseWithNoActiveRunQueuesUntilResume(t *testing.T) {
	t.Parallel()

	env := testEnv(t)
	small := &finishStreamModel{text: "title"}
	large := &pauseStreamModel{}
	// The large model should never reach the tool step in this test: the
	// run only happens after Resume.
	sa := NewSessionAgent(SessionAgentOptions{
		LargeModel: Model{Model: large, CatwalkCfg: catwalk.Model{ContextWindow: 200000, DefaultMaxTokens: 10000}},
		SmallModel: Model{Model: small, CatwalkCfg: catwalk.Model{ContextWindow: 200000, DefaultMaxTokens: 10000}},
		IsYolo:     true,
		Sessions:   env.sessions,
		Messages:   env.messages,
	}).(*sessionAgent)

	sess, err := env.sessions.Create(t.Context(), "sess")
	require.NoError(t, err)

	sa.Pause(sess.ID)
	require.True(t, sa.IsPaused(sess.ID))
	require.Empty(t, sa.BusySessions())

	res, err := sa.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "hello"})
	require.NoError(t, err)
	require.Nil(t, res, "paused sessions must queue the prompt")
	require.Equal(t, 1, sa.QueuedPrompts(sess.ID))

	sa.Resume(sess.ID)
	require.False(t, sa.IsPaused(sess.ID))
	require.Eventually(t, func() bool {
		return !sa.IsSessionBusy(sess.ID) && sa.QueuedPrompts(sess.ID) == 0
	}, 5*time.Second, 10*time.Millisecond)
}

// TestGoalCheckRunsUntilStalled proves the supervision loop schedules
// visible check turns while the goal is active and stalls after the
// consecutive-check budget is exhausted.
func TestGoalCheckRunsUntilStalled(t *testing.T) {
	t.Parallel()

	env, store := goalTestEnv(t)

	large := &finishStreamModel{text: "still working"}
	sa := NewSessionAgent(SessionAgentOptions{
		LargeModel: Model{Model: large, CatwalkCfg: catwalk.Model{ContextWindow: 200000, DefaultMaxTokens: 10000}},
		SmallModel: Model{Model: &finishStreamModel{text: "title"}, CatwalkCfg: catwalk.Model{ContextWindow: 200000, DefaultMaxTokens: 10000}},
		IsYolo:     true,
		Sessions:   env.sessions,
		Messages:   env.messages,
		Goals:      store,
	}).(*sessionAgent)

	sess, err := env.sessions.Create(t.Context(), "goal session")
	require.NoError(t, err)

	_, err = store.Set(t.Context(), sess.ID, "make every test pass")
	require.NoError(t, err)

	_, err = sa.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "start"})
	require.NoError(t, err)

	// The loop chains check turns until the consecutive budget
	// (MaxConsecutiveProds) is spent, then stalls.
	require.Eventually(t, func() bool {
		if sa.IsSessionBusy(sess.ID) || sa.QueuedPrompts(sess.ID) > 0 {
			return false
		}
		g, err := store.Get(t.Context(), sess.ID)
		return err == nil && g.Status == goal.StatusStalled
	}, 10*time.Second, 10*time.Millisecond)

	g, err := store.Get(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, goal.MaxConsecutiveProds, g.TotalProds)

	// Checks are visible transcript messages.
	msgs, err := env.messages.List(t.Context(), sess.ID)
	require.NoError(t, err)
	var checks int
	for _, m := range msgs {
		if m.Role == message.User && len(m.Content().String()) > 0 &&
			containsGoalCheck(m.Content().String()) {
			checks++
		}
	}
	require.Equal(t, goal.MaxConsecutiveProds, checks, "each check must appear as a visible user-style message")
}

// TestGoalCompleteToolStopsSupervision proves the agent can mark the goal
// complete from inside a check turn, ending the loop immediately.
func TestGoalCompleteToolStopsSupervision(t *testing.T) {
	t.Parallel()

	env, store := goalTestEnv(t)

	completeTool := agenttools.NewCompleteGoalTool(store, nil)

	// First Stream call: plain text. Second: goal_complete tool call
	// carrying the summary, which the engine executes and the goal
	// becomes terminal, so no further checks are scheduled.
	large := &goalCompleteStreamModel{}
	sa := NewSessionAgent(SessionAgentOptions{
		LargeModel: Model{Model: large, CatwalkCfg: catwalk.Model{ContextWindow: 200000, DefaultMaxTokens: 10000}},
		SmallModel: Model{Model: &finishStreamModel{text: "title"}, CatwalkCfg: catwalk.Model{ContextWindow: 200000, DefaultMaxTokens: 10000}},
		IsYolo:     true,
		Sessions:   env.sessions,
		Messages:   env.messages,
		Tools:      []fantasy.AgentTool{completeTool},
		Goals:      store,
	}).(*sessionAgent)

	sess, err := env.sessions.Create(t.Context(), "goal complete session")
	require.NoError(t, err)
	_, err = store.Set(t.Context(), sess.ID, "finish quickly")
	require.NoError(t, err)

	_, err = sa.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "go"})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		if sa.IsSessionBusy(sess.ID) {
			return false
		}
		g, err := store.Get(t.Context(), sess.ID)
		return err == nil && g.Status == goal.StatusComplete
	}, 10*time.Second, 10*time.Millisecond)

	g, err := store.Get(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, "everything shipped", g.CompleteReason)
	require.Equal(t, 1, g.TotalProds, "only one check must run: the second turn completes the goal")
}

func containsGoalCheck(s string) bool {
	if len(s) < len("[Goal check]") {
		return false
	}
	return s[:len("[Goal check]")] == "[Goal check]"
}

// goalCompleteStreamModel streams text on the first call and a
// goal_complete tool call on the second.
type goalCompleteStreamModel struct {
	calls atomic.Int64
}

func (m *goalCompleteStreamModel) Provider() string { return "fake" }
func (m *goalCompleteStreamModel) Model() string    { return "fake-model" }

func (m *goalCompleteStreamModel) Generate(ctx context.Context, call fantasy.Call) (*fantasy.Response, error) {
	return &fantasy.Response{
		Content:      fantasy.ResponseContent{fantasy.TextContent{Text: "done"}},
		FinishReason: fantasy.FinishReasonStop,
	}, nil
}

func (m *goalCompleteStreamModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	n := m.calls.Add(1)
	return func(yield func(fantasy.StreamPart) bool) {
		if n >= 2 {
			sum := `{"summary":"everything shipped"}`
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolInputStart, ID: "g1", ToolCallName: agenttools.CompleteGoalToolName}) {
				return
			}
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolInputDelta, ID: "g1", Delta: sum}) {
				return
			}
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolInputEnd, ID: "g1"}) {
				return
			}
			if !yield(fantasy.StreamPart{
				Type:          fantasy.StreamPartTypeToolCall,
				ID:            "g1",
				ToolCallName:  agenttools.CompleteGoalToolName,
				ToolCallInput: sum,
			}) {
				return
			}
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonToolCalls})
			return
		}
		if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: "1"}) {
			return
		}
		if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: "1", Delta: "working"}) {
			return
		}
		if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextEnd, ID: "1"}) {
			return
		}
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop})
	}, nil
}

func (m *goalCompleteStreamModel) GenerateObject(ctx context.Context, call fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, nil
}

func (m *goalCompleteStreamModel) StreamObject(ctx context.Context, call fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}
