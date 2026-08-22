package fireworksdsv4

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func testToolSet(t *testing.T) map[string]toolSpec {
	t.Helper()
	echo, err := compileTool("echo", "Echo", jsonSchema{
		"type": "object",
		"properties": map[string]any{
			"value": map[string]any{"type": "string"},
			"count": map[string]any{"type": "integer", "minimum": 1.0},
		},
		"required":             []string{"value"},
		"additionalProperties": false,
	}, false)
	require.NoError(t, err)
	return map[string]toolSpec{"echo": echo}
}

func dsmlEcho(value string) string {
	return toolCallsOpen + `<｜DSML｜invoke name="echo"><｜DSML｜parameter name="value" string="true">` + value + parameterClose + invokeClose + toolCallsClose
}

func TestParseCompletionValidatesDSML(t *testing.T) {
	t.Parallel()

	parsed, err := parseCompletion("reason"+thinkingEnd+dsmlEcho("hello"), true, testToolSet(t), func(name string, index int) string {
		return fmt.Sprintf("%s-%d", name, index)
	})
	require.NoError(t, err)
	require.Equal(t, "reason", parsed.Thinking)
	require.Len(t, parsed.Calls, 1)
	require.Equal(t, "echo-0", parsed.Calls[0].ID)
	require.Equal(t, "hello", parsed.Calls[0].Arguments["value"])
}

func TestParseCompletionRejectsInvalidArguments(t *testing.T) {
	t.Parallel()

	duplicate := toolCallsOpen + `<｜DSML｜invoke name="echo"><｜DSML｜parameter name="value" string="true">a` + parameterClose + `<｜DSML｜parameter name="value" string="true">b` + parameterClose + invokeClose + toolCallsClose
	_, err := parseCompletion(duplicate, false, testToolSet(t), nil)
	require.ErrorContains(t, err, "repeated parameter")

	missing := toolCallsOpen + `<｜DSML｜invoke name="echo">` + invokeClose + toolCallsClose
	_, err = parseCompletion(missing, false, testToolSet(t), nil)
	require.ErrorContains(t, err, "do not match")
}

func TestParseCompletionAllowsFlexibleDSMLAndJSON(t *testing.T) {
	t.Parallel()

	completion := " \n" +
		"<" + dsmlToken + "tool_calls \t>\n" +
		"<" + dsmlToken + "invoke\n name = 'echo' \t>\n" +
		"<" + dsmlToken + "parameter string = 'false'\n name = 'count' >\n 2 \r\n</" + dsmlToken + "parameter \t>\n" +
		"<" + dsmlToken + "parameter\tstring='true' name = \"value\" >hello</" + dsmlToken + "parameter >\n" +
		"</" + dsmlToken + "invoke \n>\n" +
		"</" + dsmlToken + "tool_calls\t> \n"
	parsed, err := parseCompletion(completion, false, testToolSet(t), nil)
	require.NoError(t, err)
	require.Len(t, parsed.Calls, 1)
	require.Equal(t, "hello", parsed.Calls[0].Arguments["value"])
	require.Equal(t, json.Number("2"), parsed.Calls[0].Arguments["count"])
}

func TestFlexibleDSMLStreamingMatchesOneShotAtEverySplit(t *testing.T) {
	t.Parallel()

	completion := "<" + dsmlToken + "tool_calls >\n" +
		"<" + dsmlToken + "invoke name = 'echo'>\n" +
		"<" + dsmlToken + "parameter name='count' string = 'false' > \n2\t </" + dsmlToken + "parameter >\n" +
		"<" + dsmlToken + "parameter string = 'true' name='value' >hello</" + dsmlToken + "parameter >\n" +
		"</" + dsmlToken + "invoke >\n" +
		"</" + dsmlToken + "tool_calls >"
	for split := 0; split <= len(completion); split++ {
		split := split
		t.Run(fmt.Sprint(split), func(t *testing.T) {
			t.Parallel()
			factory := func(name string, index int) string { return fmt.Sprintf("%s-%d", name, index) }
			decoder := newDSMLDecoder(false, testToolSet(t), factory)
			_, err := decoder.push(completion[:split])
			require.NoError(t, err)
			_, err = decoder.push(completion[split:])
			require.NoError(t, err)
			finished, err := decoder.finish(true)
			require.NoError(t, err)
			parsed, err := parseCompletion(completion, false, testToolSet(t), factory)
			require.NoError(t, err)
			require.True(t, completionsEqual(finished.Result, parsed))
		})
	}
}

func TestParseCompletionAllowsAnyOrderJSONMembers(t *testing.T) {
	t.Parallel()

	tool, err := compileTool("store", "Store", jsonSchema{
		"type": "object",
		"properties": map[string]any{
			"value": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"alpha": map[string]any{"type": "string"},
					"beta":  map[string]any{"type": "integer"},
				},
				"required":             []string{"alpha", "beta"},
				"additionalProperties": false,
			},
		},
		"required":             []string{"value"},
		"additionalProperties": false,
	}, false)
	require.NoError(t, err)
	completion := toolCallsOpen + `<｜DSML｜invoke name="store"><｜DSML｜parameter string="false" name="value">
	{
		"beta" : 2,
		"alpha" : "first"
	}
	</｜DSML｜parameter></｜DSML｜invoke>` + toolCallsClose
	parsed, err := parseCompletion(completion, false, map[string]toolSpec{"store": tool}, nil)
	require.NoError(t, err)
	value := parsed.Calls[0].Arguments["value"].(map[string]any)
	require.Equal(t, "first", value["alpha"])
	require.Equal(t, json.Number("2"), value["beta"])
}

