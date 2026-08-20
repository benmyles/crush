package model

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/goal"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/util"
	"github.com/charmbracelet/crush/internal/workspace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// goalWorkspace is a workspace stub capturing goal/pause calls.
type goalWorkspace struct {
	workspace.Workspace

	goals    map[string]goal.Goal
	setCalls int
	goalSet  map[string]string

	pauseCalled   bool
	pauseResult   bool
	resumeCalled  bool
	paused        bool
	isPausedCalls int
}

func (w *goalWorkspace) GoalSet(_ context.Context, sessionID, text string) error {
	w.setCalls++
	w.goalSet[sessionID] = text
	w.goals[sessionID] = goal.Goal{SessionID: sessionID, Text: text, Status: goal.StatusActive}
	return nil
}

func (w *goalWorkspace) GoalGet(_ context.Context, sessionID string) (goal.Goal, error) {
	return w.goals[sessionID], nil
}

func (w *goalWorkspace) GoalResume(_ context.Context, sessionID string) error {
	g := w.goals[sessionID]
	g.Status = goal.StatusActive
	w.goals[sessionID] = g
	return nil
}

func (w *goalWorkspace) GoalClear(_ context.Context, sessionID string) error {
	delete(w.goals, sessionID)
	return nil
}

func (w *goalWorkspace) AgentPause() bool {
	w.pauseCalled = true
	return w.pauseResult
}

func (w *goalWorkspace) AgentResume() {
	w.resumeCalled = true
}

func (w *goalWorkspace) AgentIsPaused(string) bool {
	w.isPausedCalls++
	return w.paused
}

func newGoalUI(ws *goalWorkspace) *UI {
	return newBusyUI(&countingWorkspace{Workspace: ws})
}

// runCmd executes a tea.Cmd fully and returns the produced message.
func runCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	ch := make(chan tea.Msg, 1)
	go func() {
		msg := cmd()
		ch <- msg
	}()
	return <-ch
}

func TestTrySlashCommandGoalVariants(t *testing.T) {
	ws := &goalWorkspace{goals: map[string]goal.Goal{}, goalSet: map[string]string{}}
	u := newGoalUI(ws)
	u.agentBusyCache.set(false)

	// /goal with text sets the goal and yields a send continuation.
	cmd, handled := u.trySlashCommand("/goal Keep working until done")
	require.True(t, handled)
	require.NotNil(t, cmd)
	msg := runCmd(t, cmd)
	gsm, ok := msg.(goalSetSuccessMsg)
	require.True(t, ok, "expected goalSetSuccessMsg, got %T", msg)
	assert.Equal(t, "Keep working until done", gsm.text)
	assert.Equal(t, "Keep working until done", ws.goalSet["s1"])
	assert.Equal(t, 1, ws.setCalls)

	// /goal:show dispatches a fetch with the show flag.
	cmd, handled = u.trySlashCommand("/goal:show")
	require.True(t, handled)
	require.NotNil(t, cmd)
	fetched := runCmd(t, cmd)
	gfm, ok := fetched.(goalFetchedMsg)
	require.True(t, ok)
	assert.True(t, gfm.show)

	// /goal:clear clears and emits goalClearedMsg.
	cmd, handled = u.trySlashCommand("/goal:clear")
	require.True(t, handled)
	require.NotNil(t, cmd)
	cleared := runCmd(t, cmd)
	_, ok = cleared.(goalClearedMsg)
	require.True(t, ok)
	assert.Empty(t, ws.goals)
}

func TestTrySlashCommandPauseResume(t *testing.T) {
	ws := &goalWorkspace{goals: map[string]goal.Goal{}, goalSet: map[string]string{}}
	u := newGoalUI(ws)
	u.agentBusyCache.set(false)

	cmd, handled := u.trySlashCommand("/pause")
	require.True(t, handled)
	require.NotNil(t, cmd)
	assert.True(t, u.pausedActive, "pause pill must flip optimistically")
	_ = runCmd(t, cmd)
	assert.True(t, ws.pauseCalled)

	cmd, handled = u.trySlashCommand("/resume")
	require.True(t, handled)
	require.NotNil(t, cmd)
	assert.False(t, u.pausedActive)
	_ = runCmd(t, cmd)
	assert.True(t, ws.resumeCalled)

	// Ordinary prompts are not intercepted.
	cmd, handled = u.trySlashCommand("keep going")
	assert.False(t, handled)
	assert.Nil(t, cmd)

	// Unknown slash commands fall through to the normal prompt send.
	cmd, handled = u.trySlashCommand("/something-else")
	assert.False(t, handled)
	assert.Nil(t, cmd)
}

func TestTrySlashGoalWithoutSession(t *testing.T) {
	ws := &goalWorkspace{goals: map[string]goal.Goal{}, goalSet: map[string]string{}}
	u := newGoalUI(ws)
	u.session = nil

	cmd, handled := u.trySlashCommand("/goal do the thing")
	require.True(t, handled)
	require.NotNil(t, cmd)
	msg := runCmd(t, cmd)
	assert.IsType(t, util.InfoMsg{}, msg)
	assert.Equal(t, 0, ws.setCalls, "no goal may be set without a session")
}

func TestGoalShowDialogOpensFromFetchedState(t *testing.T) {
	ws := &goalWorkspace{goals: map[string]goal.Goal{}, goalSet: map[string]string{}}
	u := newGoalUI(ws)
	u.agentBusyCache.set(false)
	ws.goals["s1"] = goal.Goal{SessionID: "s1", Text: "ship it", Status: goal.StatusActive}

	cmd, handled := u.trySlashCommand("/goal:show")
	require.True(t, handled)
	fetched := runCmd(t, cmd)
	gfm := fetched.(goalFetchedMsg)

	u.handleGoalFetched(gfm)

	assert.Equal(t, goal.StatusActive, u.goal.Status)
	assert.Equal(t, "ship it", u.goal.Text)
	assert.True(t, u.dialog.ContainsDialog(dialog.GoalShowID))
}
