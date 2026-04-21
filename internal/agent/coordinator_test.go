package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"charm.land/fantasy/providers/openai"
	"github.com/charmbracelet/crush/internal/config"
	openaiapi "github.com/charmbracelet/openai-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSessionAgent is a minimal mock for the SessionAgent interface.
type mockSessionAgent struct {
	model     Model
	runFunc   func(ctx context.Context, call SessionAgentCall) (*fantasy.AgentResult, error)
	cancelled []string
}

func (m *mockSessionAgent) Run(ctx context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
	return m.runFunc(ctx, call)
}

func (m *mockSessionAgent) Model() Model                        { return m.model }
func (m *mockSessionAgent) SetModels(large, small Model)        {}
func (m *mockSessionAgent) SetTools(tools []fantasy.AgentTool)  {}
func (m *mockSessionAgent) SetSystemPrompt(systemPrompt string) {}
func (m *mockSessionAgent) Cancel(sessionID string) {
	m.cancelled = append(m.cancelled, sessionID)
}
func (m *mockSessionAgent) CancelAll()                                  {}
func (m *mockSessionAgent) IsSessionBusy(sessionID string) bool         { return false }
func (m *mockSessionAgent) IsBusy() bool                                { return false }
func (m *mockSessionAgent) QueuedPrompts(sessionID string) int          { return 0 }
func (m *mockSessionAgent) QueuedPromptsList(sessionID string) []string { return nil }
func (m *mockSessionAgent) ClearQueue(sessionID string)                 {}
func (m *mockSessionAgent) Summarize(context.Context, string, fantasy.ProviderOptions) error {
	return nil
}

// newTestCoordinator creates a minimal coordinator for unit testing runSubAgent.
func newTestCoordinator(t *testing.T, env fakeEnv, providerID string, providerCfg config.ProviderConfig) *coordinator {
	cfg, err := config.Init(env.workingDir, "", false)
	require.NoError(t, err)
	cfg.Config().Providers.Set(providerID, providerCfg)
	return &coordinator{
		cfg:      cfg,
		sessions: env.sessions,
	}
}

// newMockAgent creates a mockSessionAgent with the given provider and run function.
func newMockAgent(providerID string, maxTokens int64, runFunc func(context.Context, SessionAgentCall) (*fantasy.AgentResult, error)) *mockSessionAgent {
	return &mockSessionAgent{
		model: Model{
			CatwalkCfg: catwalk.Model{
				DefaultMaxTokens: maxTokens,
			},
			ModelCfg: config.SelectedModel{
				Provider: providerID,
			},
		},
		runFunc: runFunc,
	}
}

// agentResultWithText creates a minimal AgentResult with the given text response.
func agentResultWithText(text string) *fantasy.AgentResult {
	return &fantasy.AgentResult{
		Response: fantasy.Response{
			Content: fantasy.ResponseContent{
				fantasy.TextContent{Text: text},
			},
		},
	}
}

func TestGetProviderOptionsReasoningSummary(t *testing.T) {
	t.Run("uses selected model default for OpenAI responses models", func(t *testing.T) {
		model := Model{
			CatwalkCfg: catwalk.Model{ID: "gpt-5"},
			ModelCfg: config.SelectedModel{
				Provider:         "openai",
				ReasoningSummary: "detailed",
			},
		}
		providerCfg := config.ProviderConfig{Type: catwalk.Type(openai.Name)}

		opts := getProviderOptions(model, providerCfg)
		respOpts, ok := opts[openai.Name].(*openai.ResponsesProviderOptions)
		require.True(t, ok)
		require.NotNil(t, respOpts.ReasoningSummary)
		require.Equal(t, "detailed", *respOpts.ReasoningSummary)
	})

	t.Run("explicit provider options override the selected model default", func(t *testing.T) {
		model := Model{
			CatwalkCfg: catwalk.Model{ID: "gpt-5"},
			ModelCfg: config.SelectedModel{
				Provider:         "openai",
				ReasoningSummary: "detailed",
				ProviderOptions: map[string]any{
					"reasoning_summary": "concise",
				},
			},
		}
		providerCfg := config.ProviderConfig{Type: catwalk.Type(openai.Name)}

		opts := getProviderOptions(model, providerCfg)
		respOpts, ok := opts[openai.Name].(*openai.ResponsesProviderOptions)
		require.True(t, ok)
		require.NotNil(t, respOpts.ReasoningSummary)
		require.Equal(t, "concise", *respOpts.ReasoningSummary)
	})

	t.Run("falls back to auto for reasoning models when unset", func(t *testing.T) {
		model := Model{
			CatwalkCfg: catwalk.Model{ID: "gpt-5"},
			ModelCfg: config.SelectedModel{
				Provider: "openai",
			},
		}
		providerCfg := config.ProviderConfig{Type: catwalk.Type(openai.Name)}

		opts := getProviderOptions(model, providerCfg)
		respOpts, ok := opts[openai.Name].(*openai.ResponsesProviderOptions)
		require.True(t, ok)
		require.NotNil(t, respOpts.ReasoningSummary)
		require.Equal(t, "auto", *respOpts.ReasoningSummary)
	})
}

