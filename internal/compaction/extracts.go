package compaction

import (
	"fmt"
	"strings"
)

// ExtractsLaneRequest is the input to the extractive lane: a goal-conditioned
// query, golden spans, and per-block budgeting.
type ExtractsLaneRequest struct {
	Query       string
	Span        SpanModel
	Budget      BlockRenderBudget
	KeepContext bool
	// KeepContextMaxCharsPerBlock caps force-kept characters per block.
	KeepContextMaxCharsPerBlock int
	// KeepContextMaxTotalChars caps total force-kept characters so golden
	// spans cannot starve the rest.
	KeepContextMaxTotalChars int
	// TotalCharBudget is the hard cap on the total extractive output in
	// characters, derived from plan.Extracts.TargetTokens. When > 0 the lane
	// keeps golden spans first, then allocates the remaining budget over
	// non-golden blocks (recency-weighted head/tail), dropping blocks below a
	// floor. When 0 the legacy per-block/per-golden caps apply.
	TotalCharBudget int
}

// ExtractsBlockMeta records metadata for one block sent to the compressor.
type ExtractsBlockMeta struct {
	SourceIndex int
	Kind        SpanBlockKind
	Turn        int
	Seq         int
	KeepContext bool
	Chars       int
}

// ExtractsLaneResult is the output: re-labeled, line-anchored byte-exact
// extracts.
type ExtractsLaneResult struct {
	Text        string
	Blocks      []ExtractsBlockMeta
	Query       string
	InputChars  int
	OutputChars int
}

const (
	keepOpen  = "<keepContext>"
	keepClose = "</keepContext>"
)

// BuildExtractsQuery builds a deterministic focus query from user-authored
// text only (never the generated checkpoint). Mirrors ShiftUp invariant 4.
func BuildExtractsQuery(retainedUserMessages, spanUserMessages []string, customInstructions string) string {
	clean := func(s string, max int) string {
		s = strings.Join(strings.Fields(s), " ")
		if len(s) > max {
			s = s[:max]
		}
		return s
	}
	var parts []string
	var current string
	for i := len(retainedUserMessages) - 1; i >= 0; i-- {
		if strings.TrimSpace(retainedUserMessages[i]) != "" {
			current = retainedUserMessages[i]
			break
		}
	}
	if current != "" {
		parts = append(parts, "Current task: "+clean(current, 400))
	}
	var first string
	useFirstAsTask := false
	for _, m := range spanUserMessages {
		if strings.TrimSpace(m) != "" {
			first = m
			break
		}
	}
	if first != "" && current == "" {
		parts = append(parts, "Task: "+clean(first, 300))
		useFirstAsTask = true
	}
	start := 0
	if useFirstAsTask {
		start = 1
	}
	var recent []string
	for _, m := range spanUserMessages[start:] {
		if strings.TrimSpace(m) == "" {
			continue
		}
		recent = append(recent, clean(m, 160))
	}
	if len(recent) > 3 {
		recent = recent[len(recent)-3:]
	}
	if len(recent) > 0 {
		parts = append(parts, "Recent requests: "+strings.Join(recent, " | "))
	}
	if strings.TrimSpace(customInstructions) != "" {
		parts = append(parts, "Operator focus: "+clean(customInstructions, 300))
	}
	parts = append(parts, "Keep: user instructions and constraints, decisions and their reasons, file paths and identifiers, commands run, errors and how they were fixed, test results, unfinished work.")
	q := strings.Join(parts, ". ")
	if len(q) > 1500 {
		q = q[:1500]
	}
	return q
}

// BuildExtractsLaneRequest builds the per-block rendering and golden-span
// marking for the extractive lane.
func BuildExtractsLaneRequest(span SpanModel, query string, budget BlockRenderBudget, keepContext bool) ExtractsLaneRequest {
	return ExtractsLaneRequest{
		Query:                       query,
		Span:                        span,
		Budget:                      budget,
		KeepContext:                 keepContext,
		KeepContextMaxCharsPerBlock: 2000,
		KeepContextMaxTotalChars:    24000,
	}
}

