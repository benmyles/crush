package chat

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/list"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
)

var (
	_ MessageItem         = (*SummaryMessageItem)(nil)
	_ Animatable          = (*SummaryMessageItem)(nil)
	_ Expandable          = (*SummaryMessageItem)(nil)
	_ list.MouseClickable = (*SummaryMessageItem)(nil)
)

// SummaryMessageItem renders a plain assistant summary message (the legacy
// summarize flow, which has no structured CompactionContent part). The full
// summary text is collapsed behind a one-line preview by default and only
// expands on click or the expand key, so summarization does not flood the
// chat with its always-large output.
type SummaryMessageItem struct {
	*AssistantMessageItem

	// expanded is false by default: the summary text hides behind a
	// one-line preview until the user expands the item.
	expanded bool
}

// NewSummaryMessageItem builds a collapsed summary item around the given
// message.
func NewSummaryMessageItem(sty *styles.Styles, msg *message.Message) *SummaryMessageItem {
	inner := NewAssistantMessageItem(sty, msg).(*AssistantMessageItem)
	return &SummaryMessageItem{AssistantMessageItem: inner}
}

// SetMessage forwards streaming updates to the inner assistant item, so the
// collapsed preview and (when expanded) the full text keep up with the live
// summary generation.
func (s *SummaryMessageItem) SetMessage(msg *message.Message) tea.Cmd {
	return s.AssistantMessageItem.SetMessage(msg)
}

// ToggleExpanded flips the collapsed state and bumps the render version so
// the list cache repaints the item. It shadows the assistant thinking-view
// toggle, which must not apply to the summary item.
func (s *SummaryMessageItem) ToggleExpanded() bool {
	s.expanded = !s.expanded
	s.Bump()
	return s.expanded
}

// HandleMouseClick implements list.MouseClickable. The whole collapsed line
// is the expand toggle, so any left click toggles via the Expandable path.
func (s *SummaryMessageItem) HandleMouseClick(btn ansi.MouseButton, x, y int) bool {
	return btn == ansi.MouseLeft
}

// Render draws the collapsed one-liner or the full assistant render.
func (s *SummaryMessageItem) Render(width int) string {
	if !s.expanded {
		return s.renderCollapsed(width)
	}
	return s.AssistantMessageItem.Render(width)
}

// RawRender mirrors Render without the focus prefix handling.
func (s *SummaryMessageItem) RawRender(width int) string {
	if !s.expanded {
		return s.renderCollapsed(width)
	}
	return s.AssistantMessageItem.RawRender(width)
}

// renderCollapsed draws the single-line collapsed view. Before any text
// arrives the item stays as the working spinner; once the summary streams,
// only the first line previews with an expand hint.
func (s *SummaryMessageItem) renderCollapsed(width int) string {
	inner := s.AssistantMessageItem.Render(width)
	if inner == "" {
		return ""
	}
	text := strings.TrimSpace(s.message.Content().Text)
	if text == "" {
		// Still generating: the spinner line is already compact.
		return inner
	}

	label := s.sty.Messages.AssistantInfoModel
	dim := s.sty.Messages.AssistantInfoProvider
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
