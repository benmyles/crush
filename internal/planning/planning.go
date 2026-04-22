package planning

import (
	"context"

	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/google/uuid"
)

type Submission struct {
	ID         string         `json:"id"`
	SessionID  string         `json:"session_id"`
	ToolCallID string         `json:"tool_call_id"`
	Markdown   string         `json:"markdown"`
	Todos      []session.Todo `json:"todos"`
}

type Service interface {
	pubsub.Subscriber[Submission]
	Submit(context.Context, Submission) (Submission, error)
}

type service struct {
	*pubsub.Broker[Submission]
}

func (s *service) Submit(_ context.Context, submission Submission) (Submission, error) {
	if submission.ID == "" {
		submission.ID = uuid.NewString()
	}
	s.Publish(pubsub.CreatedEvent, submission)
	return submission, nil
}

func NewService() Service {
	return &service{
		Broker: pubsub.NewBroker[Submission](),
	}
}
