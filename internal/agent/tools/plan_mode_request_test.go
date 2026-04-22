package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/planning"
	"github.com/stretchr/testify/require"
)

func TestRequestEnterPlanModeToolPublishesRequestAndStopsTurn(t *testing.T) {
	t.Parallel()

	service := planning.NewService()
	tool := NewRequestEnterPlanModeTool(service)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, SessionIDContextKey, "session-1")

	events := service.SubscribeModeChanges(ctx)
	resp, err := tool.Run(ctx, planModeRequestToolCall(t, RequestEnterPlanModeToolName, PlanModeRequestParams{
		Prompt: "Plan a large migration",
		Reason: "This is broad.",
	}))
	require.NoError(t, err)
	require.False(t, resp.IsError)
	require.True(t, resp.StopTurn)

	var meta PlanModeRequestMetadata
	require.NoError(t, json.Unmarshal([]byte(resp.Metadata), &meta))
	require.Equal(t, planning.ModeEnter, meta.Mode)
	require.Equal(t, "Plan a large migration", meta.Prompt)
	require.Equal(t, "This is broad.", meta.Reason)

	select {
	case event := <-events:
		require.Equal(t, planning.ModeEnter, event.Payload.Mode)
		require.Equal(t, "session-1", event.Payload.SessionID)
		require.Equal(t, "Plan a large migration", event.Payload.Prompt)
		require.Equal(t, "This is broad.", event.Payload.Reason)
	case <-ctx.Done():
		t.Fatal("timed out waiting for plan mode request")
	}
}

func TestRequestExitPlanModeToolPublishesRequestAndStopsTurn(t *testing.T) {
	t.Parallel()

	service := planning.NewService()
	tool := NewRequestExitPlanModeTool(service)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, SessionIDContextKey, "session-1")
	ctx = context.WithValue(ctx, PlanModeContextKey, true)

	events := service.SubscribeModeChanges(ctx)
	resp, err := tool.Run(ctx, planModeRequestToolCall(t, RequestExitPlanModeToolName, PlanModeRequestParams{
		Prompt: "Implement the approved plan",
		Reason: "User asked to proceed.",
	}))
	require.NoError(t, err)
	require.False(t, resp.IsError)
	require.True(t, resp.StopTurn)

	var meta PlanModeRequestMetadata
	require.NoError(t, json.Unmarshal([]byte(resp.Metadata), &meta))
	require.Equal(t, planning.ModeExit, meta.Mode)
	require.Equal(t, "Implement the approved plan", meta.Prompt)
	require.Equal(t, "User asked to proceed.", meta.Reason)

	select {
	case event := <-events:
		require.Equal(t, planning.ModeExit, event.Payload.Mode)
		require.Equal(t, "session-1", event.Payload.SessionID)
		require.Equal(t, "Implement the approved plan", event.Payload.Prompt)
		require.Equal(t, "User asked to proceed.", event.Payload.Reason)
	case <-ctx.Done():
		t.Fatal("timed out waiting for plan mode request")
	}
}

func planModeRequestToolCall(t *testing.T, name string, params PlanModeRequestParams) fantasy.ToolCall {
	t.Helper()

	input, err := json.Marshal(params)
	require.NoError(t, err)

	return fantasy.ToolCall{
		ID:    name + "-call",
		Name:  name,
		Input: string(input),
	}
}
