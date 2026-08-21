// Package notify defines domain notification types for agent events.
// These types are decoupled from UI concerns so the agent can publish
// events without importing UI packages.
package notify

import (
	"github.com/charmbracelet/crush/internal/goal"
	"github.com/charmbracelet/crush/internal/status"
)

// Type identifies the kind of agent notification.
type Type string

const (
	// TypeAgentFinished indicates the agent has completed its turn.
	TypeAgentFinished Type = "agent_finished"
	// TypeReAuthenticate indicates the agent encountered an
	// authentication error and the user needs to re-authenticate.
	TypeReAuthenticate Type = "re_authenticate"
	// TypeAgentError indicates the agent's turn terminated with an
	// error. The error text is carried in Notification.Message.
	TypeAgentError Type = "error"
	// TypeAWSSSOAuth indicates AWS SSO credentials have expired and the
	// coordinator is running the configured refresh command. It opens the
	// AWS SSO dialog; a follow-up with the same type carries the SSO URL
	// once it appears in the command output. AWSSOCommand carries the
	// command being run; AWSSOURL carries the verification URL when known.
	TypeAWSSSOAuth Type = "aws_sso_auth"
	// TypeAWSSSOAuthResult indicates the AWS SSO refresh command has
	// finished. Message carries the error text when it failed, empty on
	// success.
	TypeAWSSSOAuthResult Type = "aws_sso_auth_result"
	// TypeCompactionStarted indicates the compaction engine began running
	// for a session (the TUI shows a "Compacting" pulse while it runs).
	TypeCompactionStarted Type = "compaction_started"
	// TypeCompactionFinished indicates the compaction engine finished
	// (successfully or not) for a session.
	TypeCompactionFinished Type = "compaction_finished"
	// TypeCompactionProgress carries live token stats for a running
	// compaction (TokensDown/TokensOut), used to update the TUI pulse pill.
	TypeCompactionProgress Type = "compaction_progress"
	// TypeCompactionStream carries a live stream event from the compaction
	// model (reasoning or text deltas) as it generates the checkpoint. The
	// TUI renders them into a transient message in the chat, like any other
	// streaming assistant turn. Payload: CompactionStream.
	TypeCompactionStream Type = "compaction_stream"
	// TypeAgentPaused indicates a pause latch took effect: the run
	// stopped at a step boundary and is held until resumed.
	TypeAgentPaused Type = "agent_paused"
	// TypeAgentResumed indicates a pause latch was lifted and the held
	// turn is continuing.
	TypeAgentResumed Type = "agent_resumed"
	// TypeGoalStateChanged carries a goal state transition (set,
	// updated, completed, blocked, stalled). Payload: Notification.Goal.
	TypeGoalStateChanged Type = "goal_state_changed"
	// TypeStatusUpdate carries a new agent status update recorded by the
	// status_update tool. Payload: Notification.StatusUpdate.
	TypeStatusUpdate Type = "status_update"
	// TypeTerminalTitleChanged carries a new agent-curated terminal
	// window title set by the set_terminal_title tool. Payload:
	// Notification.TerminalTitle (empty clears the custom title).
	TypeTerminalTitleChanged Type = "terminal_title_changed"
)

// CompactionStreamKind identifies the kind of a live compaction stream
// event.
type CompactionStreamKind string

const (
	// CompactionStreamReset clears previously streamed output: the lane
	// attempt is starting over (escalation, retry, deterministic fallback).
	CompactionStreamReset CompactionStreamKind = "reset"
	// CompactionStreamReasoningDelta appends a reasoning delta.
	CompactionStreamReasoningDelta CompactionStreamKind = "reasoning_delta"
	// CompactionStreamReasoningEnd marks the end of the reasoning block.
	CompactionStreamReasoningEnd CompactionStreamKind = "reasoning_end"
	// CompactionStreamTextDelta appends a text delta.
	CompactionStreamTextDelta CompactionStreamKind = "text_delta"
)

// CompactionStreamEvent is the payload of TypeCompactionStream.
type CompactionStreamEvent struct {
	Kind CompactionStreamKind
	// Lane names the producing lane ("checkpoint").
	Lane string
	// Text carries the delta for the delta kinds, and the complete body
	// when a non-streaming fallback emits output.
	Text string
}

// Notification represents a domain event published by the agent.
type Notification struct {
	SessionID    string
	SessionTitle string
	Type         Type
	ProviderID   string
	// RunID, when non-empty, is the caller-supplied correlator
	// (proto.AgentMessage.RunID) for the run that produced this
	// notification. It lets observers attribute a TypeAgentError to a
	// specific request rather than to any in-flight run on the
	// session. Empty when no caller set one.
	RunID string
	// Internal marks synthetic turns (goal checks, pause resumptions)
	// so the UI can suppress the desktop "waiting" ping while still
	// treating the event as a busy-state edge.
	Internal bool
	// Goal carries the new goal state for TypeGoalStateChanged. Nil
	// for every other type.
	Goal *goal.Goal
	// StatusUpdate carries the recorded update for TypeStatusUpdate.
	// Nil for every other type.
	StatusUpdate *status.Update
	// TerminalTitle carries the agent-curated terminal window title for
	// TypeTerminalTitleChanged. Empty clears the custom title; other
	// types ignore it.
	TerminalTitle string
	// Message carries the error text for TypeAgentError. Other
	// notification types ignore it.
	Message string
	// AWSSOCommand carries the shell command for TypeAWSSSOAuth.
	AWSSOCommand string
	// AWSSOURL carries the SSO verification URL for TypeAWSSSOAuth once it
	// appears in the refresh command's output.
	AWSSOURL string
	// TokensDown carries the live estimated tokens removed from the active
	// context so far for TypeCompactionProgress.
	TokensDown int64
	// TokensOut carries the live estimated tokens composed into the summary
	// so far for TypeCompactionProgress.
	TokensOut int64
	// CompactionStream carries the live stream event for
	// TypeCompactionStream. Nil for every other type.
	CompactionStream *CompactionStreamEvent
}

// RunComplete is the authoritative end-of-run signal for a session.
// It is published exactly once per top-level agent run (per
// [sessionAgent.Run] invocation that actually executed) after all
// message updates for the turn have been flushed via
// message.Service.FlushAll. Carries the final assistant text and
// message ID so non-interactive clients can reconcile stdout even if
// SSE events arrive out of order or are dropped by the broker. Error
// is non-empty when the run terminated with an error; Cancelled is
// true when the run terminated due to context cancellation. The two
// are mutually exclusive in the success case but may overlap when a
// cancel triggers a downstream error.
//
// RunID identifies the specific request that produced this event.
// It is the value the caller set on `proto.AgentMessage.RunID` (or
// equivalently propagated via agent.WithRunID on the context that
// reaches the coordinator); empty when no caller set one. Filtering
// by RunID lets a client correlate a SendMessage call with its
// terminal event even when the session is busy and other turns are
// finishing on the same session.
type RunComplete struct {
	SessionID string
	RunID     string
	MessageID string
	Text      string
	Error     string
	Cancelled bool
}
