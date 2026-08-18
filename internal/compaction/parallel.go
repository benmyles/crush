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
// The merge is a simple concatenation with a light combine heading; the
// caller (the engine) feeds the merged result into the checkpoint lane as
// the "history" text, so the model still produces the final structured
// checkpoint from the condensed material.
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

	// Split blocks into BlockCount contiguous chunks by block count, keeping
	// turn boundaries intact where possible.
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

	var summaries []string
	var errs []error
	for _, r := range results {
		if r.err != nil {
			errs = append(errs, r.err)
			continue
		}
		if strings.TrimSpace(r.summary) != "" {
			summaries = append(summaries, r.summary)
		}
	}
	merged := strings.Join(summaries, "\n\n---\n\n")
	return ParallelBlockResult{
		Summary:    merged,
		BlockCount: len(summaries),
		Errors:     errs,
	}
}

// splitBlocksByTurns splits the span's blocks into roughly n contiguous
// chunks, breaking on turn boundaries so each chunk starts at a turn start.
func splitBlocksByTurns(span SpanModel, n int) [][]SpanBlock {
	if n <= 0 {
		n = 1
	}
	if len(span.Blocks) == 0 {
		return nil
	}
	// Use turn boundaries from span.Turns to chunk.
	turnCount := len(span.Turns)
	if turnCount == 0 {
		return [][]SpanBlock{span.Blocks}
	}
	chunkSize := (turnCount + n - 1) / n
	if chunkSize < 1 {
		chunkSize = 1
	}
	var chunks [][]SpanBlock
	for start := 0; start < turnCount; start += chunkSize {
		end := start + chunkSize
		if end > turnCount {
			end = turnCount
		}
		firstTurn := span.Turns[start]
		lastTurn := span.Turns[end-1]
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
