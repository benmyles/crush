package agent

import (
	"context"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type streamCountLanguageModel struct {
	inner    fantasy.LanguageModel
	genCalls int
}

func (s *streamCountLanguageModel) Generate(ctx context.Context, call fantasy.Call) (*fantasy.Response, error) {
	s.genCalls++
	return s.inner.Generate(ctx, call)
}

func (s *streamCountLanguageModel) Stream(context.Context, fantasy.Call) (fantasy.StreamResponse, error) {
	return nil, assert.AnError
}

func (s *streamCountLanguageModel) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, assert.AnError
}

func (s *streamCountLanguageModel) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, assert.AnError
}

func (s *streamCountLanguageModel) Provider() string { return s.inner.Provider() }

func (s *streamCountLanguageModel) Model() string { return s.inner.Model() }

// fakeGen is a minimal stub; only Provider/Model/Generate are used by the wrapper.
type fakeGen struct {
	resp *fantasy.Response
	err  error
}

func (f *fakeGen) Generate(context.Context, fantasy.Call) (*fantasy.Response, error) {
	return f.resp, f.err
}

func (f *fakeGen) Stream(context.Context, fantasy.Call) (fantasy.StreamResponse, error) {
	return nil, assert.AnError
}

func (f *fakeGen) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, assert.AnError
}

func (f *fakeGen) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, assert.AnError
}

func (f *fakeGen) Provider() string { return "p" }

func (f *fakeGen) Model() string { return "m" }

func TestWrapLanguageModelForNonStreamingStreamUsesGenerate(t *testing.T) {
	t.Parallel()

	inner := &fakeGen{
		resp: &fantasy.Response{
			Content:      fantasy.ResponseContent{fantasy.TextContent{Text: "hi"}},
			FinishReason: fantasy.FinishReasonStop,
		},
	}
	wrapped := wrapLanguageModelForNonStreaming(inner, true)
	stream, err := wrapped.Stream(t.Context(), fantasy.Call{})
	require.NoError(t, err)

	var parts []fantasy.StreamPart
	for p := range stream {
		parts = append(parts, p)
	}
	require.NotEmpty(t, parts)
	assert.Equal(t, fantasy.StreamPartTypeFinish, parts[len(parts)-1].Type)

	counters := &streamCountLanguageModel{
		inner: &fakeGen{
			resp: &fantasy.Response{
				Content:      fantasy.ResponseContent{fantasy.TextContent{Text: "x"}},
				FinishReason: fantasy.FinishReasonStop,
			},
		},
	}
	wrapped2 := wrapLanguageModelForNonStreaming(counters, true)
	s2, err := wrapped2.Stream(t.Context(), fantasy.Call{})
	require.NoError(t, err)
	for range s2 {
	}
	assert.Equal(t, 1, counters.genCalls)
}

func TestWrapLanguageModelForNonStreamingPassthrough(t *testing.T) {
	t.Parallel()

	inner := &fakeGen{
		resp: &fantasy.Response{FinishReason: fantasy.FinishReasonStop},
	}
	wrapped := wrapLanguageModelForNonStreaming(inner, false)
	_, ok := wrapped.(*fakeGen)
	assert.True(t, ok)
}

func TestYieldResponseAsStreamTextAndToolCall(t *testing.T) {
	t.Parallel()

	resp := &fantasy.Response{
		Content: fantasy.ResponseContent{
			fantasy.TextContent{Text: "\nhello"},
			fantasy.ToolCallContent{ToolCallID: "c1", ToolName: "t", Input: `{"a":1}`},
		},
		FinishReason: fantasy.FinishReasonToolCalls,
		Usage:        fantasy.Usage{InputTokens: 1, OutputTokens: 2},
	}
	var types []fantasy.StreamPartType
	yieldResponseAsStream(resp, func(p fantasy.StreamPart) bool {
		types = append(types, p.Type)
		return true
	})
	require.Contains(t, types, fantasy.StreamPartTypeTextStart)
	require.Contains(t, types, fantasy.StreamPartTypeTextDelta)
	require.Contains(t, types, fantasy.StreamPartTypeTextEnd)
	require.Contains(t, types, fantasy.StreamPartTypeToolCall)
	require.Contains(t, types, fantasy.StreamPartTypeFinish)
}
