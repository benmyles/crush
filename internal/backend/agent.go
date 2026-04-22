package backend

import (
	"context"
	"fmt"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/shell"
	"github.com/charmbracelet/crush/internal/userquestion"
)

// SendMessage sends a prompt to the agent coordinator for the given
// workspace and session.
func (b *Backend) SendMessage(ctx context.Context, workspaceID string, msg proto.AgentMessage) error {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return err
	}

	if ws.AgentCoordinator == nil {
		return ErrAgentNotInitialized
	}

	attachments := make([]message.Attachment, len(msg.Attachments))
	for i, attachment := range msg.Attachments {
		attachments[i] = message.Attachment{
			FilePath: attachment.FilePath,
			FileName: attachment.FileName,
			MimeType: attachment.MimeType,
			Content:  attachment.Content,
		}
	}

	_, err = ws.AgentCoordinator.RunWithOptions(ctx, msg.SessionID, msg.Prompt, agent.RunOptions{
		PlanMode: msg.PlanMode,
	}, attachments...)
	return err
}

func (b *Backend) RespondUserQuestion(workspaceID string, resp proto.UserQuestionResponse) error {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return err
	}

	ws.UserQuestion.Respond(userquestion.Response{
		RequestID:   resp.RequestID,
		ChoiceID:    resp.ChoiceID,
		ChoiceLabel: resp.ChoiceLabel,
		Comment:     resp.Comment,
		Dismissed:   resp.Dismissed,
	})
	return nil
}

// GetAgentInfo returns the agent's model and busy status.
func (b *Backend) GetAgentInfo(workspaceID string) (proto.AgentInfo, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return proto.AgentInfo{}, err
	}

	var agentInfo proto.AgentInfo
	if ws.AgentCoordinator != nil {
		m := ws.AgentCoordinator.Model()
		agentInfo = proto.AgentInfo{
			Model:    m.CatwalkCfg,
			ModelCfg: m.ModelCfg,
			IsBusy:   ws.AgentCoordinator.IsBusy(),
			IsReady:  true,
		}
	}
	return agentInfo, nil
}

// InitAgent initializes the coder agent for the workspace.
func (b *Backend) InitAgent(ctx context.Context, workspaceID string) error {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return err
	}

	return ws.InitCoderAgent(ctx)
}

// UpdateAgent reloads the agent model configuration.
func (b *Backend) UpdateAgent(ctx context.Context, workspaceID string) error {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return err
	}

	return ws.UpdateAgentModel(ctx)
}

// CancelSession cancels an ongoing agent operation for the given
// session.
func (b *Backend) CancelSession(workspaceID, sessionID string) error {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return err
	}

	if ws.AgentCoordinator != nil {
		ws.AgentCoordinator.Cancel(sessionID)
	}
	return nil
}

// SummarizeSession triggers a session summarization.
func (b *Backend) SummarizeSession(ctx context.Context, workspaceID, sessionID string) error {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return err
	}

	if ws.AgentCoordinator == nil {
		return ErrAgentNotInitialized
	}

	return ws.AgentCoordinator.Summarize(ctx, sessionID)
}

// CompactSession triggers Morph-backed session compaction.
func (b *Backend) CompactSession(ctx context.Context, workspaceID, sessionID string) error {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return err
	}

	if ws.AgentCoordinator == nil {
		return ErrAgentNotInitialized
	}

	return ws.AgentCoordinator.Compact(ctx, sessionID)
}

// CompactForPlan compacts session history before plan implementation using
// the specified strategy.
func (b *Backend) CompactForPlan(ctx context.Context, workspaceID, sessionID, strategy string) error {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return err
	}

	if ws.AgentCoordinator == nil {
		return ErrAgentNotInitialized
	}

	return ws.AgentCoordinator.CompactForPlan(ctx, sessionID, strategy)
}

// JobInput sends input to a running background shell.
func (b *Backend) JobInput(ctx context.Context, workspaceID, shellID, input string) error {
	if _, err := b.GetWorkspace(workspaceID); err != nil {
		return err
	}
	bgShell, ok := shell.GetBackgroundShellManager().Get(shellID)
	if !ok {
		return fmt.Errorf("background shell not found: %s", shellID)
	}
	return bgShell.WriteInput(input)
}

// JobResize resizes a running background shell terminal.
func (b *Backend) JobResize(ctx context.Context, workspaceID, shellID string, cols, rows int) error {
	if _, err := b.GetWorkspace(workspaceID); err != nil {
		return err
	}
	bgShell, ok := shell.GetBackgroundShellManager().Get(shellID)
	if !ok {
		return fmt.Errorf("background shell not found: %s", shellID)
	}
	return bgShell.Resize(cols, rows)
}

// QueuedPrompts returns the number of queued prompts for the session.
func (b *Backend) QueuedPrompts(workspaceID, sessionID string) (int, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return 0, err
	}

	if ws.AgentCoordinator == nil {
		return 0, nil
	}

	return ws.AgentCoordinator.QueuedPrompts(sessionID), nil
}

// ClearQueue clears the prompt queue for the session.
func (b *Backend) ClearQueue(workspaceID, sessionID string) error {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return err
	}

	if ws.AgentCoordinator != nil {
		ws.AgentCoordinator.ClearQueue(sessionID)
	}
	return nil
}

// QueuedPromptsList returns the list of queued prompt strings for a
// session.
func (b *Backend) QueuedPromptsList(workspaceID, sessionID string) ([]string, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}

	if ws.AgentCoordinator == nil {
		return nil, nil
	}

	return ws.AgentCoordinator.QueuedPromptsList(sessionID), nil
}

// GetDefaultSmallModel returns the default small model for a provider.
func (b *Backend) GetDefaultSmallModel(workspaceID, providerID string) (config.SelectedModel, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return config.SelectedModel{}, err
	}

	return ws.GetDefaultSmallModel(providerID), nil
}
