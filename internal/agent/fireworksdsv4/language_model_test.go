package fireworksdsv4

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func ssePayload(value map[string]any) string {
	data, _ := json.Marshal(value)
	return "data: " + string(data) + "\n\n"
}

func testModel(t *testing.T, response string, capture func(*http.Request, map[string]any)) *languageModel {
	t.Helper()
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var payload map[string]any
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		if capture != nil {
			capture(request, payload)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream; charset=utf-8"}},
			Body:       io.NopCloser(strings.NewReader(response)),
			Request:    request,
		}, nil
	})}
	provider, err := New(
		WithBaseURL("https://example.test/inference/v1"),
		WithAPIKey("secret"),
		WithDefaultReasoningEffort("none"),
		WithMaxOutputTokens(1000),
		WithHTTPClient(client),
	)
	require.NoError(t, err)
	model, err := provider.LanguageModel(t.Context(), "accounts/fireworks/models/deepseek-v4-flash")
	require.NoError(t, err)
	return model.(*languageModel)
}

func echoTool() fantasy.FunctionTool {
	return fantasy.FunctionTool{Name: "echo", Description: "Echo", InputSchema: map[string]any{
		"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}}, "required": []string{"value"}, "additionalProperties": false,
	}}
}

func collectParts(t *testing.T, model *languageModel, call fantasy.Call) []fantasy.StreamPart {
	t.Helper()
	stream, err := model.Stream(t.Context(), call)
	require.NoError(t, err)
	var parts []fantasy.StreamPart
	for part := range stream {
		parts = append(parts, part)
	}
	return parts
}

func TestStreamTranslatesChatMessagesAndToolCalls(t *testing.T) {
	t.Parallel()

	completion := toolCallsOpen +
		`<｜DSML｜invoke name="send_chat_message"><｜DSML｜parameter name="message" string="true">Working` + parameterClose + invokeClose +
		`<｜DSML｜invoke name="echo"><｜DSML｜parameter name="value" string="true">hello` + parameterClose + invokeClose + toolCallsClose
	response := ssePayload(map[string]any{"id": "resp", "choices": []any{
		map[string]any{"index": 1, "text": "ignored"},
		map[string]any{"index": 0, "text": completion},
	}}) + ssePayload(map[string]any{"choices": []any{map[string]any{"index": 0, "finish_reason": "stop"}}, "usage": map[string]any{
		"prompt_tokens": 10, "completion_tokens": 5, "prompt_tokens_details": map[string]any{"cached_tokens": 2}, "completion_tokens_details": map[string]any{"reasoning_tokens": 1},
	}}) + "data: [DONE]\n\n"

	model := testModel(t, response, func(request *http.Request, payload map[string]any) {
		require.Equal(t, "/inference/v1/completions", request.URL.Path)
		require.Equal(t, "Bearer secret", request.Header.Get("Authorization"))
		require.Equal(t, true, payload["stream"])
		require.Equal(t, float64(1), payload["temperature"])
		format := payload["response_format"].(map[string]any)
		require.Equal(t, "grammar", format["type"])
		require.Contains(t, format["grammar"], chatToolName)
	})
	parts := collectParts(t, model, fantasy.Call{
		Prompt: fantasy.Prompt{fantasy.NewSystemMessage("sys"), fantasy.NewUserMessage("work")},
		Tools:  []fantasy.Tool{echoTool()},
	})
	var text, input string
	var finish fantasy.StreamPart
	for _, part := range parts {
		switch part.Type {
		case fantasy.StreamPartTypeTextDelta:
			text += part.Delta
		case fantasy.StreamPartTypeToolCall:
			require.Equal(t, "echo", part.ToolCallName)
			input = part.ToolCallInput
		case fantasy.StreamPartTypeFinish:
			finish = part
		case fantasy.StreamPartTypeError:
			require.NoError(t, part.Error)
		}
	}
	require.Equal(t, "Working", text)
	require.JSONEq(t, `{"value":"hello"}`, input)
	require.Equal(t, fantasy.FinishReasonToolCalls, finish.FinishReason)
	require.Equal(t, int64(8), finish.Usage.InputTokens)
	require.Equal(t, int64(2), finish.Usage.CacheReadTokens)
	require.Equal(t, int64(1), finish.Usage.ReasoningTokens)
}

func TestStreamLoneChatMessageStopsAndToolFreeCallUsesText(t *testing.T) {
	t.Parallel()

	chat := toolCallsOpen + `<｜DSML｜invoke name="send_chat_message"><｜DSML｜parameter name="message" string="true">Done` + parameterClose + invokeClose + toolCallsClose
	response := ssePayload(map[string]any{"choices": []any{map[string]any{"index": 0, "text": chat, "finish_reason": "stop"}}}) + "data: [DONE]\n\n"
	parts := collectParts(t, testModel(t, response, nil), fantasy.Call{Prompt: fantasy.Prompt{fantasy.NewUserMessage("work")}, Tools: []fantasy.Tool{echoTool()}})
	require.Equal(t, fantasy.FinishReasonStop, parts[len(parts)-1].FinishReason)

	directResponse := ssePayload(map[string]any{"choices": []any{map[string]any{"index": 0, "text": "summary", "finish_reason": "stop"}}}) + "data: [DONE]\n\n"
	direct := collectParts(t, testModel(t, directResponse, func(_ *http.Request, payload map[string]any) {
		grammar := payload["response_format"].(map[string]any)["grammar"].(string)
		require.NotContains(t, grammar, chatToolName)
	}), fantasy.Call{Prompt: fantasy.Prompt{fantasy.NewUserMessage("summarize")}})
	var text string
	for _, part := range direct {
		if part.Type == fantasy.StreamPartTypeTextDelta {
			text += part.Delta
		}
	}
	require.Equal(t, "summary", text)
	require.Equal(t, fantasy.FinishReasonStop, direct[len(direct)-1].FinishReason)
}

