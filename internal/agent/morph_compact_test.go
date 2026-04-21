package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMorphCompactClientSendsRequest(t *testing.T) {
	t.Parallel()

	var got morphCompactRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/compact", r.URL.Path)
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.NotEmpty(t, r.Header.Get("User-Agent"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		_, err := w.Write([]byte(`{"id":"cmpr-test","model":"morph-compactor","output":"Compacted output","usage":{"input_tokens":10,"output_tokens":5,"compression_ratio":0.5,"processing_time_ms":12}}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	ratio := 0.7
	preserveRecent := 0
	client, err := newMorphCompactClient(config.MorphCompactOptions{
		Enabled:          true,
		APIKey:           "test-key",
		BaseURL:          server.URL + "/v1",
		CompressionRatio: &ratio,
		PreserveRecent:   &preserveRecent,
	})
	require.NoError(t, err)

	resp, err := client.compact(t.Context(), morphCompactRequest{
		Messages: []morphCompactMessage{
			{Role: "user", Content: "hello"},
		},
		Query:             "hello",
		CompressionRatio:  ratio,
		PreserveRecent:    preserveRecent,
		IncludeMarkers:    true,
		IncludeLineRanges: false,
		Model:             morphCompactModel,
	})
	require.NoError(t, err)
	require.Equal(t, "Compacted output", resp.Output)
	assert.Equal(t, "hello", got.Query)
	assert.Equal(t, 0.7, got.CompressionRatio)
	assert.Equal(t, 0, got.PreserveRecent)
	assert.True(t, got.IncludeMarkers)
	assert.False(t, got.IncludeLineRanges)
	assert.Equal(t, morphCompactModel, got.Model)
	require.Len(t, got.Messages, 1)
	assert.Equal(t, "user", got.Messages[0].Role)
	assert.Equal(t, "hello", got.Messages[0].Content)
}

func TestMorphCompactClientReturnsStatusError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "invalid key", http.StatusUnauthorized)
	}))
	defer server.Close()

	client, err := newMorphCompactClient(config.MorphCompactOptions{
		Enabled: true,
		APIKey:  "test-key",
		BaseURL: server.URL + "/v1",
	})
	require.NoError(t, err)

	_, err = client.compact(t.Context(), morphCompactRequest{
		Messages: []morphCompactMessage{{Role: "user", Content: "hello"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 401")
	assert.Contains(t, err.Error(), "invalid key")
}

func TestSessionAgentCompactCreatesSummaryAnchor(t *testing.T) {
	t.Parallel()

	var got morphCompactRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		_, err := w.Write([]byte(`{"id":"cmpr-test","model":"morph-compactor","output":"Compacted transcript"}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	env := testEnv(t)
	agent := testSessionAgent(env, newFakeLanguageModel("test-provider", "large-model"), newFakeLanguageModel("test-provider", "small-model"), "")

	currentSession, err := env.sessions.Create(t.Context(), "Session")
	require.NoError(t, err)
	_, err = env.messages.Create(t.Context(), currentSession.ID, message.CreateMessageParams{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: "Please compact this later."}},
	})
	require.NoError(t, err)

	err = agent.Compact(t.Context(), currentSession.ID, config.MorphCompactOptions{
		Enabled: true,
		APIKey:  "test-key",
		BaseURL: server.URL + "/v1",
	})
	require.NoError(t, err)

	currentSession, err = env.sessions.Get(t.Context(), currentSession.ID)
	require.NoError(t, err)
	require.NotEmpty(t, currentSession.SummaryMessageID)

	msgs, err := env.messages.List(t.Context(), currentSession.ID)
	require.NoError(t, err)
	require.Len(t, msgs, 2)

	var compactMsg message.Message
	for _, msg := range msgs {
		if msg.ID == currentSession.SummaryMessageID {
			compactMsg = msg
		}
	}
	require.True(t, compactMsg.IsSummaryMessage)
	assert.Equal(t, message.Assistant, compactMsg.Role)
	assert.Equal(t, "Compacted transcript", compactMsg.Content().Text)
	assert.Equal(t, message.FinishReasonEndTurn, compactMsg.FinishReason())
	assert.Equal(t, "morph-compactor", compactMsg.Model)
	assert.Equal(t, "morph", compactMsg.Provider)

	assert.Equal(t, "Please compact this later.", got.Query)
	assert.Equal(t, config.DefaultMorphCompactCompressionRatio, got.CompressionRatio)
	assert.Equal(t, config.DefaultMorphCompactPreserveRecent, got.PreserveRecent)
	require.Len(t, got.Messages, 1)
	assert.Equal(t, "Please compact this later.", got.Messages[0].Content)
}

func TestSessionAgentCompactUsesEffectiveHistory(t *testing.T) {
	t.Parallel()

	var got morphCompactRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		_, err := w.Write([]byte(`{"id":"cmpr-test","model":"morph-compactor","output":"New compacted transcript"}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	env := testEnv(t)
	agent := testSessionAgent(env, newFakeLanguageModel("test-provider", "large-model"), newFakeLanguageModel("test-provider", "small-model"), "")

	currentSession, err := env.sessions.Create(t.Context(), "Session")
	require.NoError(t, err)
	_, err = env.messages.Create(t.Context(), currentSession.ID, message.CreateMessageParams{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: "Old message that should be skipped."}},
	})
	require.NoError(t, err)
	summaryMsg, err := env.messages.Create(t.Context(), currentSession.ID, message.CreateMessageParams{
		Role:             message.Assistant,
		Parts:            []message.ContentPart{message.TextContent{Text: "Existing compacted context."}},
		IsSummaryMessage: true,
	})
	require.NoError(t, err)
	summaryMsg.AddFinish(message.FinishReasonEndTurn, "", "")
	require.NoError(t, env.messages.Update(t.Context(), summaryMsg))
	currentSession.SummaryMessageID = summaryMsg.ID
	_, err = env.sessions.Save(t.Context(), currentSession)
	require.NoError(t, err)

	_, err = env.messages.Create(t.Context(), currentSession.ID, message.CreateMessageParams{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: "Current task."}},
	})
	require.NoError(t, err)

	err = agent.Compact(t.Context(), currentSession.ID, config.MorphCompactOptions{
		Enabled: true,
		APIKey:  "test-key",
		BaseURL: server.URL + "/v1",
	})
	require.NoError(t, err)

	require.Len(t, got.Messages, 2)
	assert.NotContains(t, got.Messages[0].Content, "Old message")
	assert.Equal(t, "Existing compacted context.", got.Messages[0].Content)
	assert.Equal(t, "Current task.", got.Messages[1].Content)
	assert.Equal(t, "Current task.", got.Query)
}

func TestSessionAgentCompactDisabledAndMissingKey(t *testing.T) {
	t.Parallel()

	env := testEnv(t)
	agent := testSessionAgent(env, newFakeLanguageModel("test-provider", "large-model"), newFakeLanguageModel("test-provider", "small-model"), "")

	currentSession, err := env.sessions.Create(t.Context(), "Session")
	require.NoError(t, err)
	_, err = env.messages.Create(t.Context(), currentSession.ID, message.CreateMessageParams{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: "Please compact."}},
	})
	require.NoError(t, err)

	err = agent.Compact(t.Context(), currentSession.ID, config.MorphCompactOptions{})
	require.ErrorIs(t, err, ErrMorphCompactDisabled)

	err = agent.Compact(t.Context(), currentSession.ID, config.MorphCompactOptions{Enabled: true})
	require.ErrorIs(t, err, ErrMorphCompactAPIKeyMissing)
}