// RunExtractsLane produces byte-exact, line-anchored extracts with golden spans
// force-kept and the rest head/tail-truncated. This is the local extractive
// compressor (no external Morph dependency): it keeps the byte-exact,
// line-anchored, golden-span design from ShiftUp while operating in-process.
func RunExtractsLane(req ExtractsLaneRequest) ExtractsLaneResult {
	perBlockCap := req.KeepContextMaxCharsPerBlock
	if perBlockCap <= 0 {
		perBlockCap = 2000
	}
	totalCap := req.KeepContextMaxTotalChars
	if totalCap <= 0 {
		totalCap = 24000
	}
	bodies := make([]string, len(req.Span.Blocks))
	for i, b := range req.Span.Blocks {
		bodies[i] = RenderBlockBody(b, req.Budget)
	}

	// Golden spans by priority: user messages, then each turn's final
	// assistant statement (newest first), then error tails (newest first).
	golden := map[int]string{} // "head" | "tail"
	keptTotal := 0
	// When a total budget is set, golden spans count against it too so they
	// cannot starve the rest of the lane.
	goldenCap := totalCap
	if req.TotalCharBudget > 0 && req.TotalCharBudget < goldenCap {
		goldenCap = req.TotalCharBudget
	}
	if req.KeepContext {
		consider := func(idx int, mode string) {
			body := bodies[idx]
			kept := len(body)
			if kept > perBlockCap {
				kept = perBlockCap
			}
			if kept <= 0 || keptTotal+kept > goldenCap {
				return
			}
			golden[idx] = mode
			keptTotal += kept
		}
		for _, b := range req.Span.Blocks {
			if b.Kind == BlockUser && b.UserKind == UserKindUser {
				consider(b.Index, "head")
			}
		}
		for i := len(req.Span.Blocks) - 1; i >= 0; i-- {
			b := req.Span.Blocks[i]
			if b.Kind == BlockAssistantText && b.IsTurnFinalAssistantText {
				consider(b.Index, "head")
			}
		}
		for i := len(req.Span.Blocks) - 1; i >= 0; i-- {
			b := req.Span.Blocks[i]
			if b.Kind == BlockToolResult && b.IsError {
				consider(b.Index, "tail")
			}
		}
	}

	var parts []string
	var metas []ExtractsBlockMeta
	inputChars := 0
	outputChars := 0

	// When a total budget is set, golden spans are kept first and the
	// remaining character budget is allocated over non-golden blocks with
	// recency weighting (a head fraction from the oldest, a tail fraction
	// from the newest), dropping blocks below a floor. Without a total
	// budget the legacy behavior (emit every block, per-block caps on
	// golden spans) is preserved.
	totalBudget := req.TotalCharBudget
	nonGoldenFloor := 200

	// First pass: compute golden span cost and collect non-golden block
	// indices + sizes so we can allocate the remainder.
	type blockInfo struct {
		idx  int
		size int
	}
	goldenCost := 0
	var nonGolden []blockInfo
	for _, b := range req.Span.Blocks {
		body := bodies[b.Index]
		if strings.TrimSpace(body) == "" {
			continue
		}
		inputChars += len(body)
		if golden[b.Index] != "" || (b.Kind == BlockAssistantToolCalls && req.KeepContext) {
			cost := len(body)
			if cost > perBlockCap {
				cost = perBlockCap
			}
			goldenCost += cost
		} else {
			nonGolden = append(nonGolden, blockInfo{idx: b.Index, size: len(body)})
		}
	}

	// Decide which non-golden blocks to keep and how to truncate them.
	keepNonGolden := map[int]string{} // idx -> "head" | "tail" | "full"
	if totalBudget > 0 {
		remaining := totalBudget - goldenCost
		if remaining < 0 {
			remaining = 0
		}
		// Recency weighting: 60% of the remainder to the tail (newest),
		// 40% to the head (oldest).
		tailBudget := int(float64(remaining) * 0.6)
		headBudget := remaining - tailBudget

		// Tail (newest first).
		for i := len(nonGolden) - 1; i >= 0 && tailBudget > nonGoldenFloor; i-- {
			nb := nonGolden[i]
			if nb.size <= tailBudget {
				keepNonGolden[nb.idx] = "tail"
				tailBudget -= nb.size
			} else if tailBudget > nonGoldenFloor {
				keepNonGolden[nb.idx] = "tail"
				tailBudget = nonGoldenFloor
			}
		}
		// Head (oldest first).
		for i := 0; i < len(nonGolden) && headBudget > nonGoldenFloor; i++ {
			nb := nonGolden[i]
			if _, already := keepNonGolden[nb.idx]; already {
				continue
			}
			if nb.size <= headBudget {
				keepNonGolden[nb.idx] = "head"
				headBudget -= nb.size
			} else if headBudget > nonGoldenFloor {
				keepNonGolden[nb.idx] = "head"
				headBudget = nonGoldenFloor
			}
		}
	}

	for _, b := range req.Span.Blocks {
		body := bodies[b.Index]
		if strings.TrimSpace(body) == "" {
			continue
		}
		mode := golden[b.Index]
		content := body
		kept := false
		if b.Kind == BlockAssistantToolCalls && req.KeepContext {
			content = wrapToolCallHeaders(body)
			kept = true
		} else if mode == "head" {
			content = wrapHead(body, perBlockCap)
			kept = true
		} else if mode == "tail" {
			content = wrapTail(body, perBlockCap)
			kept = true
		} else if ngMode, ok := keepNonGolden[b.Index]; ok && totalBudget > 0 {
			// Budgeted non-golden block: truncate to fit.
			if ngMode == "head" {
				content = wrapHead(body, perBlockCap)
			} else {
				content = wrapTail(body, perBlockCap)
			}
			kept = true
		} else if totalBudget > 0 {
			// Over budget and not selected: drop this block entirely.
			continue
		}
		parts = append(parts, FormatBlockHeader(b, true)+":\n"+content)
		metas = append(metas, ExtractsBlockMeta{
			SourceIndex: b.Index,
			Kind:        b.Kind,
			Turn:        b.TurnIndex + 1,
			Seq:         b.Seq,
			KeepContext: kept,
			Chars:       len(body),
		})
		outputChars += len(content)
	}
	return ExtractsLaneResult{
		Text:        strings.Join(parts, "\n\n"),
		Blocks:      metas,
		Query:       req.Query,
		InputChars:  inputChars,
		OutputChars: outputChars,
	}
}

