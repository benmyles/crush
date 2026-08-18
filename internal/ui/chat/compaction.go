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

var _ MessageItem = (*CompactionMessageItem)(nil)

// CompactionMessageItem renders the structured digest of an engine-produced
// compaction summary as an always-expanded tree: checkpoint counts, ledger
// tallies, extracts, working set, and the recovery note (Option A of the
// overview designs). It replaces the plain assistant render for summary
// messages that carry a CompactionContent part.
type CompactionMessageItem struct {
	*list.Versioned
	sty  *styles.Styles
	msg  *message.Message
	part message.CompactionContent
}

// NewCompactionMessageItem builds a compaction overview item. The item is
// immutable after construction, so it reports Finished immediately and the
// F6 list cache can freeze it.
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

// Render renders the overview tree inside a section shell.
func (c *CompactionMessageItem) Render(width int) string {
	return common.Section(c.sty, c.tree(width), width)
}

// RawRender renders the tree without the section shell.
func (c *CompactionMessageItem) RawRender(width int) string {
	return c.tree(width)
}

// tree builds the mock-A layout: a header line with model, level, and token
// counts, then one branch per lane with leaf counts.
func (c *CompactionMessageItem) tree(width int) string {
	label := c.sty.Messages.AssistantInfoModel
	dim := c.sty.Messages.AssistantInfoProvider
	part := c.part

	levelName := fmt.Sprintf("level %d", part.Level)
	if name := compactionLevelName(part.Level); name != "" {
		levelName = name
	}
	header := strings.Join([]string{
		"⚡ Compaction complete",
		fmt.Sprintf("· %s", part.ModelID),
		fmt.Sprintf("· %s", levelName),
		fmt.Sprintf("· %d tokens", part.TokenCount),
	}, " ")
	header = ansi.Truncate(header, width, "…")

	br := func(label, detail string) string {
		return fmt.Sprintf("%s %s", dim.Render(label), detail)
	}

	var b strings.Builder
	b.WriteString("▾ ")
	b.WriteString(label.Render(header))
	b.WriteString("\n")
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
	b.WriteString(fmt.Sprintf("%d instructions · %d errors · %d commands · %d files",
		ledger.Instructions, ledger.Errors, ledger.Commands, ledger.Files))
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
	b.WriteString(fmt.Sprintf("%d files snapped", part.WorkingSetFiles))
	b.WriteString("\n")

	b.WriteString("├─ 🔍 ")
	b.WriteString(dim.Render("Recovery"))
	b.WriteString("\n")
	b.WriteString("│  └─ ")
	b.WriteString(fmt.Sprintf("seq %d–%d · %d messages compacted",
		part.SeqStart, part.SeqEnd, part.CompactedMessages))
	b.WriteString("\n")

	b.WriteString("└─ ")
	b.WriteString(dim.Render(fmt.Sprintf("recall_grep / recall_expand ready · summary %s", part.SummaryID)))
	b.WriteString("\n")

	return strings.TrimRight(b.String(), "\n")
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