func TestOpenAICompatExtraBody(t *testing.T) {
	t.Run("adds the selected model default when absent", func(t *testing.T) {
		got := openAICompatExtraBody(
			config.SelectedModel{ReasoningSummary: "detailed"},
			config.ProviderConfig{},
		)
		require.Equal(t, map[string]any{"reasoning_summary": "detailed"}, got)
	})

	t.Run("preserves explicit extra body overrides", func(t *testing.T) {
		providerCfg := config.ProviderConfig{
			ExtraBody: map[string]any{
				"reasoning_summary": "concise",
				"foo":               "bar",
			},
		}

		got := openAICompatExtraBody(
			config.SelectedModel{ReasoningSummary: "detailed"},
			providerCfg,
		)
		require.Equal(t, map[string]any{
			"reasoning_summary": "concise",
			"foo":               "bar",
		}, got)
		require.Equal(t, "concise", providerCfg.ExtraBody["reasoning_summary"])
	})
}

func TestOpenAICompatProviderCompletionsPath(t *testing.T) {
	tests := []struct {
		name            string
		completionsPath string
		wantPath        string
	}{
		{
			name:     "default",
			wantPath: "/v1/projects/p/locations/us/endpoints/e/chat/completions",
		},
		{
			name:            "vertex suffix",
			completionsPath: ":rawPredict",
			wantPath:        "/v1/projects/p/locations/us/endpoints/e:rawPredict",
		},
		{
			name:            "slash path",
			completionsPath: "/custom/completions",
			wantPath:        "/v1/projects/p/locations/us/endpoints/e/custom/completions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPath string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodPost, r.Method)
				gotPath = r.URL.Path
				w.Header().Set("Content-Type", "application/json")
				_, err := w.Write([]byte(`{
					"id": "chatcmpl-test",
					"object": "chat.completion",
					"created": 0,
					"model": "test-model",
					"choices": [
						{
							"index": 0,
							"message": {
								"role": "assistant",
								"content": "ok"
							},
							"finish_reason": "stop"
						}
					],
					"usage": {
						"prompt_tokens": 1,
						"completion_tokens": 1,
						"total_tokens": 2
					}
				}`))
				require.NoError(t, err)
			}))
			t.Cleanup(server.Close)

			env := testEnv(t)
			coord := newTestCoordinator(t, env, "test-provider", config.ProviderConfig{})
			provider, err := coord.buildOpenaiCompatProvider(
				t.Context(),
				server.URL+"/v1/projects/p/locations/us/endpoints/e",
				"test-key",
				map[string]string{},
				nil,
				"test-provider",
				config.ProviderAuthModeAPIKey,
				false,
				tt.completionsPath,
			)
			require.NoError(t, err)

			lm, err := provider.LanguageModel(t.Context(), "test-model")
			require.NoError(t, err)
			resp, err := lm.Generate(t.Context(), fantasy.Call{
				Prompt: fantasy.Prompt{{
					Role: fantasy.MessageRoleUser,
					Content: []fantasy.MessagePart{
						fantasy.TextPart{Text: "hello"},
					},
				}},
			})
			require.NoError(t, err)
			text, ok := resp.Content[0].(fantasy.TextContent)
			require.True(t, ok)
			require.Equal(t, "ok", text.Text)
			require.Equal(t, tt.wantPath, gotPath)
		})
	}
}

