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

type openTagStatus int

const (
	openInvalid openTagStatus = iota
	openPartial
	openFull
)

type openTagMatch struct {
	status     openTagStatus
	end        int
	name       string
	stringFlag bool
}

func matchTagPrefix(text, prefix string) openTagStatus {
	switch matchDelimiter(text, prefix) {
	case delimiterPartial:
		return openPartial
	case delimiterFull:
		return openFull
	default:
		return openInvalid
	}
}

func expectedTagAttributes(kind string) []string {
	switch kind {
	case "invoke":
		return []string{"name"}
	case "parameter":
		return []string{"name", "string"}
	default:
		return nil
	}
}

func matchTagAttribute(text string, cursor int, expected []string, seen map[string]string) (int, openTagStatus) {
	attribute := ""
	partial := false
	for _, candidate := range expected {
		if _, duplicate := seen[candidate]; duplicate {
			continue
		}
		switch matchTagPrefix(text[cursor:], candidate) {
		case openFull:
			attribute = candidate
		case openPartial:
			partial = true
		}
	}
	if attribute == "" {
		if partial {
			return cursor, openPartial
		}
		return cursor, openInvalid
	}
	cursor += len(attribute)
	cursor = skipWhitespace(text, cursor)
	if cursor == len(text) {
		return cursor, openPartial
	}
	if text[cursor] != '=' {
		return cursor, openInvalid
	}
	cursor++
	cursor = skipWhitespace(text, cursor)
	if cursor == len(text) {
		return cursor, openPartial
	}
	quote := text[cursor]
	if quote != '"' && quote != '\'' {
		return cursor, openInvalid
	}
	cursor++
	end := strings.IndexByte(text[cursor:], quote)
	if end < 0 {
		return cursor, openPartial
	}
	end += cursor
	seen[attribute] = text[cursor:end]
	return end + 1, openFull
}

func matchDSMLTag(text, kind string, closing bool) openTagMatch {
	prefix := "<" + dsmlToken + kind
	if closing {
		prefix = "</" + dsmlToken + kind
	}
	status := matchTagPrefix(text, prefix)
	if status != openFull {
		return openTagMatch{status: status}
	}
	cursor := len(prefix)
	if closing || kind == "tool_calls" {
		cursor = skipWhitespace(text, cursor)
		if cursor == len(text) {
			return openTagMatch{status: openPartial}
		}
		if text[cursor] != '>' {
			return openTagMatch{status: openInvalid}
		}
		return openTagMatch{status: openFull, end: cursor + 1}
	}

	expected := expectedTagAttributes(kind)
	if len(expected) == 0 {
		return openTagMatch{status: openInvalid}
	}
	seen := make(map[string]string, len(expected))
	for len(seen) < len(expected) {
		spaceStart := cursor
		cursor = skipWhitespace(text, cursor)
		if cursor == len(text) {
			return openTagMatch{status: openPartial}
		}
		if cursor == spaceStart {
			return openTagMatch{status: openInvalid}
		}
		var attributeStatus openTagStatus
		cursor, attributeStatus = matchTagAttribute(text, cursor, expected, seen)
		if attributeStatus != openFull {
			return openTagMatch{status: attributeStatus}
		}
	}
	cursor = skipWhitespace(text, cursor)
	if cursor == len(text) {
		return openTagMatch{status: openPartial}
	}
	if text[cursor] != '>' {
		return openTagMatch{status: openInvalid}
	}
	name := seen["name"]
	if name == "" {
		return openTagMatch{status: openInvalid}
	}
	result := openTagMatch{status: openFull, end: cursor + 1, name: name}
	if kind == "parameter" {
		switch seen["string"] {
		case "true":
			result.stringFlag = true
		case "false":
		default:
			return openTagMatch{status: openInvalid}
		}
	}
	return result
}

func matchOpeningTag(text string, index int, kind string) (openingTag, bool) {
	match := matchDSMLTag(text[index:], kind, false)
	if match.status != openFull {
		return openingTag{}, false
	}
	return openingTag{end: index + match.end, name: match.name, stringFlag: match.stringFlag}, true
}

