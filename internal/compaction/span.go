package compaction

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/crush/internal/message"
)

// SpanSegment labels whether a block belongs to finished history or to the
// prefix of a split turn that continues in the retained context.
type SpanSegment string

const (
	SegmentHistory    SpanSegment = "history"
	SegmentTurnPrefix SpanSegment = "turnPrefix"
)

// SpanBlockKind enumerates the labeled block kinds a span is rendered into.
type SpanBlockKind string

const (
	BlockUser               SpanBlockKind = "user"
	BlockAssistantThinking  SpanBlockKind = "assistantThinking"
	BlockAssistantText      SpanBlockKind = "assistantText"
	BlockAssistantToolCalls SpanBlockKind = "assistantToolCalls"
	BlockToolResult         SpanBlockKind = "toolResult"
)

// UserLikeKind distinguishes a person-authored user message from one the host
// or an extension injected.
type UserLikeKind string

const (
	UserKindUser              UserLikeKind = "user"
	UserKindShell             UserLikeKind = "shell"
	UserKindCompactionSummary UserLikeKind = "compactionSummary"
)

// SpanToolCall is a single tool call extracted from an assistant message.
type SpanToolCall struct {
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

// SpanBlock is one labeled, line-anchored block of the span. Both the
// checkpoint and extractive lanes consume the same SpanBlock stream so they
// share a faithful record of the compacted history.
type SpanBlock struct {
	Index     int           `json:"index"`
	Kind      SpanBlockKind `json:"kind"`
	Segment   SpanSegment   `json:"segment"`
	TurnIndex int           `json:"turnIndex"`
	UserKind  UserLikeKind  `json:"userKind,omitempty"`
	MorphRole string        `json:"morphRole"`
	Label     string        `json:"label"`
	MessageID string        `json:"messageId,omitempty"`
	// Seq is the 1-based ordinal of the source message within the span,
	// used as a stable physical anchor (like Pi's JSONL line numbers).
	Seq        int            `json:"seq,omitempty"`
	CreatedAt  int64          `json:"createdAt,omitempty"`
	Text       string         `json:"text"`
	Chars      int            `json:"chars"`
	ToolName   string         `json:"toolName,omitempty"`
	ToolCallID string         `json:"toolCallId,omitempty"`
	IsError    bool           `json:"isError,omitempty"`
	ToolCalls  []SpanToolCall `json:"toolCalls,omitempty"`
	// SupersededBySeq is the seq of a later read that supersedes this one.
	SupersededBySeq int `json:"supersededBySeq,omitempty"`
	// IsTurnFinalAssistantText is true for the last assistant text block of
	// its turn.
	IsTurnFinalAssistantText bool `json:"isTurnFinalAssistantText,omitempty"`
}

// SpanTurn groups the blocks of one user-started turn.
type SpanTurn struct {
	Index         int          `json:"index"`
	Segment       SpanSegment  `json:"segment"`
	FirstBlock    int          `json:"firstBlock"`
	LastBlock     int          `json:"lastBlock"`
	UserKind      UserLikeKind `json:"userKind,omitempty"`
	UserText      string       `json:"userText,omitempty"`
	UserSeq       int          `json:"userSeq,omitempty"`
	CreatedAt     int64        `json:"createdAt,omitempty"`
	FirstSeq      int          `json:"firstSeq,omitempty"`
	LastSeq       int          `json:"lastSeq,omitempty"`
	ToolCallCount int          `json:"toolCallCount"`
	ErrorCount    int          `json:"errorCount"`
	Files         []string     `json:"files"`
}

// SpanStats summarizes a span.
type SpanStats struct {
	Messages          int `json:"messages"`
	UserMessages      int `json:"userMessages"`
	AssistantMessages int `json:"assistantMessages"`
	ToolResults       int `json:"toolResults"`
	ToolCalls         int `json:"toolCalls"`
	Errors            int `json:"errors"`
	Chars             int `json:"chars"`
}

// SpanModel is the labeled, line-anchored rendering of a compacted message
// span. It is the single source both summary lanes consume.
type SpanModel struct {
	Blocks []SpanBlock `json:"blocks"`
	Turns  []SpanTurn  `json:"turns"`
	Stats  SpanStats   `json:"stats"`
}

// pathArgumentKeys are the tool-argument keys that may carry a file path.
var pathArgumentKeys = []string{"path", "file_path", "filePath", "target_filepath", "target_file", "filename", "file"}

// TextOfContent extracts the text portions of a message's parts.
func TextOfContent(parts []message.ContentPart) string {
	var sb strings.Builder
	for _, part := range parts {
		switch p := part.(type) {
		case message.TextContent:
			sb.WriteString(p.Text)
		case message.ImageURLContent:
			sb.WriteString("[image omitted]")
		}
	}
	return sb.String()
}

// FormatTimestamp renders a unix-seconds timestamp as ISO-8601 UTC without
// fractional seconds, or "" if invalid.
func FormatTimestamp(ts int64) string {
	if ts <= 0 {
		return ""
	}
	return time.Unix(ts, 0).UTC().Format("2006-01-02T15:04:05Z")
}

// ToolCallPath extracts a file-path argument from a tool call, if present.
func ToolCallPath(call SpanToolCall) string {
	for _, key := range pathArgumentKeys {
		if v, ok := call.Args[key]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

// TruncateHeadTail keeps the head and tail of a long text. Errors, test
// failures, and diff summaries live at the end of tool output, so a head-only
// cut loses them.
func TruncateHeadTail(text string, maxChars int, headFraction float64, omittedNote func(omitted int) string) (string, bool, int) {
	if maxChars <= 0 {
		note := ""
		if omittedNote != nil {
			note = omittedNote(len(text))
		}
		return note, len(text) > 0, len(text)
	}
	if len(text) <= maxChars {
		return text, false, 0
	}
	if headFraction <= 0 {
		headFraction = 0.6
	}
	if headFraction > 1 {
		headFraction = 1
	}
	headChars := int(float64(maxChars) * headFraction)
	if headChars < 0 {
		headChars = 0
	}
	tailChars := maxChars - headChars
	if tailChars < 0 {
		tailChars = 0
	}
	head := text[:headChars]
	tail := ""
	if tailChars > 0 {
		tail = text[len(text)-tailChars:]
	}
	if headBreak := strings.LastIndex(head, "\n"); headBreak > headChars/2 {
		head = head[:headBreak]
	}
	if tailBreak := strings.Index(tail, "\n"); tailBreak >= 0 && tailBreak < tailChars/2 {
		tail = tail[tailBreak+1:]
	}
	omitted := len(text) - len(head) - len(tail)
	note := fmt.Sprintf("[… %s characters omitted]", formatCount(omitted))
	if omittedNote != nil {
		note = omittedNote(omitted)
	}
	out := head + "\n" + note
	if tail != "" {
		out += "\n" + tail
	}
	return out, true, omitted
}

func formatCount(n int) string {
	return fmt.Sprintf("%d", n)
}

// ShortCallID shortens an opaque tool-call id for display.
func ShortCallID(id string) string {
	if len(id) > 18 {
		return id[:16] + "…"
	}
	return id
}

// RenderToolCallArguments renders arguments one per line so line-level pruning
// can work inside them.
func RenderToolCallArguments(call SpanToolCall, maxCharsPerValue int, seq int) string {
	var lines []string
	for _, key := range orderedKeys(call.Args) {
		rawValue := call.Args[key]
		if rawValue == nil {
			continue
		}
		var value string
		switch v := rawValue.(type) {
		case string:
			value = v
		default:
			b, err := json.MarshalIndent(v, "", " ")
			if err != nil {
				value = fmt.Sprintf("%v", v)
			} else {
				value = string(b)
			}
		}
		note := func(omitted int) string {
			suffix := ""
			if seq > 0 {
				suffix = fmt.Sprintf(" · full arguments at transcript seq %d", seq)
			}
			return fmt.Sprintf("[… %s characters of %s omitted%s]", formatCount(omitted), key, suffix)
		}
		cut, _, _ := TruncateHeadTail(value, maxCharsPerValue, 0.6, note)
		if strings.Contains(cut, "\n") {
			lines = append(lines, "    "+key+": |")
			for _, valueLine := range strings.Split(cut, "\n") {
				lines = append(lines, "      "+valueLine)
			}
		} else {
			lines = append(lines, "    "+key+": "+cut)
		}
	}
	return strings.Join(lines, "\n")
}

func orderedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Simple stable sort by key for deterministic output.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	return keys
}

// SpanInput is the input to BuildSpanModel.
type SpanInput struct {
	History    []message.Message
	TurnPrefix []message.Message
}

// BuildSpanModel renders the compacted message span as labeled, line-anchored
// blocks. Seq numbers are 1-based ordinals within the full span (history then
// turnPrefix), stable like Pi's JSONL physical line numbers.
func BuildSpanModel(input SpanInput) SpanModel {
	var blocks []SpanBlock
	var turns []SpanTurn
	stats := SpanStats{}
	toolCallsByID := map[string]SpanToolCall{}
	readResultsByKey := map[string]int{}
	var currentTurn *SpanTurn
	seqCounter := 0

	startTurn := func(segment SpanSegment, createdAt int64) *SpanTurn {
		turn := SpanTurn{
			Index:      len(turns),
			Segment:    segment,
			FirstBlock: len(blocks),
			LastBlock:  len(blocks),
		}
		if createdAt > 0 {
			turn.CreatedAt = createdAt
		}
		turns = append(turns, turn)
		currentTurn = &turns[len(turns)-1]
		return currentTurn
	}

	pushBlock := func(b SpanBlock, turn *SpanTurn) SpanBlock {
		b.Index = len(blocks)
		b.TurnIndex = turn.Index
		b.Chars = len(b.Text)
		blocks = append(blocks, b)
		turn.LastBlock = b.Index
		if b.Seq != 0 {
			if turn.FirstSeq == 0 || b.Seq < turn.FirstSeq {
				turn.FirstSeq = b.Seq
			}
			if b.Seq > turn.LastSeq {
				turn.LastSeq = b.Seq
			}
		}
		stats.Chars += b.Chars
		return b
	}

	consume := func(msgs []message.Message, segment SpanSegment) {
		for _, msg := range msgs {
			stats.Messages++
			seqCounter++
			seq := seqCounter

			if msg.Role == message.User {
				turn := startTurn(segment, msg.CreatedAt)
				text, kind := userLikeText(msg)
				if kind == UserKindUser {
					stats.UserMessages++
				}
				turn.UserText = text
				turn.UserKind = kind
				turn.UserSeq = seq
				pushBlock(SpanBlock{
					Kind:      BlockUser,
					Segment:   segment,
					MorphRole: "user",
					Label:     "User",
					Text:      text,
					UserKind:  kind,
					MessageID: msg.ID,
					Seq:       seq,
					CreatedAt: msg.CreatedAt,
				}, turn)
				continue
			}

			turn := currentTurn
			if turn == nil {
				turn = startTurn(segment, msg.CreatedAt)
			}

			if msg.Role == message.Assistant {
				stats.AssistantMessages++
				var thinking, texts []string
				var calls []SpanToolCall
				for _, part := range msg.Parts {
					switch p := part.(type) {
					case message.ReasoningContent:
						if strings.TrimSpace(p.Thinking) != "" {
							thinking = append(thinking, p.Thinking)
						}
					case message.TextContent:
						if strings.TrimSpace(p.Text) != "" {
							texts = append(texts, p.Text)
						}
					case message.ToolCall:
						call := SpanToolCall{ID: p.ID, Name: p.Name}
						if strings.TrimSpace(p.Input) != "" {
							var parsed map[string]any
							if err := json.Unmarshal([]byte(p.Input), &parsed); err == nil {
								call.Args = parsed
							} else {
								call.Args = map[string]any{"_raw": p.Input}
							}
						}
						calls = append(calls, call)
						toolCallsByID[p.ID] = call
					}
				}
				if len(thinking) > 0 {
					pushBlock(SpanBlock{
						Kind:      BlockAssistantThinking,
						Segment:   segment,
						MorphRole: "assistant",
						Label:     "Assistant thinking",
						Text:      strings.Join(thinking, "\n"),
						MessageID: msg.ID,
						Seq:       seq,
						CreatedAt: msg.CreatedAt,
					}, turn)
				}
				if len(texts) > 0 {
					pushBlock(SpanBlock{
						Kind:      BlockAssistantText,
						Segment:   segment,
						MorphRole: "assistant",
						Label:     "Assistant",
						Text:      strings.Join(texts, "\n"),
						MessageID: msg.ID,
						Seq:       seq,
						CreatedAt: msg.CreatedAt,
					}, turn)
				}
				if len(calls) > 0 {
					stats.ToolCalls += len(calls)
					turn.ToolCallCount += len(calls)
					for _, call := range calls {
						path := ToolCallPath(call)
						if path != "" && !contains(turn.Files, path) {
							turn.Files = append(turn.Files, path)
						}
					}
					var callLines []string
					for _, call := range calls {
						callLines = append(callLines, fmt.Sprintf("- %s (%s)\n%s", call.Name, ShortCallID(call.ID), RenderToolCallArguments(call, 1<<30, seq)))
					}
					pushBlock(SpanBlock{
						Kind:      BlockAssistantToolCalls,
						Segment:   segment,
						MorphRole: "assistant",
						Label:     "Assistant tool calls",
						Text:      strings.Join(callLines, "\n"),
						ToolCalls: calls,
						MessageID: msg.ID,
						Seq:       seq,
						CreatedAt: msg.CreatedAt,
					}, turn)
				}
				continue
			}

			if msg.Role == message.Tool {
				stats.ToolResults++
				for _, result := range msg.ToolResults() {
					toolName := result.Name
					call := toolCallsByID[result.ToolCallID]
					if toolName == "" {
						toolName = call.Name
					}
					if toolName == "" {
						toolName = "tool"
					}
					block := pushBlock(SpanBlock{
						Kind:       BlockToolResult,
						Segment:    segment,
						MorphRole:  "assistant",
						Label:      "Tool result · " + toolName,
						Text:       result.Content,
						ToolName:   toolName,
						ToolCallID: result.ToolCallID,
						IsError:    result.IsError,
						MessageID:  msg.ID,
						Seq:        seq,
						CreatedAt:  msg.CreatedAt,
					}, turn)
					if result.IsError {
						stats.Errors++
						turn.ErrorCount++
					}
					if call.Name == "read" || call.Name == "view" {
						path := ToolCallPath(call)
						if path != "" {
							key := path + "::"
							if prev, ok := readResultsByKey[key]; ok && prev < len(blocks) {
								blocks[prev].SupersededBySeq = block.Seq
							}
							readResultsByKey[key] = block.Index
						}
					}
				}
				continue
			}
		}
	}

	consume(input.History, SegmentHistory)
	currentTurn = nil
	consume(input.TurnPrefix, SegmentTurnPrefix)

	// Mark the final assistant text block of each turn.
	for i := range turns {
		turn := &turns[i]
		for idx := turn.LastBlock; idx >= turn.FirstBlock; idx-- {
			if idx < 0 || idx >= len(blocks) {
				break
			}
			if blocks[idx].Kind == BlockAssistantText {
				blocks[idx].IsTurnFinalAssistantText = true
				break
			}
		}
	}

	return SpanModel{Blocks: blocks, Turns: turns, Stats: stats}
}

func userLikeText(msg message.Message) (string, UserLikeKind) {
	// Shell commands are part of user messages in bang mode.
	if shellCmds := msg.ShellCommands(); len(shellCmds) > 0 {
		var sb strings.Builder
		for i, sc := range shellCmds {
			if i > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString("$ " + sc.Command)
			if sc.Output != "" {
				sb.WriteString("\n" + sc.Output)
			}
			if sc.ExitCode != 0 {
				fmt.Fprintf(&sb, "\n(exit code %d)", sc.ExitCode)
			}
		}
		text := TextOfContent(msg.Parts)
		if text != "" {
			return text + "\n\n" + sb.String(), UserKindShell
		}
		return sb.String(), UserKindShell
	}
	text := TextOfContent(msg.Parts)
	if text == "" && msg.IsSummaryMessage {
		return text, UserKindCompactionSummary
	}
	return text, UserKindUser
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// BlockRenderBudget caps per-block body rendering for each lane.
type BlockRenderBudget struct {
	MaxCharsPerUser          int
	MaxCharsPerThinking      int
	MaxCharsPerAssistantText int
	MaxCharsPerToolResult    int
	MaxCharsPerToolArgument  int
	MaxCharsSupersededRead   int
}

// CheckpointRenderBudget is the tighter budget for the checkpoint lane.
var CheckpointRenderBudget = BlockRenderBudget{
	MaxCharsPerUser:          12000,
	MaxCharsPerThinking:      3000,
	MaxCharsPerAssistantText: 8000,
	MaxCharsPerToolResult:    6000,
	MaxCharsPerToolArgument:  2500,
	MaxCharsSupersededRead:   400,
}

// ExtractsRenderBudget is the looser budget for the extractive lane.
var ExtractsRenderBudget = BlockRenderBudget{
	MaxCharsPerUser:          40000,
	MaxCharsPerThinking:      12000,
	MaxCharsPerAssistantText: 40000,
	MaxCharsPerToolResult:    40000,
	MaxCharsPerToolArgument:  12000,
	MaxCharsSupersededRead:   600,
}

// ScaleRenderBudget scales a budget by a factor with per-field floors.
func ScaleRenderBudget(b BlockRenderBudget, factor float64) BlockRenderBudget {
	scale := func(v int, floor int) int {
		out := int(float64(v) * factor)
		if out < floor {
			out = floor
		}
		return out
	}
	return BlockRenderBudget{
		MaxCharsPerUser:          scale(b.MaxCharsPerUser, 1200),
		MaxCharsPerThinking:      scale(b.MaxCharsPerThinking, 300),
		MaxCharsPerAssistantText: scale(b.MaxCharsPerAssistantText, 800),
		MaxCharsPerToolResult:    scale(b.MaxCharsPerToolResult, 500),
		MaxCharsPerToolArgument:  scale(b.MaxCharsPerToolArgument, 400),
		MaxCharsSupersededRead:   scale(b.MaxCharsSupersededRead, 120),
	}
}

// RenderBlockBody renders one block's body text under a budget.
func RenderBlockBody(block SpanBlock, budget BlockRenderBudget) string {
	omittedNote := func(omitted int) string {
		suffix := ""
		if block.Seq != 0 {
			suffix = fmt.Sprintf(" · full content at transcript seq %d", block.Seq)
		}
		return fmt.Sprintf("[… %s characters omitted%s]", formatCount(omitted), suffix)
	}
	switch block.Kind {
	case BlockUser:
		out, _, _ := TruncateHeadTail(block.Text, budget.MaxCharsPerUser, 0.7, omittedNote)
		return out
	case BlockAssistantThinking:
		out, _, _ := TruncateHeadTail(block.Text, budget.MaxCharsPerThinking, 0.5, omittedNote)
		return out
	case BlockAssistantText:
		out, _, _ := TruncateHeadTail(block.Text, budget.MaxCharsPerAssistantText, 0.6, omittedNote)
		return out
	case BlockAssistantToolCalls:
		var lines []string
		for _, call := range block.ToolCalls {
			lines = append(lines, fmt.Sprintf("- %s (%s)\n%s", call.Name, ShortCallID(call.ID), RenderToolCallArguments(call, budget.MaxCharsPerToolArgument, block.Seq)))
		}
		return strings.Join(lines, "\n")
	case BlockToolResult:
		if block.SupersededBySeq != 0 {
			note := func(omitted int) string {
				suffix := ""
				if block.Seq != 0 {
					suffix = fmt.Sprintf(" · full content at transcript seq %d", block.Seq)
				}
				return fmt.Sprintf("[… %s characters omitted%s]", formatCount(omitted), suffix)
			}
			out, _, _ := TruncateHeadTail(block.Text, budget.MaxCharsSupersededRead, 1, note)
			return "[stale: superseded by a later read of the same file]\n" + out
		}
		headFrac := 0.55
		if block.IsError {
			headFrac = 0.35
		}
		out, _, _ := TruncateHeadTail(block.Text, budget.MaxCharsPerToolResult, headFrac, omittedNote)
		return out
	default:
		return block.Text
	}
}

// FormatBlockHeader renders the labeled header line for a block.
func FormatBlockHeader(block SpanBlock, includeTime bool) string {
	var parts []string
	parts = append(parts, block.Label)
	if block.Kind == BlockToolResult && block.ToolCallID != "" {
		parts = append(parts, ShortCallID(block.ToolCallID))
	}
	if block.Kind == BlockToolResult && block.IsError {
		parts = append(parts, "error")
	}
	parts = append(parts, fmt.Sprintf("turn %d", block.TurnIndex+1))
	if block.Seq != 0 {
		parts = append(parts, fmt.Sprintf("seq %d", block.Seq))
	}
	if includeTime && (block.Kind == BlockUser || block.Kind == BlockAssistantText) {
		if ts := FormatTimestamp(block.CreatedAt); ts != "" {
			parts = append(parts, ts)
		}
	}
	return "[" + strings.Join(parts, " · ") + "]"
}

// RenderSpanForCheckpoint renders the span as labeled text for the checkpoint
// model, split by segment.
func RenderSpanForCheckpoint(model SpanModel, budget BlockRenderBudget) (history, turnPrefix string, chars int) {
	render := func(segment SpanSegment) string {
		var parts []string
		for _, block := range model.Blocks {
			if block.Segment != segment {
				continue
			}
			parts = append(parts, FormatBlockHeader(block, true)+":\n"+RenderBlockBody(block, budget))
		}
		return strings.Join(parts, "\n\n")
	}
	history = render(SegmentHistory)
	turnPrefix = render(SegmentTurnPrefix)
	chars = len(history) + len(turnPrefix)
	return
}

// RenderSpanForCheckpointWithinBudget shrinks the checkpoint rendering until it
// fits maxChars, scaling per-block caps down before falling back to the
// smallest scale.
func RenderSpanForCheckpointWithinBudget(model SpanModel, maxChars int, base BlockRenderBudget) (history, turnPrefix string, chars int, scale float64) {
	scales := []float64{1, 0.6, 0.35, 0.2, 0.1, 0.05}
	var lastH, lastT string
	lastChars := 0
	lastScale := 1.0
	for _, s := range scales {
		h, t, c := RenderSpanForCheckpoint(model, ScaleRenderBudget(base, s))
		lastH, lastT, lastChars, lastScale = h, t, c, s
		if c <= maxChars {
			return h, t, c, s
		}
	}
	return lastH, lastT, lastChars, lastScale
}
