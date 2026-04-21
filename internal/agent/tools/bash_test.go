package tools

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/shell"
	"github.com/stretchr/testify/require"
)

type mockBashPermissionService struct {
	*pubsub.Broker[permission.PermissionRequest]
}

type recordingCommandOutputPublisher struct {
	mu     sync.Mutex
	events []pubsub.Event[CommandOutputEvent]
	ch     chan pubsub.Event[CommandOutputEvent]
}

func (p *recordingCommandOutputPublisher) Publish(t pubsub.EventType, payload CommandOutputEvent) {
	event := pubsub.Event[CommandOutputEvent]{
		Type:    t,
		Payload: payload,
	}

	p.mu.Lock()
	p.events = append(p.events, event)
	p.mu.Unlock()

	if p.ch != nil {
		select {
		case p.ch <- event:
		default:
		}
	}
}

func (p *recordingCommandOutputPublisher) Events() []pubsub.Event[CommandOutputEvent] {
	p.mu.Lock()
	defer p.mu.Unlock()
	events := make([]pubsub.Event[CommandOutputEvent], len(p.events))
	copy(events, p.events)
	return events
}

func (m *mockBashPermissionService) Request(ctx context.Context, req permission.CreatePermissionRequest) (bool, error) {
	return true, nil
}

func (m *mockBashPermissionService) Grant(req permission.PermissionRequest) {}

func (m *mockBashPermissionService) Deny(req permission.PermissionRequest) {}

func (m *mockBashPermissionService) GrantPersistent(req permission.PermissionRequest) {}

func (m *mockBashPermissionService) AutoApproveSession(sessionID string) {}

func (m *mockBashPermissionService) SetSkipRequests(skip bool) {}

func (m *mockBashPermissionService) SkipRequests() bool {
	return false
}

func (m *mockBashPermissionService) SubscribeNotifications(ctx context.Context) <-chan pubsub.Event[permission.PermissionNotification] {
	return make(<-chan pubsub.Event[permission.PermissionNotification])
}

func TestBashTool_DefaultAutoBackgroundThreshold(t *testing.T) {
	workingDir := t.TempDir()
	tool := newBashToolForTest(workingDir)
	ctx := context.WithValue(context.Background(), SessionIDContextKey, "test-session")

	resp := runBashTool(t, tool, ctx, BashParams{
		Description: "default threshold",
		Command:     "echo done",
	})

	require.False(t, resp.IsError)
	var meta BashResponseMetadata
	require.NoError(t, json.Unmarshal([]byte(resp.Metadata), &meta))
	require.False(t, meta.Background)
	require.Empty(t, meta.ShellID)
	require.Contains(t, meta.Output, "done")
}

func TestBashTool_PublishesCommandOutputEvents(t *testing.T) {
	workingDir := t.TempDir()
	publisher := &recordingCommandOutputPublisher{}
	tool := newBashToolForTestWithPublisher(workingDir, publisher)
	ctx := context.WithValue(context.Background(), SessionIDContextKey, "test-session")
	ctx = context.WithValue(ctx, MessageIDContextKey, "test-message")

	resp := runBashTool(t, tool, ctx, BashParams{
		Description: "stream output",
		Command:     "echo streamed",
	})

	require.False(t, resp.IsError)
	events := publisher.Events()
	require.GreaterOrEqual(t, len(events), 2)
	require.Equal(t, pubsub.CreatedEvent, events[0].Type)
	require.Equal(t, "test-session", events[0].Payload.SessionID)
	require.Equal(t, "test-message", events[0].Payload.MessageID)
	require.Equal(t, "test-call", events[0].Payload.ToolCallID)

	final := events[len(events)-1]
	require.Equal(t, pubsub.UpdatedEvent, final.Type)
	require.True(t, final.Payload.Done)
	require.False(t, final.Payload.Background)
	require.Zero(t, final.Payload.ExitCode)
	require.Contains(t, final.Payload.Output, "streamed")
}

func TestBashTool_PublishesCommandOutputAsItAppears(t *testing.T) {
	workingDir := t.TempDir()
	publisher := &recordingCommandOutputPublisher{
		ch: make(chan pubsub.Event[CommandOutputEvent], 16),
	}
	tool := newBashToolForTestWithPublisher(workingDir, publisher)
	ctx := context.WithValue(context.Background(), SessionIDContextKey, "test-session")
	ctx = context.WithValue(ctx, MessageIDContextKey, "test-message")

	done := make(chan fantasy.ToolResponse, 1)
	go func() {
		done <- runBashTool(t, tool, ctx, BashParams{
			Description: "stream output",
			Command:     "printf 'first\\n'; sleep 1; printf 'second\\n'",
		})
	}()

	timeout := time.After(3 * time.Second)
	for {
		select {
		case event := <-publisher.ch:
			if strings.Contains(event.Payload.Output, "first") && !event.Payload.Done {
				resp := <-done
				require.False(t, resp.IsError)
				return
			}
		case <-done:
			t.Fatal("command finished before publishing partial output")
		case <-timeout:
			t.Fatal("timed out waiting for partial command output")
		}
	}
}

func TestBashTool_CustomAutoBackgroundThreshold(t *testing.T) {
	workingDir := t.TempDir()
	tool := newBashToolForTest(workingDir)
	ctx := context.WithValue(context.Background(), SessionIDContextKey, "test-session")

	resp := runBashTool(t, tool, ctx, BashParams{
		Description:         "custom threshold",
		Command:             "sleep 1.5 && echo done",
		AutoBackgroundAfter: 1,
	})

	require.False(t, resp.IsError)
	var meta BashResponseMetadata
	require.NoError(t, json.Unmarshal([]byte(resp.Metadata), &meta))
	require.True(t, meta.Background)
	require.NotEmpty(t, meta.ShellID)
	require.Contains(t, resp.Content, "moved to background")

	bgManager := shell.GetBackgroundShellManager()
	require.NoError(t, bgManager.Kill(meta.ShellID))
}

func newBashToolForTest(workingDir string) fantasy.AgentTool {
	return newBashToolForTestWithPublisher(workingDir, nil)
}

func newBashToolForTestWithPublisher(workingDir string, publisher pubsub.Publisher[CommandOutputEvent]) fantasy.AgentTool {
	permissions := &mockBashPermissionService{Broker: pubsub.NewBroker[permission.PermissionRequest]()}
	attribution := &config.Attribution{TrailerStyle: config.TrailerStyleNone}
	return NewBashTool(permissions, publisher, workingDir, attribution, "test-model")
}

func runBashTool(t *testing.T, tool fantasy.AgentTool, ctx context.Context, params BashParams) fantasy.ToolResponse {
	t.Helper()

	input, err := json.Marshal(params)
	require.NoError(t, err)

	call := fantasy.ToolCall{
		ID:    "test-call",
		Name:  BashToolName,
		Input: string(input),
	}

	resp, err := tool.Run(ctx, call)
	require.NoError(t, err)
	return resp
}
