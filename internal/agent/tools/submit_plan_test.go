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

func TestSubmitPlanToolWaitsForRevisionFeedback(t *testing.T) {
	t.Parallel()

	service := planning.NewService()
	tool := NewSubmitPlanTool(service)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, SessionIDContextKey, "session-1")
	ctx = context.WithValue(ctx, PlanModeContextKey, true)

	events := service.Subscribe(ctx)
	done := make(chan submitPlanToolResult, 1)
	call := submitPlanToolCall(t, SubmitPlanParams{
		Markdown: "## Plan\n\nDo the thing.",
		Todos: []TodoItem{{
			Content:    "Do the thing",
			Status:     "pending",
			ActiveForm: "Doing the thing",
		}},
	})
	go func() {
		resp, err := tool.Run(ctx, call)
		done <- submitPlanToolResult{resp: resp, err: err}
	}()

	var submission planning.Submission
	select {
	case event := <-events:
		submission = event.Payload
	case <-ctx.Done():
		t.Fatal("timed out waiting for plan submission")
	}

	select {
	case <-done:
		t.Fatal("submit_plan returned before the user response")
	case <-time.After(25 * time.Millisecond):
	}

	service.Respond(planning.Response{
		SubmissionID: submission.ID,
		Approved:     false,
		Comment:      "Handle edge cases first.",
	})

	select {
	case got := <-done:
		require.NoError(t, got.err)
		resp := got.resp
		require.False(t, resp.IsError)
		require.Contains(t, resp.Content, "Plan was not approved")
		require.Contains(t, resp.Content, "Handle edge cases first.")
		require.Contains(t, resp.Content, "call submit_plan again")
		require.Contains(t, resp.Content, "ask follow-up questions with ask_user")
		require.Contains(t, resp.Content, "Previous plan:")
		require.Contains(t, resp.Content, "## Plan")

		var meta SubmitPlanResponseMetadata
		require.NoError(t, json.Unmarshal([]byte(resp.Metadata), &meta))
		require.Equal(t, submission.ID, meta.ID)
		require.False(t, meta.Approved)
		require.Equal(t, "Handle edge cases first.", meta.Comment)
		require.Len(t, meta.Todos, 1)
	case <-ctx.Done():
		t.Fatal("timed out waiting for submit_plan to return")
	}
}

func TestSubmitPlanToolReportsApprovalAfterResponse(t *testing.T) {
	t.Parallel()

	service := planning.NewService()
	tool := NewSubmitPlanTool(service)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, SessionIDContextKey, "session-1")
	ctx = context.WithValue(ctx, PlanModeContextKey, true)

	events := service.Subscribe(ctx)
	done := make(chan submitPlanToolResult, 1)
	call := submitPlanToolCall(t, SubmitPlanParams{
		Markdown: "## Plan\n\nDo the thing.",
		Todos: []TodoItem{{
			Content:    "Do the thing",
			Status:     "pending",
			ActiveForm: "Doing the thing",
		}},
	})
	go func() {
		resp, err := tool.Run(ctx, call)
		done <- submitPlanToolResult{resp: resp, err: err}
	}()

	var submission planning.Submission
	select {
	case event := <-events:
		submission = event.Payload
	case <-ctx.Done():
		t.Fatal("timed out waiting for plan submission")
	}

	service.Respond(planning.Response{
		SubmissionID: submission.ID,
		Approved:     true,
		Comment:      "Looks good.",
	})

	select {
	case got := <-done:
		require.NoError(t, got.err)
		resp := got.resp
		require.False(t, resp.IsError)
		require.Contains(t, resp.Content, "Plan approved by the user")
		require.Contains(t, resp.Content, "Looks good.")
		require.Contains(t, resp.Content, "Stop planning now")
		require.True(t, resp.StopTurn)

		var meta SubmitPlanResponseMetadata
		require.NoError(t, json.Unmarshal([]byte(resp.Metadata), &meta))
		require.True(t, meta.Approved)
		require.Equal(t, "Looks good.", meta.Comment)
	case <-ctx.Done():
		t.Fatal("timed out waiting for submit_plan to return")
	}
}

type submitPlanToolResult struct {
	resp fantasy.ToolResponse
	err  error
}

func submitPlanToolCall(t *testing.T, params SubmitPlanParams) fantasy.ToolCall {
	t.Helper()

	input, err := json.Marshal(params)
	require.NoError(t, err)

	return fantasy.ToolCall{
		ID:    "submit-plan-call",
		Name:  SubmitPlanToolName,
		Input: string(input),
	}
}
