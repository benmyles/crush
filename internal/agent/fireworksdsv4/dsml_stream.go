package fireworksdsv4

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

type decoderEventType int

const (
	eventThinkingStart decoderEventType = iota
	eventThinkingDelta
	eventThinkingEnd
	eventTextStart
	eventTextDelta
	eventTextEnd
	eventToolStart
	eventToolArgument
	eventToolDelta
	eventToolEnd
)

type decoderEvent struct {
	Type     decoderEventType
	Delta    string
	Text     string
	Call     parsedCall
	Name     string
	Value    any
	Complete bool
}

type decoderFinish struct {
	Events []decoderEvent
	Result parsedCompletion
}

type decoderState int

const (
	stateThinking decoderState = iota
	stateContentPrefix
	stateText
	stateToolCalls
	stateInvoke
	stateParameterString
	stateParameterJSON
	stateAfterToolCalls
)

type delimiterMatch int

const (
	delimiterNone delimiterMatch = iota
	delimiterPartial
	delimiterFull
)

func matchDelimiter(text, delimiter string) delimiterMatch {
	if len(text) >= len(delimiter) {
		if strings.HasPrefix(text, delimiter) {
			return delimiterFull
		}
		return delimiterNone
	}
	if strings.HasPrefix(delimiter, text) {
		return delimiterPartial
	}
	return delimiterNone
}

func heldSuffixLength(text, delimiter string) int {
	maximum := min(len(text), len(delimiter)-1)
	for length := maximum; length > 0; length-- {
		if strings.HasPrefix(delimiter, text[len(text)-length:]) {
			return length
		}
	}
	return 0
}

func matchInvokeOpen(text string) openTagMatch {
	return matchDSMLTag(text, "invoke", false)
}

func matchParameterOpen(text string) openTagMatch {
	return matchDSMLTag(text, "parameter", false)
}

type activeInvocation struct {
	call           parsedCall
	tool           toolSpec
	parameterCount int
	parameterName  string
	stringValue    string
	jsonSource     string
	jsonInString   bool
	jsonEscaped    bool
}

type dsmlDecoder struct {
	state           decoderState
	pending         string
	consumed        int
	thinkingText    string
	thinkingStarted bool
	textText        string
	textStarted     bool
	tools           map[string]toolSpec
	idFactory       callIDFactory
	createdIDs      []string
	calls           []parsedCall
	active          *activeInvocation
}

func newDSMLDecoder(thinkingEnabled bool, tools map[string]toolSpec, idFactory callIDFactory) *dsmlDecoder {
	state := stateContentPrefix
	if thinkingEnabled {
		state = stateThinking
	}
	if idFactory == nil {
		idFactory = defaultCallID
	}
	return &dsmlDecoder{state: state, tools: tools, idFactory: idFactory}
}

func (d *dsmlDecoder) push(delta string) ([]decoderEvent, error) {
	d.pending += delta
	var events []decoderEvent
	for {
		progress, err := d.step(&events)
		if err != nil {
			return nil, err
		}
		if !progress {
			return events, nil
		}
	}
}

func (d *dsmlDecoder) drop(length int) string {
	value := d.pending[:length]
	d.pending = d.pending[length:]
	d.consumed += length
	return value
}

func (d *dsmlDecoder) emitThinking(value string, events *[]decoderEvent) {
	if value == "" {
		return
	}
	if !d.thinkingStarted {
		d.thinkingStarted = true
		*events = append(*events, decoderEvent{Type: eventThinkingStart})
	}
	d.thinkingText += value
	*events = append(*events, decoderEvent{Type: eventThinkingDelta, Delta: value})
}

func (d *dsmlDecoder) endThinking(events *[]decoderEvent) {
	if d.thinkingStarted {
		*events = append(*events, decoderEvent{Type: eventThinkingEnd, Text: d.thinkingText})
	}
}

func (d *dsmlDecoder) emitText(value string, events *[]decoderEvent) {
	if value == "" {
		return
	}
	if !d.textStarted {
		d.textStarted = true
		*events = append(*events, decoderEvent{Type: eventTextStart})
	}
	d.textText += value
	*events = append(*events, decoderEvent{Type: eventTextDelta, Delta: value})
}

