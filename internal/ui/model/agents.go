package model

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
)

// agentEntryStatus is the lifecycle state of one sub-agent row.
type agentEntryStatus uint8

const (
	// agentStatusWaiting means the run is registered but has not
	// produced any tool activity yet.
	agentStatusWaiting agentEntryStatus = iota
	// agentStatusWorking means the run is executing tools.
	agentStatusWorking
	// agentStatusDone means the run returned; the row lingers briefly
	// with a success marker before the panel prunes it.
	agentStatusDone
	// agentStatusCanceled means the user canceled the run; the row
	// lingers briefly before the panel prunes it.
	agentStatusCanceled
)

// agentDoneLinger is how long finished rows stay visible before the
// panel prunes them.
const agentDoneLinger = 4 * time.Second

// agentPanelRowCap bounds how many entries render at once so a burst of
// parallel sub-agents cannot eat the whole terminal. The selection
// window scrolls within the list.
const agentPanelRowCap = 3

// agentPanelPadLeft matches the pills area's implicit left padding so
// dock content aligns with the chat gutter.
const agentPanelPadLeft = 3

// agentEntry is one live sub-agent tracked by the dock.
type agentEntry struct {
	// toolCallID keys the entry and matches the parent agent tool call.
	toolCallID string
	// sessionID is the child session targeted by cancel and send.
	sessionID string
	// kind is "agent" or "agentic_fetch".
	kind string
	// prompt is the delegated prompt (truncated at render time).
	prompt string

	startedAt time.Time
	// changedAt is when the status last changed; done/canceled rows
	// persist agentDoneLinger past this point.
	changedAt time.Time
	status    agentEntryStatus

	// currentTool is the nested tool name most recently observed.
	currentTool string
	// inputRunes drives the live data meter (streamed nested input).
	inputRunes int
}

// AgentsPanel is the bottom dock showing live sub-agent progress. It is
// a stateful component driven imperatively by the main UI model: the
// model feeds lifecycle events in, and Draw paints one row per agent
// plus a footer with per-agent actions (send message, cancel).
type AgentsPanel struct {
	sty *styles.Styles

	entries []*agentEntry
	// selected indexes entries; it always points at the entry with
	// compose/cancel focus, preferring running entries.
	selected int

	// composing toggles the inline message input shown in the footer.
	composing    bool
	composeValue string

	// cursorX/cursorY are where the compose cursor should sit for the
	// last Draw; valid only while composing and focused.
	cursorX, cursorY int
}

// NewAgentsPanel creates an empty dock.
func NewAgentsPanel(sty *styles.Styles) *AgentsPanel {
	return &AgentsPanel{sty: sty}
}

// Register adds a running sub-agent entry, or enriches an existing one
// when the started notification carries a prompt the tool call did not
// yet stream. It is a no-op when toolCallID is empty.
func (p *AgentsPanel) Register(toolCallID, sessionID, kind, prompt string) {
	if toolCallID == "" {
		return
	}
	for _, e := range p.entries {
		if e.toolCallID == toolCallID {
			if prompt != "" {
				e.prompt = prompt
			}
			return
		}
	}
	e := &agentEntry{
		toolCallID: toolCallID,
		sessionID:  sessionID,
		kind:       kind,
		prompt:     prompt,
		startedAt:  time.Now(),
		changedAt:  time.Now(),
		status:     agentStatusWaiting,
	}
	p.entries = append(p.entries, e)
	p.clampSelected()
}

// SetActivity records live progress for an entry: the current nested
// tool and the streamed input characters driving the data meter.
func (p *AgentsPanel) SetActivity(toolCallID, toolName string, inputRunes int) {
	for _, e := range p.entries {
		if e.toolCallID != toolCallID {
			continue
		}
		if e.status == agentStatusDone || e.status == agentStatusCanceled {
			return
		}
		e.status = agentStatusWorking
		e.currentTool = toolName
		e.inputRunes = inputRunes
		return
	}
}

