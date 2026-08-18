package message

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCompactionContentRoundTrip verifies the CompactionContent part survives
// the parts codec (marshal to JSON + unmarshal back), which is how it is
// persisted to and restored from SQLite.
func TestCompactionContentRoundTrip(t *testing.T) {
	t.Parallel()

	part := CompactionContent{
		SummaryID:           "summary-1",
		Level:               1,
		TokenCount:          4200,
		TokensBefore:        180000,
		ModelProvider:       "fireworks",
		ModelID:             "accounts/fireworks/models/deepseek-v4-flash-0731",
		CompactedMessages:   120,
		SeqStart:            1,
		SeqEnd:              120,
		FirstRetainedSeq:    121,
		ExtractsKeptBlocks:  34,
		ExtractsTotalBlocks: 52,
		OlderLaneCompressed: true,
		WorkingSetFiles:     3,
	}
	part.Checkpoint.Goals = 2
	part.Checkpoint.Constraints = 2
	part.Checkpoint.Decisions = 4
	part.Checkpoint.DeadEnds = 1
	part.Checkpoint.Questions = 1
	part.Checkpoint.Done = 5
	part.Checkpoint.InProgress = 1
	part.Checkpoint.Blocked = 0
	part.Checkpoint.NextActions = 2
	part.Ledger.Instructions = 3
	part.Ledger.Errors = 1
	part.Ledger.Files = 9
	part.Ledger.Commands = 7

	m := Message{
		Role: Assistant,
		Parts: []ContentPart{
			TextContent{Text: "compacted summary"},
			part,
			Finish{Reason: FinishReasonEndTurn},
		},
	}
	encoded, err := marshalParts(m.Parts)
	require.NoError(t, err)

	decoded, err := unmarshalParts(encoded)
	require.NoError(t, err)
	require.Len(t, decoded, 3)

	got, ok := decoded[1].(CompactionContent)
	require.True(t, ok, "part must decode back as CompactionContent")
	require.Equal(t, part, got)
}

// TestCompactionPartLookup verifies CompactionPart finds the part through the
// mixed parts slice.
func TestCompactionPartLookup(t *testing.T) {
	t.Parallel()

	m := Message{Parts: []ContentPart{TextContent{Text: "x"}, CompactionContent{SummaryID: "s1"}}}
	got, ok := m.CompactionPart()
	require.True(t, ok)
	require.Equal(t, "s1", got.SummaryID)

	empty := Message{Parts: []ContentPart{TextContent{Text: "x"}}}
	_, ok = empty.CompactionPart()
	require.False(t, ok)
}

// TestCompactionContentUnknownFieldsAreIgnored keeps the codec forward
// compatible: future fields on the part must not break unmarshal.
func TestCompactionContentUnknownFieldsAreIgnored(t *testing.T) {
	t.Parallel()

	decoded, err := unmarshalParts([]byte(`[{"type":"compaction","data":{"summary_id":"s","future_field":42}}]`))
	require.NoError(t, err)
	require.Len(t, decoded, 1)
	part, ok := decoded[0].(CompactionContent)
	require.True(t, ok)
	require.Equal(t, "s", part.SummaryID)
}