func (d *dsmlDecoder) endText(events *[]decoderEvent) {
	if d.textStarted {
		*events = append(*events, decoderEvent{Type: eventTextEnd, Text: d.textText})
	}
}

func (d *dsmlDecoder) step(events *[]decoderEvent) (bool, error) {
	switch d.state {
	case stateThinking:
		boundary := strings.Index(d.pending, thinkingEnd)
		if boundary < 0 {
			hold := heldSuffixLength(d.pending, thinkingEnd)
			if hold == len(d.pending) {
				return false, nil
			}
			d.emitThinking(d.drop(len(d.pending)-hold), events)
			return true, nil
		}
		d.emitThinking(d.drop(boundary), events)
		d.endThinking(events)
		d.drop(len(thinkingEnd))
		d.state = stateContentPrefix
		return true, nil
	case stateContentPrefix:
		start := skipWhitespace(d.pending, 0)
		if start == len(d.pending) {
			return false, nil
		}
		opening := matchDSMLTag(d.pending[start:], "tool_calls", false)
		if opening.status == openPartial {
			return false, nil
		}
		if opening.status == openFull {
			d.drop(start + opening.end)
			d.state = stateToolCalls
			return true, nil
		}
		d.state = stateText
		d.emitText(d.drop(len(d.pending)), events)
		return true, nil
	case stateText:
		if d.pending == "" {
			return false, nil
		}
		d.emitText(d.drop(len(d.pending)), events)
		return true, nil
	case stateToolCalls:
		return d.stepToolCalls(events)
	case stateInvoke:
		return d.stepInvoke(events)
	case stateParameterString:
		return d.stepParameterString(events)
	case stateParameterJSON:
		return d.stepParameterJSON(events)
	case stateAfterToolCalls:
		if d.pending == "" {
			return false, nil
		}
		if strings.TrimSpace(d.pending) != "" {
			return false, fmt.Errorf("the DSML completion contains text after its tool_calls block")
		}
		d.drop(len(d.pending))
		return true, nil
	default:
		return false, fmt.Errorf("the DSML decoder entered an unknown state")
	}
}

func (d *dsmlDecoder) stepToolCalls(events *[]decoderEvent) (bool, error) {
	start := skipWhitespace(d.pending, 0)
	if start == len(d.pending) {
		if start == 0 {
			return false, nil
		}
		d.drop(start)
		return true, nil
	}
	rest := d.pending[start:]
	closing := matchDSMLTag(rest, "tool_calls", true)
	if closing.status == openFull {
		if len(d.calls) == 0 {
			return false, fmt.Errorf("the DSML tool_calls block must contain an invocation")
		}
		d.drop(start + closing.end)
		d.state = stateAfterToolCalls
		return true, nil
	}
	if len(d.calls) >= maxToolCalls {
		if closing.status == openPartial {
			return false, nil
		}
		return false, fmt.Errorf("the DSML completion exceeds the %d-call limit", maxToolCalls)
	}
	opening := matchInvokeOpen(rest)
	if opening.status == openPartial {
		return false, nil
	}
	if opening.status == openInvalid {
		if closing.status == openPartial {
			return false, nil
		}
		return false, fmt.Errorf("expected a DSML invocation at byte %d", d.consumed+start)
	}
	tool, ok := d.tools[opening.name]
	if !ok {
		return false, fmt.Errorf("the DSML completion invoked unavailable tool %q", opening.name)
	}
	d.drop(start + opening.end)
	id := d.idFactory(opening.name, len(d.calls))
	d.active = &activeInvocation{
		call: parsedCall{ID: id, Name: opening.name, Arguments: make(map[string]any), Synthetic: tool.Synthetic},
		tool: tool,
	}
	d.createdIDs = append(d.createdIDs, id)
	*events = append(*events, decoderEvent{Type: eventToolStart, Call: d.active.call})
	d.state = stateInvoke
	return true, nil
}

