package model

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	agenttools "github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/crush/internal/workspace"
	"github.com/stretchr/testify/require"
)

type recordingJobWorkspace struct {
	workspace.Workspace
	shellID string
	input   string
}

func (w *recordingJobWorkspace) JobInput(ctx context.Context, shellID, input string) error {
	w.shellID = shellID
	w.input = input
	return nil
}

func TestHandleCommandOutputMessageCreatesLiveToolItem(t *testing.T) {
	sty := styles.DefaultStyles()
	com := &common.Common{Styles: &sty}
	parent := &message.Message{
		ID:        "message-1",
		SessionID: "session-1",
		Role:      message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: "Running command."},
		},
	}

	ui := &UI{
		com:                   com,
		session:               &session.Session{ID: "session-1"},
		chat:                  NewChat(com),
		pendingCommandOutputs: make(map[string]agenttools.CommandOutputEvent),
	}
	ui.chat.AppendMessages(chat.NewAssistantMessageItem(&sty, parent))

	cmd := ui.handleCommandOutputMessage(pubsub.Event[agenttools.CommandOutputEvent]{
		Type: pubsub.UpdatedEvent,
		Payload: agenttools.CommandOutputEvent{
			SessionID:  "session-1",
			MessageID:  "message-1",
			ToolCallID: "tool-1",
			ShellID:    "001",
			Command:    "printf 'first\\n'",
			Output:     "first\n",
		},
	})

	require.NotNil(t, cmd)
	item := ui.chat.MessageItem("tool-1")
	require.NotNil(t, item)
	require.Contains(t, strings.TrimSpace(item.Render(80)), "first")
}

func TestAttachSelectedJobSendsTerminalInput(t *testing.T) {
	sty := styles.DefaultStyles()
	workspace := &recordingJobWorkspace{}
	com := &common.Common{Styles: &sty, Workspace: workspace}
	parent := &message.Message{
		ID:        "message-1",
		SessionID: "session-1",
		Role:      message.Assistant,
	}

	ui := &UI{
		com:                   com,
		session:               &session.Session{ID: "session-1"},
		chat:                  NewChat(com),
		pendingCommandOutputs: make(map[string]agenttools.CommandOutputEvent),
	}
	ui.chat.AppendMessages(chat.NewAssistantMessageItem(&sty, parent))
	ui.handleCommandOutputMessage(pubsub.Event[agenttools.CommandOutputEvent]{
		Type: pubsub.UpdatedEvent,
		Payload: agenttools.CommandOutputEvent{
			SessionID:        "session-1",
			MessageID:        "message-1",
			ToolCallID:       "tool-1",
			ShellID:          "001",
			Command:          "read answer",
			Output:           "Answer: ",
			Background:       true,
			SupportsInput:    true,
			WorkingDirectory: t.TempDir(),
		},
	})
	ui.chat.SetSelected(1)

	cmd := ui.attachSelectedJob()
	require.NotNil(t, cmd)
	require.NotNil(t, ui.attachedJob)
	require.Equal(t, uiFocusTerminal, ui.focus)

	cmd = ui.handleAttachedJobKey(tea.KeyPressMsg(tea.Key{Text: "y", Code: 'y'}))
	require.NotNil(t, cmd)
	msg := cmd()
	require.Nil(t, msg)
	require.Equal(t, "001", workspace.shellID)
	require.Equal(t, "y", workspace.input)
}
