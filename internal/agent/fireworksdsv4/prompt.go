package fireworksdsv4

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"charm.land/fantasy"
)

type promptTurn struct {
	assistant       bool
	message         fantasy.Message
	blocks          []string
	acceptsUserText bool
}

func reasoningGuidance(level string) string {
	switch level {
	case "high", "xhigh":
		return highReasoningGuidance
	case "max":
		return maxReasoningGuidance
	default:
		return ""
	}
}

func renderTools(tools []toolSpec, chatRequired bool) (string, error) {
	schemas := make([]string, 0, len(tools))
	for _, tool := range tools {
		data, err := json.Marshal(map[string]any{
			"name":        tool.Name,
			"description": tool.Description,
			"parameters":  tool.Schema,
		})
		if err != nil {
			return "", fmt.Errorf("marshaling DSV4 tool %q: %w", tool.Name, err)
		}
		schemas = append(schemas, string(data))
	}

	nextAction := fmt.Sprintf("Decide whether tools are needed. If so, respond with at least one and no more than %d DSML invocations. Otherwise respond directly in text.", maxToolCalls)
	if chatRequired {
		nextAction = fmt.Sprintf("Every response in this chat MUST contain at least one and no more than %d DSML tool invocations. Direct text responses are unavailable.\n\nUse `%s` often for concise progress updates during meaningful multi-step work. Batch an update with at least one non-terminating work tool so the run continues. Call `%s` by itself only when the task is complete; that message is the final answer and ends the run.", maxToolCalls, chatToolName, chatToolName)
	}

	return fmt.Sprintf(`## Tools

You have access to a set of tools to help answer the user's question. You can invoke tools by writing a "<%[1]stool_calls>" block like the following:

<%[1]stool_calls>
<%[1]sinvoke name="$TOOL_NAME">
<%[1]sparameter name="$PARAMETER_NAME" string="true|false">$PARAMETER_VALUE</%[1]sparameter>
...
</%[1]sinvoke>
<%[1]sinvoke name="$TOOL_NAME2">
...
</%[1]sinvoke>
</%[1]stool_calls>

String parameters should be specified as is and set `+"`string=\"true\"`"+`. For all other types (numbers, booleans, arrays, objects), pass the value in JSON format and set `+"`string=\"false\"`"+`.

If thinking mode is enabled (triggered by %[2]s), you MUST output your complete reasoning inside %[2]s...%[3]s BEFORE any tool calls or final response.

Otherwise, output directly after %[3]s with tool calls or final response.

### Available Tool Schemas

%[4]s

You MUST strictly follow the above defined tool name and parameter schemas to invoke tool calls. A raw `+"`string=true`"+` value must not contain DSML `+"`tool_calls`"+`, `+"`invoke`"+`, or `+"`parameter`"+` tags. Put every sibling field in its own named parameter.

## Next Action

%[5]s`, dsmlToken, thinkingStart, thinkingEnd, strings.Join(schemas, "\n"), nextAction), nil
}

func messageText(message fantasy.Message) (string, error) {
	var values []string
	for _, part := range message.Content {
		switch part.GetType() {
		case fantasy.ContentTypeText:
			text, ok := fantasy.AsMessagePart[fantasy.TextPart](part)
			if ok {
				values = append(values, text.Text)
			}
		case fantasy.ContentTypeFile:
			return "", fmt.Errorf("fireworks DeepSeek V4 raw transport does not support image input")
		}
	}
	return strings.Join(values, "\n\n"), nil
}

func toolResultText(message fantasy.Message) string {
	var values []string
	for _, part := range message.Content {
		result, ok := fantasy.AsMessagePart[fantasy.ToolResultPart](part)
		if !ok {
			continue
		}
		if result.Output == nil {
			values = append(values, "[Tool returned non-text content]")
			continue
		}
		switch result.Output.GetType() {
		case fantasy.ToolResultContentTypeText:
			if text, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentText](result.Output); ok {
				values = append(values, text.Text)
			}
		case fantasy.ToolResultContentTypeError:
			if output, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentError](result.Output); ok && output.Error != nil {
				values = append(values, output.Error.Error())
			}
		case fantasy.ToolResultContentTypeMedia:
			if media, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentMedia](result.Output); ok && media.Text != "" {
				values = append(values, media.Text)
			} else {
				values = append(values, "[Tool returned non-text content]")
			}
		}
	}
	text := strings.Join(values, "\n\n")
	if text == "" {
		text = "[Tool returned non-text content]"
	}
	return "<tool_result>" + text + "</tool_result>"
}