func (d *dsmlDecoder) stepInvoke(events *[]decoderEvent) (bool, error) {
	active := d.active
	if active == nil {
		return false, fmt.Errorf("the DSML decoder lost its active invocation")
	}
	start := skipWhitespace(d.pending, 0)
	if start == len(d.pending) {
		if start == 0 {
			return false, nil
		}
		d.drop(start)
		return true, nil
	}
	rest := d.pending[start:]
	invokeEnd := matchDSMLTag(rest, "invoke", true)
	if invokeEnd.status == openFull {
		d.drop(start + invokeEnd.end)
		return true, d.endInvocation(events)
	}
	toolEnd := matchDSMLTag(rest, "tool_calls", true)
	if toolEnd.status == openFull {
		return false, fmt.Errorf("the DSML invocation %s closed the tool block before closing itself", active.call.Name)
	}
	parameter := matchParameterOpen(rest)
	if parameter.status == openFull {
		if _, duplicate := active.call.Arguments[parameter.name]; duplicate {
			return false, fmt.Errorf("the DSML invocation %s repeated parameter %s", active.call.Name, parameter.name)
		}
		d.drop(start + parameter.end)
		active.parameterName = parameter.name
		prefix := ","
		if active.parameterCount == 0 {
			prefix = "{"
		}
		name, _ := json.Marshal(parameter.name)
		prefix += string(name) + ":"
		if parameter.stringFlag {
			prefix += `"`
		}
		*events = append(*events, decoderEvent{Type: eventToolDelta, Delta: prefix, Call: active.call})
		active.parameterCount++
		if parameter.stringFlag {
			active.stringValue = ""
			d.state = stateParameterString
		} else {
			active.jsonSource = ""
			active.jsonInString = false
			active.jsonEscaped = false
			d.state = stateParameterJSON
		}
		return true, nil
	}
	if parameter.status == openPartial || invokeEnd.status == openPartial || toolEnd.status == openPartial {
		return false, nil
	}
	return false, fmt.Errorf("expected a DSML parameter at byte %d", d.consumed+start)
}

func (d *dsmlDecoder) emitArgument(value any, events *[]decoderEvent) {
	active := d.active
	active.call.Arguments[active.parameterName] = value
	*events = append(*events, decoderEvent{Type: eventToolArgument, Name: active.parameterName, Value: value, Call: active.call})
}

func (d *dsmlDecoder) endInvocation(events *[]decoderEvent) error {
	active := d.active
	if err := validateArguments(active.tool, active.call.Arguments); err != nil {
		return err
	}
	delta := "}"
	if active.parameterCount == 0 {
		delta = "{}"
	}
	*events = append(*events, decoderEvent{Type: eventToolDelta, Delta: delta, Call: active.call})
	d.calls = append(d.calls, active.call)
	*events = append(*events, decoderEvent{Type: eventToolEnd, Call: active.call, Complete: true})
	d.active = nil
	d.state = stateToolCalls
	return nil
}

func (d *dsmlDecoder) endTruncatedInvocation(events *[]decoderEvent) {
	active := d.active
	d.calls = append(d.calls, active.call)
	*events = append(*events, decoderEvent{Type: eventToolEnd, Call: active.call})
	d.active = nil
}

func escapeJSONStringContent(value string) string {
	data, _ := json.Marshal(value)
	if len(data) < 2 {
		return ""
	}
	return string(data[1 : len(data)-1])
}

func (d *dsmlDecoder) emitString(value string, events *[]decoderEvent) {
	if value == "" {
		return
	}
	d.active.stringValue += value
	d.emitArgument(d.active.stringValue, events)
	*events = append(*events, decoderEvent{Type: eventToolDelta, Delta: escapeJSONStringContent(value), Call: d.active.call})
}

func (d *dsmlDecoder) stepParameterString(events *[]decoderEvent) (bool, error) {
	boundary, closeEnd, status := findDSMLClosingTag(d.pending, "parameter")
	if status != openFull {
		if status == openPartial {
			if boundary == 0 {
				return false, nil
			}
			d.emitString(d.drop(boundary), events)
			return true, nil
		}
		if d.pending == "" {
			return false, nil
		}
		d.emitString(d.drop(len(d.pending)), events)
		return true, nil
	}
	d.emitString(d.drop(boundary), events)
	d.drop(closeEnd - boundary)
	if _, exists := d.active.call.Arguments[d.active.parameterName]; !exists {
		d.emitArgument(d.active.stringValue, events)
	}
	*events = append(*events, decoderEvent{Type: eventToolDelta, Delta: `"`, Call: d.active.call})
	d.state = stateInvoke
	return true, nil
}

