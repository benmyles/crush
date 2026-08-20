package chat

import (
	"strings"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/anim"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/list"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
)

// LiveCompactionMessageID is the chat-list id of the transient message that
// streams a running compaction's checkpoint generation. It is a UI-only item:
// nothing is persisted under this id.
const LiveCompactionMessageID = "live-compaction-message"

var (
	_ MessageItem         = (*CompactionLiveItem)(nil)
	_ Animatable          = (*CompactionLiveItem)(nil)
	_ Expandable          = (*CompactionLiveItem)(nil)
	_ list.MouseClickable = (*CompactionLiveItem)(nil)
)

// CompactionLiveItem is the transient assistant item that streams the
// compaction model's checkpoint reasoning and text into the chat while the
// engine runs. The streaming body is collapsed by default so the
// always-large checkpoint does not flood the chat; press space or click
// to watch the live generation. It renders inside a purple frame
// (Messages.CompactionLiveBox) so the compaction reads as its own thing,
// and its "waiting" spinner pulses on the compaction gradient with a
// "Compacting" label. The item is removed when the engine finishes and the
// real summary message replaces it.
type CompactionLiveItem struct {
	*AssistantMessageItem

	// expanded is false by default: the checkpoint streams behind a
	// one-line preview until the user expands the item.
	expanded bool
}

// NewCompactionLiveItem builds a live compaction item around the given
// (empty, streaming) message.
func NewCompactionLiveItem(sty *styles.Styles, msg *message.Message) *CompactionLiveItem {
	inner := NewAssistantMessageItem(sty, msg).(*AssistantMessageItem)
	inner.liveCompaction = true
	inner.anim = anim.New(anim.Settings{
		ID:          inner.ID(),
		Size:        15,
		GradColorA:  sty.CompactGradFromColor,
		GradColorB:  sty.CompactGradToColor,
		LabelColor:  sty.WorkingLabelColor,
		CycleColors: true,
		Suffix: func() string {
			return common.Elapsed()
		},
		SuffixColor: sty.WorkingTimerColor,
	})
	return &CompactionLiveItem{AssistantMessageItem: inner}
}

// ToggleExpanded flips the collapsed state and bumps the render version so
// the list cache repaints the item. It shadows the assistant thinking-view
// toggle, which must not apply to the live compaction item.
func (c *CompactionLiveItem) ToggleExpanded() bool {
	c.expanded = !c.expanded
	c.Bump()
	return c.expanded
}

// HandleMouseClick implements list.MouseClickable. The whole collapsed line
// is the expand toggle, so any left click toggles via the Expandable path.
func (c *CompactionLiveItem) HandleMouseClick(btn ansi.MouseButton, x, y int) bool {
	return btn == ansi.MouseLeft
}

// Render draws the collapsed one-liner or the streaming assistant content
// inside the purple compaction frame.
func (c *CompactionLiveItem) Render(width int) string {
	if !c.expanded {
		return c.sty.Messages.CompactionLiveBox.Width(width).Render(c.renderCollapsed(width))
	}
	inner := c.AssistantMessageItem.Render(width)
	if inner == "" {
		return ""
	}
	return c.sty.Messages.CompactionLiveBox.Width(width).Render(inner)
}

// RawRender mirrors Render without the outer frame.
func (c *CompactionLiveItem) RawRender(width int) string {
	if !c.expanded {
		return c.renderCollapsed(width)
	}
	return c.AssistantMessageItem.RawRender(width)
}

// renderCollapsed draws the single-line collapsed view. Before any text
// arrives the item stays as the working spinner; once the checkpoint
// streams, only the first line previews with an expand hint.
func (c *CompactionLiveItem) renderCollapsed(width int) string {
	inner := c.AssistantMessageItem.Render(width)
	if inner == "" {
		return ""
	}
	text := strings.TrimSpace(c.message.Content().Text)
	if text == "" {
		// Still reasoning: the spinner line is already compact.
		return inner
	}

	label := c.sty.Messages.AssistantInfoModel
	dim := c.sty.Messages.AssistantInfoProvider
	prefix := "▸ "
	suffix := dim.Render(" · space to expand")
	head, _, _ := strings.Cut(text, "\n")
	head = ansi.Truncate(
		label.Render(strings.TrimSpace(head)),
		max(0, width-ansi.StringWidth(prefix)-ansi.StringWidth(suffix)),
		"…",
	)
	return ansi.Truncate(prefix+head+suffix, width, "…")
}
