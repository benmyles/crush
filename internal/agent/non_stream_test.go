package agent

import (
	"context"
	"errors"
	"sync"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type generateBehavior func(context.Context, fantasy.Call) (*fantasy.Response, error)

type fakeLanguageModel struct {
	provider string
	model    string

	mu sync.Mutex

	generateBehaviors []generateBehavior
	generateCalls     []fantasy.Call
	streamCalls       int
}

func newFakeLanguageModel(provider, model string, behaviors ...generateBehavior) *fakeLanguageModel {
	return &fakeLanguageModel{
		provider:          provider,
		model:             model,
		generateBehaviors: behaviors,
	}
}

func (f *fakeLanguageModel) Generate(ctx context.Context, call fantasy.Call) (*fantasy.Response, error) {
	f.mu.Lock()
	callIndex := len(f.generateCalls)
	f.generateCalls = append(f.generateCalls, call)
	if callIndex >= len(f.generateBehaviors) {
		f.mu.Unlock()
		return nil, errors.New("unexpected generate call")
	}
	behavior := f.generateBehaviors[callIndex]
	f.mu.Unlock()

	return behavior(ctx, call)
}

func (f *fakeLanguageModel) Stream(context.Context, fantasy.Call) (fantasy.StreamResponse, error) {
	f.mu.Lock()
	f.streamCalls++
	f.mu.Unlock()
	return nil, errors.New("stream should not be called")
}

func (f *fakeLanguageModel) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeLanguageModel) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeLanguageModel) Provider() string {
	return f.provider
}

func (f *fakeLanguageModel) Model() string {
	return f.model
}

func (f *fakeLanguageModel) GenerateCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.generateCalls)
}

func (f *fakeLanguageModel) StreamCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.streamCalls
}

type echoToolInput struct {
	Value string `json:"value"`
}

func newNonStreamingModel(provider, model string, lm fantasy.LanguageModel) Model {
	return Model{
		Model: lm,
		CatwalkCfg: catwalk.Model{
			Name:             model,
			ContextWindow:    200000,
			DefaultMaxTokens: 10000,
		},
		ModelCfg: config.SelectedModel{
			Model:    model,
			Provider: provider,
		},
		DisableStreaming: true,
	}
}

func filterMessagesByRole(msgs []message.Message, role message.MessageRole) []message.Message {
	filtered := make([]message.Message, 0, len(msgs))
	for _, msg := range msgs {
		if msg.Role == role {
			filtered = append(filtered, msg)
		}
	}
	return filtered
}

func hasPromptRole(prompt fantasy.Prompt, role fantasy.MessageRole) bool {
	for _, msg := range prompt {
		if msg.Role == role {
			return true
		}
	}
	return false
}