func (d *dsmlDecoder) flushJSON(value string, events *[]decoderEvent) {
	if value == "" {
		return
	}
	d.active.jsonSource += value
	*events = append(*events, decoderEvent{Type: eventToolDelta, Delta: value, Call: d.active.call})
}

func (d *dsmlDecoder) emitBestEffortJSON(events *[]decoderEvent) {
	if value, err := decodeJSON(d.active.jsonSource); err == nil {
		d.emitArgument(value, events)
	}
}

func (d *dsmlDecoder) stepParameterJSON(events *[]decoderEvent) (bool, error) {
	active := d.active
	scan := 0
	for scan < len(d.pending) {
		character := d.pending[scan]
		if active.jsonEscaped {
			active.jsonEscaped = false
			scan++
			continue
		}
		if active.jsonInString {
			switch character {
			case '\\':
				active.jsonEscaped = true
			case '"':
				active.jsonInString = false
			}
			scan++
			continue
		}
		if character == '"' {
			active.jsonInString = true
			scan++
			continue
		}
		if character == '<' {
			closing := matchDSMLTag(d.pending[scan:], "parameter", true)
			if closing.status == openPartial {
				break
			}
			if closing.status == openFull {
				candidate := active.jsonSource + d.pending[:scan]
				value, err := decodeJSON(candidate)
				if err == nil {
					if _, isString := value.(string); isString {
						return false, fmt.Errorf("a DSML string value must use string=\"true\"")
					}
					d.flushJSON(d.drop(scan), events)
					d.drop(closing.end)
					d.emitArgument(value, events)
					d.state = stateInvoke
					return true, nil
				}
			}
		}
		scan++
	}
	if scan == 0 {
		return false, nil
	}
	d.flushJSON(d.drop(scan), events)
	return true, nil
}

func (d *dsmlDecoder) finish(complete bool) (decoderFinish, error) {
	var events []decoderEvent
	switch d.state {
	case stateThinking:
		if complete {
			return decoderFinish{}, fmt.Errorf("the DeepSeek V4 completion is missing %s", thinkingEnd)
		}
		d.emitThinking(d.drop(len(d.pending)), &events)
		d.endThinking(&events)
	case stateContentPrefix:
		if complete {
			d.state = stateText
			d.emitText(d.drop(len(d.pending)), &events)
			d.endText(&events)
		} else {
			d.drop(len(d.pending))
		}
	case stateText:
		d.endText(&events)
	case stateToolCalls:
		if complete {
			return decoderFinish{}, fmt.Errorf("expected a DSML invocation at byte %d", d.consumed+skipWhitespace(d.pending, 0))
		}
		d.drop(len(d.pending))
	case stateInvoke:
		if complete {
			return decoderFinish{}, fmt.Errorf("expected a DSML parameter at byte %d", d.consumed+skipWhitespace(d.pending, 0))
		}
		d.drop(len(d.pending))
		d.endTruncatedInvocation(&events)
	case stateParameterString:
		if complete {
			return decoderFinish{}, fmt.Errorf("the DSML parameter %s is missing its closing tag", d.active.parameterName)
		}
		d.emitString(d.drop(len(d.pending)), &events)
		if _, exists := d.active.call.Arguments[d.active.parameterName]; !exists {
			d.emitArgument(d.active.stringValue, &events)
		}
		d.endTruncatedInvocation(&events)
	case stateParameterJSON:
		if complete {
			return decoderFinish{}, fmt.Errorf("a DSML JSON parameter has no valid value followed by its closing tag")
		}
		d.emitBestEffortJSON(&events)
		d.flushJSON(d.drop(len(d.pending)), &events)
		d.endTruncatedInvocation(&events)
	case stateAfterToolCalls:
	}
	return decoderFinish{Events: events, Result: parsedCompletion{Thinking: d.thinkingText, Text: d.textText, Calls: append([]parsedCall(nil), d.calls...)}}, nil
}

func completionsEqual(left, right parsedCompletion) bool {
	if left.Thinking != right.Thinking || left.Text != right.Text || len(left.Calls) != len(right.Calls) {
		return false
	}
	for index := range left.Calls {
		l, r := left.Calls[index], right.Calls[index]
		if l.ID != r.ID || l.Name != r.Name || l.Synthetic != r.Synthetic || !reflect.DeepEqual(l.Arguments, r.Arguments) {
			return false
		}
	}
	return true
}
