package server

import (
	"encoding/json"
	"testing"

	agenttools "github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/stretchr/testify/require"
)

func TestWrapEventCommandOutput(t *testing.T) {
	wrapped := wrapEvent(pubsub.Event[agenttools.CommandOutputEvent]{
		Type: pubsub.UpdatedEvent,
		Payload: agenttools.CommandOutputEvent{
			SessionID:        "session-id",
			MessageID:        "message-id",
			ToolCallID:       "tool-call-id",
			ShellID:          "001",
			Command:          "go test ./...",
			Description:      "run tests",
			WorkingDirectory: "/tmp/project",
			Output:           "ok",
			Background:       true,
			Done:             true,
			StartTime:        100,
			EndTime:          200,
			UpdatedAt:        200,
		},
	})

	require.NotNil(t, wrapped)
	require.Equal(t, pubsub.PayloadTypeCommandOutput, wrapped.Type)

	var event pubsub.Event[proto.CommandOutputEvent]
	require.NoError(t, json.Unmarshal(wrapped.Payload, &event))
	require.Equal(t, pubsub.UpdatedEvent, event.Type)
	require.Equal(t, "session-id", event.Payload.SessionID)
	require.Equal(t, "message-id", event.Payload.MessageID)
	require.Equal(t, "tool-call-id", event.Payload.ToolCallID)
	require.Equal(t, "001", event.Payload.ShellID)
	require.Equal(t, "ok", event.Payload.Output)
	require.True(t, event.Payload.Background)
	require.True(t, event.Payload.Done)
}
