package compaction

import (
	"context"
	"strings"
	"sync"
)

// ParallelBlockInput is the input to parallel block compaction.
type ParallelBlockInput struct {
	// Span is the full span to compact.
	Span SpanModel
	// BlockCount is the number of parallel blocks to split into.
	BlockCount int
	// Budget is the render budget for each block.
	Budget BlockRenderBudget
	// Summarize is the per-block summary function. It receives the rendered
	// block text and returns a summary. Implementations wrap the model
	// completion + escalation guard.
	Summarize func(ctx context.Context, blockText string) (string, error)
}

// ParallelBlockResult is the output of parallel block compaction.
type ParallelBlockResult struct {
	// Summary is the merged result of all per-block summaries.
	Summary string
	// BlockCount is the number of blocks actually summarized.
	BlockCount int
	// Errors records any per-block errors (non-fatal; the merge skips failed
	// blocks).
	Errors []error
}

// RunParallelBlockCompaction splits a large span into N independent blocks,
// summarizes each in parallel, and merges the results with a single
// combine pass. This gives predictable summary volume (block count is the
// knob) and higher throughput for very large spans, addressing the finding
// that single-pass summarization attends poorly over 96k+ tokens.
//
// Fail-closed: any block error is fatal (the merge does not silently skip
// failed blocks). Turn-prefix blocks are excluded from parallel summary;
// they are passed separately to the checkpoint lane.
func RunParallelBlockCompaction(ctx context.Context, in ParallelBlockInput) ParallelBlockResult {
	if in.BlockCount <= 1 || len(in.Span.Blocks) == 0 {
		// No parallelism needed.
		history, _, _ := RenderSpanForCheckpoint(in.Span, in.Budget)
		summary, err := in.Summarize(ctx, history)
		if err != nil {
			return ParallelBlockResult{Errors: []error{err}}
		}
		return ParallelBlockResult{Summary: summary, BlockCount: 1}
	}

	// Split history-segment blocks into BlockCount contiguous chunks by turn
	// boundaries. Turn-prefix blocks are excluded (they are passed separately).
	chunks := splitBlocksByTurns(in.Span, in.BlockCount)

	type blockResult struct {
		index   int
		summary string
		err     error
	}
	results := make([]blockResult, len(chunks))
	var wg sync.WaitGroup
	for i, chunk := range chunks {
		wg.Add(1)
		go func(idx int, blocks []SpanBlock) {
			defer wg.Done()
			var parts []string
			for _, b := range blocks {
				parts = append(parts, FormatBlockHeader(b, true)+":\n"+RenderBlockBody(b, in.Budget))
			}
			text := strings.Join(parts, "\n\n")
			summary, err := in.Summarize(ctx, text)
			results[idx] = blockResult{index: idx, summary: summary, err: err}
		}(i, chunk)
	}
	wg.Wait()

	// Fail closed: any block error is fatal.
	var errs []error
	for _, r := range results {
		if r.err != nil {
			errs = append(errs, r.err)
		}
	}
	if len(errs) > 0 {
		return ParallelBlockResult{Errors: errs}
	}

	var summaries []string
	for _, r := range results {
		if strings.TrimSpace(r.summary) != "" {
			summaries = append(summaries, r.summary)
		}
	}
	merged := strings.Join(summaries, "\n\n---\n\n")
	return ParallelBlockResult{
		Summary:    merged,
		BlockCount: len(summaries),
	}
}

// splitBlocksByTurns splits the span's history-segment blocks into roughly n
// contiguous chunks, breaking on turn boundaries so each chunk starts at a
// turn start. Turn-prefix segment blocks are excluded (they are passed
// separately to the checkpoint lane).
func splitBlocksByTurns(span SpanModel, n int) [][]SpanBlock {
	if n <= 0 {
		n = 1
	}
	// Collect only history-segment turns; turn-prefix turns are excluded.
	var historyTurns []SpanTurn
	for _, t := range span.Turns {
		if t.Segment == SegmentHistory {
			historyTurns = append(historyTurns, t)
		}
	}
	if len(historyTurns) == 0 {
		// Fallback: use all history-segment blocks directly.
		var histBlocks []SpanBlock
		for _, b := range span.Blocks {
			if b.Segment == SegmentHistory {
				histBlocks = append(histBlocks, b)
			}
		}
		if len(histBlocks) == 0 {
			return nil
		}
		return [][]SpanBlock{histBlocks}
	}
	chunkSize := (len(historyTurns) + n - 1) / n
	if chunkSize < 1 {
		chunkSize = 1
	}
	var chunks [][]SpanBlock
	for start := 0; start < len(historyTurns); start += chunkSize {
		end := start + chunkSize
		if end > len(historyTurns) {
			end = len(historyTurns)
		}
		firstTurn := historyTurns[start]
		lastTurn := historyTurns[end-1]
		startBlock := firstTurn.FirstBlock
		endBlock := lastTurn.LastBlock
		if startBlock < 0 {
			startBlock = 0
		}
		if endBlock >= len(span.Blocks) {
			endBlock = len(span.Blocks) - 1
		}
		chunks = append(chunks, span.Blocks[startBlock:endBlock+1])
	}
	return chunks
}
