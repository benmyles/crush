package fireworksdsv4

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"strings"

	"charm.land/fantasy"
	"charm.land/fantasy/object"
)

var protectedPayloadKeys = map[string]struct{}{
	"echo": {}, "messages": {}, "model": {}, "n": {}, "prompt": {},
	"response_format": {}, "stop": {}, "stream": {}, "stream_options": {},
	"tool_choice": {}, "tools": {},
}

type languageModel struct {
	provider        string
	modelID         string
	baseURL         string
	apiKey          string
	headers         map[string]string
	extraBody       map[string]any
	defaultEffort   string
	maxOutputTokens int64
	httpClient      *http.Client
}

func (l *languageModel) Provider() string { return l.provider }
func (l *languageModel) Model() string    { return l.modelID }

func callProviderOptions(call fantasy.Call) *ProviderOptions {
	if value, ok := call.ProviderOptions[Name]; ok {
		if options, ok := value.(*ProviderOptions); ok {
			return options
		}
	}
	return nil
}

func (l *languageModel) requestPayload(call fantasy.Call, tools preparedTools) (map[string]any, string, error) {
	options := callProviderOptions(call)
	effort := l.defaultEffort
	if options != nil && options.ReasoningEffort != "" {
		effort = options.ReasoningEffort
	}
	if effort == "off" {
		effort = "none"
	}
	thinkingEnabled := effort != "" && effort != "none"
	prompt, err := buildPrompt(call.Prompt, tools.tools, effort, tools.chatRequired)
	if err != nil {
		return nil, "", err
	}
	grammar, err := buildGrammar(tools.tools, thinkingEnabled, tools.grammarMode)
	if err != nil {
		return nil, "", err
	}
	payload := make(map[string]any)
	if call.Temperature != nil {
		payload["temperature"] = *call.Temperature
	}
	if call.TopP != nil {
		payload["top_p"] = *call.TopP
	}
	if call.TopK != nil {
		payload["top_k"] = *call.TopK
	}
	if call.FrequencyPenalty != nil {
		payload["frequency_penalty"] = *call.FrequencyPenalty
	}
	if call.PresencePenalty != nil {
		payload["presence_penalty"] = *call.PresencePenalty
	}
	mergeExtra := func(values map[string]any) {
		for key, value := range values {
			if _, protected := protectedPayloadKeys[key]; !protected {
				payload[key] = value
			}
		}
	}
	mergeExtra(l.extraBody)
	if options != nil {
		mergeExtra(options.ExtraBody)
	}
	if _, ok := payload["temperature"]; !ok {
		payload["temperature"] = 1
	}
	maxTokens := l.maxOutputTokens
	if call.MaxOutputTokens != nil {
		maxTokens = *call.MaxOutputTokens
	}
	if l.maxOutputTokens > 0 && maxTokens > l.maxOutputTokens {
		maxTokens = l.maxOutputTokens
	}
	payload["model"] = l.modelID
	payload["prompt"] = prompt
	payload["stream"] = true
	payload["n"] = 1
	payload["echo"] = false
	if maxTokens > 0 {
		payload["max_tokens"] = maxTokens
	}
	payload["stop"] = []string{endToken}
	payload["stream_options"] = map[string]any{"include_usage": true, "buffer_tokens": 1, "buffer_ms": 1}
	payload["response_format"] = map[string]any{"type": "grammar", "grammar": grammar}
	return payload, effort, nil
}

func numericValue(value any) float64 {
	switch number := value.(type) {
	case json.Number:
		result, _ := number.Float64()
		if result > 0 {
			return result
		}
	case float64:
		if number > 0 {
			return number
		}
	case int:
		if number > 0 {
			return float64(number)
		}
	case int64:
		if number > 0 {
			return float64(number)
		}
	}
	return 0
}

func mapValue(value any) map[string]any {
	result, _ := value.(map[string]any)
	return result
}

func updateUsage(current fantasy.Usage, raw any) fantasy.Usage {
	usage := mapValue(raw)
	if usage == nil {
		return current
	}
	prompt := numericValue(usage["prompt_tokens"])
	promptDetails := mapValue(usage["prompt_tokens_details"])
	completionDetails := mapValue(usage["completion_tokens_details"])
	cacheRead := numericValue(promptDetails["cached_tokens"])
	if cacheRead == 0 {
		cacheRead = numericValue(usage["prompt_cache_hit_tokens"])
	}
	cacheRead = min(prompt, cacheRead)
	cacheWrite := min(max(0, prompt-cacheRead), numericValue(promptDetails["cache_write_tokens"]))
	output := numericValue(usage["completion_tokens"])
	reasoning := min(output, numericValue(completionDetails["reasoning_tokens"]))
	return fantasy.Usage{
		InputTokens:         int64(max(0, prompt-cacheRead-cacheWrite)),
		OutputTokens:        int64(output),
		CacheReadTokens:     int64(cacheRead),
		CacheCreationTokens: int64(cacheWrite),
		ReasoningTokens:     int64(reasoning),
		TotalTokens:         int64(max(0, prompt-cacheRead-cacheWrite) + output + cacheRead + cacheWrite),
	}
}