// MarkDone retires an entry into its linger window.
func (p *AgentsPanel) MarkDone(toolCallID string) {
	for _, e := range p.entries {
		if e.toolCallID != toolCallID {
			continue
		}
		if e.status != agentStatusCanceled {
			e.status = agentStatusDone
		}
		e.changedAt = time.Now()
		return
	}
}

// MarkCanceled retires an entry via user cancel.
func (p *AgentsPanel) MarkCanceled(toolCallID string) {
	for _, e := range p.entries {
		if e.toolCallID != toolCallID {
			continue
		}
		e.status = agentStatusCanceled
		e.changedAt = time.Now()
		return
	}
}

// Prune drops done/canceled entries once their linger window elapses.
// It reports whether the visible set changed.
func (p *AgentsPanel) Prune(now time.Time) bool {
	kept := p.entries[:0]
	for _, e := range p.entries {
		switch e.status {
		case agentStatusDone, agentStatusCanceled:
			if now.Sub(e.changedAt) >= agentDoneLinger {
				continue
			}
		}
		kept = append(kept, e)
	}
	if len(kept) == len(p.entries) {
		return false
	}
	p.entries = kept
	p.clampSelected()
	return true
}

// Visible reports whether the dock should render: any running entry or
// any entry still inside its linger window.
func (p *AgentsPanel) Visible() bool {
	now := time.Now()
	for _, e := range p.entries {
		switch e.status {
		case agentStatusDone, agentStatusCanceled:
			if now.Sub(e.changedAt) >= agentDoneLinger {
				continue
			}
		}
		return true
	}
	return false
}

// RunningCount reports how many entries are not yet terminal.
func (p *AgentsPanel) RunningCount() int {
	var n int
	for _, e := range p.entries {
		switch e.status {
		case agentStatusWaiting, agentStatusWorking:
			n++
		}
	}
	return n
}

// Len reports the number of tracked entries.
func (p *AgentsPanel) Len() int { return len(p.entries) }

func (p *AgentsPanel) clampSelected() {
	if len(p.entries) == 0 {
		p.selected = 0
		return
	}
	if p.selected >= len(p.entries) {
		p.selected = len(p.entries) - 1
	}
}

// selectedEntry returns the currently selected entry, nil when empty.
func (p *AgentsPanel) selectedEntry() *agentEntry {
	if len(p.entries) == 0 {
		return nil
	}
	p.clampSelected()
	// Lift a stale selection off a dead row onto the nearest live run.
	if e := p.entries[p.selected]; e.status == agentStatusCanceled {
		p.SelectNext()
	}
	return p.entries[p.selected]
}

// SelectNext moves selection to the next running entry, or wraps.
func (p *AgentsPanel) SelectNext() {
	if len(p.entries) < 2 {
		return
	}
	for i := 1; i <= len(p.entries); i++ {
		next := (p.selected + i) % len(p.entries)
		if p.entries[next].status != agentStatusCanceled {
			p.selected = next
			return
		}
	}
}

// SelectPrev moves selection to the previous running entry, or wraps.
func (p *AgentsPanel) SelectPrev() {
	if len(p.entries) < 2 {
		return
	}
	for i := 1; i <= len(p.entries); i++ {
		prev := (p.selected - i + len(p.entries)) % len(p.entries)
		if p.entries[prev].status != agentStatusCanceled {
			p.selected = prev
			return
		}
	}
}

// Focus brings the dock into focus, defaulting the selection to the
// most recent running entry when the current one is stale.
func (p *AgentsPanel) Focus() {
	if len(p.entries) == 0 {
		return
	}
	p.clampSelected()
	if e := p.entries[p.selected]; e.status == agentStatusCanceled {
		p.SelectNext()
	}
}

// Selected returns the selected entry, nil when none.
func (p *AgentsPanel) Selected() *agentEntry { return p.selectedEntry() }

// StartCompose opens the inline message input for the selected entry.
func (p *AgentsPanel) StartCompose() {
	if sel := p.selectedEntry(); sel != nil && sel.status != agentStatusCanceled {
		p.composing = true
		p.composeValue = ""
	}
}

