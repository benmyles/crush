package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// TestCompactionMessageItemRendersTree asserts that a summary message with a
// CompactionContent part renders the always-expanded overview tree (Option A)
// instead of the plain assistant render, with all lane branches and counts.
func TestCompactionMessageItemRendersTree(t *testing.T) {
	t.Parallel()

	sty := styles.CharmtonePantera()
	part := message.CompactionContent{
		SummaryID:           "summary-42",
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
	part.Checkpoint.NextActions = 2
	part.Ledger.Instructions = 3
	part.Ledger.Errors = 1
	part.Ledger.Files = 9
	part.Ledger.Commands = 7

	msg := &message.Message{
		ID:               "msg-1",
		Role:             message.Assistant,
		IsSummaryMessage: true,
		Parts: []message.ContentPart{
			message.TextContent{Text: "summary text"},
			part,
			message.Finish{Reason: message.FinishReasonEndTurn},
		},
	}

	items := ExtractMessageItems(&sty, msg, nil, "")
	require.Len(t, items, 1)
	item, ok := items[0].(*CompactionMessageItem)
	require.True(t, ok, "summary messages with a digest must render as the compaction tree")

	out := item.Render(90)
	for _, want := range []string{
		"Compaction complete",
		"bullet points",
		"Checkpoint",
		"2 items",
		"2 · Decisions 4 · Dead Ends 1 · Questions 1",
		"5 done · 1 in progress · 0 blocked",
		"Ledger",
		"3 instructions · 1 errors · 7 commands · 9 files",
		"34/52 golden spans kept",
		"older history re-compressed",
		"3 files snapped",
		"seq 1–120 · 120 messages compacted",
		"recall_grep / recall_expand ready · summary summary-42",
	} {
		require.Contains(t, out, want)
	}

	require.Equal(t, "compaction-summary-42", item.ID())
	require.True(t, item.Finished(), "the overview is static and can be frozen by the list cache")
}

// TestLegacySummaryMessageStillRendersAssistant asserts that a summary
// message WITHOUT a CompactionContent part (the legacy summarize path) keeps
// the plain assistant render.
func TestLegacySummaryMessageStillRendersAssistant(t *testing.T) {
	t.Parallel()

	sty := styles.CharmtonePantera()
	msg := &message.Message{
		ID:               "legacy-1",
		Role:             message.Assistant,
		IsSummaryMessage: true,
		Parts: []message.ContentPart{
			message.TextContent{Text: "legacy summary body"},
			message.Finish{Reason: message.FinishReasonEndTurn},
		},
	}

	items := ExtractMessageItems(&sty, msg, nil, "")
	require.Len(t, items, 1)
	_, ok := items[0].(*CompactionMessageItem)
	require.False(t, ok, "legacy summaries must not render as the compaction tree")
}

// TestCompactionMessageItemTruncatesHeader asserts the header line never
// exceeds the render width.
func TestCompactionMessageItemTruncatesHeader(t *testing.T) {
	t.Parallel()

	sty := styles.CharmtonePantera()
	msg := &message.Message{
		ID:               "msg-2",
		Role:             message.Assistant,
		IsSummaryMessage: true,
		Parts: []message.ContentPart{
			message.TextContent{Text: "x"},
			message.CompactionContent{
				SummaryID:         "s",
				ModelID:           strings.Repeat("m", 200),
				CompactedMessages: 0,
			},
		},
	}
	items := ExtractMessageItems(&sty, msg, nil, "")
	require.Len(t, items, 1)
	lines := strings.Split(items[0].RawRender(40), "\n")
	require.NotEmpty(t, lines)
	// The "▾ " prefix plus the truncated header must stay inside the width.
	require.LessOrEqual(t, ansi.StringWidth(lines[0]), 42, "header must be truncated to the render width")
}