func choiceZero(payload map[string]any) map[string]any {
	choices, _ := payload["choices"].([]any)
	var unindexed map[string]any
	for _, value := range choices {
		choice, ok := value.(map[string]any)
		if !ok {
			continue
		}
		indexValue, indexed := choice["index"]
		if !indexed && unindexed == nil {
			unindexed = choice
		}
		if indexed && isZeroIndex(indexValue) {
			return choice
		}
	}
	return unindexed
}

func isZeroIndex(value any) bool {
	switch number := value.(type) {
	case json.Number:
		return string(number) == "0"
	case float64:
		return number == 0
	case int:
		return number == 0
	case int64:
		return number == 0
	default:
		return false
	}
}

func nonRetryableStreamError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return &fantasy.Error{Title: "Fireworks DSV4 stream failed", Message: err.Error()}
}

func (l *languageModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	tools, err := prepareToolSpecs(call)
	if err != nil {
		return nil, err
	}
	payload, effort, err := l.requestPayload(call, tools)
	if err != nil {
		return nil, err
	}
	response, err := l.postCompletion(ctx, call, payload) //nolint:bodyclose // The stream iterator owns and closes the response body.
	if err != nil {
		return nil, err
	}
	thinkingEnabled := effort != "" && effort != "none" && effort != "off"

	return func(yield func(fantasy.StreamPart) bool) {
		decoder := newDSMLDecoder(thinkingEnabled, tools.byName(), nil)
		var completion, finishReason, responseID string
		var usage fantasy.Usage
		emitted := false
		stopped := false
		toolInputs := make(map[string]string)
		chatValues := make(map[string]string)
		chatStarted := make(map[string]bool)
		emit := func(part fantasy.StreamPart) bool {
			if part.Type == fantasy.StreamPartTypeReasoningDelta || part.Type == fantasy.StreamPartTypeTextDelta || part.Type == fantasy.StreamPartTypeToolInputStart || part.Type == fantasy.StreamPartTypeToolInputDelta {
				emitted = true
			}
			if !yield(part) {
				stopped = true
				return false
			}
			return true
		}
		emitError := func(streamErr error) {
			if streamErr == nil || stopped {
				return
			}
			if emitted {
				streamErr = nonRetryableStreamError(streamErr)
			} else if errors.Is(streamErr, io.ErrUnexpectedEOF) {
				streamErr = fantasy.NewIncompleteStreamError()
			}
			emit(fantasy.StreamPart{Type: fantasy.StreamPartTypeError, Error: streamErr})
		}
		emitDecoder := func(events []decoderEvent) bool {
			for _, event := range events {
				callID := event.Call.ID
				synthetic := event.Call.Synthetic
				switch event.Type {
				case eventThinkingStart:
					if !emit(fantasy.StreamPart{Type: fantasy.StreamPartTypeReasoningStart, ID: "dsv4-reasoning"}) {
						return false
					}
				case eventThinkingDelta:
					if !emit(fantasy.StreamPart{Type: fantasy.StreamPartTypeReasoningDelta, ID: "dsv4-reasoning", Delta: event.Delta}) {
						return false
					}
				case eventThinkingEnd:
					if !emit(fantasy.StreamPart{Type: fantasy.StreamPartTypeReasoningEnd, ID: "dsv4-reasoning"}) {
						return false
					}
				case eventTextStart:
					if !emit(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: "dsv4-text"}) {
						return false
					}
				case eventTextDelta:
					if !emit(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: "dsv4-text", Delta: event.Delta}) {
						return false
					}
				case eventTextEnd:
					if !emit(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextEnd, ID: "dsv4-text"}) {
						return false
					}
				case eventToolStart:
					if !synthetic && !emit(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolInputStart, ID: callID, ToolCallName: event.Call.Name}) {
						return false
					}
				case eventToolArgument:
					if synthetic && event.Name == "message" {
						value, _ := event.Value.(string)
						previous := chatValues[callID]
						delta := value
						if strings.HasPrefix(value, previous) {
							delta = value[len(previous):]
						}
						chatValues[callID] = value
						if !chatStarted[callID] {
							chatStarted[callID] = true
							if !emit(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: "dsv4-chat-" + callID}) {
								return false
							}
						}
						if delta != "" && !emit(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: "dsv4-chat-" + callID, Delta: delta}) {
							return false
						}
					}
				case eventToolDelta:
					if !synthetic {
						toolInputs[callID] += event.Delta
						if !emit(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolInputDelta, ID: callID, Delta: event.Delta}) {
							return false
						}
					}
				case eventToolEnd:
					if synthetic {
						if !chatStarted[callID] {
							chatStarted[callID] = true
							if !emit(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: "dsv4-chat-" + callID}) {
								return false
							}
						}
						if !emit(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextEnd, ID: "dsv4-chat-" + callID}) {
							return false
						}
					} else {
						if !emit(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolInputEnd, ID: callID}) {
							return false
						}
						if event.Complete && !emit(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolCall, ID: callID, ToolCallName: event.Call.Name, ToolCallInput: toolInputs[callID]}) {
							return false
						}
					}
				}
			}
			return true
		}

		consumeErr := consumePayloads(ctx, response, func(payload map[string]any) error {
			if stopped {
				return errStopStream
			}
			if rawError, ok := payload["error"]; ok && rawError != nil {
				data, _ := json.Marshal(rawError)
				return fmt.Errorf("fireworks stream error: %s", data)
			}
			if id, ok := payload["id"].(string); ok {
				responseID = id
			}
			usage = updateUsage(usage, payload["usage"])
			choice := choiceZero(payload)
			if choice == nil {
				return nil
			}
			if text, ok := choice["text"].(string); ok && text != "" {
				if finishReason != "" {
					return fmt.Errorf("fireworks emitted completion text after finish reason %q", finishReason)
				}
				completion += text
				events, err := decoder.push(text)
				if err != nil {
					return err
				}
				if !emitDecoder(events) {
					return errStopStream
				}
			}
			if reason, ok := choice["finish_reason"].(string); ok && reason != "" {
				if finishReason != "" && finishReason != reason {
					return fmt.Errorf("fireworks returned conflicting finish reasons %q and %q", finishReason, reason)
				}
				finishReason = reason
			}
			return nil
		})
		if errors.Is(consumeErr, errStopStream) || stopped {
			return
		}
		if consumeErr != nil {
			emitError(consumeErr)
			return
		}
		if finishReason == "" {
			emitError(io.ErrUnexpectedEOF)
			return
		}
		var final fantasy.FinishReason
		switch finishReason {
		case "length", "max_tokens":
			finished, err := decoder.finish(false)
			if err != nil {
				emitError(err)
				return
			}
			if !emitDecoder(finished.Events) {
				return
			}
			final = fantasy.FinishReasonLength
		case "stop":
			finished, err := decoder.finish(true)
			if err != nil {
				emitError(err)
				return
			}
			if !emitDecoder(finished.Events) {
				return
			}
			parsed, err := parseCompletion(completion, thinkingEnabled, tools.byName(), func(name string, index int) string {
				if index < len(decoder.createdIDs) {
					return decoder.createdIDs[index]
				}
				return defaultCallID(name, index)
			})
			if err != nil {
				emitError(err)
				return
			}
			if !completionsEqual(finished.Result, parsed) {
				emitError(fmt.Errorf("fireworks streamed DSML disagrees with its final validation"))
				return
			}
			final = fantasy.FinishReasonStop
			for _, call := range parsed.Calls {
				if !call.Synthetic {
					final = fantasy.FinishReasonToolCalls
					break
				}
			}
		default:
			emitError(fmt.Errorf("fireworks returned an unsupported finish reason %q", finishReason))
			return
		}
		metadata := fantasy.ProviderMetadata{Name: &Metadata{ResponseID: responseID, RawFinishReason: finishReason}}
		emit(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, Usage: usage, FinishReason: final, ProviderMetadata: metadata})
	}, nil
}

