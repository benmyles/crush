package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"
)

// fakeCompactContextAgent is a stub CompactContextAgent for tool tests.
type fakeCompactContextAgent struct {
	status       CompactContextStatus
	statusErr    error
	sessions     []string
	instructions string
}

func (f *fakeCompactContextAgent) RequestCompaction(sessionID, instructions string) {
	f.sessions = append(f.sessions, sessionID)
	f.instructions = instructions
}

func (f *fakeCompactContextAgent) CompactionStatus(_ context.Context, _ string) (CompactContextStatus, error) {
	return f.status, f.statusErr
}

func runCompactContextTool(t *testing.T, a CompactContextAgent, ctx context.Context, params CompactContextParams) fantasy.ToolResponse {
	t.Helper()
	tool := NewCompactContextTool(func() CompactContextAgent { return a })
	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)
	resp, err := tool.Run(ctx, fantasy.ToolCall{ID: "t1", Name: CompactContextToolName, Input: string(paramsJSON)})
	require.NoError(t, err)
	return resp
}

func TestCompactContext_DeclinesBelowSoftThreshold(t *testing.T) {
	t.Parallel()

	agent := &fakeCompactContextAgent{
		status: CompactContextStatus{
			UsageTokens:         139999, // just below the soft threshold
			ContextWindow:       200000,
			SoftThresholdTokens: 140000,
		},
	}
	ctx := context.WithValue(context.Background(), SessionIDContextKey, "sess-1")

	resp := runCompactContextTool(t, agent, ctx, CompactContextParams{})
	require.Empty(t, agent.sessions, "compaction must not be scheduled below the soft threshold")
	require.Contains(t, resp.Content, "Compaction not needed yet")
	require.Contains(t, resp.Content, "139999 tokens")
	require.Contains(t, resp.Content, "soft threshold of 140000 tokens")
	require.Contains(t, resp.Content, "automatically")
	require.Contains(t, resp.Content, "again later")
}

func TestCompactContext_SchedulesAtOrAboveSoftThreshold(t *testing.T) {
	t.Parallel()

	agent := &fakeCompactContextAgent{
		status: CompactContextStatus{
			UsageTokens:         140000,
			ContextWindow:       200000,
			SoftThresholdTokens: 140000,
		},
	}
	ctx := context.WithValue(context.Background(), SessionIDContextKey, "sess-1")

	resp := runCompactContextTool(t, agent, ctx, CompactContextParams{Instructions: "focus on files"})
	require.Equal(t, []string{"sess-1"}, agent.sessions)
	require.Equal(t, "focus on files", agent.instructions)
	require.Contains(t, resp.Content, "Compaction scheduled")
	require.Contains(t, resp.Content, "focus on files")
}

func TestCompactContext_SchedulesWithUnknownWindow(t *testing.T) {
	t.Parallel()

	agent := &fakeCompactContextAgent{}
	ctx := context.WithValue(context.Background(), SessionIDContextKey, "sess-1")

	resp := runCompactContextTool(t, agent, ctx, CompactContextParams{})
	require.Equal(t, []string{"sess-1"}, agent.sessions)
	require.Contains(t, resp.Content, "Compaction scheduled")
}

func TestCompactContext_FailsOpenOnStatusError(t *testing.T) {
	t.Parallel()

	agent := &fakeCompactContextAgent{statusErr: errors.New("db down")}
	ctx := context.WithValue(context.Background(), SessionIDContextKey, "sess-1")

	resp := runCompactContextTool(t, agent, ctx, CompactContextParams{})
	require.Equal(t, []string{"sess-1"}, agent.sessions)
	require.Contains(t, resp.Content, "Compaction scheduled")
}

func TestCompactContext_RequiresSession(t *testing.T) {
	t.Parallel()

	agent := &fakeCompactContextAgent{}
	resp := runCompactContextTool(t, agent, context.Background(), CompactContextParams{})
	require.Empty(t, agent.sessions)
	require.Contains(t, resp.Content, "no active session")
}

func TestCompactContext_RequiresAgent(t *testing.T) {
	t.Parallel()

	tool := NewCompactContextTool(func() CompactContextAgent { return nil })
	ctx := context.WithValue(context.Background(), SessionIDContextKey, "sess-1")
	resp, err := tool.Run(ctx, fantasy.ToolCall{ID: "t2", Name: CompactContextToolName, Input: "{}"})
	require.NoError(t, err)
	require.Contains(t, resp.Content, "no active agent")
}
