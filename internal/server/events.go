package server

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/charmbracelet/crush/internal/agent/notify"
	agenttools "github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/agent/tools/mcp"
	"github.com/charmbracelet/crush/internal/app"
	"github.com/charmbracelet/crush/internal/history"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/planning"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/userquestion"
)

// wrapEvent converts a raw tea.Msg (a pubsub.Event[T] from the app
// event fan-in) into a pubsub.Payload envelope with the correct
// PayloadType discriminator and a proto-typed inner payload that has
// proper JSON tags. Returns nil if the event type is unrecognized.
func wrapEvent(ev any) *pubsub.Payload {
	switch e := ev.(type) {
	case pubsub.Event[app.LSPEvent]:
		return envelope(pubsub.PayloadTypeLSPEvent, pubsub.Event[proto.LSPEvent]{
			Type: e.Type,
			Payload: proto.LSPEvent{
				Type:            proto.LSPEventType(e.Payload.Type),
				Name:            e.Payload.Name,
				State:           e.Payload.State,
				Error:           e.Payload.Error,
				DiagnosticCount: e.Payload.DiagnosticCount,
			},
		})
	case pubsub.Event[mcp.Event]:
		return envelope(pubsub.PayloadTypeMCPEvent, pubsub.Event[proto.MCPEvent]{
			Type: e.Type,
			Payload: proto.MCPEvent{
				Type:      mcpEventTypeToProto(e.Payload.Type),
				Name:      e.Payload.Name,
				State:     proto.MCPState(e.Payload.State),
				Error:     e.Payload.Error,
				ToolCount: e.Payload.Counts.Tools,
			},
		})
	case pubsub.Event[permission.PermissionRequest]:
		return envelope(pubsub.PayloadTypePermissionRequest, pubsub.Event[proto.PermissionRequest]{
			Type: e.Type,
			Payload: proto.PermissionRequest{
				ID:          e.Payload.ID,
				SessionID:   e.Payload.SessionID,
				ToolCallID:  e.Payload.ToolCallID,
				ToolName:    e.Payload.ToolName,
				Description: e.Payload.Description,
				Action:      e.Payload.Action,
				Path:        e.Payload.Path,
				Params:      e.Payload.Params,
			},
		})
	case pubsub.Event[permission.PermissionNotification]:
		return envelope(pubsub.PayloadTypePermissionNotification, pubsub.Event[proto.PermissionNotification]{
			Type: e.Type,
			Payload: proto.PermissionNotification{
				ToolCallID: e.Payload.ToolCallID,
				Granted:    e.Payload.Granted,
				Denied:     e.Payload.Denied,
			},
		})
	case pubsub.Event[message.Message]:
		return envelope(pubsub.PayloadTypeMessage, pubsub.Event[proto.Message]{
			Type:    e.Type,
			Payload: messageToProto(e.Payload),
		})
	case pubsub.Event[session.Session]:
		return envelope(pubsub.PayloadTypeSession, pubsub.Event[proto.Session]{
			Type:    e.Type,
			Payload: sessionToProto(e.Payload),
		})
	case pubsub.Event[history.File]:
		return envelope(pubsub.PayloadTypeFile, pubsub.Event[proto.File]{
			Type:    e.Type,
			Payload: fileToProto(e.Payload),
		})
	case pubsub.Event[notify.Notification]:
		return envelope(pubsub.PayloadTypeAgentEvent, pubsub.Event[proto.AgentEvent]{
			Type: e.Type,
			Payload: proto.AgentEvent{
				SessionID:    e.Payload.SessionID,
				SessionTitle: e.Payload.SessionTitle,
				Type:         proto.AgentEventType(e.Payload.Type),
			},
		})
	case pubsub.Event[agenttools.CommandOutputEvent]:
		return envelope(pubsub.PayloadTypeCommandOutput, pubsub.Event[proto.CommandOutputEvent]{
			Type:    e.Type,
			Payload: commandOutputToProto(e.Payload),
		})
	case pubsub.Event[planning.Submission]:
		return envelope(pubsub.PayloadTypePlanSubmission, pubsub.Event[proto.PlanSubmission]{
			Type:    e.Type,
			Payload: planSubmissionToProto(e.Payload),
		})
	case pubsub.Event[planning.ModeChangeRequest]:
		return envelope(pubsub.PayloadTypePlanModeChangeRequest, pubsub.Event[proto.PlanModeChangeRequest]{
			Type:    e.Type,
			Payload: planModeChangeRequestToProto(e.Payload),
		})
	case pubsub.Event[userquestion.Request]:
		return envelope(pubsub.PayloadTypeUserQuestion, pubsub.Event[proto.UserQuestionRequest]{
			Type:    e.Type,
			Payload: userQuestionToProto(e.Payload),
		})
	default:
		slog.Warn("Unrecognized event type for SSE wrapping", "type", fmt.Sprintf("%T", ev))
		return nil
	}
}

func commandOutputToProto(e agenttools.CommandOutputEvent) proto.CommandOutputEvent {
	return proto.CommandOutputEvent{
		SessionID:        e.SessionID,
		MessageID:        e.MessageID,
		ToolCallID:       e.ToolCallID,
		ShellID:          e.ShellID,
		Command:          e.Command,
		Description:      e.Description,
		WorkingDirectory: e.WorkingDirectory,
		Output:           e.Output,
		Background:       e.Background,
		Done:             e.Done,
		SupportsInput:    e.SupportsInput,
		ExitCode:         e.ExitCode,
		Error:            e.Error,
		StartTime:        e.StartTime,
		EndTime:          e.EndTime,
		UpdatedAt:        e.UpdatedAt,
	}
}

