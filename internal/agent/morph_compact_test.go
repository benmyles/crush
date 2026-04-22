package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/pubsub"
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
		_, err := w.Write([]byte(`{"id":"cmpr-test","model":"morph-compactor","output":"Compacted transcript","usage":{"input_tokens":100,"output_tokens":7}}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	env := testEnv(t)
	large := newFakeLanguageModel("test-provider", "large-model")
	agent := newMorphCompactTestAgent(env, large, newFakeLanguageModel("test-provider", "small-model"))

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
	}, nil)
	require.NoError(t, err)

	currentSession, err = env.sessions.Get(t.Context(), currentSession.ID)
	require.NoError(t, err)
	require.NotEmpty(t, currentSession.SummaryMessageID)
	assert.Zero(t, currentSession.PromptTokens)
	assert.Equal(t, int64(7), currentSession.CompletionTokens)

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
	assert.Contains(t, compactMsg.Content().Text, `<compacted_context strategy="morph" source="morph">`)
	assert.Contains(t, compactMsg.Content().Text, "Crush compacted the earlier conversation with Morph")
	assert.Contains(t, compactMsg.Content().Text, "Compacted transcript")
	assert.Equal(t, message.FinishReasonEndTurn, compactMsg.FinishReason())
	assert.Equal(t, "morph-compactor", compactMsg.Model)
	assert.Equal(t, "morph", compactMsg.Provider)
	assert.Zero(t, large.GenerateCallCount())

	assert.Equal(t, "Please compact this later.", got.Query)
	assert.Equal(t, config.DefaultMorphCompactCompressionRatio, got.CompressionRatio)
	assert.Equal(t, config.DefaultMorphCompactPreserveRecent, got.PreserveRecent)
	require.Len(t, got.Messages, 1)
	assert.Equal(t, "Please compact this later.", got.Messages[0].Content)
	assert.NotContains(t, got.Messages[0].Content, "Model summary")
}

func TestSessionAgentSummarizeThenCompactCreatesMorphAnchorAndModelSummary(t *testing.T) {
	t.Parallel()

	var got morphCompactRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		_, err := w.Write([]byte(`{"id":"cmpr-test","model":"morph-compactor","output":"Compacted transcript","usage":{"input_tokens":100,"output_tokens":7}}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	env := testEnv(t)
	agent := newMorphCompactTestAgent(env, newSummaryFakeLanguageModel(t, "Model summary", 5), newFakeLanguageModel("test-provider", "small-model"))

	currentSession, err := env.sessions.Create(t.Context(), "Session")
	require.NoError(t, err)
	_, err = env.messages.Create(t.Context(), currentSession.ID, message.CreateMessageParams{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: "Please compact this later."}},
	})
	require.NoError(t, err)

	err = agent.SummarizeThenCompact(t.Context(), currentSession.ID, config.MorphCompactOptions{
		Enabled: true,
		APIKey:  "test-key",
		BaseURL: server.URL + "/v1",
	}, nil)
	require.NoError(t, err)

	currentSession, err = env.sessions.Get(t.Context(), currentSession.ID)
	require.NoError(t, err)
	require.NotEmpty(t, currentSession.SummaryMessageID)
	assert.Zero(t, currentSession.PromptTokens)
	assert.Equal(t, int64(12), currentSession.CompletionTokens)

	msgs, err := env.messages.List(t.Context(), currentSession.ID)
	require.NoError(t, err)
	require.Len(t, msgs, 3)

	var compactMsg, modelSummaryMsg message.Message
	for _, msg := range msgs {
		if msg.ID == currentSession.SummaryMessageID {
			compactMsg = msg
			continue
		}
		if msg.IsSummaryMessage {
			modelSummaryMsg = msg
		}
	}
	require.True(t, compactMsg.IsSummaryMessage)
	assert.Contains(t, compactMsg.Content().Text, `<compacted_context strategy="summarize_then_morph" source="morph">`)
	assert.Contains(t, compactMsg.Content().Text, "Crush first summarized the earlier conversation and then compacted it with Morph")
	assert.Contains(t, compactMsg.Content().Text, "Compacted transcript")
	assert.Equal(t, "morph-compactor", compactMsg.Model)
	assert.Equal(t, "morph", compactMsg.Provider)
	require.True(t, modelSummaryMsg.IsSummaryMessage)
	assert.Contains(t, modelSummaryMsg.Content().Text, `<compacted_context strategy="summarize_then_morph" source="model_summary">`)
	assert.Contains(t, modelSummaryMsg.Content().Text, "Crush generated this model summary before running Morph")
	assert.Contains(t, modelSummaryMsg.Content().Text, "Model summary")
	assert.Equal(t, "large-model", modelSummaryMsg.Model)
	assert.Equal(t, "test-provider", modelSummaryMsg.Provider)

	assert.Equal(t, "Please compact this later.", got.Query)
	require.Len(t, got.Messages, 1)
	assert.Equal(t, "Please compact this later.", got.Messages[0].Content)
	assert.NotContains(t, got.Messages[0].Content, "Model summary")
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
	agent := newMorphCompactTestAgent(env, newSummaryFakeLanguageModel(t, "Model summary", 4), newFakeLanguageModel("test-provider", "small-model"))

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
	}, nil)
	require.NoError(t, err)

	require.Len(t, got.Messages, 2)
	assert.NotContains(t, got.Messages[0].Content, "Old message")
	assert.Equal(t, "Existing compacted context.", got.Messages[0].Content)
	assert.Equal(t, "Current task.", got.Messages[1].Content)
	assert.Equal(t, "Current task.", got.Query)
	assert.NotContains(t, got.Messages[0].Content, "Model summary")
	assert.NotContains(t, got.Messages[1].Content, "Model summary")
}

