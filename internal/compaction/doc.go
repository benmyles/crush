// Package compaction implements Crush's context compaction engine.
//
// The engine combines the best of Lossless Context Management (LCM) and
// ShiftUp's pi-shiftup-compaction: a dual-state memory where raw messages are
// never mutated (the immutable store) and compaction produces derived summary
// views over them (the active context). Each compaction saves a structured
// self-addressed checkpoint, a deterministic session ledger and transcript map,
// labeled byte-exact extracts, a working-set snapshot, and an exact recovery
// index into the canonical message store.
//
// Raw messages remain the sole source of truth; summaries are materialized
// views reachable via the recall_* tools. The engine is fail-closed: a failed
// mandatory lane cancels the compaction and saves nothing.
package compaction

// CharsPerToken is the rough character-to-token ratio used for budget sizing.
// It matches the estimate used by the ShiftUp budget governor.
const CharsPerToken = 4

// EstimateTokens returns a rough token count for a character length.
func EstimateTokens(chars int) int {
	if chars <= 0 {
		return 0
	}
	return (chars + CharsPerToken - 1) / CharsPerToken
}

// boolVal dereferences a *bool, returning false when nil. Used to read the
// *bool config fields (Enabled, Ledger, TranscriptMap) that are nil when unset.
func boolVal(p *bool) bool {
	return p != nil && *p
}
