package fireworksdsv4

import (
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"
)

func TestBuildPromptUsesRawDSV4Protocol(t *testing.T) {
	t.Parallel()

	tools, err := prepareToolSpecs(fantasy.Call{Tools: []fantasy.Tool{fantasy.FunctionTool{
		Name: "echo", Description: "Echo text", InputSchema: map[string]any{
			"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}}, "required": []string{"value"},
		},
	}}})
	require.NoError(t, err)
	prompt, err := buildPrompt(fantasy.Prompt{
		fantasy.NewSystemMessage("System instructions"),
		fantasy.NewUserMessage("Hello"),
	}, tools.tools, "high", tools.chatRequired)
	require.NoError(t, err)
	require.Contains(t, prompt, beginToken+highReasoningGuidance+"System instructions")
	require.Contains(t, prompt, `"name":"echo"`)
	require.Contains(t, prompt, `"name":"send_chat_message"`)
	require.Contains(t, prompt, userToken+"Hello"+assistantToken+thinkingStart)
}

func TestBuildPromptReplaysCallsAndToolResults(t *testing.T) {
	t.Parallel()

	prompt, err := buildPrompt(fantasy.Prompt{
		fantasy.NewUserMessage("First"),
		{Role: fantasy.MessageRoleAssistant, Content: []fantasy.MessagePart{
			fantasy.ReasoningPart{Text: "reason"},
			fantasy.ToolCallPart{ToolCallID: "call", ToolName: "echo", Input: `{"value":"hello"}`},
		}},
		{Role: fantasy.MessageRoleTool, Content: []fantasy.MessagePart{
			fantasy.ToolResultPart{ToolCallID: "call", Output: fantasy.ToolResultOutputContentText{Text: "ok"}},
		}},
		fantasy.NewUserMessage("Continue"),
	}, nil, "low", false)
	require.NoError(t, err)
	require.Contains(t, prompt, "reason"+thinkingEnd)
	require.Contains(t, prompt, `<｜DSML｜invoke name="echo">`)
	require.Contains(t, prompt, "<tool_result>ok</tool_result>\n\nContinue")
}

func TestBuildPromptRejectsFiles(t *testing.T) {
	t.Parallel()

	_, err := buildPrompt(fantasy.Prompt{{Role: fantasy.MessageRoleUser, Content: []fantasy.MessagePart{
		fantasy.TextPart{Text: "look"}, fantasy.FilePart{MediaType: "image/png", Data: []byte("x")},
	}}}, nil, "none", false)
	require.ErrorContains(t, err, "does not support image input")
}
