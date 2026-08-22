package fireworksdsv4

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
)

type parsedCall struct {
	ID        string
	Name      string
	Arguments map[string]any
	Synthetic bool
}

type parsedCompletion struct {
	Thinking string
	Text     string
	Calls    []parsedCall
}

type callIDFactory func(name string, index int) string

func defaultCallID(name string, _ int) string {
	return "dsv4-" + name + "-" + uuid.NewString()
}

func skipWhitespace(text string, start int) int {
	for start < len(text) {
		switch text[start] {
		case ' ', '\t', '\n', '\r':
			start++
		default:
			return start
		}
	}
	return start
}

type openingTag struct {
	end        int
	name       string
	stringFlag bool
}

func matchOpeningTag(text string, index int, kind string) (openingTag, bool) {
	prefix := "<" + dsmlToken + kind + ` name="`
	if !strings.HasPrefix(text[index:], prefix) {
		return openingTag{}, false
	}
	start := index + len(prefix)
	quote := strings.IndexByte(text[start:], '"')
	if quote <= 0 {
		return openingTag{}, false
	}
	quote += start
	name := text[start:quote]
	if kind == "invoke" {
		if quote+1 >= len(text) || text[quote+1] != '>' {
			return openingTag{}, false
		}
		return openingTag{end: quote + 2, name: name}, true
	}
	rest := text[quote:]
	attribute := `" string="`
	if !strings.HasPrefix(rest, attribute) {
		return openingTag{}, false
	}
	flagStart := quote + len(attribute)
	var flag string
	switch {
	case strings.HasPrefix(text[flagStart:], `true">`):
		flag = "true"
	case strings.HasPrefix(text[flagStart:], `false">`):
		flag = "false"
	default:
		return openingTag{}, false
	}
	return openingTag{end: flagStart + len(flag) + 2, name: name, stringFlag: flag == "true"}, true
}

func decodeJSON(source string) (any, error) {
	decoder := json.NewDecoder(strings.NewReader(source))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("trailing JSON content")
		}
		return nil, err
	}
	return value, nil
}

func parseJSONParameter(text string, start int) (int, any, error) {
	candidate := strings.Index(text[start:], parameterClose)
	if candidate >= 0 {
		candidate += start
	}
	for candidate >= 0 {
		value, err := decodeJSON(text[start:candidate])
		if err == nil {
			if _, stringValue := value.(string); stringValue {
				return 0, nil, fmt.Errorf("a DSML string value must use string=\"true\"")
			}
			return candidate + len(parameterClose), value, nil
		}
		next := strings.Index(text[candidate+1:], parameterClose)
		if next < 0 {
			break
		}
		candidate += next + 1
	}
	return 0, nil, fmt.Errorf("a DSML JSON parameter has no valid value followed by its closing tag")
}

func parseParameter(text string, start int) (end int, name string, value any, err error) {
	opening, ok := matchOpeningTag(text, start, "parameter")
	if !ok {
		return 0, "", nil, fmt.Errorf("expected a DSML parameter at byte %d", start)
	}
	if opening.stringFlag {
		closeIndex := strings.Index(text[opening.end:], parameterClose)
		if closeIndex < 0 {
			return 0, "", nil, fmt.Errorf("the DSML parameter %s is missing its closing tag", opening.name)
		}
		closeIndex += opening.end
		return closeIndex + len(parameterClose), opening.name, text[opening.end:closeIndex], nil
	}
	end, value, err = parseJSONParameter(text, opening.end)
	return end, opening.name, value, err
}

func validateArguments(tool toolSpec, arguments map[string]any) error {
	result := tool.Validator.Validate(arguments)
	if result.IsValid() {
		return nil
	}
	var messages []string
	for field, validationErr := range result.Errors {
		messages = append(messages, field+": "+validationErr.Message)
	}
	return fmt.Errorf("the DSML arguments for %s do not match the active tool schema: %s", tool.Name, strings.Join(messages, "; "))
}

func parseInvocation(text string, start int, tools map[string]toolSpec, index int, idFactory callIDFactory) (int, parsedCall, error) {
	opening, ok := matchOpeningTag(text, start, "invoke")
	if !ok {
		return 0, parsedCall{}, fmt.Errorf("expected a DSML invocation at byte %d", start)
	}
	tool, ok := tools[opening.name]
	if !ok {
		return 0, parsedCall{}, fmt.Errorf("the DSML completion invoked unavailable tool %q", opening.name)
	}
	arguments := make(map[string]any)
	cursor := skipWhitespace(text, opening.end)
	for !strings.HasPrefix(text[cursor:], invokeClose) {
		if strings.HasPrefix(text[cursor:], toolCallsClose) {
			return 0, parsedCall{}, fmt.Errorf("the DSML invocation %s closed the tool block before closing itself", opening.name)
		}
		end, name, value, err := parseParameter(text, cursor)
		if err != nil {
			return 0, parsedCall{}, err
		}
		if _, duplicate := arguments[name]; duplicate {
			return 0, parsedCall{}, fmt.Errorf("the DSML invocation %s repeated parameter %s", opening.name, name)
		}
		arguments[name] = value
		cursor = skipWhitespace(text, end)
	}
	cursor += len(invokeClose)
	if err := validateArguments(tool, arguments); err != nil {
		return 0, parsedCall{}, err
	}
	return cursor, parsedCall{ID: idFactory(opening.name, index), Name: opening.name, Arguments: arguments, Synthetic: tool.Synthetic}, nil
}

func parseToolCalls(text string, start int, tools map[string]toolSpec, idFactory callIDFactory) ([]parsedCall, error) {
	if !strings.HasPrefix(text[start:], toolCallsOpen) {
		return nil, fmt.Errorf("the DSML tool completion is missing its opening tool_calls tag")
	}
	cursor := skipWhitespace(text, start+len(toolCallsOpen))
	var calls []parsedCall
	for !strings.HasPrefix(text[cursor:], toolCallsClose) {
		if len(calls) >= maxToolCalls {
			return nil, fmt.Errorf("the DSML completion exceeds the %d-call limit", maxToolCalls)
		}
		end, call, err := parseInvocation(text, cursor, tools, len(calls), idFactory)
		if err != nil {
			return nil, err
		}
		calls = append(calls, call)
		cursor = skipWhitespace(text, end)
	}
	if len(calls) == 0 {
		return nil, fmt.Errorf("the DSML tool_calls block must contain an invocation")
	}
	cursor += len(toolCallsClose)
	if strings.TrimSpace(text[cursor:]) != "" {
		return nil, fmt.Errorf("the DSML completion contains text after its tool_calls block")
	}
	return calls, nil
}

func parseCompletion(completion string, thinkingEnabled bool, tools map[string]toolSpec, idFactory callIDFactory) (parsedCompletion, error) {
	result := parsedCompletion{}
	content := completion
	if thinkingEnabled {
		boundary := strings.Index(content, thinkingEnd)
		if boundary < 0 {
			return result, fmt.Errorf("the DeepSeek V4 completion is missing %s", thinkingEnd)
		}
		result.Thinking = content[:boundary]
		content = content[boundary+len(thinkingEnd):]
	}
	contentStart := skipWhitespace(content, 0)
	if !strings.HasPrefix(content[contentStart:], toolCallsOpen) {
		result.Text = content
		return result, nil
	}
	if idFactory == nil {
		idFactory = defaultCallID
	}
	calls, err := parseToolCalls(content, contentStart, tools, idFactory)
	if err != nil {
		return result, err
	}
	result.Calls = calls
	return result, nil
}