func TestLengthTruncationNeverEmitsExecutableToolCall(t *testing.T) {
	t.Parallel()

	partial := toolCallsOpen + `<｜DSML｜invoke name="echo"><｜DSML｜parameter name="value" string="true">part`
	response := ssePayload(map[string]any{"choices": []any{map[string]any{"index": 0, "text": partial}}}) +
		ssePayload(map[string]any{"choices": []any{map[string]any{"index": 0, "finish_reason": "length"}}}) + "data: [DONE]\n\n"
	parts := collectParts(t, testModel(t, response, nil), fantasy.Call{Prompt: fantasy.Prompt{fantasy.NewUserMessage("work")}, Tools: []fantasy.Tool{echoTool()}})
	var sawStart, sawEnd, sawCall bool
	for _, part := range parts {
		switch part.Type {
		case fantasy.StreamPartTypeToolInputStart:
			sawStart = true
		case fantasy.StreamPartTypeToolInputEnd:
			sawEnd = true
		case fantasy.StreamPartTypeToolCall:
			sawCall = true
		}
	}
	require.True(t, sawStart)
	require.True(t, sawEnd)
	require.False(t, sawCall)
	require.Equal(t, fantasy.FinishReasonLength, parts[len(parts)-1].FinishReason)
}

func TestFantasyAgentExecutesValidatedCallsAndRejectsTruncatedCalls(t *testing.T) {
	t.Parallel()

	type echoParams struct {
		Value string `json:"value"`
	}
	newTool := func(calls *atomic.Int64) fantasy.AgentTool {
		return fantasy.NewAgentTool("echo", "Echo", func(_ context.Context, params echoParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			calls.Add(1)
			return fantasy.ToolResponse{Type: "text", Content: params.Value, StopTurn: true}, nil
		})
	}

	complete := dsmlEcho("hello")
	completeResponse := ssePayload(map[string]any{"choices": []any{map[string]any{"index": 0, "text": complete, "finish_reason": "stop"}}}) + "data: [DONE]\n\n"
	var completeCalls atomic.Int64
	agent := fantasy.NewAgent(testModel(t, completeResponse, nil), fantasy.WithTools(newTool(&completeCalls)), fantasy.WithMaxRetries(0))
	result, err := agent.Stream(t.Context(), fantasy.AgentStreamCall{Prompt: "work"})
	require.NoError(t, err)
	require.Equal(t, int64(1), completeCalls.Load())
	require.Equal(t, fantasy.FinishReasonToolCalls, result.Response.FinishReason)

	partial := toolCallsOpen + `<｜DSML｜invoke name="echo"><｜DSML｜parameter name="value" string="true">part`
	partialResponse := ssePayload(map[string]any{"choices": []any{map[string]any{"index": 0, "text": partial}}}) +
		ssePayload(map[string]any{"choices": []any{map[string]any{"index": 0, "finish_reason": "length"}}}) + "data: [DONE]\n\n"
	var partialCalls atomic.Int64
	truncatedAgent := fantasy.NewAgent(testModel(t, partialResponse, nil), fantasy.WithTools(newTool(&partialCalls)), fantasy.WithMaxRetries(0))
	result, err = truncatedAgent.Stream(t.Context(), fantasy.AgentStreamCall{Prompt: "work"})
	require.NoError(t, err)
	require.Zero(t, partialCalls.Load())
	require.Equal(t, fantasy.FinishReasonLength, result.Response.FinishReason)
}

func TestMissingDoneFailsWithoutAcceptingPartialCompletion(t *testing.T) {
	t.Parallel()

	response := ssePayload(map[string]any{"choices": []any{map[string]any{"index": 0, "finish_reason": "stop"}}})
	parts := collectParts(t, testModel(t, response, nil), fantasy.Call{Prompt: fantasy.Prompt{fantasy.NewUserMessage("work")}})
	require.Len(t, parts, 1)
	require.Equal(t, fantasy.StreamPartTypeError, parts[0].Type)
	var providerErr *fantasy.ProviderError
	require.ErrorAs(t, parts[0].Error, &providerErr)
	require.True(t, providerErr.IsRetryable())
}

func TestStreamCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	model := testModel(t, "", nil)
	stream, err := model.Stream(ctx, fantasy.Call{Prompt: fantasy.Prompt{fantasy.NewUserMessage("work")}})
	if err != nil {
		require.ErrorIs(t, err, context.Canceled)
		return
	}
	var streamErr error
	for part := range stream {
		if part.Type == fantasy.StreamPartTypeError {
			streamErr = part.Error
		}
	}
	require.ErrorIs(t, streamErr, context.Canceled)
}
