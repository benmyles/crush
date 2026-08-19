package chat

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/list"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
)

var (
	_ MessageItem         = (*CompactionMessageItem)(nil)
	_ Expandable          = (*CompactionMessageItem)(nil)
	_ list.MouseClickable = (*CompactionMessageItem)(nil)
)

// CompactionMessageItem renders the structured digest of an engine-produced
// compaction summary. It is collapsed to a one-line header by default and
// only expands to the full tree (checkpoint counts, ledger tallies,
// extracts, working set, recovery note) when the user clicks it or presses
// the expand key. It replaces the plain assistant render for summary
// messages that carry a CompactionContent part.
type CompactionMessageItem struct {
	*list.Versioned
	sty  *styles.Styles
	msg  *message.Message
	part message.CompactionContent

	expanded bool
}

// NewCompactionMessageItem builds a compaction overview item, collapsed so
// the always-large summary does not dominate the chat. The item reports
// Finished immediately and the F6 list cache can freeze it; toggling
// expansion bumps the version so the cache re-renders.
func NewCompactionMessageItem(sty *styles.Styles, msg *message.Message, part message.CompactionContent) *CompactionMessageItem {
	return &CompactionMessageItem{
		Versioned: list.NewVersioned(),
		sty:       sty,
		msg:       msg,
		part:      part,
	}
}

// ID identifies the item by the engine summary node id.
func (c *CompactionMessageItem) ID() string {
	if c.part.SummaryID != "" {
		return "compaction-" + c.part.SummaryID
	}
	return c.msg.ID
}

// Finished is always true: the digest never streams or animates.
func (c *CompactionMessageItem) Finished() bool {
	return true
}

// ToggleExpanded flips the collapsed state and bumps the render version so
// the list cache repaints the item. It returns whether the item is now
// expanded.
func (c *CompactionMessageItem) ToggleExpanded() bool {
	c.expanded = !c.expanded
	c.Bump()
	return c.expanded
}

// HandleMouseClick implements list.MouseClickable. The whole summary line
// is the expand toggle, so any left click on the item expands or collapses
// it via the generic Expandable path.
func (c *CompactionMessageItem) HandleMouseClick(btn ansi.MouseButton, x, y int) bool {
	return btn == ansi.MouseLeft
}

// Render renders the header and, when expanded, the overview tree inside a
// section shell.
func (c *CompactionMessageItem) Render(width int) string {
	return common.Section(c.sty, c.render(width), width)
}

// RawRender renders the header and tree without the section shell.
func (c *CompactionMessageItem) RawRender(width int) string {
	return c.render(width)
}

// render builds the collapsed one-line header or the full tree depending on
// the expansion state.
func (c *CompactionMessageItem) render(width int) string {
	label := c.sty.Messages.AssistantInfoModel
	dim := c.sty.Messages.AssistantInfoProvider
	header := ansi.Truncate(c.headerLine(), width, "…")

	var b strings.Builder
	if !c.expanded {
		suffix := dim.Render(" · click to expand")
		prefix := "▸ "
		head := c.headerLine()
		// Reserve space for the prefix and the hint so the finished line
		// fits the render width.
		head = ansi.Truncate(head, width-ansi.StringWidth(prefix)-ansi.StringWidth(suffix), "…")
		b.WriteString(prefix)
		b.WriteString(label.Render(head))
		b.WriteString(suffix)
		return ansi.Truncate(b.String(), width, "…")
	}

	b.WriteString("▾ ")
	b.WriteString(label.Render(header))
	b.WriteString("\n")
	c.tree(&b)

	return strings.TrimRight(b.String(), "\n")
}

// headerLine is the single-line summary: completion marker, model, level,
// and token counts.
func (c *CompactionMessageItem) headerLine() string {
	part := c.part
	levelName := fmt.Sprintf("level %d", part.Level)
	if name := compactionLevelName(part.Level); name != "" {
		levelName = name
	}
	return strings.Join([]string{
		"⚡ Compaction complete",
		fmt.Sprintf("· %s", part.ModelID),
		fmt.Sprintf("· %s", levelName),
		fmt.Sprintf("· %d tokens", part.TokenCount),
	}, " ")
}

// tree writes the overview tree: a branch per lane with leaf counts.
func (c *CompactionMessageItem) tree(b *strings.Builder) {
	part := c.part
	dim := c.sty.Messages.AssistantInfoProvider
	br := func(label, detail string) string {
		return fmt.Sprintf("%s %s", dim.Render(label), detail)
	}

	checkpoint := part.Checkpoint
	cp := []string{
		br("Goal & User Intent", fmt.Sprintf("· %d items", checkpoint.Goals)),
		br("Constraints", fmt.Sprintf("%d · Decisions %d · Dead Ends %d · Questions %d",
			checkpoint.Constraints, checkpoint.Decisions, checkpoint.DeadEnds, checkpoint.Questions)),
		br("Progress", fmt.Sprintf("✓ %d done · %d in progress · %d blocked",
			checkpoint.Done, checkpoint.InProgress, checkpoint.Blocked)),
		br("Next Action", fmt.Sprintf("· %d steps", checkpoint.NextActions)),
	}
	b.WriteString("├─ 📋 ")
	b.WriteString(dim.Render("Checkpoint"))
	b.WriteString("\n")
	for i, line := range cp {
		connector := "│  ├─ "
		if i == len(cp)-1 {
			connector = "│  └─ "
		}
		b.WriteString(connector)
		b.WriteString(line)
		b.WriteString("\n")
	}

	ledger := part.Ledger
	b.WriteString("├─ 🗂 ")
	b.WriteString(dim.Render("Ledger"))
	b.WriteString("\n")
	b.WriteString("│  └─ ")
	fmt.Fprintf(b, "%d instructions · %d errors · %d commands · %d files",
		ledger.Instructions, ledger.Errors, ledger.Commands, ledger.Files)
	b.WriteString("\n")

	extracts := fmt.Sprintf("%d/%d golden spans kept", part.ExtractsKeptBlocks, part.ExtractsTotalBlocks)
	if part.OlderLaneCompressed {
		extracts += " · older history re-compressed"
	}
	b.WriteString("├─ 📜 ")
	b.WriteString(dim.Render("Extracts"))
	b.WriteString("\n")
	b.WriteString("│  └─ ")
	b.WriteString(extracts)
	b.WriteString("\n")

	b.WriteString("├─ 📁 ")
	b.WriteString(dim.Render("Working set"))
	b.WriteString("\n")
	b.WriteString("│  └─ ")
	fmt.Fprintf(b, "%d files snapped", part.WorkingSetFiles)
	b.WriteString("\n")

	b.WriteString("├─ 🔍 ")
	b.WriteString(dim.Render("Recovery"))
	b.WriteString("\n")
	b.WriteString("│  └─ ")
	fmt.Fprintf(b, "seq %d–%d · %d messages compacted",
		part.SeqStart, part.SeqEnd, part.CompactedMessages)
	b.WriteString("\n")

	b.WriteString("└─ ")
	b.WriteString(dim.Render(fmt.Sprintf("recall_grep / recall_expand ready · summary %s", part.SummaryID)))
	b.WriteString("\n")
}

// compactionLevelName maps an escalation level to its display name.
func compactionLevelName(level int) string {
	switch level {
	case 0:
		return "preserve details"
	case 1:
		return "bullet points"
	case 2:
		return "deterministic"
	}
	return ""
}
