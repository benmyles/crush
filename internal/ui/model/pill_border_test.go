package model

import (
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/x/ansi"
)

// roundedBorderRunes are chars that only appear when a pill has a visible
// rounded border.
const roundedBorderRunes = "╭╮╰╯"

func hasRoundedBorder(s string) bool {
	return strings.ContainsAny(s, roundedBorderRunes)
}

// queuePillHasBorder reports whether the "N Queued" pill is wrapped in a
// rounded border by checking the line directly above the queue label for a
// top border corner.
func queuePillHasBorder(view string) bool {
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		if !strings.Contains(line, "Queued") {
			continue
		}
		if i == 0 {
			return false
		}
		return strings.ContainsAny(lines[i-1], "╭╮")
	}
	return false
}

// TestQueuePillAlwaysHasBorder guards CHARM-1678: the queued-prompts pill must
// render with its rounded border regardless of panel expansion or which pill
// section is nominally focused.
func TestQueuePillAlwaysHasBorder(t *testing.T) {
	incompleteTodos := []session.Todo{{Content: "a", Status: session.TodoStatusPending}}

	cases := []struct {
		name           string
		expanded       bool
		focusedSection pillSection
		todos          []session.Todo
		queue          int
	}{
		{"collapsed only queue", false, pillSectionTodos, nil, 2},
		{"collapsed queue+todos", false, pillSectionTodos, incompleteTodos, 2},
		{"expanded queue focused", true, pillSectionQueue, nil, 2},
		{"expanded stale todos focus only queue", true, pillSectionTodos, nil, 2},
		{"expanded todos focused queue+todos", true, pillSectionTodos, incompleteTodos, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u := newTestUI()
			u.session = &session.Session{ID: "s1", Todos: tc.todos}
			u.promptQueue = tc.queue
			u.pillsExpanded = tc.expanded
			u.focusedPillSection = tc.focusedSection
			u.updateLayoutAndSize()
			u.renderPills()

			if !hasRoundedBorder(u.pillsView) {
				t.Fatalf("expected a rounded border somewhere in pills view:\n%s", u.pillsView)
			}
			if !queuePillHasBorder(u.pillsView) {
				t.Fatalf("expected the queue pill to have a border:\n%s", u.pillsView)
			}
		})
	}
}

// TestEffectiveFocusedSectionFallsThrough verifies that a stale focused section
// (pointing at a section with no content) resolves to the section that still
// has content, so the expanded list stays populated.
func TestEffectiveFocusedSectionFallsThrough(t *testing.T) {
	cases := []struct {
		name     string
		stored   pillSection
		todos    []session.Todo
		queue    int
		expected pillSection
	}{
		{"todos focus but only queue", pillSectionTodos, nil, 2, pillSectionQueue},
		{"queue focus but only todos", pillSectionQueue, []session.Todo{{Content: "a", Status: session.TodoStatusPending}}, 0, pillSectionTodos},
		{"todos focus with todos", pillSectionTodos, []session.Todo{{Content: "a", Status: session.TodoStatusPending}}, 2, pillSectionTodos},
		{"queue focus with queue", pillSectionQueue, nil, 2, pillSectionQueue},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u := newTestUI()
			u.session = &session.Session{ID: "s1", Todos: tc.todos}
			u.promptQueue = tc.queue
			u.focusedPillSection = tc.stored
			if got := u.effectiveFocusedSection(); got != tc.expected {
				t.Fatalf("effectiveFocusedSection() = %d, want %d", got, tc.expected)
			}
		})
	}
}

// TestCompactPillLabelWidthIsStableAcrossFrames pins the resize-bug fix: the
// label must not grow/shrink as the pulse animates (the old suffix-dot
// animation changed width every frame and made the pill reflow).
func TestCompactPillLabelWidthIsStableAcrossFrames(t *testing.T) {
	u := newTestUI()
	widths := make(map[int]struct{})
	for frame := range 64 {
		pill := compactPill(frame, 0, u.com.Styles)
		widths[ansi.StringWidth(ansi.Strip(pill))] = struct{}{}
	}
	if len(widths) != 1 {
		t.Fatalf("compact pill width must not change across animation frames, got widths %v", widths)
	}
}

// TestCompactPillAlwaysHasBorder guards the compaction pulse pill: while the
// engine runs, the "Compacting" pill must render with its rounded border just
// like the queue pill, and disappears again once the run finishes.
func TestCompactPillAlwaysHasBorder(t *testing.T) {
	u := newTestUI()
	u.session = &session.Session{ID: "s1"}
	u.compacting = true
	u.updateLayoutAndSize()
	u.renderPills()

	if !strings.Contains(u.pillsView, "Compacting") {
		t.Fatalf("expected the Compacting pill while compacting:\n%s", u.pillsView)
	}
	if !hasRoundedBorder(u.pillsView) {
		t.Fatalf("expected a rounded border somewhere in pills view:\n%s", u.pillsView)
	}

	u.compacting = false
	u.renderPills()
	if strings.Contains(u.pillsView, "Compacting") {
		t.Fatalf("the Compacting pill must clear when the run finishes:\n%s", u.pillsView)
	}
}

// TestCompactPillShowsLiveTokenStats verifies the pulse pill appends the
// live "↓ N" token stats once the engine publishes progress, in the range
// pill style the user asked for (e.g. "↓ 34K").
func TestCompactPillShowsLiveTokenStats(t *testing.T) {
	u := newTestUI()
	u.session = &session.Session{ID: "s1"}
	u.compacting = true
	u.compactTokensDown = 34_000
	u.updateLayoutAndSize()
	u.renderPills()

	if !strings.Contains(u.pillsView, "Compacting · ↓ 34K") {
		t.Fatalf("expected live token stats in the pill:\n%s", u.pillsView)
	}

	u.compactTokensDown = 0
	u.renderPills()
	if strings.Contains(u.pillsView, "↓") {
		t.Fatalf("no stats suffix without progress data:\n%s", u.pillsView)
	}
}

// TestShortTokens pins the compact token formatting.
func TestShortTokens(t *testing.T) {
	cases := map[int64]string{
		0:         "0",
		512:       "512",
		999:       "999",
		1_000:     "1.0K",
		3_456:     "3.5K",
		9_499:     "9.5K",
		9_500:     "10K",
		34_000:    "34K",
		123_456:   "123K",
		999_949:   "1000K",
		1_000_000: "1.0M",
		1_234_567: "1.2M",
	}
	for in, want := range cases {
		if got := shortTokens(in); got != want {
			t.Errorf("shortTokens(%d) = %q, want %q", in, got, want)
		}
	}
}