func TestSessionAgentCompactPublishesCompactionNotifications(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte(`{"id":"cmpr-test","model":"morph-compactor","output":"Compacted transcript"}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	env := testEnv(t)
	publisher := &recordingNotifyPublisher{}
	agent := newMorphCompactTestAgentWithNotify(
		env,
		newSummaryFakeLanguageModel(t, "Model summary", 5),
		newFakeLanguageModel("test-provider", "small-model"),
		publisher,
	)

	currentSession, err := env.sessions.Create(t.Context(), "Session")
	require.NoError(t, err)
	_, err = env.messages.Create(t.Context(), currentSession.ID, message.CreateMessageParams{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: "Please compact."}},
	})
	require.NoError(t, err)

	err = agent.Compact(t.Context(), currentSession.ID, config.MorphCompactOptions{
		Enabled: true,
		APIKey:  "test-key",
		BaseURL: server.URL + "/v1",
	}, nil)
	require.NoError(t, err)

	events := publisher.Events()
	require.Len(t, events, 2)
	assert.Equal(t, pubsub.CreatedEvent, events[0].Type)
	assert.Equal(t, notify.TypeCompactionStarted, events[0].Payload.Type)
	assert.Equal(t, currentSession.ID, events[0].Payload.SessionID)
	assert.Equal(t, "Session", events[0].Payload.SessionTitle)
	assert.Equal(t, pubsub.CreatedEvent, events[1].Type)
	assert.Equal(t, notify.TypeCompactionFinished, events[1].Payload.Type)
	assert.Equal(t, currentSession.ID, events[1].Payload.SessionID)
	assert.Equal(t, "Session", events[1].Payload.SessionTitle)
}

func TestSessionAgentSummarizePublishesCompactionNotifications(t *testing.T) {
	t.Parallel()

	env := testEnv(t)
	publisher := &recordingNotifyPublisher{}
	agent := newMorphCompactTestAgentWithNotify(
		env,
		newSummaryFakeLanguageModel(t, "Model summary", 5),
		newFakeLanguageModel("test-provider", "small-model"),
		publisher,
	)

	currentSession, err := env.sessions.Create(t.Context(), "Session")
	require.NoError(t, err)
	_, err = env.messages.Create(t.Context(), currentSession.ID, message.CreateMessageParams{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: "Please summarize."}},
	})
	require.NoError(t, err)

	err = agent.Summarize(t.Context(), currentSession.ID, nil)
	require.NoError(t, err)

	events := publisher.Events()
	require.Len(t, events, 2)
	assert.Equal(t, pubsub.CreatedEvent, events[0].Type)
	assert.Equal(t, notify.TypeCompactionStarted, events[0].Payload.Type)
	assert.Equal(t, currentSession.ID, events[0].Payload.SessionID)
	assert.Equal(t, "Session", events[0].Payload.SessionTitle)
	assert.Equal(t, pubsub.CreatedEvent, events[1].Type)
	assert.Equal(t, notify.TypeCompactionFinished, events[1].Payload.Type)
	assert.Equal(t, currentSession.ID, events[1].Payload.SessionID)
	assert.Equal(t, "Session", events[1].Payload.SessionTitle)
}

func TestSessionAgentCompactMorphFailureDoesNotPersistSummaryAnchor(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "compact failed", http.StatusInternalServerError)
	}))
	defer server.Close()

	env := testEnv(t)
	large := newSummaryFakeLanguageModel(t, "Model summary", 5)
	publisher := &recordingNotifyPublisher{}
	agent := newMorphCompactTestAgentWithNotify(env, large, newFakeLanguageModel("test-provider", "small-model"), publisher)

	currentSession, err := env.sessions.Create(t.Context(), "Session")
	require.NoError(t, err)
	_, err = env.messages.Create(t.Context(), currentSession.ID, message.CreateMessageParams{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: "Please compact."}},
	})
	require.NoError(t, err)

	err = agent.Compact(t.Context(), currentSession.ID, config.MorphCompactOptions{
		Enabled: true,
		APIKey:  "test-key",
		BaseURL: server.URL + "/v1",
	}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 500")
	assert.Zero(t, large.GenerateCallCount())

	currentSession, err = env.sessions.Get(t.Context(), currentSession.ID)
	require.NoError(t, err)
	assert.Empty(t, currentSession.SummaryMessageID)

	msgs, err := env.messages.List(t.Context(), currentSession.ID)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.False(t, msgs[0].IsSummaryMessage)

	events := publisher.Events()
	require.Len(t, events, 2)
	assert.Equal(t, notify.TypeCompactionStarted, events[0].Payload.Type)
	assert.Equal(t, notify.TypeCompactionFinished, events[1].Payload.Type)
}

func TestSessionAgentCompactDoesNotPublishNotificationsForEmptySession(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte(`{"id":"cmpr-test","model":"morph-compactor","output":"Compacted transcript"}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	env := testEnv(t)
	publisher := &recordingNotifyPublisher{}
	agent := newMorphCompactTestAgentWithNotify(
		env,
		newSummaryFakeLanguageModel(t, "Model summary", 5),
		newFakeLanguageModel("test-provider", "small-model"),
		publisher,
	)

	currentSession, err := env.sessions.Create(t.Context(), "Session")
	require.NoError(t, err)

	err = agent.Compact(t.Context(), currentSession.ID, config.MorphCompactOptions{
		Enabled: true,
		APIKey:  "test-key",
		BaseURL: server.URL + "/v1",
	}, nil)
	require.NoError(t, err)
	assert.Empty(t, publisher.Events())
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

	err = agent.Compact(t.Context(), currentSession.ID, config.MorphCompactOptions{}, nil)
	require.ErrorIs(t, err, ErrMorphCompactDisabled)

	err = agent.Compact(t.Context(), currentSession.ID, config.MorphCompactOptions{Enabled: true}, nil)
	require.ErrorIs(t, err, ErrMorphCompactAPIKeyMissing)
}

func newSummaryFakeLanguageModel(t *testing.T, text string, outputTokens int64) *fakeLanguageModel {
	t.Helper()
	return newFakeLanguageModel("test-provider", "large-model", func(_ context.Context, call fantasy.Call) (*fantasy.Response, error) {
		require.True(t, promptTextContains(call.Prompt, "You are summarizing a conversation"))
		return &fantasy.Response{
			Content: fantasy.ResponseContent{
				fantasy.TextContent{Text: text},
			},
			FinishReason: fantasy.FinishReasonStop,
			Usage: fantasy.Usage{
				InputTokens:  10,
				OutputTokens: outputTokens,
			},
		}, nil
	})
}

func newMorphCompactTestAgent(env fakeEnv, large, small fantasy.LanguageModel) SessionAgent {
	return newMorphCompactTestAgentWithNotify(env, large, small, nil)
}

func newMorphCompactTestAgentWithNotify(env fakeEnv, large, small fantasy.LanguageModel, publisher pubsub.Publisher[notify.Notification]) SessionAgent {
	return NewSessionAgent(SessionAgentOptions{
		LargeModel:   newNonStreamingModel("test-provider", "large-model", large),
		SmallModel:   newNonStreamingModel("test-provider", "small-model", small),
		SystemPrompt: "Test system prompt",
		Sessions:     env.sessions,
		Messages:     env.messages,
		IsYolo:       true,
		Notify:       publisher,
	})
}

type recordingNotifyPublisher struct {
	events []pubsub.Event[notify.Notification]
}

func (p *recordingNotifyPublisher) Publish(t pubsub.EventType, payload notify.Notification) {
	p.events = append(p.events, pubsub.Event[notify.Notification]{
		Type:    t,
		Payload: payload,
	})
}

func (p *recordingNotifyPublisher) Events() []pubsub.Event[notify.Notification] {
	events := make([]pubsub.Event[notify.Notification], len(p.events))
	copy(events, p.events)
	return events
}
