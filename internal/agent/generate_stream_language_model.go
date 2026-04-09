package agent

import (
	"context"
	"strconv"
	"strings"

	"charm.land/fantasy"
)

// generateAsStreamLanguageModel implements [fantasy.LanguageModel.Stream] by calling
// [fantasy.LanguageModel.Generate] and yielding synthetic stream parts. That lets
// the fantasy agent stay on the Stream path (tool callbacks, text deltas) while the
// provider still receives non-streaming HTTP requests.
type generateAsStreamLanguageModel struct {
	inner fantasy.LanguageModel
}

func wrapLanguageModelForNonStreaming(lm fantasy.LanguageModel, disable bool) fantasy.LanguageModel {
	if !disable {
		return lm
	}
	return &generateAsStreamLanguageModel{inner: lm}
}

func (m *generateAsStreamLanguageModel) Generate(ctx context.Context, call fantasy.Call) (*fantasy.Response, error) {
	return m.inner.Generate(ctx, call)
}

func (m *generateAsStreamLanguageModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	return func(yield func(fantasy.StreamPart) bool) {
		resp, err := m.inner.Generate(ctx, call)
		if err != nil {
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeError, Error: err})
			return
		}
		yieldResponseAsStream(resp, yield)
	}, nil
}

func (m *generateAsStreamLanguageModel) GenerateObject(ctx context.Context, call fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return m.inner.GenerateObject(ctx, call)
}

func (m *generateAsStreamLanguageModel) StreamObject(ctx context.Context, call fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return m.inner.StreamObject(ctx, call)
}

func (m *generateAsStreamLanguageModel) Provider() string {
	return m.inner.Provider()
}

func (m *generateAsStreamLanguageModel) Model() string {
	return m.inner.Model()
}

// yieldResponseAsStream converts a non-streaming [fantasy.Response] into the stream
// parts expected by fantasy’s [agent.processStepStream].
func yieldResponseAsStream(resp *fantasy.Response, yield func(fantasy.StreamPart) bool) {
	if len(resp.Warnings) > 0 {
		if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeWarnings, Warnings: resp.Warnings}) {
			return
		}
	}

	textIDCounter := 0
	reasoningIDCounter := 0
	var hadAssistantContentBeforeFirstText bool

	for _, c := range resp.Content {
		switch c.GetType() {
		case fantasy.ContentTypeText:
			text, ok := fantasy.AsContentType[fantasy.TextContent](c)
			if !ok {
				continue
			}
			body := text.Text
			if !hadAssistantContentBeforeFirstText {
				body = strings.TrimPrefix(body, "\n")
			}
			id := "text-" + strconv.Itoa(textIDCounter)
			textIDCounter++
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: id}) {
				return
			}
			if body != "" {
				if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: id, Delta: body}) {
					return
				}
			}
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextEnd, ID: id, ProviderMetadata: text.ProviderMetadata}) {
				return
			}
			hadAssistantContentBeforeFirstText = true

		case fantasy.ContentTypeReasoning:
			reasoning, ok := fantasy.AsContentType[fantasy.ReasoningContent](c)
			if !ok {
				continue
			}
			id := "reasoning-" + strconv.Itoa(reasoningIDCounter)
			reasoningIDCounter++
			if !yield(fantasy.StreamPart{
				Type:             fantasy.StreamPartTypeReasoningStart,
				ID:               id,
				Delta:            "",
				ProviderMetadata: reasoning.ProviderMetadata,
			}) {
				return
			}
			if reasoning.Text != "" {
				if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeReasoningDelta, ID: id, Delta: reasoning.Text}) {
					return
				}
			}
			if !yield(fantasy.StreamPart{
				Type:             fantasy.StreamPartTypeReasoningEnd,
				ID:               id,
				ProviderMetadata: reasoning.ProviderMetadata,
			}) {
				return
			}
			hadAssistantContentBeforeFirstText = true

		case fantasy.ContentTypeToolCall:
			tc, ok := fantasy.AsContentType[fantasy.ToolCallContent](c)
			if !ok {
				continue
			}
			input := tc.Input
			if input == "" {
				input = "{}"
			}
			if !yield(fantasy.StreamPart{
				Type:             fantasy.StreamPartTypeToolCall,
				ID:               tc.ToolCallID,
				ToolCallName:     tc.ToolName,
				ToolCallInput:    input,
				ProviderExecuted: tc.ProviderExecuted,
				ProviderMetadata: tc.ProviderMetadata,
			}) {
				return
			}
			hadAssistantContentBeforeFirstText = true

		case fantasy.ContentTypeToolResult:
			tr, ok := fantasy.AsContentType[fantasy.ToolResultContent](c)
			if !ok || !tr.ProviderExecuted {
				continue
			}
			if !yield(fantasy.StreamPart{
				Type:             fantasy.StreamPartTypeToolResult,
				ID:               tr.ToolCallID,
				ToolCallName:     tr.ToolName,
				ProviderExecuted: true,
				ProviderMetadata: tr.ProviderMetadata,
			}) {
				return
			}
			hadAssistantContentBeforeFirstText = true

		case fantasy.ContentTypeSource:
			src, ok := fantasy.AsContentType[fantasy.SourceContent](c)
			if !ok {
				continue
			}
			id := src.ID
			if id == "" {
				id = src.URL
			}
			if !yield(fantasy.StreamPart{
				Type:             fantasy.StreamPartTypeSource,
				ID:               id,
				SourceType:       src.SourceType,
				URL:              src.URL,
				Title:            src.Title,
				ProviderMetadata: src.ProviderMetadata,
			}) {
				return
			}

		case fantasy.ContentTypeFile:
			// processStepStream has no file StreamPart; omit (rare in chat).
		}
	}

	yield(fantasy.StreamPart{
		Type:             fantasy.StreamPartTypeFinish,
		Usage:            resp.Usage,
		FinishReason:     resp.FinishReason,
		ProviderMetadata: resp.ProviderMetadata,
	})
}
