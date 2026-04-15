package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openaicompat"
	openaisdk "github.com/charmbracelet/openai-go"
)

const openAICompatReasoningStartedCtx = "openai_compat_reasoning_started"

type openAICompatReasoningDetail struct {
	ID        string `json:"id,omitempty"`
	Type      string `json:"type,omitempty"`
	Text      string `json:"text,omitempty"`
	Data      string `json:"data,omitempty"`
	Format    string `json:"format,omitempty"`
	Summary   string `json:"summary,omitempty"`
	Signature string `json:"signature,omitempty"`
	Index     int    `json:"index"`
}

type openAICompatReasoningData struct {
	Reasoning        string                        `json:"reasoning,omitempty"`
	ReasoningContent string                        `json:"reasoning_content,omitempty"`
	ReasoningDetails []openAICompatReasoningDetail `json:"reasoning_details,omitempty"`
}

type openAICompatReasoningState struct {
	metadata         *openai.ResponsesReasoningMetadata
	format           string
	lastSummaryIndex int
}

func isResponsesStyleReasoningDetail(detail openAICompatReasoningDetail) bool {
	return strings.HasPrefix(detail.Format, "openai-responses") ||
		strings.HasPrefix(detail.Format, "xai-responses") ||
		detail.Type == "reasoning.summary" ||
		detail.Type == "reasoning.encrypted" ||
		detail.Summary != ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func openAICompatExtraContentFunc(choice openaisdk.ChatCompletionChoice) []fantasy.Content {
	reasoningData := openAICompatReasoningData{}
	if err := json.Unmarshal([]byte(choice.Message.RawJSON()), &reasoningData); err != nil {
		return nil
	}

	content := make([]fantasy.Content, 0, len(reasoningData.ReasoningDetails)+1)
	responsesReasoningBlocks := make([]openai.ResponsesReasoningMetadata, 0)
	var otherReasoning []string

	for _, detail := range reasoningData.ReasoningDetails {
		switch {
		case strings.HasPrefix(detail.Format, "google-gemini"),
			strings.HasPrefix(detail.Format, "anthropic-claude"):
			if text := firstNonEmpty(detail.Text, detail.Summary); text != "" {
				content = append(content, fantasy.ReasoningContent{Text: text})
			}
		case isResponsesStyleReasoningDetail(detail):
			for len(responsesReasoningBlocks) <= detail.Index {
				responsesReasoningBlocks = append(responsesReasoningBlocks, openai.ResponsesReasoningMetadata{})
			}
			block := responsesReasoningBlocks[detail.Index]
			if text := firstNonEmpty(detail.Summary, detail.Text); text != "" {
				block.Summary = append(block.Summary, text)
			}
			if detail.Type == "reasoning.encrypted" && detail.Data != "" {
				block.EncryptedContent = &detail.Data
			}
			if detail.ID != "" {
				block.ItemID = detail.ID
			}
			responsesReasoningBlocks[detail.Index] = block
		default:
			if text := firstNonEmpty(detail.Text, detail.Summary); text != "" {
				otherReasoning = append(otherReasoning, text)
			}
		}
	}

	for _, block := range responsesReasoningBlocks {
		content = append(content, fantasy.ReasoningContent{
			Text: strings.Join(block.Summary, "\n"),
			ProviderMetadata: fantasy.ProviderMetadata{
				openai.Name: &block,
			},
		})
	}

	if len(content) == 0 && len(otherReasoning) == 0 {
		if reasoningData.ReasoningContent != "" {
			content = append(content, fantasy.ReasoningContent{Text: reasoningData.ReasoningContent})
		} else if reasoningData.Reasoning != "" {
			content = append(content, fantasy.ReasoningContent{Text: reasoningData.Reasoning})
		}
	}

	for _, reasoning := range otherReasoning {
		content = append(content, fantasy.ReasoningContent{Text: reasoning})
	}

	return content
}

func extractOpenAICompatReasoningContext(ctx map[string]any) *openAICompatReasoningState {
	reasoningStarted, ok := ctx[openAICompatReasoningStartedCtx]
	if !ok {
		return nil
	}
	state, ok := reasoningStarted.(*openAICompatReasoningState)
	if !ok {
		return nil
	}
	return state
}

func openAICompatStreamExtraFunc(chunk openaisdk.ChatCompletionChunk, yield func(fantasy.StreamPart) bool, ctx map[string]any) (map[string]any, bool) {
	if len(chunk.Choices) == 0 {
		return ctx, true
	}

	inx := 0
	choice := chunk.Choices[inx]
	reasoningData := openAICompatReasoningData{}
	if err := json.Unmarshal([]byte(choice.Delta.RawJSON()), &reasoningData); err != nil {
		yield(fantasy.StreamPart{
			Type:  fantasy.StreamPartTypeError,
			Error: &fantasy.Error{Title: "stream error", Message: "error unmarshalling delta", Cause: err},
		})
		return ctx, false
	}

	currentState := extractOpenAICompatReasoningContext(ctx)
	detail := openAICompatReasoningDetail{}
	hasDetail := len(reasoningData.ReasoningDetails) > 0
	if hasDetail {
		detail = reasoningData.ReasoningDetails[0]
	}

	reasoningDelta := ""
	if hasDetail {
		if isResponsesStyleReasoningDetail(detail) {
			reasoningDelta = firstNonEmpty(detail.Summary, detail.Text)
		} else {
			reasoningDelta = firstNonEmpty(detail.Text, detail.Summary)
		}
	} else {
		reasoningDelta = firstNonEmpty(reasoningData.ReasoningContent, reasoningData.Reasoning)
	}

	shouldEnd := false
	if hasDetail {
		shouldEnd = detail.Type == "reasoning.encrypted" || detail.Signature != ""
	}

	if currentState == nil {
		if !hasDetail && reasoningDelta == "" {
			return ctx, true
		}
		currentState = &openAICompatReasoningState{format: detail.Format, lastSummaryIndex: detail.Index}
		var providerMetadata fantasy.ProviderMetadata
		if hasDetail && isResponsesStyleReasoningDetail(detail) {
			currentState.metadata = &openai.ResponsesReasoningMetadata{}
			for len(currentState.metadata.Summary) <= detail.Index {
				currentState.metadata.Summary = append(currentState.metadata.Summary, "")
			}
			currentState.metadata.Summary[detail.Index] += reasoningDelta
			if detail.Type == "reasoning.encrypted" && detail.Data != "" {
				currentState.metadata.EncryptedContent = &detail.Data
			}
			if detail.ID != "" {
				currentState.metadata.ItemID = detail.ID
			}
			providerMetadata = fantasy.ProviderMetadata{
				openai.Name: currentState.metadata,
			}
		}
		ctx[openAICompatReasoningStartedCtx] = currentState
		if !yield(fantasy.StreamPart{
			Type:             fantasy.StreamPartTypeReasoningStart,
			ID:               fmt.Sprintf("%d", inx),
			Delta:            reasoningDelta,
			ProviderMetadata: providerMetadata,
		}) {
			return ctx, false
		}
		if shouldEnd {
			ctx[openAICompatReasoningStartedCtx] = nil
			return ctx, yield(fantasy.StreamPart{
				Type:             fantasy.StreamPartTypeReasoningEnd,
				ID:               fmt.Sprintf("%d", inx),
				ProviderMetadata: providerMetadata,
			})
		}
		return ctx, true
	}

	if !hasDetail && reasoningDelta == "" {
		if choice.Delta.Content != "" || len(choice.Delta.ToolCalls) > 0 {
			ctx[openAICompatReasoningStartedCtx] = nil
			return ctx, yield(fantasy.StreamPart{
				Type: fantasy.StreamPartTypeReasoningEnd,
				ID:   fmt.Sprintf("%d", inx),
			})
		}
		return ctx, true
	}

	if reasoningDelta != "" {
		var providerMetadata fantasy.ProviderMetadata
		if hasDetail && isResponsesStyleReasoningDetail(detail) && detail.Index > currentState.lastSummaryIndex {
			reasoningDelta = "\n" + reasoningDelta
		}
		if hasDetail && isResponsesStyleReasoningDetail(detail) {
			if currentState.metadata == nil {
				currentState.metadata = &openai.ResponsesReasoningMetadata{}
			}
			for len(currentState.metadata.Summary) <= detail.Index {
				currentState.metadata.Summary = append(currentState.metadata.Summary, "")
			}
			currentState.metadata.Summary[detail.Index] += firstNonEmpty(detail.Summary, detail.Text)
			if detail.ID != "" {
				currentState.metadata.ItemID = detail.ID
			}
			providerMetadata = fantasy.ProviderMetadata{
				openai.Name: currentState.metadata,
			}
		}
		currentState.format = detail.Format
		currentState.lastSummaryIndex = detail.Index
		ctx[openAICompatReasoningStartedCtx] = currentState
		if !yield(fantasy.StreamPart{
			Type:             fantasy.StreamPartTypeReasoningDelta,
			ID:               fmt.Sprintf("%d", inx),
			Delta:            reasoningDelta,
			ProviderMetadata: providerMetadata,
		}) {
			return ctx, false
		}
	}

	if shouldEnd {
		var providerMetadata fantasy.ProviderMetadata
		if hasDetail && isResponsesStyleReasoningDetail(detail) && currentState.metadata != nil {
			if detail.Type == "reasoning.encrypted" && detail.Data != "" {
				currentState.metadata.EncryptedContent = &detail.Data
			}
			if detail.ID != "" {
				currentState.metadata.ItemID = detail.ID
			}
			providerMetadata = fantasy.ProviderMetadata{
				openai.Name: currentState.metadata,
			}
		}
		ctx[openAICompatReasoningStartedCtx] = nil
		return ctx, yield(fantasy.StreamPart{
			Type:             fantasy.StreamPartTypeReasoningEnd,
			ID:               fmt.Sprintf("%d", inx),
			ProviderMetadata: providerMetadata,
		})
	}

	return ctx, true
}

func openAICompatLanguageModelOptions() []openai.LanguageModelOption {
	return []openai.LanguageModelOption{
		openai.WithLanguageModelPrepareCallFunc(openaicompat.PrepareCallFunc),
		openai.WithLanguageModelStreamExtraFunc(openAICompatStreamExtraFunc),
		openai.WithLanguageModelExtraContentFunc(openAICompatExtraContentFunc),
		openai.WithLanguageModelToPromptFunc(openaicompat.ToPromptFunc),
	}
}
