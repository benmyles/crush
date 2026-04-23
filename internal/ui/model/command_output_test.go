package model

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/agent/notify"
	agenttools "github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/crush/internal/workspace"
	"github.com/charmbracelet/x/ansi"
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

func TestAgentFinishedSyncReconcilesFinalToolResult(t *testing.T) {
	sty := styles.DefaultStyles()
	toolCall := message.ToolCall{
		ID:       "tool-1",
		Name:     agenttools.BashToolName,
		Input:    `{"command":"printf final"}`,
		Finished: true,
	}
	finishedAssistant := message.Message{
		ID:        "assistant-1",
		SessionID: "session-1",
		Role:      message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: "Done."},
			toolCall,
			message.Finish{Reason: message.FinishReasonEndTurn},
		},
	}
	toolResult := message.Message{
		ID:        "tool-result-1",
		SessionID: "session-1",
		Role:      message.Tool,
		Parts: []message.ContentPart{
			message.ToolResult{
				ToolCallID: "tool-1",
				Name:       agenttools.BashToolName,
				Content:    "final output",
			},
		},
	}
	ws := &testWorkspace{
		messages: []message.Message{finishedAssistant, toolResult},
	}
	com := &common.Common{
		Workspace: ws,
		Styles:    &sty,
	}
	ui := &UI{
		com:                   com,
		session:               &session.Session{ID: "session-1"},
		chat:                  NewChat(com),
		status:                NewStatus(com, nil),
		pendingCommandOutputs: make(map[string]agenttools.CommandOutputEvent),
		state:                 uiChat,
	}

	runCommand(t, ui.appendSessionMessage(message.Message{
		ID:        "assistant-1",
		SessionID: "session-1",
		Role:      message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: "Done."},
			toolCall,
			message.Finish{Reason: message.FinishReasonToolUse},
		},
	}))
	require.True(t, ui.chat.SetCommandOutput(agenttools.CommandOutputEvent{
		SessionID:  "session-1",
		MessageID:  "assistant-1",
		ToolCallID: "tool-1",
		Command:    "printf final",
		Output:     "partial output",
	}))

	cmd := ui.handleAgentNotification(notify.Notification{
		SessionID: "session-1",
		Type:      notify.TypeAgentFinished,
	})

	var synced sessionMessagesSyncedMsg
	for _, msg := range collectCommandMessages(cmd) {
		if sessionMsg, ok := msg.(sessionMessagesSyncedMsg); ok {
			synced = sessionMsg
			break
		}
	}
	require.Equal(t, "session-1", synced.sessionID)
	require.Equal(t, "session-1", ws.listMessagesSessionID)

	_, updateCmd := ui.Update(synced)
	runCommand(t, updateCmd)

	item := ui.chat.MessageItem("tool-1")
	require.NotNil(t, item)
	rendered := ansi.Strip(item.Render(80))
	require.Contains(t, rendered, "final output")
	require.NotContains(t, rendered, "partial output")
	require.NotContains(t, rendered, "Waiting for tool response")
	require.NotContains(t, rendered, "down 0")
}

func TestReconcileCreatesAssistantItemForUnseenTextAndToolsMessage(t *testing.T) {
	t.Parallel()

	sty := styles.DefaultStyles()
	toolCall := message.ToolCall{
		ID:       "tool-1",
		Name:     agenttools.BashToolName,
		Input:    `{"command":"echo hi"}`,
		Finished: true,
	}
	assistant := message.Message{
		ID:        "assistant-1",
		SessionID: "session-1",
		Role:      message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: "Here is the output:"},
			toolCall,
			message.Finish{Reason: message.FinishReasonEndTurn},
		},
	}
	toolResult := message.Message{
		ID:        "tool-result-1",
		SessionID: "session-1",
		Role:      message.Tool,
		Parts: []message.ContentPart{
			message.ToolResult{
				ToolCallID: "tool-1",
				Name:       agenttools.BashToolName,
				Content:    "hi",
			},
		},
	}
	ws := &testWorkspace{
		messages: []message.Message{assistant, toolResult},
	}
	com := &common.Common{
		Workspace: ws,
		Styles:    &sty,
	}
	ui := &UI{
		com:                   com,
		session:               &session.Session{ID: "session-1"},
		chat:                  NewChat(com),
		status:                NewStatus(com, nil),
		pendingCommandOutputs: make(map[string]agenttools.CommandOutputEvent),
		state:                 uiChat,
	}

	cmd := ui.reconcileSessionMessages([]message.Message{assistant, toolResult})
	runCommand(t, cmd)

	assistantItem := ui.chat.MessageItem("assistant-1")
	require.NotNil(t, assistantItem, "expected an AssistantMessageItem to be created for unseen message with text+tools")
	rendered := ansi.Strip(assistantItem.Render(80))
	require.Contains(t, rendered, "Here is the output:")

	toolItem := ui.chat.MessageItem("tool-1")
	require.NotNil(t, toolItem, "expected a ToolMessageItem to be created")
	toolRendered := ansi.Strip(toolItem.Render(80))
	require.Contains(t, toolRendered, "hi")
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