func normalizeTurns(messages fantasy.Prompt) ([]promptTurn, []string, error) {
	var turns []promptTurn
	var systems []string
	for _, message := range messages {
		switch message.Role {
		case fantasy.MessageRoleSystem:
			text, err := messageText(message)
			if err != nil {
				return nil, nil, err
			}
			if text != "" {
				systems = append(systems, text)
			}
		case fantasy.MessageRoleAssistant:
			turns = append(turns, promptTurn{assistant: true, message: message})
		case fantasy.MessageRoleTool:
			block := toolResultText(message)
			if len(turns) > 0 && !turns[len(turns)-1].assistant && turns[len(turns)-1].acceptsUserText {
				turns[len(turns)-1].blocks = append(turns[len(turns)-1].blocks, block)
			} else {
				turns = append(turns, promptTurn{blocks: []string{block}, acceptsUserText: true})
			}
		case fantasy.MessageRoleUser:
			text, err := messageText(message)
			if err != nil {
				return nil, nil, err
			}
			if len(turns) > 0 && !turns[len(turns)-1].assistant && turns[len(turns)-1].acceptsUserText {
				turns[len(turns)-1].blocks = append(turns[len(turns)-1].blocks, text)
				turns[len(turns)-1].acceptsUserText = false
			} else {
				turns = append(turns, promptTurn{blocks: []string{text}})
			}
		}
	}
	return turns, systems, nil
}

func renderArguments(call fantasy.ToolCallPart) (string, error) {
	var arguments map[string]any
	decoder := json.NewDecoder(strings.NewReader(call.Input))
	decoder.UseNumber()
	if err := decoder.Decode(&arguments); err != nil {
		return "", fmt.Errorf("stored tool call %s contains invalid JSON: %w", call.ToolName, err)
	}
	values := make([]string, 0, len(arguments))
	keys := make([]string, 0, len(arguments))
	for name := range arguments {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		value := arguments[name]
		isString := false
		var rendered string
		if text, ok := value.(string); ok {
			isString = true
			rendered = text
		} else {
			data, err := json.Marshal(value)
			if err != nil {
				return "", fmt.Errorf("stored tool call %s.%s contains a non-JSON value: %w", call.ToolName, name, err)
			}
			rendered = string(data)
		}
		values = append(values, fmt.Sprintf("<%sparameter name=%q string=%q>%s</%sparameter>", dsmlToken, name, fmt.Sprint(isString), rendered, dsmlToken))
	}
	return strings.Join(values, "\n"), nil
}

func renderAssistant(message fantasy.Message, thinkingEnabled bool) (string, error) {
	var reasoning strings.Builder
	var text strings.Builder
	var calls []fantasy.ToolCallPart
	for _, part := range message.Content {
		switch part.GetType() {
		case fantasy.ContentTypeReasoning:
			if value, ok := fantasy.AsMessagePart[fantasy.ReasoningPart](part); ok {
				reasoning.WriteString(value.Text)
			}
		case fantasy.ContentTypeText:
			if value, ok := fantasy.AsMessagePart[fantasy.TextPart](part); ok {
				text.WriteString(value.Text)
			}
		case fantasy.ContentTypeToolCall:
			if value, ok := fantasy.AsMessagePart[fantasy.ToolCallPart](part); ok {
				calls = append(calls, value)
			}
		case fantasy.ContentTypeFile:
			return "", fmt.Errorf("fireworks DeepSeek V4 raw transport does not support image input")
		}
	}
	var renderedCalls []string
	for _, call := range calls {
		arguments, err := renderArguments(call)
		if err != nil {
			return "", err
		}
		renderedCalls = append(renderedCalls, fmt.Sprintf("<%sinvoke name=%q>\n%s\n</%sinvoke>", dsmlToken, call.ToolName, arguments, dsmlToken))
	}
	toolBlock := ""
	if len(renderedCalls) > 0 {
		toolBlock = "\n\n" + toolCallsOpen + "\n" + strings.Join(renderedCalls, "\n") + "\n" + toolCallsClose
	}
	prefix := ""
	if thinkingEnabled {
		prefix = reasoning.String() + thinkingEnd
	}
	return prefix + text.String() + toolBlock + endToken, nil
}

func buildPrompt(messages fantasy.Prompt, tools []toolSpec, effort string, chatRequired bool) (string, error) {
	thinkingEnabled := effort != "" && effort != "none" && effort != "off"
	turns, systems, err := normalizeTurns(messages)
	if err != nil {
		return "", err
	}
	if len(tools) > 0 {
		toolText, err := renderTools(tools, chatRequired)
		if err != nil {
			return "", err
		}
		systems = append(systems, toolText)
	}
	prompt := beginToken + reasoningGuidance(effort) + strings.Join(systems, "\n\n")
	for i, turn := range turns {
		if turn.assistant {
			rendered, err := renderAssistant(turn.message, thinkingEnabled)
			if err != nil {
				return "", err
			}
			prompt += rendered
			continue
		}
		prompt += userToken + strings.Join(turn.blocks, "\n\n")
		if i+1 == len(turns) || turns[i+1].assistant {
			prompt += assistantToken
			if thinkingEnabled {
				prompt += thinkingStart
			} else {
				prompt += thinkingEnd
			}
		}
	}
	if len(turns) == 0 || turns[len(turns)-1].assistant {
		return "", fmt.Errorf("fireworks DeepSeek V4 context must end with a user or tool-result message")
	}
	return prompt, nil
}