func (l *languageModel) Generate(ctx context.Context, call fantasy.Call) (*fantasy.Response, error) {
	stream, err := l.Stream(ctx, call)
	if err != nil {
		return nil, err
	}
	var content fantasy.ResponseContent
	indices := make(map[string]int)
	response := &fantasy.Response{}
	for part := range stream {
		switch part.Type {
		case fantasy.StreamPartTypeTextStart:
			indices[part.ID] = len(content)
			content = append(content, fantasy.TextContent{})
		case fantasy.StreamPartTypeTextDelta:
			index := indices[part.ID]
			text, _ := fantasy.AsContentType[fantasy.TextContent](content[index])
			text.Text += part.Delta
			content[index] = text
		case fantasy.StreamPartTypeReasoningStart:
			indices[part.ID] = len(content)
			content = append(content, fantasy.ReasoningContent{})
		case fantasy.StreamPartTypeReasoningDelta:
			index := indices[part.ID]
			reasoning, _ := fantasy.AsContentType[fantasy.ReasoningContent](content[index])
			reasoning.Text += part.Delta
			content[index] = reasoning
		case fantasy.StreamPartTypeToolCall:
			content = append(content, fantasy.ToolCallContent{ToolCallID: part.ID, ToolName: part.ToolCallName, Input: part.ToolCallInput})
		case fantasy.StreamPartTypeFinish:
			response.Usage, response.FinishReason = part.Usage, part.FinishReason
			response.ProviderMetadata = maps.Clone(part.ProviderMetadata)
		case fantasy.StreamPartTypeError:
			return nil, part.Error
		}
	}
	response.Content = content
	return response, nil
}

func (l *languageModel) GenerateObject(ctx context.Context, call fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return object.GenerateWithTool(ctx, l, call)
}

func (l *languageModel) StreamObject(ctx context.Context, call fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return object.StreamWithTool(ctx, l, call)
}
