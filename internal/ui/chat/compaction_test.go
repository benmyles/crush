package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// testCompactionPart builds the CompactionContent used across the tests.
func testCompactionPart() message.CompactionContent {
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
	return part
}

// TestCompactionMessageItemCollapsedByDefault asserts that a summary message
// with a CompactionContent part renders as a one-line collapsed header until
// the user expands it.
func TestCompactionMessageItemCollapsedByDefault(t *testing.T) {
	t.Parallel()

	sty := styles.CharmtonePantera()
	msg := &message.Message{
		ID:               "msg-1",
		Role:             message.Assistant,
		IsSummaryMessage: true,
		Parts: []message.ContentPart{
			message.TextContent{Text: "summary text"},
			testCompactionPart(),
			message.Finish{Reason: message.FinishReasonEndTurn},
		},
	}

	items := ExtractMessageItems(&sty, msg, nil, "")
	require.Len(t, items, 1)
	item, ok := items[0].(*CompactionMessageItem)
	require.True(t, ok, "summary messages with a digest must render as the compaction tree")

	// Collapsed by default: the one-line header only, no tree branches.
	out := item.Render(90)
	for _, want := range []string{
		"Compaction complete",
		"click to expand",
	} {
		require.Contains(t, out, want)
	}
	require.NotContains(t, out, "Checkpoint")
	require.NotContains(t, out, "Ledger")

	require.Equal(t, "compaction-summary-42", item.ID())
	require.True(t, item.Finished(), "the overview is static and can be frozen by the list cache")
}

// TestCompactionMessageItemExpandsOnToggle asserts that toggling the item
// expands the full overview tree and toggling again collapses it back.
func TestCompactionMessageItemExpandsOnToggle(t *testing.T) {
	t.Parallel()

	sty := styles.CharmtonePantera()
	msg := &message.Message{
		ID:               "msg-1",
		Role:             message.Assistant,
		IsSummaryMessage: true,
		Parts: []message.ContentPart{
			message.TextContent{Text: "summary text"},
			testCompactionPart(),
			message.Finish{Reason: message.FinishReasonEndTurn},
		},
	}

	items := ExtractMessageItems(&sty, msg, nil, "")
	item := items[0].(*CompactionMessageItem)

	require.True(t, item.ToggleExpanded(), "first toggle expands")
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

	require.False(t, item.ToggleExpanded(), "second toggle collapses")
	require.NotContains(t, item.Render(90), "Checkpoint")
}

// TestCompactionMessageItemHandleMouseClick asserts left clicks anywhere on
// the item claim it so the generic Expandable path can toggle it.
func TestCompactionMessageItemHandleMouseClick(t *testing.T) {
	t.Parallel()

	sty := styles.CharmtonePantera()
	msg := &message.Message{
		ID:               "msg-3",
		Role:             message.Assistant,
		IsSummaryMessage: true,
		Parts: []message.ContentPart{
			message.CompactionContent{SummaryID: "s"},
		},
	}
	item := ExtractMessageItems(&sty, msg, nil, "")[0].(*CompactionMessageItem)

	require.True(t, item.HandleMouseClick(ansi.MouseButton1, 0, 0))
	require.False(t, item.HandleMouseClick(ansi.MouseButton3, 0, 0), "only left clicks toggle")
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

// TestCompactionMessageItemTruncatesHeader asserts the collapsed line never
// exceeds the render width, even for absurdly long model IDs.
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
	require.LessOrEqual(t, ansi.StringWidth(lines[0]), 40, "collapsed line must be truncated to the render width")
}