// Composing reports whether the inline message input is open.
func (p *AgentsPanel) Composing() bool { return p.composing }

// CancelCompose closes the inline input, discarding its value.
func (p *AgentsPanel) CancelCompose() {
	p.composing = false
	p.composeValue = ""
}

// Blur dismisses the inline input when focus leaves the dock.
func (p *AgentsPanel) Blur() {
	p.composing = false
	p.composeValue = ""
}

// ComposeAppend appends a rune to the inline input.
func (p *AgentsPanel) ComposeAppend(r rune) {
	p.composeValue += string(r)
}

// ComposeBackspace deletes the last rune of the inline input.
func (p *AgentsPanel) ComposeBackspace() {
	if p.composeValue == "" {
		return
	}
	_, size := utf8.DecodeLastRuneInString(p.composeValue)
	p.composeValue = p.composeValue[:len(p.composeValue)-size]
}

// ComposeValue holds the current inline input text.
func (p *AgentsPanel) ComposeValue() string { return p.composeValue }

// Height returns the dock rows: header, up to agentPanelRowCap entry
// rows, and the footer. Zero when the panel is not visible.
func (p *AgentsPanel) Height() int {
	if !p.Visible() {
		return 0
	}
	return 1 + min(len(p.entries), agentPanelRowCap) + 1
}

// scrollWindow returns the entry range rendered inside the dock.
func (p *AgentsPanel) scrollWindow() (start, end int) {
	if len(p.entries) <= agentPanelRowCap {
		return 0, len(p.entries)
	}
	start = max(p.selected-agentPanelRowCap/2, 0)
	start = min(start, len(p.entries)-agentPanelRowCap)
	return start, start + agentPanelRowCap
}

// Draw paints the dock into area and returns the compose cursor
// position when the inline input is open, else nil.
func (p *AgentsPanel) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	p.cursorX, p.cursorY = 0, 0
	if area.Dy() <= 0 || !p.Visible() {
		return nil
	}
	width := area.Dx()
	view := uv.NewStyledString(p.Render(width))
	view.Draw(scr, area)

	if !p.composing {
		return nil
	}
	// Compose cursor sits just past the last character of the input.
	p.cursorX = area.Min.X + agentPanelPadLeft + lipgloss.Width("msg ▸ ") + lipgloss.Width(p.composeValue)
	p.cursorY = area.Min.Y + p.Height() - 1
	return tea.NewCursor(p.cursorX, p.cursorY)
}

// Render renders the dock to a string of exactly Height() rows at the
// given total width.
func (p *AgentsPanel) Render(width int) string {
	t := p.sty.Agents
	contentWidth := max(width-2*agentPanelPadLeft, 1)

	// Header row: "Agents" plus live counts.
	header := t.Title.Render("Agents")
	if running := p.RunningCount(); running > 0 {
		header += " " + t.TitleCount.Render(fmt.Sprintf("· %d running", running))
	}
	start, end := p.scrollWindow()
	doneVisible := 0
	for _, e := range p.entries {
		if e.status == agentStatusDone {
			doneVisible++
		}
	}
	if doneVisible > 0 {
		header += " " + t.DoneCount.Render(fmt.Sprintf("· %d done", doneVisible))
	}

	rows := []string{p.pad(header, contentWidth)}

	for i := start; i < end; i++ {
		e := p.entries[i]
		rows = append(rows, p.pad(p.renderRow(e, i == p.selected, contentWidth), contentWidth))
	}

	rows = append(rows, p.pad(p.renderFooter(contentWidth), contentWidth))

	content := lipgloss.JoinVertical(lipgloss.Left, rows...)
	return t.Area.Width(width).Render(content)
}

func (p *AgentsPanel) pad(row string, width int) string {
	return tpad(row, width)
}

// tpad left-pads a row so content aligns with the chat gutter.
func tpad(row string, width int) string {
	return strings.Repeat(" ", agentPanelPadLeft) + lipgloss.NewStyle().MaxWidth(width).Render(row)
}

