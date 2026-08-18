package compaction

import (
	"testing"

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

func TestCosine(t *testing.T) {
	t.Parallel()
	// Identical vectors -> 1.
	require.InDelta(t, float32(1.0), cosine([]float32{1, 0, 0}, []float32{1, 0, 0}), 0.001)
	// Orthogonal -> 0.
	require.InDelta(t, float32(0.0), cosine([]float32{1, 0}, []float32{0, 1}), 0.001)
	// Opposite -> -1.
	require.InDelta(t, float32(-1.0), cosine([]float32{1, 0}, []float32{-1, 0}), 0.001)
}

func TestEncodeDecodeFloat32s(t *testing.T) {
	t.Parallel()
	original := []float32{1.0, 2.5, -3.14, 0.0}
	blob := encodeFloat32s(original)
	decoded, ok := decodeFloat32s(blob)
	require.True(t, ok)
	require.InDeltaSlice(t, original, decoded, 0.001)
}

func TestDecodeFloat32s_Invalid(t *testing.T) {
	t.Parallel()
	_, ok := decodeFloat32s([]byte{1, 2, 3})
	require.False(t, ok)
	_, ok = decodeFloat32s(nil)
	require.False(t, ok)
}
