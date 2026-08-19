package chat

import (
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/anim"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
)

// LiveCompactionMessageID is the chat-list id of the transient message that
// streams a running compaction's checkpoint generation. It is a UI-only item:
// nothing is persisted under this id.
const LiveCompactionMessageID = "live-compaction-message"

var (
	_ MessageItem = (*CompactionLiveItem)(nil)
	_ Animatable  = (*CompactionLiveItem)(nil)
	_ Expandable  = (*CompactionLiveItem)(nil)
)

// CompactionLiveItem is the transient assistant item that streams the
// compaction model's checkpoint reasoning and text into the chat while the
// engine runs, exactly like a normal assistant turn. It renders inside a
// purple frame (Messages.CompactionLiveBox) so the compaction reads as its
// own thing, and its "waiting" spinner pulses on the compaction gradient with
// a "Compacting" label. The item is removed when the engine finishes and the
// real summary message replaces it.
type CompactionLiveItem struct {
	*AssistantMessageItem
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

// Render draws the streaming assistant content inside the purple compaction
// frame.
func (c *CompactionLiveItem) Render(width int) string {
	inner := c.AssistantMessageItem.Render(width)
	if inner == "" {
		return ""
	}
	return c.sty.Messages.CompactionLiveBox.Width(width).Render(inner)
}
