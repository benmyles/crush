package config

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"cloud.google.com/go/auth"
	"github.com/charmbracelet/crush/internal/env"
	"github.com/charmbracelet/crush/internal/googleadc"
	"github.com/stretchr/testify/require"
)

type staticTokenProvider struct {
	token *auth.Token
}

func (s staticTokenProvider) Token(context.Context) (*auth.Token, error) {
	return s.token, nil
}

func TestProviderConfigTestConnectionGoogleADC(t *testing.T) {
	t.Parallel()

	type requestMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	var gotPath string
	var gotAuthHeader string
	var gotTestHeader string
	var gotContentType string
	var payload map[string]json.RawMessage

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)

		gotPath = r.URL.Path
		gotAuthHeader = r.Header.Get("Authorization")
		gotTestHeader = r.Header.Get("X-Test")
		gotContentType = r.Header.Get("Content-Type")

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &payload))

		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(server.Close)

	originalClientFactory := newGoogleADCHTTPClient
	newGoogleADCHTTPClient = func(context.Context, *http.Client) (*http.Client, error) {
		return googleadc.NewHTTPClientWithTokenProvider(nil, staticTokenProvider{
			token: &auth.Token{Value: "adc-token"},
		}), nil
	}
	t.Cleanup(func() {
		newGoogleADCHTTPClient = originalClientFactory
	})

	cfg := ProviderConfig{
		ID:       "vertex-openapi",
		Type:     catwalk.TypeOpenAICompat,
		AuthMode: ProviderAuthModeGoogleADC,
		BaseURL:  server.URL + "/v1/projects/test/locations/us-central1/endpoints/openapi",
		ExtraHeaders: map[string]string{
			"Authorization": "Bearer stale-token",
			"X-Test":        "expected",
		},
		Models: []catwalk.Model{{
			ID: "zai-org/glm-5-maas",
		}},
	}

	resolver := NewEnvironmentVariableResolver(env.NewFromMap(map[string]string{}))
	err := cfg.TestConnection(resolver)
	require.NoError(t, err)

	require.Equal(t, "/v1/projects/test/locations/us-central1/endpoints/openapi/chat/completions", gotPath)
	require.Equal(t, "Bearer adc-token", gotAuthHeader)
	require.Equal(t, "expected", gotTestHeader)
	require.Equal(t, "application/json", gotContentType)

	require.Contains(t, payload, "model")
	require.Contains(t, payload, "stream")
	require.Contains(t, payload, "messages")
	require.Contains(t, payload, "max_tokens")

	var model string
	require.NoError(t, json.Unmarshal(payload["model"], &model))
	require.Equal(t, "zai-org/glm-5-maas", model)

	var stream bool
	require.NoError(t, json.Unmarshal(payload["stream"], &stream))
	require.False(t, stream)

	var maxTokens int
	require.NoError(t, json.Unmarshal(payload["max_tokens"], &maxTokens))
	require.Equal(t, 1, maxTokens)

	var messages []requestMessage
	require.NoError(t, json.Unmarshal(payload["messages"], &messages))
	require.Len(t, messages, 1)
	require.Equal(t, "user", messages[0].Role)
	require.Equal(t, "ping", messages[0].Content)
}
