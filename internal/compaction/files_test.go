package compaction

import (
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

func TestExplorationSummary_JSON(t *testing.T) {
	t.Parallel()
	summary := ExplorationSummary("data.json", []byte(`{"name":"crush","version":1,"deps":["a","b"]}`), "application/json")
	require.Contains(t, summary, "JSON object with keys")
	require.Contains(t, summary, "name")
}

func TestExplorationSummary_JSONArray(t *testing.T) {
	t.Parallel()
	summary := ExplorationSummary("items.json", []byte(`[{"id":1,"x":2},{"id":2,"x":3}]`), "application/json")
	require.Contains(t, summary, "JSON array with 2 items")
	require.Contains(t, summary, "first item keys")
}

func TestExplorationSummary_CSV(t *testing.T) {
	t.Parallel()
	content := []byte("id,name\n1,alice\n2,bob\n")
	summary := ExplorationSummary("data.csv", content, "text/csv")
	require.Contains(t, summary, "CSV file: 3 rows")
	require.Contains(t, summary, "header: id,name")
}

func TestExplorationSummary_Code(t *testing.T) {
	t.Parallel()
	content := []byte("package main\n\nfunc foo() int { return 1 }\n\ntype Bar struct{ X int }\n")
	summary := ExplorationSummary("main.go", content, "text/x-go")
	require.Contains(t, summary, "Code file main.go")
	require.Contains(t, summary, "func foo() int")
	require.Contains(t, summary, "type Bar struct")
}

func TestExplorationSummary_Text(t *testing.T) {
	t.Parallel()
	content := []byte("This is a readme.\nMore content here.\n")
	summary := ExplorationSummary("readme.md", content, "text/markdown")
	require.Contains(t, summary, "Text file")
	require.Contains(t, summary, "lines")
	require.Contains(t, summary, "This is a readme")
}

func TestRunExtractsLane_RespectsTotalCharBudget(t *testing.T) {
	t.Parallel()
	// Build a span with many large non-golden blocks so the total budget is
	// the binding constraint, not the per-block cap.
	var history []message.Message
	for i := 0; i < 10; i++ {
		history = append(history, message.Message{
			ID:        "u" + strconv.Itoa(i),
			Role:      message.User,
			Parts:     []message.ContentPart{message.TextContent{Text: strings.Repeat("x", 5000)}},
			CreatedAt: int64(100 + i),
		})
		history = append(history, message.Message{
			ID:        "a" + strconv.Itoa(i),
			Role:      message.Assistant,
			Parts:     []message.ContentPart{message.TextContent{Text: strings.Repeat("y", 5000)}, message.Finish{Reason: message.FinishReasonEndTurn}},
			CreatedAt: int64(101 + i),
		})
	}
	span := BuildSpanModel(SpanInput{History: history})
	req := BuildExtractsLaneRequest(span, "query", ExtractsRenderBudget, true)
	req.TotalCharBudget = 10000 // far below the ~100k input
	res := RunExtractsLane(req)
	require.Less(t, res.OutputChars, req.TotalCharBudget*2, "output must be bounded by the total budget, not emit every block")
	require.Less(t, res.OutputChars, res.InputChars, "extracts must compress")
}

func TestRunExtractsLane_OlderLaneDecays(t *testing.T) {
	t.Parallel()
	prev := strings.Repeat("older line\n", 5000) // ~50k chars
	maxIn := 8000
	out := RenderOlderLane(prev, maxIn)
	require.Less(t, len(out), len(prev), "older lane must truncate")
	require.Contains(t, out, "omitted")
}
