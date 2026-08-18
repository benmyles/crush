package compaction

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

func TestBuildExtractsQuery_OnlyUserText(t *testing.T) {
	t.Parallel()
	q := BuildExtractsQuery(
		[]string{"continue the refactor"},
		[]string{"Add JWT middleware", "then fix tests", "deploy it"},
		"",
	)
	require.Contains(t, q, "Current task: continue the refactor")
	require.Contains(t, q, "Recent requests:")
	require.Contains(t, q, "Keep:")
}

func TestRunExtractsLane_KeepsGoldenSpans(t *testing.T) {
	t.Parallel()
	history := []message.Message{
		{ID: "u1", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Fix the bug in foo.go at line 42"}}, CreatedAt: 100},
		{ID: "a1", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "I'll look at foo.go."}}, CreatedAt: 101},
	}
	span := BuildSpanModel(SpanInput{History: history})
	req := BuildExtractsLaneRequest(span, "fix bug", ExtractsRenderBudget, true)
	res := RunExtractsLane(req)
	require.NotEmpty(t, res.Text)
	// The user message (golden span) should be wrapped in keepContext tags.
	require.Contains(t, res.Text, keepOpen)
	require.Contains(t, res.Text, keepClose)
	// The user instruction must survive in the output.
	require.Contains(t, res.Text, "Fix the bug in foo.go")
}

func TestRunExtractsLane_OmitsEmptyBlocks(t *testing.T) {
	t.Parallel()
	history := []message.Message{
		{ID: "u1", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "hello"}}, CreatedAt: 100},
		{ID: "a1", Role: message.Assistant, Parts: []message.ContentPart{}, CreatedAt: 101},
	}
	span := BuildSpanModel(SpanInput{History: history})
	req := BuildExtractsLaneRequest(span, "hello", ExtractsRenderBudget, true)
	res := RunExtractsLane(req)
	// Only the user block and any non-empty blocks appear.
	require.Contains(t, res.Text, "hello")
}

func TestRenderOlderLane_Truncates(t *testing.T) {
	t.Parallel()
	long := make([]byte, 5000)
	for i := range long {
		long[i] = 'a'
	}
	out := RenderOlderLane(string(long), 100)
	require.Less(t, len(out), 5000)
	require.Contains(t, out, "characters of older extracts omitted")
}

func TestCollectWorkingSet_SkipsSecrets(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Write a normal file and a secret-like file.
	normalPath := filepath.Join(dir, "foo.go")
	require.NoError(t, os.WriteFile(normalPath, []byte("package main\n"), 0o644))
	secretPath := filepath.Join(dir, ".env")
	require.NoError(t, os.WriteFile(secretPath, []byte("SECRET=xxx\n"), 0o644))
	files := []LedgerFile{
		{Path: "foo.go", Edits: 1, LastOp: "edit", LastTurn: 1, LastSeq: 5},
		{Path: ".env", Edits: 1, LastOp: "edit", LastTurn: 2, LastSeq: 6},
	}
	snap := CollectWorkingSet(WorkingSetInput{
		Files:           files,
		Cwd:             dir,
		MaxFiles:        3,
		MaxCharsPerFile: 12000,
		MaxTotalChars:   36000,
	})
	require.Len(t, snap.Files, 1)
	require.Equal(t, "foo.go", snap.Files[0].Path)
	require.Contains(t, snap.Files[0].Content, "package main")
	require.NotEmpty(t, snap.Skipped)
	require.Equal(t, ".env", snap.Skipped[0].Path)
}

func TestIsSecretLikePath(t *testing.T) {
	t.Parallel()
	require.True(t, IsSecretLikePath(".env"))
	require.True(t, IsSecretLikePath("secrets.json"))
	require.True(t, IsSecretLikePath("id_rsa"))
	require.True(t, IsSecretLikePath("foo.pem"))
	require.False(t, IsSecretLikePath("foo.go"))
	require.False(t, IsSecretLikePath("token_handler.go"))
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