func TestDecodeJSONRejectsDuplicateMembers(t *testing.T) {
	t.Parallel()

	_, err := decodeJSON(`{"outer":{"value":1,"value":2}}`)
	require.ErrorContains(t, err, `repeats member "value"`)
}

func TestParseJSONParameterAllowsClosingTagTextInsideNestedString(t *testing.T) {
	t.Parallel()

	tool, err := compileTool("store", "Store", jsonSchema{
		"type": "object",
		"properties": map[string]any{
			"value": map[string]any{"type": "object"},
		},
		"required": []string{"value"},
	}, false)
	require.NoError(t, err)
	completion := toolCallsOpen + `<｜DSML｜invoke name="store"><｜DSML｜parameter name="value" string="false">{"nested":"` + parameterClose + `"}` + parameterClose + invokeClose + toolCallsClose
	parsed, err := parseCompletion(completion, false, map[string]toolSpec{"store": tool}, nil)
	require.NoError(t, err)
	nested := parsed.Calls[0].Arguments["value"].(map[string]any)
	require.Equal(t, parameterClose, nested["nested"])
}

func TestStreamingDecoderMatchesOneShotAtEverySplit(t *testing.T) {
	t.Parallel()

	completion := "reason" + thinkingEnd + dsmlEcho("hello")
	for split := 0; split <= len(completion); split++ {
		split := split
		t.Run(fmt.Sprint(split), func(t *testing.T) {
			t.Parallel()
			factory := func(name string, index int) string { return fmt.Sprintf("%s-%d", name, index) }
			decoder := newDSMLDecoder(true, testToolSet(t), factory)
			_, err := decoder.push(completion[:split])
			require.NoError(t, err)
			_, err = decoder.push(completion[split:])
			require.NoError(t, err)
			finished, err := decoder.finish(true)
			require.NoError(t, err)
			parsed, err := parseCompletion(completion, true, testToolSet(t), factory)
			require.NoError(t, err)
			require.True(t, completionsEqual(finished.Result, parsed))
		})
	}
}

func TestStreamingDecoderKeepsPartialStringWithoutCompletingCall(t *testing.T) {
	t.Parallel()

	partial := toolCallsOpen + `<｜DSML｜invoke name="echo"><｜DSML｜parameter name="value" string="true">part`
	decoder := newDSMLDecoder(false, testToolSet(t), nil)
	_, err := decoder.push(partial)
	require.NoError(t, err)
	finished, err := decoder.finish(false)
	require.NoError(t, err)
	require.Len(t, finished.Result.Calls, 1)
	require.Equal(t, "part", finished.Result.Calls[0].Arguments["value"])
	require.False(t, finished.Events[len(finished.Events)-1].Complete)
}

func TestBuildGrammarHandlesNestedAndAnyOrderSchemas(t *testing.T) {
	t.Parallel()

	tool, err := compileTool("complex", "Complex", jsonSchema{
		"type": "object",
		"properties": map[string]any{
			"tuple":  map[string]any{"type": "array", "prefixItems": []any{map[string]any{"type": "string"}, map[string]any{"type": "integer"}}, "items": false},
			"nested": map[string]any{"type": "object", "properties": map[string]any{"enabled": map[string]any{"type": "boolean"}}, "required": []string{"enabled"}},
		},
		"required": []string{"tuple", "nested"},
	}, false)
	require.NoError(t, err)
	grammar, err := buildGrammar([]toolSpec{tool}, true, "required")
	require.NoError(t, err)
	require.Contains(t, grammar, "root ::=")
	require.Contains(t, grammar, `dsv4-tool-calls-block`)
	require.Contains(t, grammar, `json-property-enabled`)
	require.Contains(t, grammar, `dsml-gap ::= [\t\n\r ]*`)
	require.Contains(t, grammar, `dsml-required-gap ::= [\t\n\r ]+`)
	require.NotContains(t, grammar, `{0,64}`)
}

func TestAnyOrderGrammarSizeGrowsLinearly(t *testing.T) {
	t.Parallel()

	script := newGrammarScript()
	entries := make([]string, 8)
	for index := range entries {
		entries[index] = literal(fmt.Sprintf("field-%02d", index))
	}
	maximum := len(entries)
	_, err := buildAnyOrderMembersRule(anyOrderOptions{
		script:          script,
		entryRules:      entries,
		requiredIndices: map[int]struct{}{0: {}, 3: {}, 7: {}},
		minimum:         3,
		maximum:         &maximum,
		separator:       literal(","),
		label:           "Test object",
	})
	require.NoError(t, err)
	grammar := script.render()
	require.Less(t, len(grammar), 1_000)
	require.Equal(t, 1, strings.Count(grammar, "any-order-members ::="))
}
