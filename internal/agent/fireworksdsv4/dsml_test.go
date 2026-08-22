package fireworksdsv4

import (
	"fmt"
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
}