func TestOpenAICompatExtraContentFunc(t *testing.T) {
	t.Run("parses responses-style reasoning details", func(t *testing.T) {
		choice := mustUnmarshalOpenAICompatChoice(t, `{
			"index": 0,
			"message": {
				"role": "assistant",
				"reasoning_details": [
					{
						"id": "rs_1",
						"type": "reasoning.summary",
						"format": "openai-responses-v1",
						"summary": "first summary",
						"index": 0
					},
					{
						"id": "rs_1",
						"type": "reasoning.summary",
						"format": "openai-responses-v1",
						"summary": "second summary",
						"index": 0
					},
					{
						"id": "rs_1",
						"type": "reasoning.encrypted",
						"format": "openai-responses-v1",
						"data": "encrypted",
						"index": 0
					}
				]
			}
		}`)

		content := openAICompatExtraContentFunc(choice)
		require.Len(t, content, 1)
		reasoning, ok := fantasy.AsContentType[fantasy.ReasoningContent](content[0])
		require.True(t, ok)
		require.Equal(t, "first summary\nsecond summary", reasoning.Text)
		responsesMetadata, ok := reasoning.ProviderMetadata[openai.Name].(*openai.ResponsesReasoningMetadata)
		require.True(t, ok)
		require.Equal(t, "rs_1", responsesMetadata.ItemID)
		require.NotNil(t, responsesMetadata.EncryptedContent)
		require.Equal(t, "encrypted", *responsesMetadata.EncryptedContent)
	})

	t.Run("falls back to reasoning field when details are absent", func(t *testing.T) {
		choice := mustUnmarshalOpenAICompatChoice(t, `{
			"index": 0,
			"message": {
				"role": "assistant",
				"reasoning": "fallback reasoning"
			}
		}`)

		content := openAICompatExtraContentFunc(choice)
		require.Len(t, content, 1)
		reasoning, ok := fantasy.AsContentType[fantasy.ReasoningContent](content[0])
		require.True(t, ok)
		require.Equal(t, "fallback reasoning", reasoning.Text)
	})
}

func TestOpenAICompatStreamExtraFunc(t *testing.T) {
	t.Run("streams responses-style reasoning summaries", func(t *testing.T) {
		ctx := map[string]any{}
		var parts []fantasy.StreamPart
		yield := func(part fantasy.StreamPart) bool {
			parts = append(parts, part)
			return true
		}

		for _, raw := range []string{
			`{
				"choices": [
					{
						"index": 0,
						"delta": {
							"reasoning_details": [
								{
									"type": "reasoning.summary",
									"format": "openai-responses-v1",
									"summary": "first summary",
									"index": 0
								}
							]
						}
					}
				]
			}`,
			`{
				"choices": [
					{
						"index": 0,
						"delta": {
							"reasoning_details": [
								{
									"type": "reasoning.summary",
									"format": "openai-responses-v1",
									"summary": "second summary",
									"index": 1
								}
							]
						}
					}
				]
			}`,
			`{
				"choices": [
					{
						"index": 0,
						"delta": {
							"reasoning_details": [
								{
									"id": "rs_2",
									"type": "reasoning.encrypted",
									"format": "openai-responses-v1",
									"data": "encrypted",
									"index": 1
								}
							]
						}
					}
				]
			}`,
		} {
			chunk := mustUnmarshalOpenAICompatChunk(t, raw)
			var ok bool
			ctx, ok = openAICompatStreamExtraFunc(chunk, yield, ctx)
			require.True(t, ok)
		}

		require.Len(t, parts, 3)
		require.Equal(t, fantasy.StreamPartTypeReasoningStart, parts[0].Type)
		require.Equal(t, "first summary", parts[0].Delta)
		require.Equal(t, fantasy.StreamPartTypeReasoningDelta, parts[1].Type)
		require.Equal(t, "\nsecond summary", parts[1].Delta)
		require.Equal(t, fantasy.StreamPartTypeReasoningEnd, parts[2].Type)
		responsesMetadata, ok := parts[2].ProviderMetadata[openai.Name].(*openai.ResponsesReasoningMetadata)
		require.True(t, ok)
		require.Equal(t, "rs_2", responsesMetadata.ItemID)
		require.NotNil(t, responsesMetadata.EncryptedContent)
		require.Equal(t, "encrypted", *responsesMetadata.EncryptedContent)
	})

	t.Run("ends reasoning when regular content starts after reasoning_content", func(t *testing.T) {
		ctx := map[string]any{}
		var parts []fantasy.StreamPart
		yield := func(part fantasy.StreamPart) bool {
			parts = append(parts, part)
			return true
		}

		startChunk := mustUnmarshalOpenAICompatChunk(t, `{
			"choices": [
				{
					"index": 0,
					"delta": {
						"reasoning_content": "thinking"
					}
				}
			]
		}`)
		var ok bool
		ctx, ok = openAICompatStreamExtraFunc(startChunk, yield, ctx)
		require.True(t, ok)

		endChunk := mustUnmarshalOpenAICompatChunk(t, `{
			"choices": [
				{
					"index": 0,
					"delta": {
						"content": "answer"
					}
				}
			]
		}`)
		_, ok = openAICompatStreamExtraFunc(endChunk, yield, ctx)
		require.True(t, ok)

		require.Len(t, parts, 2)
		require.Equal(t, fantasy.StreamPartTypeReasoningStart, parts[0].Type)
		require.Equal(t, "thinking", parts[0].Delta)
		require.Equal(t, fantasy.StreamPartTypeReasoningEnd, parts[1].Type)
	})
}

