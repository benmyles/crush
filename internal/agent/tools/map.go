package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"charm.land/fantasy"

	"github.com/charmbracelet/crush/internal/filepathext"
	"github.com/charmbracelet/crush/internal/fsext"
	"github.com/charmbracelet/crush/internal/permission"
)

// LLMMapToolName is the tool name for llm_map.
const LLMMapToolName = "llm_map"

const llmMapDescription = `Apply a prompt to every item in a JSONL input file in parallel, writing structured results to an output JSONL file.

This is an operator-level recursion primitive (from LCM): it moves the control flow of iterating over a dataset from the model layer to the deterministic engine. The model never sees the raw dataset in its context; it specifies a per-item prompt and output schema, and the engine returns aggregated results. Use this for high-throughput, side-effect-free tasks: classification, entity extraction, scoring, transformation.

When to use it:
- Processing a dataset that would overflow your context window if loaded inline.
- Applying the same extraction/classification to many items.
- Any data-parallel task where each item is independent.

The input and output are files on disk (JSONL), external to your context, so datasets of arbitrary size can be processed without polluting your window.`

const agenticMapDescription = `Spawn a full sub-agent for every item in a JSONL input file in parallel, writing structured results to an output JSONL file.

Like llm_map, but each item is processed by a full sub-agent session with access to tools (file reads, code execution) for multi-step reasoning, rather than a single stateless LLM call. Use this when per-item processing requires tool use or multi-turn reasoning that cannot be captured in a single prompt.

The input and output are files on disk (JSONL), external to your context. The scope-reduction invariant applies to nested delegation.`

// LLMMapParams are the params for llm_map.
type LLMMapParams struct {
	InputPath    string `json:"input_path" description:"Path to the JSONL input file (one JSON object per line)"`
	Prompt       string `json:"prompt" description:"The prompt to apply to each item. The item JSON is appended to this prompt."`
	OutputPath   string `json:"output_path" description:"Path to write the JSONL output file"`
	OutputSchema string `json:"output_schema,omitempty" description:"Optional JSON Schema string to validate each output against"`
	Concurrency  int    `json:"concurrency,omitempty" description:"Parallel workers (default 16)"`
}

// MapCompleter is the per-item completion function for llm_map.
type MapCompleter func(ctx context.Context, prompt string) (string, error)

// MapSubAgentRunner is the per-item sub-agent function for agentic_map.
type MapSubAgentRunner func(ctx context.Context, prompt string, readOnly bool) (string, error)

