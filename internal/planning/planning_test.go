package planning

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSubmitBlocksUntilResponse(t *testing.T) {
	t.Parallel()

	service := NewService()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	events := service.Subscribe(ctx)
	type result struct {
		submission Submission
		response   Response
		err        error
	}
	done := make(chan result, 1)

	go func() {
		submission, response, err := service.Submit(ctx, Submission{
			SessionID:  "session-1",
			ToolCallID: "tool-call-1",
			Markdown:   "## Plan",
		})
		done <- result{submission: submission, response: response, err: err}
	}()

	var submission Submission
	select {
	case event := <-events:
		submission = event.Payload
	case <-ctx.Done():
		t.Fatal("timed out waiting for plan submission event")
	}
	require.NotEmpty(t, submission.ID)

	select {
	case <-done:
		t.Fatal("submit returned before a response was provided")
	case <-time.After(25 * time.Millisecond):
	}

	service.Respond(Response{
		SubmissionID: submission.ID,
		Approved:     false,
		Comment:      "Revise this.",
	})

	select {
	case got := <-done:
		require.NoError(t, got.err)
		require.Equal(t, submission.ID, got.submission.ID)
		require.Equal(t, Response{
			SubmissionID: submission.ID,
			Approved:     false,
			Comment:      "Revise this.",
		}, got.response)
	case <-ctx.Done():
		t.Fatal("timed out waiting for submit to return")
	}
}

func TestSubmitReturnsContextErrorWhileWaiting(t *testing.T) {
	t.Parallel()

	service := NewService()
	ctx, cancel := context.WithCancel(context.Background())
	subCtx, cancelSub := context.WithCancel(context.Background())
	defer cancelSub()
	events := service.Subscribe(subCtx)
	done := make(chan error, 1)

	go func() {
		_, _, err := service.Submit(ctx, Submission{
			SessionID: "session-1",
			Markdown:  "## Plan",
		})
		done <- err
	}()

	select {
	case <-events:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for plan submission event")
	}

	cancel()

	select {
	case err := <-done:
		require.Error(t, err)
		require.True(t, errors.Is(err, context.Canceled))
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for submit to return")
	}
}

func TestRequestModeChangePublishesRequest(t *testing.T) {
	t.Parallel()

	service := NewService()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	events := service.SubscribeModeChanges(ctx)
	request, err := service.RequestModeChange(ctx, ModeChangeRequest{
		SessionID:  "session-1",
		ToolCallID: "tool-call-1",
		Mode:       ModeEnter,
		Prompt:     "Plan this task",
		Reason:     "Large task",
	})
	require.NoError(t, err)
	require.NotEmpty(t, request.ID)

	select {
	case event := <-events:
		require.Equal(t, request, event.Payload)
	case <-ctx.Done():
		t.Fatal("timed out waiting for plan mode request event")
	}
}