func mustUnmarshalOpenAICompatChoice(t *testing.T, raw string) openaiapi.ChatCompletionChoice {
	t.Helper()

	var choice openaiapi.ChatCompletionChoice
	require.NoError(t, json.Unmarshal([]byte(raw), &choice))
	return choice
}

func mustUnmarshalOpenAICompatChunk(t *testing.T, raw string) openaiapi.ChatCompletionChunk {
	t.Helper()

	var chunk openaiapi.ChatCompletionChunk
	require.NoError(t, json.Unmarshal([]byte(raw), &chunk))
	return chunk
}

func TestRunSubAgent(t *testing.T) {
	const providerID = "test-provider"
	providerCfg := config.ProviderConfig{ID: providerID}

	t.Run("happy path", func(t *testing.T) {
		env := testEnv(t)
		coord := newTestCoordinator(t, env, providerID, providerCfg)

		parentSession, err := env.sessions.Create(t.Context(), "Parent")
		require.NoError(t, err)

		agent := newMockAgent(providerID, 4096, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
			assert.Equal(t, "do something", call.Prompt)
			assert.Equal(t, int64(4096), call.MaxOutputTokens)
			return agentResultWithText("done"), nil
		})

		resp, err := coord.runSubAgent(t.Context(), subAgentParams{
			Agent:          agent,
			SessionID:      parentSession.ID,
			AgentMessageID: "msg-1",
			ToolCallID:     "call-1",
			Prompt:         "do something",
			SessionTitle:   "Test Session",
		})
		require.NoError(t, err)
		assert.Equal(t, "done", resp.Content)
		assert.False(t, resp.IsError)
	})

	t.Run("ModelCfg.MaxTokens overrides default", func(t *testing.T) {
		env := testEnv(t)
		coord := newTestCoordinator(t, env, providerID, providerCfg)

		parentSession, err := env.sessions.Create(t.Context(), "Parent")
		require.NoError(t, err)

		agent := &mockSessionAgent{
			model: Model{
				CatwalkCfg: catwalk.Model{
					DefaultMaxTokens: 4096,
				},
				ModelCfg: config.SelectedModel{
					Provider:  providerID,
					MaxTokens: 8192,
				},
			},
			runFunc: func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
				assert.Equal(t, int64(8192), call.MaxOutputTokens)
				return agentResultWithText("ok"), nil
			},
		}

		resp, err := coord.runSubAgent(t.Context(), subAgentParams{
			Agent:          agent,
			SessionID:      parentSession.ID,
			AgentMessageID: "msg-1",
			ToolCallID:     "call-1",
			Prompt:         "test",
			SessionTitle:   "Test",
		})
		require.NoError(t, err)
		assert.Equal(t, "ok", resp.Content)
	})

	t.Run("session creation failure with canceled context", func(t *testing.T) {
		env := testEnv(t)
		coord := newTestCoordinator(t, env, providerID, providerCfg)

		parentSession, err := env.sessions.Create(t.Context(), "Parent")
		require.NoError(t, err)

		agent := newMockAgent(providerID, 4096, nil)

		// Use a canceled context to trigger CreateTaskSession failure.
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		_, err = coord.runSubAgent(ctx, subAgentParams{
			Agent:          agent,
			SessionID:      parentSession.ID,
			AgentMessageID: "msg-1",
			ToolCallID:     "call-1",
			Prompt:         "test",
			SessionTitle:   "Test",
		})
		require.Error(t, err)
	})

	t.Run("provider not configured", func(t *testing.T) {
		env := testEnv(t)
		coord := newTestCoordinator(t, env, providerID, providerCfg)

		parentSession, err := env.sessions.Create(t.Context(), "Parent")
		require.NoError(t, err)

		// Agent references a provider that doesn't exist in config.
		agent := newMockAgent("unknown-provider", 4096, nil)

		_, err = coord.runSubAgent(t.Context(), subAgentParams{
			Agent:          agent,
			SessionID:      parentSession.ID,
			AgentMessageID: "msg-1",
			ToolCallID:     "call-1",
			Prompt:         "test",
			SessionTitle:   "Test",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "model provider not configured")
	})

	t.Run("agent run error returns error response", func(t *testing.T) {
		env := testEnv(t)
		coord := newTestCoordinator(t, env, providerID, providerCfg)

		parentSession, err := env.sessions.Create(t.Context(), "Parent")
		require.NoError(t, err)

		agent := newMockAgent(providerID, 4096, func(_ context.Context, _ SessionAgentCall) (*fantasy.AgentResult, error) {
			return nil, errors.New("agent exploded")
		})

		resp, err := coord.runSubAgent(t.Context(), subAgentParams{
			Agent:          agent,
			SessionID:      parentSession.ID,
			AgentMessageID: "msg-1",
			ToolCallID:     "call-1",
			Prompt:         "test",
			SessionTitle:   "Test",
		})
		// runSubAgent returns (errorResponse, nil) when agent.Run fails — not a Go error.
		require.NoError(t, err)
		assert.True(t, resp.IsError)
		assert.Equal(t, "error generating response", resp.Content)
	})

	t.Run("session setup callback is invoked", func(t *testing.T) {
		env := testEnv(t)
		coord := newTestCoordinator(t, env, providerID, providerCfg)

		parentSession, err := env.sessions.Create(t.Context(), "Parent")
		require.NoError(t, err)

		var setupCalledWith string
		agent := newMockAgent(providerID, 4096, func(_ context.Context, _ SessionAgentCall) (*fantasy.AgentResult, error) {
			return agentResultWithText("ok"), nil
		})

		_, err = coord.runSubAgent(t.Context(), subAgentParams{
			Agent:          agent,
			SessionID:      parentSession.ID,
			AgentMessageID: "msg-1",
			ToolCallID:     "call-1",
			Prompt:         "test",
			SessionTitle:   "Test",
			SessionSetup: func(sessionID string) {
				setupCalledWith = sessionID
			},
		})
		require.NoError(t, err)
		assert.NotEmpty(t, setupCalledWith, "SessionSetup should have been called")
	})

	t.Run("cost propagation to parent session", func(t *testing.T) {
		env := testEnv(t)
		coord := newTestCoordinator(t, env, providerID, providerCfg)

		parentSession, err := env.sessions.Create(t.Context(), "Parent")
		require.NoError(t, err)

		agent := newMockAgent(providerID, 4096, func(ctx context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
			// Simulate the agent incurring cost by updating the child session.
			childSession, err := env.sessions.Get(ctx, call.SessionID)
			if err != nil {
				return nil, err
			}
			childSession.Cost = 0.05
			_, err = env.sessions.Save(ctx, childSession)
			if err != nil {
				return nil, err
			}
			return agentResultWithText("ok"), nil
		})

		_, err = coord.runSubAgent(t.Context(), subAgentParams{
			Agent:          agent,
			SessionID:      parentSession.ID,
			AgentMessageID: "msg-1",
			ToolCallID:     "call-1",
			Prompt:         "test",
			SessionTitle:   "Test",
		})
		require.NoError(t, err)

		updated, err := env.sessions.Get(t.Context(), parentSession.ID)
		require.NoError(t, err)
		assert.InDelta(t, 0.05, updated.Cost, 1e-9)
	})
}

