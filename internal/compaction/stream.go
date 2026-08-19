package compaction

import "context"

// StreamKind identifies the kind of a live stream event published while a
// compaction lane generates its output.
type StreamKind int

const (
	// StreamReset clears any previously streamed lane output: an attempt is
	// starting over (escalation, truncation retry, deterministic fallback).
	StreamReset StreamKind = iota
	// StreamReasoningDelta appends a reasoning delta to the lane output.
	StreamReasoningDelta
	// StreamReasoningEnd marks the end of the reasoning block.
	StreamReasoningEnd
	// StreamTextDelta appends a text delta to the lane output.
	StreamTextDelta
)

// LaneCheckpoint names the checkpoint lane, the only lane that streams live
// output.
const LaneCheckpoint = "checkpoint"

// StreamEvent is one live stream event for a running lane.
type StreamEvent struct {
	Kind StreamKind
	// Lane names the producing lane ("checkpoint").
	Lane string
	// Text carries the delta for the delta kinds, and the complete body when
	// the deterministic fallback emits its non-streamed output.
	Text string
}

// StreamObserver receives live stream events for a session. It must be fast
// and must not block the compaction.
type StreamObserver func(sessionID string, ev StreamEvent)

type streamCtxKey struct{}

// WithStream attaches an observer to the context so lanes and completers can
// emit live events. A nil observer detaches.
func WithStream(ctx context.Context, obs StreamObserver) context.Context {
	if obs == nil {
		return ctx
	}
	return context.WithValue(ctx, streamCtxKey{}, obs)
}

// StreamFrom returns the observer attached to the context, if any.
func StreamFrom(ctx context.Context) (StreamObserver, bool) {
	obs, ok := ctx.Value(streamCtxKey{}).(StreamObserver)
	if ok && obs != nil {
		return obs, true
	}
	return nil, false
}

// emitStream forwards a stream event to the context-attached observer. A
// missing observer makes it a no-op.
func emitStream(ctx context.Context, sessionID string, ev StreamEvent) {
	if obs, ok := StreamFrom(ctx); ok {
		obs(sessionID, ev)
	}
}
