package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"
)

func TestNewLLMMapTool_ProcessesJSONL(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.jsonl")
	outputPath := filepath.Join(dir, "output.jsonl")
	input := `{"name":"alice","age":30}
{"name":"bob","age":25}
{"name":"carol","age":35}
`
	require.NoError(t, os.WriteFile(inputPath, []byte(input), 0o644))

	// A completer that returns a JSON object with an "adult" field.
	completer := func(_ context.Context, prompt string) (string, error) {
		// Echo back a fixed shape so validation passes.
		return `{"adult": true, "processed": true}`, nil
	}
	tool := NewLLMMapTool(completer, nil, dir)
	paramsJSON, err := json.Marshal(LLMMapParams{
		InputPath:    inputPath,
		Prompt:       "Is this person an adult? Return JSON.",
		OutputPath:   outputPath,
		OutputSchema: `{"type":"object","required":["adult"]}`,
		Concurrency:  3,
	})
	require.NoError(t, err)
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{ID: "t1", Name: LLMMapToolName, Input: string(paramsJSON)})
	require.NoError(t, err)
	require.Contains(t, resp.Content, "3 items")
	require.Contains(t, resp.Content, "3 ok")

	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	require.Contains(t, string(data), "alice")
	require.Contains(t, string(data), "carol")
	require.Contains(t, string(data), "adult")
}

func TestNewLLMMapTool_RequiresPaths(t *testing.T) {
	t.Parallel()
	tool := NewLLMMapTool(func(_ context.Context, _ string) (string, error) { return "", nil }, nil, "")
	paramsJSON, err := json.Marshal(LLMMapParams{
		Prompt: "x",
	})
	require.NoError(t, err)
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{ID: "t2", Name: LLMMapToolName, Input: string(paramsJSON)})
	require.NoError(t, err)
	require.Contains(t, resp.Content, "required")
}

func TestNewLLMMapTool_EmptyInput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "empty.jsonl")
	outputPath := filepath.Join(dir, "out.jsonl")
	require.NoError(t, os.WriteFile(inputPath, []byte(""), 0o644))
	tool := NewLLMMapTool(func(_ context.Context, _ string) (string, error) { return "{}", nil }, nil, dir)
	paramsJSON, err := json.Marshal(LLMMapParams{
		InputPath:  inputPath,
		Prompt:     "x",
		OutputPath: outputPath,
	})
	require.NoError(t, err)
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{ID: "t3", Name: LLMMapToolName, Input: string(paramsJSON)})
	require.NoError(t, err)
	require.Contains(t, resp.Content, "empty")
}

func TestValidateAgainstSchema_TypeMismatch(t *testing.T) {
	t.Parallel()
	err := validateAgainstSchema("not an object", `{"type":"object"}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "type mismatch")
}

func TestValidateAgainstSchema_MissingRequired(t *testing.T) {
	t.Parallel()
	err := validateAgainstSchema(map[string]any{"a": 1}, `{"type":"object","required":["a","b"]}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing required field: b")
}

func TestValidateAgainstSchema_OK(t *testing.T) {
	t.Parallel()
	err := validateAgainstSchema(map[string]any{"a": 1, "b": 2}, `{"type":"object","required":["a","b"]}`)
	require.NoError(t, err)
}