func TestResolveDisableStreaming(t *testing.T) {
	t.Parallel()

	t.Run("provider model default is used when selected model does not override it", func(t *testing.T) {
		t.Parallel()

		disabled := resolveDisableStreaming(
			config.SelectedModel{Model: "zai-org/glm-5-maas"},
			config.ProviderConfig{DisableStreamingModels: []string{"zai-org/glm-5-maas"}},
		)
		require.True(t, disabled)
	})

	t.Run("selected model can explicitly re-enable streaming", func(t *testing.T) {
		t.Parallel()

		override := false
		disabled := resolveDisableStreaming(
			config.SelectedModel{
				Model:            "zai-org/glm-5-maas",
				DisableStreaming: &override,
			},
			config.ProviderConfig{DisableStreamingModels: []string{"zai-org/glm-5-maas"}},
		)
		require.False(t, disabled)
	})

	t.Run("selected model can explicitly disable streaming", func(t *testing.T) {
		t.Parallel()

		override := true
		disabled := resolveDisableStreaming(
			config.SelectedModel{
				Model:            "zai-org/glm-5-maas",
				DisableStreaming: &override,
			},
			config.ProviderConfig{},
		)
		require.True(t, disabled)
	})
}

func TestUpdateParentSessionCost(t *testing.T) {
	t.Run("accumulates cost correctly", func(t *testing.T) {
		env := testEnv(t)
		cfg, err := config.Init(env.workingDir, "", false)
		require.NoError(t, err)
		coord := &coordinator{cfg: cfg, sessions: env.sessions}

		parent, err := env.sessions.Create(t.Context(), "Parent")
		require.NoError(t, err)

		child, err := env.sessions.CreateTaskSession(t.Context(), "tool-1", parent.ID, "Child")
		require.NoError(t, err)

		// Set child cost.
		child.Cost = 0.10
		_, err = env.sessions.Save(t.Context(), child)
		require.NoError(t, err)

		err = coord.updateParentSessionCost(t.Context(), child.ID, parent.ID)
		require.NoError(t, err)

		updated, err := env.sessions.Get(t.Context(), parent.ID)
		require.NoError(t, err)
		assert.InDelta(t, 0.10, updated.Cost, 1e-9)
	})

	t.Run("accumulates multiple child costs", func(t *testing.T) {
		env := testEnv(t)
		cfg, err := config.Init(env.workingDir, "", false)
		require.NoError(t, err)
		coord := &coordinator{cfg: cfg, sessions: env.sessions}

		parent, err := env.sessions.Create(t.Context(), "Parent")
		require.NoError(t, err)

		child1, err := env.sessions.CreateTaskSession(t.Context(), "tool-1", parent.ID, "Child1")
		require.NoError(t, err)
		child1.Cost = 0.05
		_, err = env.sessions.Save(t.Context(), child1)
		require.NoError(t, err)

		child2, err := env.sessions.CreateTaskSession(t.Context(), "tool-2", parent.ID, "Child2")
		require.NoError(t, err)
		child2.Cost = 0.03
		_, err = env.sessions.Save(t.Context(), child2)
		require.NoError(t, err)

		err = coord.updateParentSessionCost(t.Context(), child1.ID, parent.ID)
		require.NoError(t, err)
		err = coord.updateParentSessionCost(t.Context(), child2.ID, parent.ID)
		require.NoError(t, err)

		updated, err := env.sessions.Get(t.Context(), parent.ID)
		require.NoError(t, err)
		assert.InDelta(t, 0.08, updated.Cost, 1e-9)
	})

	t.Run("child session not found", func(t *testing.T) {
		env := testEnv(t)
		cfg, err := config.Init(env.workingDir, "", false)
		require.NoError(t, err)
		coord := &coordinator{cfg: cfg, sessions: env.sessions}

		parent, err := env.sessions.Create(t.Context(), "Parent")
		require.NoError(t, err)

		err = coord.updateParentSessionCost(t.Context(), "non-existent", parent.ID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "get child session")
	})

	t.Run("parent session not found", func(t *testing.T) {
		env := testEnv(t)
		cfg, err := config.Init(env.workingDir, "", false)
		require.NoError(t, err)
		coord := &coordinator{cfg: cfg, sessions: env.sessions}

		parent, err := env.sessions.Create(t.Context(), "Parent")
		require.NoError(t, err)
		child, err := env.sessions.CreateTaskSession(t.Context(), "tool-1", parent.ID, "Child")
		require.NoError(t, err)

		err = coord.updateParentSessionCost(t.Context(), child.ID, "non-existent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "get parent session")
	})

	t.Run("zero cost handled correctly", func(t *testing.T) {
		env := testEnv(t)
		cfg, err := config.Init(env.workingDir, "", false)
		require.NoError(t, err)
		coord := &coordinator{cfg: cfg, sessions: env.sessions}

		parent, err := env.sessions.Create(t.Context(), "Parent")
		require.NoError(t, err)
		child, err := env.sessions.CreateTaskSession(t.Context(), "tool-1", parent.ID, "Child")
		require.NoError(t, err)

		err = coord.updateParentSessionCost(t.Context(), child.ID, parent.ID)
		require.NoError(t, err)

		updated, err := env.sessions.Get(t.Context(), parent.ID)
		require.NoError(t, err)
		assert.InDelta(t, 0.0, updated.Cost, 1e-9)
	})
}
