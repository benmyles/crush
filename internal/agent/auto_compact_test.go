package agent

import (
	"context"
	"strings"
	"testing"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutoCompactTokenThreshold(t *testing.T) {
	t.Parallel()

	explicit := int64(1234)
	threshold, ok := autoCompactTokenThreshold(0, AutoCompactOptions{TokenThreshold: &explicit})
	require.True(t, ok)
	assert.Equal(t, int64(1234), threshold)

	threshold, ok = autoCompactTokenThreshold(200_000, AutoCompactOptions{})
	require.True(t, ok)
	assert.Equal(t, int64(160_000), threshold)

	threshold, ok = autoCompactTokenThreshold(300_000, AutoCompactOptions{})
	require.True(t, ok)
	assert.Equal(t, int64(280_000), threshold)

	_, ok = autoCompactTokenThreshold(0, AutoCompactOptions{})
	assert.False(t, ok)
}

func TestLatestStepContextTokensIncludesToolResultEstimate(t *testing.T) {
	t.Parallel()

	steps := []fantasy.StepResult{
		{
			Response: fantasy.Response{
				Usage: fantasy.Usage{
					InputTokens:  10,
					OutputTokens: 5,
				},
				Content: fantasy.ResponseContent{
					fantasy.ToolResultContent{
						ToolCallID: "call-1",
						ToolName:   "large_tool",
						Result: fantasy.ToolResultOutputContentText{
							Text: strings.Repeat("x", 40),
						},
					},
				},
			},
		},
	}

	assert.Equal(t, int64(25), latestStepContextTokens(steps))
}

func TestSessionAgentAutoCompactsAfterToolResultBeforeNextModelCall(t *testing.T) {
	t.Parallel()

	env := testEnv(t)
	threshold := int64(50)
	toolOutput := strings.Repeat("x", 400)

	large := newFakeLanguageModel(
		"test-provider",
		"large-model",
		func(_ context.Context, call fantasy.Call) (*fantasy.Response, error) {
			require.False(t, promptTextContains(call.Prompt, "You are summarizing a conversation"))
			require.False(t, hasPromptRole(call.Prompt, fantasy.MessageRoleTool))
			return &fantasy.Response{
				Content: fantasy.ResponseContent{
					fantasy.ToolCallContent{
						ToolCallID: "tool-call-1",
						ToolName:   "large_tool",
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
			require.True(t, promptTextContains(call.Prompt, "You are summarizing a conversation"))
			require.True(t, hasPromptRole(call.Prompt, fantasy.MessageRoleTool))
			return &fantasy.Response{
				Content: fantasy.ResponseContent{
					fantasy.TextContent{Text: "Summary output"},
				},
				FinishReason: fantasy.FinishReasonStop,
				Usage: fantasy.Usage{
					InputTokens:  20,
					OutputTokens: 6,
				},
			}, nil
		},
		func(_ context.Context, call fantasy.Call) (*fantasy.Response, error) {
			require.False(t, promptTextContains(call.Prompt, "You are summarizing a conversation"))
			require.False(t, hasPromptRole(call.Prompt, fantasy.MessageRoleTool))
			return &fantasy.Response{
				Content: fantasy.ResponseContent{
					fantasy.TextContent{Text: "Final answer"},
				},
				FinishReason: fantasy.FinishReasonStop,
				Usage: fantasy.Usage{
					InputTokens:  12,
					OutputTokens: 5,
				},
			}, nil
		},
	)
	small := newFakeLanguageModel(
		"test-provider",
		"small-model",
		func(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
			return &fantasy.Response{
				Content: fantasy.ResponseContent{
					fantasy.TextContent{Text: "Generated title"},
				},
				FinishReason: fantasy.FinishReasonStop,
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
				"large_tool",
				"Returns a large result.",
				func(_ context.Context, _ echoToolInput, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
					return fantasy.NewTextResponse(toolOutput), nil
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
		AutoCompact: AutoCompactOptions{
			Strategy:       config.PlanCompactStrategySummarize,
			TokenThreshold: &threshold,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "Final answer", result.Response.Content.Text())
	assert.Equal(t, 3, large.GenerateCallCount())

	currentSession, err = env.sessions.Get(t.Context(), currentSession.ID)
	require.NoError(t, err)
	require.NotEmpty(t, currentSession.SummaryMessageID)
}

func TestSessionAgentAutoCompactMorphRequiresMorphOptions(t *testing.T) {
	t.Parallel()

	env := testEnv(t)
	threshold := int64(10)

	large := newFakeLanguageModel(
		"test-provider",
		"large-model",
		func(_ context.Context, call fantasy.Call) (*fantasy.Response, error) {
			require.False(t, promptTextContains(call.Prompt, "You are summarizing a conversation"))
			return &fantasy.Response{
				Content: fantasy.ResponseContent{
					fantasy.TextContent{Text: "Initial answer"},
				},
				FinishReason: fantasy.FinishReasonStop,
				Usage: fantasy.Usage{
					InputTokens:  10,
					OutputTokens: 5,
				},
			}, nil
		},
	)
	small := newFakeLanguageModel(
		"test-provider",
		"small-model",
		func(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
			return &fantasy.Response{
				Content: fantasy.ResponseContent{
					fantasy.TextContent{Text: "Generated title"},
				},
				FinishReason: fantasy.FinishReasonStop,
			}, nil
		},
	)

	agent := NewSessionAgent(SessionAgentOptions{
		LargeModel:   newNonStreamingModel("test-provider", "large-model", large),
		SmallModel:   newNonStreamingModel("test-provider", "small-model", small),
		SystemPrompt: "Test system prompt",
		Sessions:     env.sessions,
		Messages:     env.messages,
		IsYolo:       true,
	})

	currentSession, err := env.sessions.Create(t.Context(), DefaultSessionName)
	require.NoError(t, err)

	result, err := agent.Run(t.Context(), SessionAgentCall{
		SessionID:       currentSession.ID,
		Prompt:          "Answer directly.",
		MaxOutputTokens: 128,
		AutoCompact: AutoCompactOptions{
			Strategy:       config.PlanCompactStrategyMorph,
			TokenThreshold: &threshold,
		},
	})
	require.ErrorIs(t, err, ErrMorphCompactDisabled)
	require.Nil(t, result)
	assert.Equal(t, 1, large.GenerateCallCount())

	currentSession, err = env.sessions.Get(t.Context(), currentSession.ID)
	require.NoError(t, err)
	assert.Empty(t, currentSession.SummaryMessageID)
}

func TestSessionAgentDisableAutoSummarizePreventsCompaction(t *testing.T) {
	t.Parallel()

	env := testEnv(t)
	threshold := int64(50)
	toolOutput := strings.Repeat("x", 400)

	large := newFakeLanguageModel(
		"test-provider",
		"large-model",
		func(_ context.Context, call fantasy.Call) (*fantasy.Response, error) {
			require.False(t, promptTextContains(call.Prompt, "You are summarizing a conversation"))
			return &fantasy.Response{
				Content: fantasy.ResponseContent{
					fantasy.ToolCallContent{
						ToolCallID: "tool-call-1",
						ToolName:   "large_tool",
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
			require.False(t, promptTextContains(call.Prompt, "You are summarizing a conversation"))
			return &fantasy.Response{
				Content: fantasy.ResponseContent{
					fantasy.TextContent{Text: "Final answer"},
				},
				FinishReason: fantasy.FinishReasonStop,
				Usage: fantasy.Usage{
					InputTokens:  12,
					OutputTokens: 5,
				},
			}, nil
		},
	)
	small := newFakeLanguageModel(
		"test-provider",
		"small-model",
		func(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
			return &fantasy.Response{
				Content: fantasy.ResponseContent{
					fantasy.TextContent{Text: "Generated title"},
				},
				FinishReason: fantasy.FinishReasonStop,
			}, nil
		},
	)

	agent := NewSessionAgent(SessionAgentOptions{
		LargeModel:           newNonStreamingModel("test-provider", "large-model", large),
		SmallModel:           newNonStreamingModel("test-provider", "small-model", small),
		SystemPrompt:         "Test system prompt",
		Sessions:             env.sessions,
		Messages:             env.messages,
		DisableAutoSummarize: true,
		Tools: []fantasy.AgentTool{
			fantasy.NewAgentTool(
				"large_tool",
				"Returns a large result.",
				func(_ context.Context, _ echoToolInput, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
					return fantasy.NewTextResponse(toolOutput), nil
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
		AutoCompact: AutoCompactOptions{
			TokenThreshold: &threshold,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "Final answer", result.Response.Content.Text())
	assert.Equal(t, 2, large.GenerateCallCount())

	currentSession, err = env.sessions.Get(t.Context(), currentSession.ID)
	require.NoError(t, err)
	assert.Empty(t, currentSession.SummaryMessageID)
}

func TestSessionAgentDisableAutoSummarizeRespectsExplicitStrategy(t *testing.T) {
	t.Parallel()

	env := testEnv(t)
	threshold := int64(10)

	large := newFakeLanguageModel(
		"test-provider",
		"large-model",
		func(_ context.Context, call fantasy.Call) (*fantasy.Response, error) {
			require.False(t, promptTextContains(call.Prompt, "You are summarizing a conversation"))
			return &fantasy.Response{
				Content: fantasy.ResponseContent{
					fantasy.TextContent{Text: "Initial answer"},
				},
				FinishReason: fantasy.FinishReasonStop,
				Usage: fantasy.Usage{
					InputTokens:  10,
					OutputTokens: 5,
				},
			}, nil
		},
		func(_ context.Context, call fantasy.Call) (*fantasy.Response, error) {
			require.True(t, promptTextContains(call.Prompt, "You are summarizing a conversation"))
			return &fantasy.Response{
				Content: fantasy.ResponseContent{
					fantasy.TextContent{Text: "Fallback summary"},
				},
				FinishReason: fantasy.FinishReasonStop,
				Usage: fantasy.Usage{
					InputTokens:  8,
					OutputTokens: 4,
				},
			}, nil
		},
	)
	small := newFakeLanguageModel(
		"test-provider",
		"small-model",
		func(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
			return &fantasy.Response{
				Content: fantasy.ResponseContent{
					fantasy.TextContent{Text: "Generated title"},
				},
				FinishReason: fantasy.FinishReasonStop,
			}, nil
		},
	)

	agent := NewSessionAgent(SessionAgentOptions{
		LargeModel:           newNonStreamingModel("test-provider", "large-model", large),
		SmallModel:           newNonStreamingModel("test-provider", "small-model", small),
		SystemPrompt:         "Test system prompt",
		Sessions:             env.sessions,
		Messages:             env.messages,
		DisableAutoSummarize: true,
		IsYolo:               true,
	})

	currentSession, err := env.sessions.Create(t.Context(), DefaultSessionName)
	require.NoError(t, err)

	result, err := agent.Run(t.Context(), SessionAgentCall{
		SessionID:       currentSession.ID,
		Prompt:          "Answer directly.",
		MaxOutputTokens: 128,
		AutoCompact: AutoCompactOptions{
			Strategy:       config.PlanCompactStrategySummarize,
			TokenThreshold: &threshold,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "Initial answer", result.Response.Content.Text())
	assert.Equal(t, 2, large.GenerateCallCount())

	currentSession, err = env.sessions.Get(t.Context(), currentSession.ID)
	require.NoError(t, err)
	require.NotEmpty(t, currentSession.SummaryMessageID)
}

func promptTextContains(prompt fantasy.Prompt, needle string) bool {
	for _, msg := range prompt {
		for _, part := range msg.Content {
			textPart, ok := fantasy.AsMessagePart[fantasy.TextPart](part)
			if ok && strings.Contains(textPart.Text, needle) {
				return true
			}
		}
	}
	return false
}
