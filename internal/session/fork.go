package session

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/crush/internal/message"
)

// ForkResult contains the new session and any prompt text that should be
// prefilled but not submitted.
type ForkResult struct {
	Session Session
	Prefill string
}

// Fork creates a new visible session from the source session's messages up to
// the selected message. Selecting a user message excludes that message from the
// fork and returns its text as Prefill.
func Fork(ctx context.Context, sessions Service, messageSvc message.Service, sessionID, messageID string) (ForkResult, error) {
	if strings.TrimSpace(sessionID) == "" {
		return ForkResult{}, fmt.Errorf("session ID is required")
	}
	if strings.TrimSpace(messageID) == "" {
		return ForkResult{}, fmt.Errorf("message ID is required")
	}

	source, err := sessions.Get(ctx, sessionID)
	if err != nil {
		return ForkResult{}, fmt.Errorf("failed to get source session: %w", err)
	}

	messages, err := messageSvc.List(ctx, sessionID)
	if err != nil {
		return ForkResult{}, fmt.Errorf("failed to list source messages: %w", err)
	}

	selectedIndex := -1
	for i := range messages {
		if messages[i].ID == messageID {
			selectedIndex = i
			break
		}
	}
	if selectedIndex == -1 {
		return ForkResult{}, fmt.Errorf("message %s not found in session %s", messageID, sessionID)
	}

	selected := messages[selectedIndex]
	copyThrough := selectedIndex
	var prefill string
	if selected.Role == message.User {
		prefill = selected.Content().Text
	} else {
		copyThrough = forkCopyThrough(messages, selectedIndex)
	}

	forked, err := sessions.Create(ctx, forkTitle(source.Title))
	if err != nil {
		return ForkResult{}, fmt.Errorf("failed to create forked session: %w", err)
	}

	var summaryMessageID string
	for _, msg := range messages[:copyThrough] {
		created, err := messageSvc.Create(ctx, forked.ID, message.CreateMessageParams{
			Role:             msg.Role,
			Parts:            partsForFork(msg),
			Model:            msg.Model,
			Provider:         msg.Provider,
			IsSummaryMessage: msg.IsSummaryMessage,
		})
		if err != nil {
			return ForkResult{}, fmt.Errorf("failed to copy message %s: %w", msg.ID, err)
		}
		if msg.ID == source.SummaryMessageID {
			summaryMessageID = created.ID
		}
	}

	if summaryMessageID != "" {
		forked.SummaryMessageID = summaryMessageID
		forked, err = sessions.Save(ctx, forked)
		if err != nil {
			return ForkResult{}, fmt.Errorf("failed to save forked session: %w", err)
		}
	}

	return ForkResult{
		Session: forked,
		Prefill: prefill,
	}, nil
}

func forkTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" || title == "New Session" {
		return "Forked Session"
	}
	return "Forked: " + title
}

func forkCopyThrough(messages []message.Message, selectedIndex int) int {
	copyThrough := selectedIndex + 1
	selected := messages[selectedIndex]
	if selected.Role != message.Assistant {
		return copyThrough
	}

	toolCallIDs := make(map[string]struct{})
	for _, toolCall := range selected.ToolCalls() {
		toolCallIDs[toolCall.ID] = struct{}{}
	}
	if len(toolCallIDs) == 0 {
		return copyThrough
	}

	for copyThrough < len(messages) && isToolResultFor(messages[copyThrough], toolCallIDs) {
		copyThrough++
	}
	return copyThrough
}

func isToolResultFor(msg message.Message, toolCallIDs map[string]struct{}) bool {
	if msg.Role != message.Tool {
		return false
	}
	results := msg.ToolResults()
	if len(results) == 0 {
		return false
	}
	for _, result := range results {
		if _, ok := toolCallIDs[result.ToolCallID]; !ok {
			return false
		}
	}
	return true
}

func partsForFork(msg message.Message) []message.ContentPart {
	parts := make([]message.ContentPart, 0, len(msg.Parts))
	for _, part := range msg.Parts {
		if _, ok := part.(message.Finish); ok && msg.Role != message.Assistant {
			continue
		}
		parts = append(parts, part)
	}
	return parts
}
