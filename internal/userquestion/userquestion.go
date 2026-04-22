package userquestion

import (
	"context"
	"fmt"
	"sync"

	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/google/uuid"
)

type Choice struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type Request struct {
	ID          string   `json:"id"`
	SessionID   string   `json:"session_id"`
	ToolCallID  string   `json:"tool_call_id"`
	Question    string   `json:"question"`
	Description string   `json:"description,omitempty"`
	Choices     []Choice `json:"choices"`
}

type Response struct {
	RequestID   string `json:"request_id"`
	ChoiceID    string `json:"choice_id,omitempty"`
	ChoiceLabel string `json:"choice_label,omitempty"`
	Comment     string `json:"comment,omitempty"`
	Dismissed   bool   `json:"dismissed,omitempty"`
}

type CreateRequest struct {
	SessionID   string
	ToolCallID  string
	Question    string
	Description string
	Choices     []Choice
}

type Service interface {
	pubsub.Subscriber[Request]
	Request(context.Context, CreateRequest) (Response, error)
	Respond(Response)
}

type service struct {
	*pubsub.Broker[Request]

	pending   map[string]chan Response
	pendingMu sync.Mutex
}

func (s *service) Request(ctx context.Context, opts CreateRequest) (Response, error) {
	req := Request{
		ID:          uuid.NewString(),
		SessionID:   opts.SessionID,
		ToolCallID:  opts.ToolCallID,
		Question:    opts.Question,
		Description: opts.Description,
		Choices:     opts.Choices,
	}

	respCh := make(chan Response, 1)
	s.pendingMu.Lock()
	s.pending[req.ID] = respCh
	s.pendingMu.Unlock()
	defer func() {
		s.pendingMu.Lock()
		delete(s.pending, req.ID)
		s.pendingMu.Unlock()
	}()

	s.Publish(pubsub.CreatedEvent, req)

	select {
	case <-ctx.Done():
		return Response{}, ctx.Err()
	case resp := <-respCh:
		return resp, nil
	}
}

func (s *service) Respond(resp Response) {
	s.pendingMu.Lock()
	respCh, ok := s.pending[resp.RequestID]
	s.pendingMu.Unlock()
	if !ok {
		return
	}
	select {
	case respCh <- resp:
	default:
	}
}

func NewService() Service {
	return &service{
		Broker:  pubsub.NewBroker[Request](),
		pending: make(map[string]chan Response),
	}
}

func ValidateChoices(choices []Choice) error {
	if len(choices) < 2 {
		return fmt.Errorf("at least two choices are required")
	}
	seen := make(map[string]struct{}, len(choices))
	for _, choice := range choices {
		if choice.ID == "" {
			return fmt.Errorf("choice id is required")
		}
		if choice.Label == "" {
			return fmt.Errorf("choice label is required")
		}
		if _, ok := seen[choice.ID]; ok {
			return fmt.Errorf("duplicate choice id %q", choice.ID)
		}
		seen[choice.ID] = struct{}{}
	}
	return nil
}