func planSubmissionToProto(p planning.Submission) proto.PlanSubmission {
	return proto.PlanSubmission{
		ID:         p.ID,
		SessionID:  p.SessionID,
		ToolCallID: p.ToolCallID,
		Markdown:   p.Markdown,
		Todos:      todosToProto(p.Todos),
	}
}

func planModeChangeRequestToProto(p planning.ModeChangeRequest) proto.PlanModeChangeRequest {
	return proto.PlanModeChangeRequest{
		ID:         p.ID,
		SessionID:  p.SessionID,
		ToolCallID: p.ToolCallID,
		Mode:       proto.PlanMode(p.Mode),
		Prompt:     p.Prompt,
		Reason:     p.Reason,
	}
}

func userQuestionToProto(q userquestion.Request) proto.UserQuestionRequest {
	choices := make([]proto.UserQuestionChoice, len(q.Choices))
	for i, choice := range q.Choices {
		choices[i] = proto.UserQuestionChoice{
			ID:          choice.ID,
			Label:       choice.Label,
			Description: choice.Description,
		}
	}
	return proto.UserQuestionRequest{
		ID:          q.ID,
		SessionID:   q.SessionID,
		ToolCallID:  q.ToolCallID,
		Question:    q.Question,
		Description: q.Description,
		Choices:     choices,
	}
}

// envelope marshals the inner event and wraps it in a pubsub.Payload.
func envelope(payloadType pubsub.PayloadType, inner any) *pubsub.Payload {
	raw, err := json.Marshal(inner)
	if err != nil {
		slog.Error("Failed to marshal event payload", "error", err)
		return nil
	}
	return &pubsub.Payload{
		Type:    payloadType,
		Payload: raw,
	}
}

func mcpEventTypeToProto(t mcp.EventType) proto.MCPEventType {
	switch t {
	case mcp.EventStateChanged:
		return proto.MCPEventStateChanged
	case mcp.EventToolsListChanged:
		return proto.MCPEventToolsListChanged
	case mcp.EventPromptsListChanged:
		return proto.MCPEventPromptsListChanged
	case mcp.EventResourcesListChanged:
		return proto.MCPEventResourcesListChanged
	default:
		return proto.MCPEventStateChanged
	}
}

func sessionToProto(s session.Session) proto.Session {
	return proto.Session{
		ID:               s.ID,
		ParentSessionID:  s.ParentSessionID,
		Title:            s.Title,
		SummaryMessageID: s.SummaryMessageID,
		MessageCount:     s.MessageCount,
		PromptTokens:     s.PromptTokens,
		CompletionTokens: s.CompletionTokens,
		Cost:             s.Cost,
		Todos:            todosToProto(s.Todos),
		CreatedAt:        s.CreatedAt,
		UpdatedAt:        s.UpdatedAt,
	}
}

func todosToProto(todos []session.Todo) []proto.Todo {
	result := make([]proto.Todo, len(todos))
	for i, todo := range todos {
		result[i] = proto.Todo{
			Content:    todo.Content,
			Status:     string(todo.Status),
			ActiveForm: todo.ActiveForm,
		}
	}
	return result
}

func fileToProto(f history.File) proto.File {
	return proto.File{
		ID:        f.ID,
		SessionID: f.SessionID,
		Path:      f.Path,
		Content:   f.Content,
		Version:   f.Version,
		CreatedAt: f.CreatedAt,
		UpdatedAt: f.UpdatedAt,
	}
}

func messageToProto(m message.Message) proto.Message {
	msg := proto.Message{
		ID:        m.ID,
		SessionID: m.SessionID,
		Role:      proto.MessageRole(m.Role),
		Model:     m.Model,
		Provider:  m.Provider,
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
	}

	for _, p := range m.Parts {
		switch v := p.(type) {
		case message.TextContent:
			msg.Parts = append(msg.Parts, proto.TextContent{Text: v.Text})
		case message.ReasoningContent:
			msg.Parts = append(msg.Parts, proto.ReasoningContent{
				Thinking:   v.Thinking,
				Signature:  v.Signature,
				StartedAt:  v.StartedAt,
				FinishedAt: v.FinishedAt,
			})
		case message.ToolCall:
			msg.Parts = append(msg.Parts, proto.ToolCall{
				ID:       v.ID,
				Name:     v.Name,
				Input:    v.Input,
				Finished: v.Finished,
			})
		case message.ToolResult:
			msg.Parts = append(msg.Parts, proto.ToolResult{
				ToolCallID: v.ToolCallID,
				Name:       v.Name,
				Content:    v.Content,
				IsError:    v.IsError,
			})
		case message.Finish:
			msg.Parts = append(msg.Parts, proto.Finish{
				Reason:  proto.FinishReason(v.Reason),
				Time:    v.Time,
				Message: v.Message,
				Details: v.Details,
			})
		case message.ImageURLContent:
			msg.Parts = append(msg.Parts, proto.ImageURLContent{URL: v.URL, Detail: v.Detail})
		case message.BinaryContent:
			msg.Parts = append(msg.Parts, proto.BinaryContent{Path: v.Path, MIMEType: v.MIMEType, Data: v.Data})
		}
	}

	return msg
}

func messagesToProto(msgs []message.Message) []proto.Message {
	out := make([]proto.Message, len(msgs))
	for i, m := range msgs {
		out[i] = messageToProto(m)
	}
	return out
}