func TestSessionAgentRunNonStreamingPersistsToolCalls(t *testing.T) {
	t.Parallel()

	env := testEnv(t)

	large := newFakeLanguageModel(
		"test-provider",
		"large-model",
		func(_ context.Context, call fantasy.Call) (*fantasy.Response, error) {
			require.True(t, hasPromptRole(call.Prompt, fantasy.MessageRoleUser))
			require.False(t, hasPromptRole(call.Prompt, fantasy.MessageRoleTool))

			return &fantasy.Response{
				Content: fantasy.ResponseContent{
					fantasy.ToolCallContent{
						ToolCallID: "tool-call-1",
						ToolName:   "echo_tool",
						Input:      `{"value":"hello"}`,
					},
				},
				FinishReason: fantasy.FinishReasonToolCalls,
				Usage: fantasy.Usage{
					InputTokens:  10,
					OutputTokens: 4,
				},
			}, nil
		},
		func(_ context.Context, call fantasy.Call) (*fantasy.Response, error) {
			require.True(t, hasPromptRole(call.Prompt, fantasy.MessageRoleTool))

			return &fantasy.Response{
				Content: fantasy.ResponseContent{
					fantasy.TextContent{Text: "Final answer"},
				},
				FinishReason: fantasy.FinishReasonStop,
				Usage: fantasy.Usage{
					InputTokens:  12,
					OutputTokens: 6,
				},
			}, nil
		},
	)
	small := newFakeLanguageModel(
		"test-provider",
		"small-model",
		func(_ context.Context, call fantasy.Call) (*fantasy.Response, error) {
			require.True(t, hasPromptRole(call.Prompt, fantasy.MessageRoleUser))

			return &fantasy.Response{
				Content: fantasy.ResponseContent{
					fantasy.TextContent{Text: "Generated title"},
				},
				FinishReason: fantasy.FinishReasonStop,
				Usage: fantasy.Usage{
					InputTokens:  3,
					OutputTokens: 2,
				},
			}, nil
		},
	)

	agent := NewSessionAgent(SessionAgentOptions{
		LargeModel:   newNonStreamingModel("test-provider", "large-model", large),
		SmallModel:   newNonStreamingModel("test-provider", "small-model", small),
		SystemPrompt: "Test system prompt",
		Sessions:     env.sessions,
		Messages:     env.messages,
		Tools: []fantasy.AgentTool{
			fantasy.NewAgentTool(
				"echo_tool",
				"Echoes the provided value.",
				func(_ context.Context, input echoToolInput, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
					return fantasy.NewTextResponse("tool saw: " + input.Value), nil
				},
			),
		},
		IsYolo: true,
	})

	currentSession, err := env.sessions.Create(t.Context(), DefaultSessionName)
	require.NoError(t, err)

	result, err := agent.Run(t.Context(), SessionAgentCall{
		SessionID:       currentSession.ID,
		Prompt:          "Use the tool first.",
		MaxOutputTokens: 128,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, 2, large.GenerateCallCount())
	assert.Zero(t, large.StreamCallCount())
	assert.Equal(t, 1, small.GenerateCallCount())
	assert.Zero(t, small.StreamCallCount())
	assert.Equal(t, "Final answer", result.Response.Content.Text())

	currentSession, err = env.sessions.Get(t.Context(), currentSession.ID)
	require.NoError(t, err)
	assert.Equal(t, "Generated title", currentSession.Title)

	msgs, err := env.messages.List(t.Context(), currentSession.ID)
	require.NoError(t, err)
	require.Len(t, msgs, 4)

	assistantMsgs := filterMessagesByRole(msgs, message.Assistant)
	toolMsgs := filterMessagesByRole(msgs, message.Tool)

	require.Len(t, assistantMsgs, 2)
	require.Len(t, toolMsgs, 1)

	toolCalls := assistantMsgs[0].ToolCalls()
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "echo_tool", toolCalls[0].Name)
	assert.Equal(t, `{"value":"hello"}`, toolCalls[0].Input)
	assert.True(t, toolCalls[0].Finished)
	assert.Equal(t, message.FinishReasonToolUse, assistantMsgs[0].FinishReason())

	toolResults := toolMsgs[0].ToolResults()
	require.Len(t, toolResults, 1)
	assert.Equal(t, "echo_tool", toolResults[0].Name)
	assert.Equal(t, "tool saw: hello", toolResults[0].Content)
	assert.False(t, toolResults[0].IsError)

	assert.Equal(t, "Final answer", assistantMsgs[1].Content().Text)
	assert.Equal(t, message.FinishReasonEndTurn, assistantMsgs[1].FinishReason())
}

func TestSessionAgentSummarizeNonStreaming(t *testing.T) {
	t.Parallel()

	env := testEnv(t)

	large := newFakeLanguageModel(
		"test-provider",
		"large-model",
		func(_ context.Context, call fantasy.Call) (*fantasy.Response, error) {
			require.True(t, hasPromptRole(call.Prompt, fantasy.MessageRoleUser))

			return &fantasy.Response{
				Content: fantasy.ResponseContent{
					fantasy.TextContent{Text: "Summary output"},
				},
				FinishReason: fantasy.FinishReasonStop,
				Usage: fantasy.Usage{
					InputTokens:  8,
					OutputTokens: 5,
				},
			}, nil
		},
	)
	small := newFakeLanguageModel("test-provider", "small-model")

	agent := NewSessionAgent(SessionAgentOptions{
		LargeModel:   newNonStreamingModel("test-provider", "large-model", large),
		SmallModel:   newNonStreamingModel("test-provider", "small-model", small),
		SystemPrompt: "Test system prompt",
		Sessions:     env.sessions,
		Messages:     env.messages,
		IsYolo:       true,
	})

	currentSession, err := env.sessions.Create(t.Context(), "Session")
	require.NoError(t, err)

	_, err = env.messages.Create(t.Context(), currentSession.ID, message.CreateMessageParams{
		Role: message.User,
		Parts: []message.ContentPart{
			message.TextContent{Text: "Please summarize this later."},
		},
	})
	require.NoError(t, err)

	err = agent.Summarize(t.Context(), currentSession.ID, nil)
	require.NoError(t, err)

	assert.Equal(t, 1, large.GenerateCallCount())
	assert.Zero(t, large.StreamCallCount())
	assert.Zero(t, small.GenerateCallCount())
	assert.Zero(t, small.StreamCallCount())

	currentSession, err = env.sessions.Get(t.Context(), currentSession.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, currentSession.SummaryMessageID)

	msgs, err := env.messages.List(t.Context(), currentSession.ID)
	require.NoError(t, err)
	require.Len(t, msgs, 2)

	var summaryMsg message.Message
	foundSummary := false
	for _, msg := range msgs {
		if msg.IsSummaryMessage {
			summaryMsg = msg
			foundSummary = true
			break
		}
	}
	require.True(t, foundSummary)
	assert.Equal(t, currentSession.SummaryMessageID, summaryMsg.ID)
	assert.Equal(t, "Summary output", summaryMsg.Content().Text)
	assert.Equal(t, message.FinishReasonEndTurn, summaryMsg.FinishReason())
}
