package workspace

import (
	"testing"

	agenttools "github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/stretchr/testify/require"
)

func TestTranslateEventCommandOutput(t *testing.T) {
	msg := translateEvent(pubsub.Event[proto.CommandOutputEvent]{
		Type: pubsub.UpdatedEvent,
		Payload: proto.CommandOutputEvent{
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

	event, ok := msg.(pubsub.Event[agenttools.CommandOutputEvent])
	require.True(t, ok)
	require.Equal(t, pubsub.UpdatedEvent, event.Type)
	require.Equal(t, "session-id", event.Payload.SessionID)
	require.Equal(t, "message-id", event.Payload.MessageID)
	require.Equal(t, "tool-call-id", event.Payload.ToolCallID)
	require.Equal(t, "001", event.Payload.ShellID)
	require.Equal(t, "ok", event.Payload.Output)
	require.True(t, event.Payload.Background)
	require.True(t, event.Payload.Done)
}
