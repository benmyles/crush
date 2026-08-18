package compaction

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParseCheckpointOverview verifies the deterministic digest the TUI
// renders as the "Compaction complete" tree.
func TestParseCheckpointOverview(t *testing.T) {
	t.Parallel()

	text := `# Session Checkpoint

## Goal & User Intent

- Rewrite the indexer for batching.

## Constraints

- [C1] Must stay backward compatible with the v1 API.
- [C2] Keyboard-only navigation.

## Key Decisions

- [D1] Use the streaming pipeline.
- [D2] Keep the SQL layer untouched.

## Dead Ends

- [X1] The protobuf route was rejected.

## Open Questions

- [Q1] Whether to keep the legacy flag.

## Progress

### Done

- Rebased on main.
- Fixed the failing config test.

### In Progress

- UI pulse indicator.

### Blocked

- Waiting on credentials.

## Next Action

- Ship the tree rendering.
- Write release notes.
`

	ov := ParseCheckpointOverview(text)

	require.Equal(t, 1, ov.Goals, "one list line under Goal & User Intent")
	require.Equal(t, 2, ov.Constraints)
	require.Equal(t, 2, ov.Decisions)
	require.Equal(t, 1, ov.DeadEnds)
	require.Equal(t, 1, ov.Questions)
	require.Equal(t, 2, ov.Done)
	require.Equal(t, 1, ov.InProgress)
	require.Equal(t, 1, ov.Blocked)
	require.Equal(t, 2, ov.NextActions)
}

// TestParseCheckpointOverviewEmpty verifies zero-value output for an empty
// or unrelated checkpoint.
func TestParseCheckpointOverviewEmpty(t *testing.T) {
	t.Parallel()

	ov := ParseCheckpointOverview("no checkpoint here")
	require.Equal(t, CheckpointOverview{}, ov)
}

// TestParseCheckpointOverviewGoalFamilyIDs counts G-family stable IDs when a
// checkpoint numbers its goals.
func TestParseCheckpointOverviewGoalFamilyIDs(t *testing.T) {
	t.Parallel()

	text := `## Goal & User Intent

- [G1] Compact the session.
- [G2] Keep history recoverable.

## Progress

### Done

- One item.
`
	ov := ParseCheckpointOverview(text)
	require.Equal(t, 2, ov.Goals)
	require.Equal(t, 0, ov.Constraints)
	require.Equal(t, 1, ov.Done)
}