func findDSMLClosingTag(text, kind string) (start, end int, status openTagStatus) {
	for cursor := 0; cursor < len(text); {
		offset := strings.IndexByte(text[cursor:], '<')
		if offset < 0 {
			break
		}
		start = cursor + offset
		match := matchDSMLTag(text[start:], kind, true)
		if match.status == openFull {
			return start, start + match.end, openFull
		}
		if match.status == openPartial {
			return start, len(text), openPartial
		}
		cursor = start + 1
	}
	return -1, -1, openInvalid
}

func decodeJSON(source string) (any, error) {
	decoder := json.NewDecoder(strings.NewReader(source))
	decoder.UseNumber()
	value, err := decodeJSONValue(decoder)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("trailing JSON content")
		}
		return nil, err
	}
	return value, nil
}

func decodeJSONValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return token, nil
	}
	switch delimiter {
	case '{':
		value := make(map[string]any)
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, fmt.Errorf("JSON object member name is not a string")
			}
			if _, duplicate := value[key]; duplicate {
				return nil, fmt.Errorf("JSON object repeats member %q", key)
			}
			item, err := decodeJSONValue(decoder)
			if err != nil {
				return nil, err
			}
			value[key] = item
		}
		closing, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		if closing != json.Delim('}') {
			return nil, fmt.Errorf("JSON object is missing its closing delimiter")
		}
		return value, nil
	case '[':
		var value []any
		for decoder.More() {
			item, err := decodeJSONValue(decoder)
			if err != nil {
				return nil, err
			}
			value = append(value, item)
		}
		closing, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		if closing != json.Delim(']') {
			return nil, fmt.Errorf("JSON array is missing its closing delimiter")
		}
		return value, nil
	default:
		return nil, fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
}

func parseJSONParameter(text string, start int) (int, any, error) {
	search := start
	for search < len(text) {
		candidate, closeEnd, status := findDSMLClosingTag(text[search:], "parameter")
		if status != openFull {
			break
		}
		candidate += search
		closeEnd += search
		value, err := decodeJSON(text[start:candidate])
		if err == nil {
			if _, stringValue := value.(string); stringValue {
				return 0, nil, fmt.Errorf("a DSML string value must use string=\"true\"")
			}
			return closeEnd, value, nil
		}
		search = candidate + 1
	}
	return 0, nil, fmt.Errorf("a DSML JSON parameter has no valid value followed by its closing tag")
}

func parseParameter(text string, start int) (end int, name string, value any, err error) {
	opening, ok := matchOpeningTag(text, start, "parameter")
	if !ok {
		return 0, "", nil, fmt.Errorf("expected a DSML parameter at byte %d", start)
	}
	if opening.stringFlag {
		closeIndex, closeEnd, status := findDSMLClosingTag(text[opening.end:], "parameter")
		if status != openFull {
			return 0, "", nil, fmt.Errorf("the DSML parameter %s is missing its closing tag", opening.name)
		}
		closeIndex += opening.end
		closeEnd += opening.end
		return closeEnd, opening.name, text[opening.end:closeIndex], nil
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
	for {
		closing := matchDSMLTag(text[cursor:], "invoke", true)
		if closing.status == openFull {
			cursor += closing.end
			break
		}
		if matchDSMLTag(text[cursor:], "tool_calls", true).status == openFull {
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
	if err := validateArguments(tool, arguments); err != nil {
		return 0, parsedCall{}, err
	}
	return cursor, parsedCall{ID: idFactory(opening.name, index), Name: opening.name, Arguments: arguments, Synthetic: tool.Synthetic}, nil
}

func parseToolCalls(text string, start int, tools map[string]toolSpec, idFactory callIDFactory) ([]parsedCall, error) {
	opening := matchDSMLTag(text[start:], "tool_calls", false)
	if opening.status != openFull {
		return nil, fmt.Errorf("the DSML tool completion is missing its opening tool_calls tag")
	}
	cursor := skipWhitespace(text, start+opening.end)
	var calls []parsedCall
	for matchDSMLTag(text[cursor:], "tool_calls", true).status != openFull {
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
	cursor += matchDSMLTag(text[cursor:], "tool_calls", true).end
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
	if matchDSMLTag(content[contentStart:], "tool_calls", false).status != openFull {
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