// renderRow renders one entry line: selection glyph, kind tag,
// activity (current tool or prompt fallback), meter, and elapsed time.
func (p *AgentsPanel) renderRow(e *agentEntry, selected bool, width int) string {
	t := p.sty.Agents

	var glyph string
	switch {
	case selected:
		glyph = t.SelectedGlyph.Render("▶")
	case e.status == agentStatusDone:
		glyph = t.Done.Render("✓")
	case e.status == agentStatusCanceled:
		glyph = t.Canceled.Render("✗")
	default:
		glyph = t.Glyph.Render("•")
	}

	kind := e.kind
	if kind == "" {
		kind = "agent"
	}
	kindTag := t.KindTag.Render(kind)

	// Activity: prefer the live tool, fall back to the prompt while the
	// sub-agent is still bootstrapping.
	var activity string
	switch e.status {
	case agentStatusWaiting:
		activity = truncateText(strings.ReplaceAll(e.prompt, "\n", " "), 48)
		if activity == "" {
			activity = "starting"
		}
		activity = t.Waiting.Render(activity + "…")
	case agentStatusDone:
		activity = t.Done.Render("done")
	case agentStatusCanceled:
		activity = t.Canceled.Render("canceled")
	default:
		activity = t.ActiveTool.Render(e.currentTool)
	}

	segments := []string{glyph, kindTag, activity}

	if e.status == agentStatusWorking || e.status == agentStatusWaiting {
		meter := t.Meter.Render(agentMeter(e.inputRunes))
		elapsed := t.Elapsed.Render(elapsedSince(e.startedAt))
		segments = append(segments, meter, elapsed)
	}

	row := lipgloss.JoinHorizontal(lipgloss.Left, segmentSpace(segments)...)
	return t.Row.MaxWidth(width).Render(row)
}

// segmentSpace interleaves segments with single spaces, skipping empty
// segments.
func segmentSpace(segs []string) []string {
	var out []string
	for _, s := range segs {
		if s == "" {
			continue
		}
		if len(out) > 0 {
			out = append(out, " ")
		}
		out = append(out, s)
	}
	return out
}

// renderFooter renders the dock's final row: the compose input when
// open, otherwise the action hints.
func (p *AgentsPanel) renderFooter(width int) string {
	t := p.sty.Agents
	if p.composing {
		inner := lipgloss.NewStyle().MaxWidth(max(width-lipgloss.Width("msg ▸ "), 1))
		return t.Row.Render(
			t.ComposePrompt.Render("msg ▸ ") +
				t.ComposeInput.Render(inner.Render(p.composeValue)) +
				t.HelpText.Render("  enter send · esc back"),
		)
	}
	hints := [][2]string{
		{"m", "message"},
		{"x", "cancel"},
		{"esc", "chat"},
		{"tab", "editor"},
	}
	var parts []string
	for _, h := range hints {
		parts = append(parts, t.HelpKey.Render(h[0])+" "+t.HelpText.Render(h[1]))
	}
	return t.Row.Render(strings.Join(parts, "  "))
}

// elapsedSince renders a compact duration ("41s", "2m", "1h02m").
func elapsedSince(start time.Time) string {
	d := time.Since(start)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

// agentMeter renders a bracketed fill meter scaling with the streamed
// input runes, mirroring the chat loader's received-data meter.
func agentMeter(inputRunes int) string {
	const meterWidth = 6
	completed := inputRunes % 1000
	filled := completed / (1000 / meterWidth)
	if completed > 0 && filled == 0 {
		filled = 1
	}
	var b strings.Builder
	b.WriteByte('[')
	for i := range meterWidth {
		if i < filled {
			b.WriteString("=")
		} else {
			b.WriteString(".")
		}
	}
	b.WriteByte(']')
	return b.String()
}

// truncateText truncates s to maxLen runes, appending an ellipsis.
func truncateText(s string, maxLen int) string {
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxLen-1]) + "…"
}