func wrapHead(text string, maxChars int) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	if len(text) <= maxChars {
		return keepOpen + "\n" + text + "\n" + keepClose
	}
	cut := text[:maxChars]
	if br := strings.LastIndex(cut, "\n"); br > maxChars/2 {
		cut = cut[:br]
	}
	return keepOpen + "\n" + cut + "\n" + keepClose
}

func wrapTail(text string, maxChars int) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	if len(text) <= maxChars {
		return keepOpen + "\n" + text + "\n" + keepClose
	}
	tail := text[len(text)-maxChars:]
	if br := strings.Index(tail, "\n"); br >= 0 && br < maxChars/2 {
		tail = tail[br+1:]
	}
	return keepOpen + "\n" + tail + "\n" + keepClose
}

func wrapToolCallHeaders(text string) string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "- ") && strings.Contains(line, "(") {
			out = append(out, keepOpen, line, keepClose)
		} else {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// RenderOlderLane re-compresses the previous compaction's extracts at a decayed
// ratio so older history decays gracefully instead of vanishing.
func RenderOlderLane(previousExtracts string, maxInputChars int) string {
	cut, _, _ := TruncateHeadTail(previousExtracts, maxInputChars, 0.3, func(omitted int) string {
		return fmt.Sprintf("[… %s characters of older extracts omitted]", formatCount(omitted))
	})
	return cut
}
