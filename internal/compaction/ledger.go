package compaction

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// LedgerUserInstruction is a verbatim user instruction extracted from the span.
type LedgerUserInstruction struct {
	Turn      int    `json:"turn"`
	Seq       int    `json:"seq,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
	Chars     int    `json:"chars"`
}

// LedgerFile tracks read/edit/write counts for a file path.
type LedgerFile struct {
	Path     string `json:"path"`
	Reads    int    `json:"reads"`
	Edits    int    `json:"edits"`
	Writes   int    `json:"writes"`
	LastSeq  int    `json:"lastSeq,omitempty"`
	LastOp   string `json:"lastOp"`
	LastTurn int    `json:"lastTurn"`
}

// LedgerCommand is a shell command run by the agent.
type LedgerCommand struct {
	Turn      int    `json:"turn"`
	Seq       int    `json:"seq,omitempty"`
	Command   string `json:"command"`
	IsError   bool   `json:"isError"`
	Truncated bool   `json:"truncated"`
}

// LedgerError is a deduplicated error signature.
type LedgerError struct {
	Turn      int    `json:"turn"`
	Seq       int    `json:"seq,omitempty"`
	Tool      string `json:"tool"`
	Signature string `json:"signature"`
	Count     int    `json:"count"`
}

// LedgerInjectedMessage is a turn starter that wasn't typed by the user.
type LedgerInjectedMessage struct {
	Turn int          `json:"turn"`
	Seq  int          `json:"seq,omitempty"`
	Kind UserLikeKind `json:"kind"`
	Text string       `json:"preview"`
}

// LedgerAssistantStatement is a turn's final assistant text.
type LedgerAssistantStatement struct {
	Turn      int    `json:"turn"`
	Seq       int    `json:"seq,omitempty"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
}

// CausalityEdge is one action -> result -> state-changed edge.
type CausalityEdge struct {
	Turn         int      `json:"turn"`
	Seq          int      `json:"seq,omitempty"`
	ToolCallID   string   `json:"toolCallId,omitempty"`
	Tool         string   `json:"tool"`
	ArgsHash     string   `json:"argsHash,omitempty"`
	IsError      bool     `json:"isError"`
	FilesChanged []string `json:"filesChanged"`
}

// SessionLedger is the deterministic, model-free extract of the span.
type SessionLedger struct {
	UserInstructions    []LedgerUserInstruction    `json:"userInstructions"`
	InjectedMessages    []LedgerInjectedMessage    `json:"injectedMessages"`
	AssistantStatements []LedgerAssistantStatement `json:"assistantStatements"`
	Files               []LedgerFile               `json:"files"`
	Commands            []LedgerCommand            `json:"commands"`
	Errors              []LedgerError              `json:"errors"`
	Causality           []CausalityEdge            `json:"causality"`
	Stats               SpanStats                  `json:"stats"`
}

// LedgerLimits caps each ledger section.
type LedgerLimits struct {
	MaxUserInstructionChars    int
	MaxAssistantStatementChars int
	MaxCommandChars            int
	MaxCommands                int
	MaxErrors                  int
	MaxAssistantStatements     int
}

// DefaultLedgerLimits matches the ShiftUp defaults.
var DefaultLedgerLimits = LedgerLimits{
	MaxUserInstructionChars:    1200,
	MaxAssistantStatementChars: 600,
	MaxCommandChars:            240,
	MaxCommands:                40,
	MaxErrors:                  15,
	MaxAssistantStatements:     30,
}

var (
	writeTools = map[string]bool{"write": true}
	editTools  = map[string]bool{"edit": true, "multiedit": true, "apply_patch": true, "morph_edit": true, "lsp_rename": true, "lsp_replace_symbol": true}
	readTools  = map[string]bool{"read": true, "view": true}
	cmdTools   = map[string]bool{"bash": true, "shell": true, "powershell": true}
)

var errorSignatureRe = regexp.MustCompile(`(?i)\b(error|exception|failed|fatal|panic|traceback)\b`)

func errorSignature(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	var preferred string
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if errorSignatureRe.MatchString(t) {
			preferred = t
			break
		}
	}
	if preferred == "" && len(lines) > 0 {
		preferred = strings.TrimSpace(lines[len(lines)-1])
	}
	if len(preferred) > 200 {
		preferred = preferred[:200]
	}
	return preferred
}

