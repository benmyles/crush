package model

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testStyles returns the base theme for render-pipeline assertions.
func testStyles() *styles.Styles {
	s := styles.CharmtonePantera()
	return &s
}

func TestAgentsPanelLifecycle(t *testing.T) {
	t.Parallel()
	p := NewAgentsPanel(nil)

	// Empty panel is invisible, height zero.
	assert.False(t, p.Visible())
	assert.Equal(t, 0, p.Height())
	assert.Equal(t, 0, p.RunningCount())

	p.Register("tc-1", "sess-1", "agent", "do the thing")
	assert.True(t, p.Visible())
	assert.Equal(t, 1, p.Len())
	assert.Equal(t, 1, p.RunningCount())
	// Header + 1 row + footer.
	assert.Equal(t, 3, p.Height())

	// Registering again enriches but does not duplicate.
	p.Register("tc-1", "sess-1", "agent", "better prompt")
	assert.Equal(t, 1, p.Len())
	assert.Equal(t, "better prompt", p.entries[0].prompt)

	// Done rows linger, keep the panel visible, then prune.
	p.MarkDone("tc-1")
	assert.Equal(t, 1, p.Len())
	assert.Equal(t, 0, p.RunningCount())
	assert.True(t, p.Visible())
	assert.True(t, p.Prune(time.Now().Add(agentDoneLinger+time.Second)))
	assert.False(t, p.Visible())
	assert.Equal(t, 0, p.Height())
}

func TestAgentsPanelCancelKeepsTerminalStatus(t *testing.T) {
	t.Parallel()
	p := NewAgentsPanel(nil)
	p.Register("tc-1", "sess-1", "agent", "x")
	p.MarkCanceled("tc-1")
	// A late tool result must not resurrect the row into "done".
	p.MarkDone("tc-1")
	require.Len(t, p.entries, 1)
	assert.Equal(t, agentStatusCanceled, p.entries[0].status)
	// Canceled rows are skipped by selection wrapping.
	p.SelectNext()
	assert.Equal(t, agentStatusCanceled, p.Selected().status)
	p.SelectPrev()
	assert.Equal(t, agentStatusCanceled, p.Selected().status)
}

func TestAgentsPanelRowCapAndWindow(t *testing.T) {
	t.Parallel()
	p := NewAgentsPanel(nil)
	for i := range 6 {
		p.Register("tc-"+string(rune('0'+i)), "sess", "agent", "prompt")
	}
	// Height caps at header + 3 rows + footer.
	assert.Equal(t, 5, p.Height())
	assert.Equal(t, 6, p.Len())

	// Selection window scrolls with the cursor, staying in bounds.
	p.selected = 5
	start, end := p.scrollWindow()
	assert.Equal(t, 3, start)
	assert.Equal(t, 6, end)
	p.selected = 0
	start, end = p.scrollWindow()
	assert.Equal(t, 0, start)
	assert.Equal(t, 3, end)

	// Prune everything at once keeps selection clamped.
	for _, e := range p.entries {
		e.status = agentStatusDone
	}
	assert.True(t, p.Prune(time.Now().Add(agentDoneLinger+time.Second)))
	assert.Equal(t, 0, p.Len())
	assert.False(t, p.Visible())
}

func TestAgentsPanelComposeInput(t *testing.T) {
	t.Parallel()
	p := NewAgentsPanel(nil)
	p.Register("tc-1", "sess-1", "agent", "x")
	p.Focus()
	p.StartCompose()
	assert.True(t, p.Composing())
	p.ComposeAppend('h')
	p.ComposeAppend('i')
	assert.Equal(t, "hi", p.ComposeValue())
	p.ComposeBackspace()
	assert.Equal(t, "h", p.ComposeValue())
	p.CancelCompose()
	assert.False(t, p.Composing())
	assert.Equal(t, "", p.ComposeValue())
}

func TestAgentsPanelRenderIncludesRowsAndCounts(t *testing.T) {
	t.Parallel()
	p := NewAgentsPanel(testStyles())
	p.Register("tc-1", "sess-1", "agent", "prompt one")
	p.Register("tc-2", "sess-2", "agentic_fetch", "prompt two")
	p.SetActivity("tc-1", "bash", 1200)
	p.MarkDone("tc-2")

	view := p.Render(80)
	lines := strings.Split(view, "\n")
	assert.Len(t, lines, 4) // header + 2 rows + footer

	assert.Contains(t, view, "1 running")
	assert.Contains(t, view, "1 done")
	assert.Contains(t, view, "bash")
	assert.Contains(t, view, "✓")
	assert.Contains(t, view, "agentic_fetch")
	// Footer hints present.
	assert.Contains(t, view, "message")
	assert.Contains(t, view, "cancel")
}

func TestAgentsPanelComposeRenderCursor(t *testing.T) {
	t.Parallel()
	p := NewAgentsPanel(testStyles())
	p.Register("tc-1", "sess-1", "agent", "x")
	p.Focus()
	p.StartCompose()
	p.ComposeAppend('a')
	p.ComposeAppend('b')

	view := p.Render(80)
	assert.Contains(t, view, "msg")
	assert.Contains(t, view, "ab")
}

func TestAgentsPanelActivityIgnoresTerminal(t *testing.T) {
	t.Parallel()
	p := NewAgentsPanel(nil)
	p.Register("tc-1", "sess-1", "agent", "x")
	p.MarkDone("tc-1")
	p.SetActivity("tc-1", "bash", 10)
	assert.Equal(t, agentStatusDone, p.entries[0].status)
	assert.Equal(t, "", p.entries[0].currentTool)
}

func TestAgentsPanelUsageAndDoing(t *testing.T) {
	t.Parallel()
	p := NewAgentsPanel(testStyles())
	p.Register("tc-1", "sess-1", "agent", "x")
	p.SetDoing("tc-1", "Running bash")
	p.SetUsage("tc-1", 300, 700)
	p.AddOutput("tc-1", 300)
	p.SetActivity("tc-1", "bash", 1200)

	view := p.Render(80)
	assert.Contains(t, view, "Running bash")
	assert.Contains(t, view, "1.0k tok")

	// Awaiting-status text renders once the next tool result arrives.
	p.SetDoing("tc-1", "Awaiting model")
	view = p.Render(80)
	assert.Contains(t, view, "Awaiting model")

	// Terminal rows ignore late usage and doing updates.
	p.MarkDone("tc-1")
	p.SetDoing("tc-1", "Running grep")
	p.SetUsage("tc-1", 5, 5)
	assert.Equal(t, "Awaiting model", p.entries[0].doing)
	assert.Equal(t, int64(1000), p.entries[0].tokens)
}

func TestAgentsPanelRetryCountdown(t *testing.T) {
	t.Parallel()
	p := NewAgentsPanel(testStyles())
	p.Register("tc-1", "sess-1", "agent", "x")
	p.SetActivity("tc-1", "bash", 1200)

	// A retry notice arms the backoff countdown and hides the doing text.
	p.SetRetry("tc-1", 30*time.Second)
	view := p.Render(80)
	assert.Contains(t, view, "retrying in")
	assert.NotContains(t, view, "bash")

	// Fresh activity (streamed output, tool calls, usage) clears it.
	p.AddOutput("tc-1", 10)
	view = p.Render(80)
	assert.NotContains(t, view, "retrying in")

	// Terminal rows ignore retry notices.
	p.SetRetry("tc-1", time.Second)
	p.MarkDone("tc-1")
	view = p.Render(80)
	assert.NotContains(t, view, "retrying in")
}
