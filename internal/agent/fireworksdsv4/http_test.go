package fireworksdsv4

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"
)

func TestCompletionEndpoint(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"https://api.fireworks.ai/inference":           "https://api.fireworks.ai/inference/v1/completions",
		"https://api.fireworks.ai/inference/v1/":       "https://api.fireworks.ai/inference/v1/completions",
		"https://api.fireworks.ai/inference?ignored=1": "https://api.fireworks.ai/inference/v1/completions",
	}
	for input, expected := range tests {
		actual, err := completionEndpoint(input)
		require.NoError(t, err)
		require.Equal(t, expected, actual)
	}
}

func TestConsumeSSEHandlesCRAndMultilineData(t *testing.T) {
	t.Parallel()

	body := "data: {\"choices\":\rdata: [{\"index\":0,\"text\":\"ok\",\"finish_reason\":\"stop\"}]}\r\rdata: [DONE]\r\r"
	var payloads []map[string]any
	err := consumeSSE(t.Context(), bytes.NewBufferString(body), func(payload map[string]any) error {
		payloads = append(payloads, payload)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, payloads, 1)
	require.Equal(t, "ok", choiceZero(payloads[0])["text"])
}

func TestConsumeSSERequiresDone(t *testing.T) {
	t.Parallel()

	err := consumeSSE(t.Context(), bytes.NewBufferString("data: {}\n\n"), func(map[string]any) error { return nil })
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

func TestHTTPErrorRetryClassification(t *testing.T) {
	t.Parallel()

	responseFor := func(status int, headers http.Header) *languageModel {
		client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: headers, Body: io.NopCloser(bytes.NewBufferString("failed")), Request: request}, nil
		})}
		return &languageModel{modelID: "accounts/fireworks/models/deepseek-v4-flash", baseURL: "https://example.test/v1", httpClient: client}
	}
	call := fantasy.Call{Prompt: fantasy.Prompt{fantasy.NewUserMessage("hello")}}

	_, err := responseFor(http.StatusInternalServerError, http.Header{"X-Should-Retry": []string{"false"}}).Stream(t.Context(), call)
	var fantasyErr *fantasy.Error
	require.ErrorAs(t, err, &fantasyErr)

	_, err = responseFor(http.StatusTooEarly, nil).Stream(t.Context(), call)
	var providerErr *fantasy.ProviderError
	require.ErrorAs(t, err, &providerErr)
	require.True(t, providerErr.IsRetryable())

	_, err = responseFor(http.StatusTooManyRequests, http.Header{"Retry-After": []string{"120"}}).Stream(t.Context(), call)
	require.ErrorAs(t, err, &fantasyErr)
}

func TestPostCompletionHonorsContextCancellation(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	model := &languageModel{baseURL: "https://example.test/v1", httpClient: client}
	ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
	defer cancel()
	response, err := model.postCompletion(ctx, fantasy.Call{}, map[string]any{})
	if response != nil {
		require.NoError(t, response.Body.Close())
	}
	require.True(t, errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled))
}

func TestJSONResponseFallback(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(`{"choices":[{"index":0,"text":"done","finish_reason":"stop"}]}`)),
			Request:    request,
		}, nil
	})}
	provider, err := New(WithBaseURL("https://example.test/v1"), WithDefaultReasoningEffort("none"), WithHTTPClient(client))
	require.NoError(t, err)
	modelValue, err := provider.LanguageModel(t.Context(), "accounts/fireworks/models/deepseek-v4-flash")
	require.NoError(t, err)
	response, err := modelValue.Generate(t.Context(), fantasy.Call{Prompt: fantasy.Prompt{fantasy.NewUserMessage("hello")}})
	require.NoError(t, err)
	require.Equal(t, "done", response.Content.Text())
	require.Equal(t, fantasy.FinishReasonStop, response.FinishReason)
}