// mapResult is one item's result.
type mapResult struct {
	Index  int    `json:"index"`
	Input  any    `json:"input"`
	Output any    `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
	Status string `json:"status"`
}

// runLLMMap is the shared engine for llm_map: a worker pool that reads JSONL,
// applies the completer to each item, validates against the schema (best-effort
// JSON shape check), retries once on validation failure, and writes results to
// the output JSONL.
func runLLMMap(ctx context.Context, params LLMMapParams, complete MapCompleter) (fantasy.ToolResponse, error) {
	if strings.TrimSpace(params.InputPath) == "" || strings.TrimSpace(params.OutputPath) == "" {
		return fantasy.NewTextErrorResponse("input_path and output_path are required"), nil
	}
	if strings.TrimSpace(params.Prompt) == "" {
		return fantasy.NewTextErrorResponse("prompt is required"), nil
	}
	concurrency := params.Concurrency
	if concurrency <= 0 {
		concurrency = 16
	}
	items, err := readJSONL(params.InputPath)
	if err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("llm_map: failed to read input: %v", err)), nil
	}
	if len(items) == 0 {
		return fantasy.NewTextResponse("Input file is empty; no items to process."), nil
	}

	results := make([]mapResult, len(items))
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)
	var processed int64
	for i, item := range items {
		wg.Add(1)
		go func(idx int, in any) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			prompt := params.Prompt + "\n\nItem:\n" + mustJSONString(in)
			out, err := complete(ctx, prompt)
			status := "ok"
			errMsg := ""
			if err != nil {
				status = "error"
				errMsg = err.Error()
			} else {
				// Best-effort parse + validation.
				parsed, parseErr := parseJSON(out)
				if parseErr == nil && params.OutputSchema != "" {
					if vErr := validateAgainstSchema(parsed, params.OutputSchema); vErr != nil {
						// Retry once with the validation error in the prompt.
						out2, err2 := complete(ctx, prompt+"\n\n[validation error: "+vErr.Error()+". Return valid JSON.]")
						if err2 != nil {
							status = "error"
							errMsg = err2.Error()
						} else {
							parsed2, pErr2 := parseJSON(out2)
							if pErr2 != nil {
								status = "error"
								errMsg = "output was not valid JSON: " + pErr2.Error()
							} else {
								results[idx] = mapResult{Index: idx, Input: in, Output: parsed2, Status: "ok"}
								atomic.AddInt64(&processed, 1)
								return
							}
						}
					}
				}
				if status == "ok" && parseErr == nil {
					results[idx] = mapResult{Index: idx, Input: in, Output: parsed, Status: "ok"}
					atomic.AddInt64(&processed, 1)
					return
				}
				if status == "ok" && parseErr != nil {
					// Output isn't JSON; keep the raw text.
					results[idx] = mapResult{Index: idx, Input: in, Output: out, Status: "ok"}
					atomic.AddInt64(&processed, 1)
					return
				}
			}
			results[idx] = mapResult{Index: idx, Input: in, Status: status, Error: truncateErr(errMsg)}
			atomic.AddInt64(&processed, 1)
		}(i, item)
	}
	wg.Wait()

	if err := writeJSONL(params.OutputPath, results); err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("llm_map: failed to write output: %v", err)), nil
	}
	ok, errs := countStatuses(results)
	abs, _ := filepath.Abs(params.OutputPath)
	return fantasy.NewTextResponse(fmt.Sprintf("llm_map processed %d items: %d ok, %d errors. Output: %s", len(items), ok, errs, abs)), nil
}

// NewLLMMapTool creates the llm_map tool. The completer is the per-item
// stateless LLM call (no tools, no side effects). Paths are resolved against
// the working directory and the output write is gated by the permission
// service so llm_map cannot bypass Crush's filesystem permissions.
func NewLLMMapTool(completer MapCompleter, permissions permission.Service, workingDir string) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		LLMMapToolName,
		llmMapDescription,
		func(ctx context.Context, params LLMMapParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			// Resolve paths against the working directory so relative paths work
			// and absolute paths outside the cwd are visible to the permission
			// service.
			params.InputPath = filepathext.SmartJoin(workingDir, params.InputPath)
			params.OutputPath = filepathext.SmartJoin(workingDir, params.OutputPath)
			sessionID := GetSessionFromContext(ctx)
			if permissions != nil && sessionID != "" {
				p, err := permissions.Request(ctx, permission.CreatePermissionRequest{
					SessionID:   sessionID,
					Path:        fsext.PathOrPrefix(params.OutputPath, workingDir),
					ToolCallID:  call.ID,
					ToolName:    LLMMapToolName,
					Action:      "write",
					Description: fmt.Sprintf("llm_map write output to %s", params.OutputPath),
				})
				if err != nil {
					return fantasy.ToolResponse{}, err
				}
				if !p {
					return NewPermissionDeniedResponse(), nil
				}
			}
			return runLLMMap(ctx, params, completer)
		},
	)
}

// readJSONL reads a JSONL file into a slice of generic values.
func readJSONL(path string) ([]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var items []any
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var v any
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			return nil, fmt.Errorf("line %d: %w", len(items)+1, err)
		}
		items = append(items, v)
	}
	return items, nil
}

// writeJSONL writes results to a JSONL file.
func writeJSONL(path string, results []mapResult) error {
	dir := filepath.Dir(path)
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, r := range results {
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	return nil
}

func parseJSON(s string) (any, error) {
	s = strings.TrimSpace(s)
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, err
	}
	return v, nil
}

// validateAgainstSchema does a lightweight structural check against a JSON
// Schema string (top-level type and required fields only). A full validator is
// out of scope for the in-process engine; this catches the common case of a
// wrong shape.
func validateAgainstSchema(value any, schemaStr string) error {
	var schema map[string]any
	if err := json.Unmarshal([]byte(schemaStr), &schema); err != nil {
		return fmt.Errorf("invalid schema: %w", err)
	}
	wantType, _ := schema["type"].(string)
	if wantType != "" {
		gotType := jsonTypeOf(value)
		if gotType != wantType {
			return fmt.Errorf("type mismatch: want %s, got %s", wantType, gotType)
		}
	}
	if req, ok := schema["required"].([]any); ok {
		obj, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("expected object for required fields, got %T", value)
		}
		for _, r := range req {
			if key, ok := r.(string); ok {
				if _, present := obj[key]; !present {
					return fmt.Errorf("missing required field: %s", key)
				}
			}
		}
	}
	return nil
}

func jsonTypeOf(v any) string {
	switch v.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	case nil:
		return "null"
	default:
		return "object"
	}
}

func mustJSONString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func truncateErr(s string) string {
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}

func countStatuses(results []mapResult) (ok, errs int) {
	for _, r := range results {
		if r.Status == "ok" {
			ok++
		} else {
			errs++
		}
	}
	return
}
