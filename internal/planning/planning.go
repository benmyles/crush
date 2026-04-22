package planning

import (
	"context"
	"sync"

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

type Response struct {
	SubmissionID   string `json:"submission_id"`
	Approved       bool   `json:"approved"`
	Comment        string `json:"comment,omitempty"`
	CompactHistory bool   `json:"compact_history,omitempty"`
}

type Mode string

const (
	ModeEnter Mode = "enter"
	ModeExit  Mode = "exit"
)

type ModeChangeRequest struct {
	ID         string `json:"id"`
	SessionID  string `json:"session_id"`
	ToolCallID string `json:"tool_call_id"`
	Mode       Mode   `json:"mode"`
	Prompt     string `json:"prompt,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type Service interface {
	pubsub.Subscriber[Submission]
	Submit(context.Context, Submission) (Submission, Response, error)
	Respond(Response)
	RequestModeChange(context.Context, ModeChangeRequest) (ModeChangeRequest, error)
	SubscribeModeChanges(context.Context) <-chan pubsub.Event[ModeChangeRequest]
}

type service struct {
	*pubsub.Broker[Submission]

	modeChanges *pubsub.Broker[ModeChangeRequest]
	pending     map[string]chan Response
	pendingMu   sync.Mutex
}

func (s *service) Submit(ctx context.Context, submission Submission) (Submission, Response, error) {
	if submission.ID == "" {
		submission.ID = uuid.NewString()
	}

	respCh := make(chan Response, 1)
	s.pendingMu.Lock()
	s.pending[submission.ID] = respCh
	s.pendingMu.Unlock()
	defer func() {
		s.pendingMu.Lock()
		delete(s.pending, submission.ID)
		s.pendingMu.Unlock()
	}()

	s.Publish(pubsub.CreatedEvent, submission)

	select {
	case <-ctx.Done():
		return submission, Response{}, ctx.Err()
	case resp := <-respCh:
		if resp.SubmissionID == "" {
			resp.SubmissionID = submission.ID
		}
		return submission, resp, nil
	}
}

func (s *service) Respond(resp Response) {
	s.pendingMu.Lock()
	respCh, ok := s.pending[resp.SubmissionID]
	s.pendingMu.Unlock()
	if !ok {
		return
	}
	select {
	case respCh <- resp:
	default:
	}
}

func (s *service) RequestModeChange(ctx context.Context, request ModeChangeRequest) (ModeChangeRequest, error) {
	if request.ID == "" {
		request.ID = uuid.NewString()
	}

	select {
	case <-ctx.Done():
		return request, ctx.Err()
	default:
	}

	s.modeChanges.Publish(pubsub.CreatedEvent, request)
	return request, nil
}

func (s *service) SubscribeModeChanges(ctx context.Context) <-chan pubsub.Event[ModeChangeRequest] {
	return s.modeChanges.Subscribe(ctx)
}

func NewService() Service {
	return &service{
		Broker:      pubsub.NewBroker[Submission](),
		modeChanges: pubsub.NewBroker[ModeChangeRequest](),
		pending:     make(map[string]chan Response),
	}
}
