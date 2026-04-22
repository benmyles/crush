package dialog

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/planning"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/stretchr/testify/require"
)

func TestPlanApprovalCommentsDoNotTriggerResponses(t *testing.T) {
	t.Parallel()

	plan := NewPlanApproval(common.DefaultCommon(nil), planning.Submission{
		Markdown: "## Plan\n\nDo the thing.",
	})

	for _, msg := range []tea.KeyPressMsg{
		keyPress("y"),
		keyPress("n"),
		keyPress("K"),
		keyPress("J"),
		keyCode(tea.KeyEnter),
	} {
		action := plan.HandleMsg(msg)
		_, responded := action.(ActionPlanApprovalResponse)
		require.False(t, responded)
	}
	require.Contains(t, plan.comments.Value(), "y")
	require.Contains(t, plan.comments.Value(), "n")
	require.Contains(t, plan.comments.Value(), "K")
	require.Contains(t, plan.comments.Value(), "J")
	require.Contains(t, plan.comments.Value(), "\n")

	// Tab from Comments → Compact.
	action := plan.HandleMsg(keyCode(tea.KeyTab))
	require.Nil(t, action)
	require.Equal(t, planApprovalFocusCompact, plan.focus)

	// Tab from Compact → Actions.
	action = plan.HandleMsg(keyCode(tea.KeyTab))
	require.Nil(t, action)
	require.Equal(t, planApprovalFocusActions, plan.focus)

	action = plan.HandleMsg(keyPress("y"))
	resp, ok := action.(ActionPlanApprovalResponse)
	require.True(t, ok)
	require.True(t, resp.Approved)
	require.Contains(t, resp.Comment, "yn")
}

func TestPlanApprovalCompactToggle(t *testing.T) {
	t.Parallel()

	plan := NewPlanApproval(common.DefaultCommon(nil), planning.Submission{
		Markdown: "## Plan\n\nDo the thing.",
	})

	// Default is checked when strategy is not disabled.
	require.True(t, plan.compactHistory)

	// Tab to compact focus.
	plan.HandleMsg(keyCode(tea.KeyTab))
	require.Equal(t, planApprovalFocusCompact, plan.focus)

	// Toggle off.
	plan.HandleMsg(keyCode(tea.KeySpace))
	require.False(t, plan.compactHistory)

	// Toggle back on.
	plan.HandleMsg(keyCode(tea.KeySpace))
	require.True(t, plan.compactHistory)

	// Approve with compact checked — CompactHistory should be true.
	plan.HandleMsg(keyCode(tea.KeyTab)) // Tab to Actions.
	resp := plan.HandleMsg(keyPress("y"))
	r, ok := resp.(ActionPlanApprovalResponse)
	require.True(t, ok)
	require.True(t, r.Approved)
	require.True(t, r.CompactHistory)
}

func TestPlanApprovalCompactHistoryOnlyOnApproval(t *testing.T) {
	t.Parallel()

	plan := NewPlanApproval(common.DefaultCommon(nil), planning.Submission{
		Markdown: "## Plan\n\nDo the thing.",
	})
	require.True(t, plan.compactHistory)

	// Tab to actions.
	plan.HandleMsg(keyCode(tea.KeyTab)) // Comments → Compact
	plan.HandleMsg(keyCode(tea.KeyTab)) // Compact → Actions

	// Revise with compact checked — CompactHistory should be false since not approved.
	resp := plan.HandleMsg(keyPress("n"))
	r, ok := resp.(ActionPlanApprovalResponse)
	require.True(t, ok)
	require.False(t, r.Approved)
	require.False(t, r.CompactHistory)
}

func keyPress(text string) tea.KeyPressMsg {
	runes := []rune(text)
	return tea.KeyPressMsg(tea.Key{Text: text, Code: runes[0]})
}

func keyCode(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code})
}