func argsHash(call SpanToolCall) string {
	// A short, stable fingerprint of the tool arguments for causality edges.
	// Not cryptographic; just for dedup display.
	parts := make([]string, 0, len(call.Args))
	for _, k := range orderedKeys(call.Args) {
		v := call.Args[k]
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	h := strings.Join(parts, "|")
	if len(h) > 80 {
		h = h[:80]
	}
	return h
}

func fileOp(toolName string) (op string, tracked bool) {
	if writeTools[toolName] {
		return "write", true
	}
	if editTools[toolName] {
		return "edit", true
	}
	if readTools[toolName] {
		return "read", true
	}
	return "read", false
}

// BuildSessionLedger extracts the deterministic ledger from a span model. No
// model is involved; facts come straight from the transcript.
func BuildSessionLedger(model SpanModel, limits LedgerLimits) SessionLedger {
	ledger := SessionLedger{Stats: model.Stats}
	ledger.Stats.Messages = model.Stats.Messages
	files := map[string]*LedgerFile{}
	errorsBySig := map[string]*LedgerError{}
	resultByCallID := map[string]SpanBlock{}

	for _, block := range model.Blocks {
		if block.Kind == BlockToolResult && block.ToolCallID != "" {
			resultByCallID[block.ToolCallID] = block
		}
	}

	for _, block := range model.Blocks {
		switch block.Kind {
		case BlockUser:
			if strings.TrimSpace(block.Text) == "" {
				continue
			}
			if block.UserKind != "" && block.UserKind != UserKindUser {
				preview := strings.TrimSpace(strings.Join(strings.Fields(block.Text), " "))
				if len(preview) > 100 {
					preview = preview[:100]
				}
				ledger.InjectedMessages = append(ledger.InjectedMessages, LedgerInjectedMessage{
					Turn: block.TurnIndex + 1,
					Seq:  block.Seq,
					Kind: block.UserKind,
					Text: preview,
				})
				continue
			}
			cut, trunc, _ := TruncateHeadTail(block.Text, limits.MaxUserInstructionChars, 0.75, func(omitted int) string {
				suffix := ""
				if block.Seq != 0 {
					suffix = fmt.Sprintf(" · full message at transcript seq %d", block.Seq)
				}
				return fmt.Sprintf("[… %s characters omitted%s]", formatCount(omitted), suffix)
			})
			ledger.UserInstructions = append(ledger.UserInstructions, LedgerUserInstruction{
				Turn:      block.TurnIndex + 1,
				Seq:       block.Seq,
				Timestamp: FormatTimestamp(block.CreatedAt),
				Text:      cut,
				Truncated: trunc,
				Chars:     block.Chars,
			})

		case BlockAssistantText:
			if !block.IsTurnFinalAssistantText || strings.TrimSpace(block.Text) == "" {
				continue
			}
			cut, trunc, _ := TruncateHeadTail(block.Text, limits.MaxAssistantStatementChars, 0.5, func(omitted int) string {
				suffix := ""
				if block.Seq != 0 {
					suffix = fmt.Sprintf(" · transcript seq %d", block.Seq)
				}
				return fmt.Sprintf("[… %s characters omitted%s]", formatCount(omitted), suffix)
			})
			ledger.AssistantStatements = append(ledger.AssistantStatements, LedgerAssistantStatement{
				Turn:      block.TurnIndex + 1,
				Seq:       block.Seq,
				Text:      cut,
				Truncated: trunc,
			})

		case BlockAssistantToolCalls:
			for _, call := range block.ToolCalls {
				path := ToolCallPath(call)
				op, tracked := fileOp(call.Name)
				if tracked && path != "" {
					existing := files[path]
					if existing == nil {
						existing = &LedgerFile{Path: path}
						files[path] = existing
					}
					switch op {
					case "read":
						existing.Reads++
					case "edit":
						existing.Edits++
					case "write":
						existing.Writes++
					}
					existing.LastOp = op
					existing.LastTurn = block.TurnIndex + 1
					if block.Seq != 0 {
						existing.LastSeq = block.Seq
					}
				}
				if cmdTools[call.Name] {
					if cmd, ok := call.Args["command"].(string); ok && strings.TrimSpace(cmd) != "" {
						cut, trunc, _ := TruncateHeadTail(strings.TrimSpace(cmd), limits.MaxCommandChars, 0.7, func(omitted int) string {
							return fmt.Sprintf("[… %s chars]", formatCount(omitted))
						})
						result := resultByCallID[call.ID]
						ledger.Commands = append(ledger.Commands, LedgerCommand{
							Turn:      block.TurnIndex + 1,
							Seq:       block.Seq,
							Command:   cut,
							IsError:   result.IsError,
							Truncated: trunc,
						})
					}
				}
				// Causality edge: record tool, args fingerprint, and files
				// touched by this call.
				var changed []string
				if path != "" && (op == "edit" || op == "write") {
					changed = append(changed, path)
				}
				edge := CausalityEdge{
					Turn:         block.TurnIndex + 1,
					Seq:          block.Seq,
					ToolCallID:   call.ID,
					Tool:         call.Name,
					ArgsHash:     argsHash(call),
					FilesChanged: changed,
				}
				if res, ok := resultByCallID[call.ID]; ok {
					edge.IsError = res.IsError
				}
				ledger.Causality = append(ledger.Causality, edge)
			}

		case BlockToolResult:
			if !block.IsError {
				continue
			}
			sig := errorSignature(block.Text)
			if sig == "" {
				sig = "(error without text)"
			}
			key := block.ToolName + "::" + sig
			existing := errorsBySig[key]
			if existing != nil {
				existing.Count++
				existing.Turn = block.TurnIndex + 1
				if block.Seq != 0 {
					existing.Seq = block.Seq
				}
			} else {
				errorsBySig[key] = &LedgerError{
					Turn:      block.TurnIndex + 1,
					Seq:       block.Seq,
					Tool:      block.ToolName,
					Signature: sig,
					Count:     1,
				}
			}
		}
	}

	// Finalize files sorted by path.
	fileList := make([]LedgerFile, 0, len(files))
	for _, f := range files {
		fileList = append(fileList, *f)
	}
	sort.Slice(fileList, func(i, j int) bool { return fileList[i].Path < fileList[j].Path })
	ledger.Files = fileList

	// Trim capped sections from the least-valuable end. Build the sorted
	// error list from errorsBySig, then cap to MaxErrors (keeping the most
	// recent). The previous code checked len(ledger.Errors) before it was
	// populated, so MaxErrors was never applied.
	var errs []LedgerError
	for _, e := range errorsBySig {
		errs = append(errs, *e)
	}
	sort.Slice(errs, func(i, j int) bool { return errs[i].Turn < errs[j].Turn })
	if len(errs) > limits.MaxErrors {
		errs = errs[len(errs)-limits.MaxErrors:]
	}
	ledger.Errors = errs
	if len(ledger.Commands) > limits.MaxCommands {
		ledger.Commands = ledger.Commands[len(ledger.Commands)-limits.MaxCommands:]
	}
	if len(ledger.AssistantStatements) > limits.MaxAssistantStatements {
		ledger.AssistantStatements = ledger.AssistantStatements[len(ledger.AssistantStatements)-limits.MaxAssistantStatements:]
	}
	return ledger
}

// RenderSessionLedger renders the ledger as Markdown under a character budget.
// User instructions are kept first; other groups shrink or drop from the least
// valuable end.
func RenderSessionLedger(ledger SessionLedger, maxChars int) string {
	type section struct {
		priority int
		text     string
	}
	var sections []section

	if len(ledger.UserInstructions) > 0 {
		var lines []string
		for _, item := range ledger.UserInstructions {
			anchor := ""
			if item.Seq != 0 {
				anchor = fmt.Sprintf(" (seq %d)", item.Seq)
			}
			ts := ""
			if item.Timestamp != "" {
				ts = " " + item.Timestamp
			}
			lines = append(lines, fmt.Sprintf("- T%d%s%s: %s", item.Turn, anchor, ts, item.Text))
		}
		sections = append(sections, section{0, fmt.Sprintf("### User instructions (verbatim, %d)\n%s", len(ledger.UserInstructions), strings.Join(lines, "\n"))})
	}
	if len(ledger.Errors) > 0 {
		var lines []string
		for _, item := range ledger.Errors {
			anchor := ""
			if item.Seq != 0 {
				anchor = fmt.Sprintf(" (seq %d)", item.Seq)
			}
			cnt := ""
			if item.Count > 1 {
				cnt = fmt.Sprintf(" ×%d", item.Count)
			}
			lines = append(lines, fmt.Sprintf("- T%d%s %s%s: %s", item.Turn, anchor, item.Tool, cnt, item.Signature))
		}
		sections = append(sections, section{1, fmt.Sprintf("### Errors seen (%d distinct)\n%s", len(ledger.Errors), strings.Join(lines, "\n"))})
	}
	if len(ledger.Files) > 0 {
		var lines []string
		for _, f := range ledger.Files {
			ops := []string{}
			if f.Writes > 0 {
				ops = append(ops, fmt.Sprintf("write×%d", f.Writes))
			}
			if f.Edits > 0 {
				ops = append(ops, fmt.Sprintf("edit×%d", f.Edits))
			}
			if f.Reads > 0 {
				ops = append(ops, fmt.Sprintf("read×%d", f.Reads))
			}
			anchor := ""
			if f.LastSeq != 0 {
				anchor = fmt.Sprintf(" (seq %d)", f.LastSeq)
			}
			lines = append(lines, fmt.Sprintf("- %s — %s; last %s T%d%s", f.Path, strings.Join(ops, " "), f.LastOp, f.LastTurn, anchor))
		}
		sections = append(sections, section{2, fmt.Sprintf("### Files touched (%d)\n%s", len(ledger.Files), strings.Join(lines, "\n"))})
	}
	if len(ledger.Commands) > 0 {
		var lines []string
		for _, item := range ledger.Commands {
			anchor := ""
			if item.Seq != 0 {
				anchor = fmt.Sprintf(" (seq %d)", item.Seq)
			}
			errMark := ""
			if item.IsError {
				errMark = " ✗"
			}
			cmd := strings.ReplaceAll(item.Command, "`", "‘")
			lines = append(lines, fmt.Sprintf("- T%d%s%s: `%s`", item.Turn, anchor, errMark, cmd))
		}
		sections = append(sections, section{3, fmt.Sprintf("### Commands run (last %d)\n%s", len(ledger.Commands), strings.Join(lines, "\n"))})
	}
	if len(ledger.Causality) > 0 {
		var lines []string
		shown := ledger.Causality
		if len(shown) > 30 {
			shown = shown[len(shown)-30:]
		}
		for _, e := range shown {
			anchor := ""
			if e.Seq != 0 {
				anchor = fmt.Sprintf(" (seq %d)", e.Seq)
			}
			errMark := ""
			if e.IsError {
				errMark = " ✗"
			}
			files := ""
			if len(e.FilesChanged) > 0 {
				files = " → " + strings.Join(e.FilesChanged, ", ")
			}
			lines = append(lines, fmt.Sprintf("- T%d%s %s%s%s", e.Turn, anchor, e.Tool, errMark, files))
		}
		sections = append(sections, section{1, fmt.Sprintf("### Causality (last %d of %d)\n%s", len(shown), len(ledger.Causality), strings.Join(lines, "\n"))})
	}
	if len(ledger.InjectedMessages) > 0 {
		var lines []string
		for _, item := range ledger.InjectedMessages {
			anchor := ""
			if item.Seq != 0 {
				anchor = fmt.Sprintf(" (seq %d)", item.Seq)
			}
			lines = append(lines, fmt.Sprintf("- T%d%s %s: %s", item.Turn, anchor, item.Kind, item.Text))
		}
		sections = append(sections, section{5, fmt.Sprintf("### Injected (non-user) turn starters (%d)\n%s", len(ledger.InjectedMessages), strings.Join(lines, "\n"))})
	}
	if len(ledger.AssistantStatements) > 0 {
		var lines []string
		for _, item := range ledger.AssistantStatements {
			anchor := ""
			if item.Seq != 0 {
				anchor = fmt.Sprintf(" (seq %d)", item.Seq)
			}
			lines = append(lines, fmt.Sprintf("- T%d%s: %s", item.Turn, anchor, item.Text))
		}
		sections = append(sections, section{4, fmt.Sprintf("### Assistant turn conclusions (%d)\n%s", len(ledger.AssistantStatements), strings.Join(lines, "\n"))})
	}

	sort.SliceStable(sections, func(i, j int) bool { return sections[i].priority < sections[j].priority })
	header := fmt.Sprintf("## Session Ledger (deterministic; extracted from the transcript, not model-generated)\nSpan: %d messages · %d user messages · %d tool calls · %d errors. `Tn` = turn within this span; `seq nn` = physical transcript sequence number.", ledger.Stats.Messages, ledger.Stats.UserMessages, ledger.Stats.ToolCalls, ledger.Stats.Errors)
	body := strings.Join(func() []string {
		out := make([]string, 0, len(sections))
		for _, s := range sections {
			out = append(out, s.text)
		}
		return out
	}(), "\n\n")

	// Drop least-valuable sections until we fit.
	for len(header)+len(body)+2 > maxChars && len(sections) > 1 {
		sections = sections[:len(sections)-1]
		body = strings.Join(func() []string {
			out := make([]string, 0, len(sections))
			for _, s := range sections {
				out = append(out, s.text)
			}
			return out
		}(), "\n\n")
	}
	if len(header)+len(body)+2 > maxChars {
		cut, _, _ := TruncateHeadTail(body, maxChars-len(header)-2, 1, func(omitted int) string {
			return fmt.Sprintf("[… %s characters of ledger omitted]", formatCount(omitted))
		})
		body = cut
	}
	if body == "" {
		return header
	}
	return header + "\n\n" + body
}

// TranscriptMapTurn is one row of the per-turn transcript map.
type TranscriptMapTurn struct {
	Turn      int         `json:"turn"`
	Segment   SpanSegment `json:"segment"`
	StartSeq  int         `json:"startSeq,omitempty"`
	EndSeq    int         `json:"endSeq,omitempty"`
	Timestamp string      `json:"timestamp,omitempty"`
	Preview   string      `json:"preview,omitempty"`
	ToolCalls int         `json:"toolCalls"`
	Errors    int         `json:"errors"`
	Files     []string    `json:"files"`
}

// TranscriptMap is the per-turn index into the canonical message store.
type TranscriptMap struct {
	Turns []TranscriptMapTurn `json:"turns"`
}

// BuildTranscriptMap builds the per-turn map from a span model.
func BuildTranscriptMap(model SpanModel, previewChars int) TranscriptMap {
	if previewChars <= 0 {
		previewChars = 120
	}
	turns := make([]TranscriptMapTurn, 0, len(model.Turns))
	for _, turn := range model.Turns {
		raw := strings.TrimSpace(strings.Join(strings.Fields(turn.UserText), " "))
		prefix := ""
		if turn.UserKind != "" && turn.UserKind != UserKindUser {
			prefix = fmt.Sprintf("[%s] ", turn.UserKind)
		}
		preview := ""
		if raw != "" {
			if len(raw) > previewChars {
				raw = raw[:previewChars]
			}
			preview = prefix + raw
		}
		files := turn.Files
		if len(files) > 6 {
			files = files[:6]
		}
		turns = append(turns, TranscriptMapTurn{
			Turn:      turn.Index + 1,
			Segment:   turn.Segment,
			StartSeq:  turn.FirstSeq,
			EndSeq:    turn.LastSeq,
			Timestamp: FormatTimestamp(turn.CreatedAt),
			Preview:   preview,
			ToolCalls: turn.ToolCallCount,
			Errors:    turn.ErrorCount,
			Files:     files,
		})
	}
	return TranscriptMap{Turns: turns}
}

// RenderTranscriptMap renders the map as Markdown under a character budget.
func RenderTranscriptMap(m TranscriptMap, maxChars int) string {
	lines := []string{"## Transcript Map (where things happened; use with the shell cheat sheet below)"}
	lines = append(lines, fmt.Sprintf("This compaction (%d turns):", len(m.Turns)))
	for _, turn := range m.Turns {
		rng := "seq ?"
		if turn.StartSeq != 0 && turn.EndSeq != 0 {
			if turn.StartSeq == turn.EndSeq {
				rng = fmt.Sprintf("seq %d", turn.StartSeq)
			} else {
				rng = fmt.Sprintf("seq %d-%d", turn.StartSeq, turn.EndSeq)
			}
		}
		var parts []string
		parts = append(parts, fmt.Sprintf("T%d", turn.Turn), rng, turn.Timestamp)
		if turn.Segment == SegmentTurnPrefix {
			parts = append(parts, "(in-flight turn prefix)")
		}
		if turn.Preview != "" {
			parts = append(parts, "“"+turn.Preview+"”")
		} else {
			parts = append(parts, "(no user message; continuation)")
		}
		parts = append(parts, fmt.Sprintf("%d tool calls", turn.ToolCalls))
		if turn.Errors > 0 {
			parts = append(parts, fmt.Sprintf("%d errors", turn.Errors))
		}
		if len(turn.Files) > 0 {
			parts = append(parts, "files: "+strings.Join(turn.Files, ", "))
		}
		lines = append(lines, "- "+strings.Join(parts, " · "))
	}
	text := strings.Join(lines, "\n")
	if len(text) > maxChars {
		cut, _, _ := TruncateHeadTail(text, maxChars, 0.5, func(omitted int) string {
			return fmt.Sprintf("[… %s characters of map omitted]", formatCount(omitted))
		})
		text = cut
	}
	return text
}
