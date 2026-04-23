package dialog

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/planning"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/common"
	xansi "github.com/charmbracelet/x/ansi"
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

func TestPlanApprovalCompactRenderShowsClearCheckboxStates(t *testing.T) {
	t.Parallel()

	plan := NewPlanApproval(common.DefaultCommon(nil), planning.Submission{
		Markdown: "## Plan\n\nDo the thing.",
	})

	checked := xansi.Strip(plan.renderCompact(80))
	require.Contains(t, checked, "[✓] Compact before starting")

	plan.HandleMsg(keyCode(tea.KeyTab))
	plan.HandleMsg(keyCode(tea.KeySpace))

	unchecked := xansi.Strip(plan.renderCompact(80))
	require.Contains(t, unchecked, "[ ] Compact before starting")
	require.NotContains(t, unchecked, "[✓]")
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

func TestPlanApprovalCommentsAcceptPaste(t *testing.T) {
	t.Parallel()

	plan := NewPlanApproval(common.DefaultCommon(nil), planning.Submission{
		Markdown: "## Plan\n\nDo the thing.",
	})

	action := plan.HandleMsg(tea.PasteMsg{Content: "Please keep the full output."})
	require.Nil(t, action)
	require.Equal(t, "Please keep the full output.", plan.comments.Value())

	action = plan.HandleMsg(tea.KeyPressMsg(tea.Key{Code: 'v', Mod: tea.ModCtrl}))
	_, ok := action.(ActionCmd)
	require.True(t, ok)

	action = plan.HandleMsg(tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))
	_, ok = action.(ActionCmd)
	require.True(t, ok)
}

func TestPlanApprovalInsertTextTargetsComments(t *testing.T) {
	t.Parallel()

	plan := NewPlanApproval(common.DefaultCommon(nil), planning.Submission{
		Markdown: "## Plan",
	})

	cmd := plan.InsertText("Looks good")
	require.Nil(t, cmd)
	require.Equal(t, planApprovalFocusComments, plan.focus)
	require.Equal(t, "Looks good", plan.comments.Value())
}

func TestPlanApprovalCopiesFullPlanFromActionButton(t *testing.T) {
	t.Parallel()

	plan := NewPlanApproval(common.DefaultCommon(nil), planning.Submission{
		Markdown: "## Plan\n\nDo the thing.",
		Todos: []session.Todo{{
			Content:    "Do the thing",
			Status:     session.TodoStatusInProgress,
			ActiveForm: "Doing the thing",
		}},
	})

	plan.HandleMsg(keyCode(tea.KeyTab)) // Comments to compact.
	plan.HandleMsg(keyCode(tea.KeyTab)) // Compact to actions.
	plan.HandleMsg(keyCode(tea.KeyRight))
	plan.HandleMsg(keyCode(tea.KeyRight))
	require.Equal(t, 2, plan.selected)
	require.Contains(t, xansi.Strip(plan.renderButtons()), "Copy Plan")

	action := plan.HandleMsg(keyCode(tea.KeyEnter))
	_, ok := action.(ActionCmd)
	require.True(t, ok)
	require.Contains(t, plan.planClipboardText(), "## Plan")
	require.Contains(t, plan.planClipboardText(), "## Tasks")
	require.Contains(t, plan.planClipboardText(), "- [~] Do the thing (Doing the thing)")
}

func keyPress(text string) tea.KeyPressMsg {
	runes := []rune(text)
	return tea.KeyPressMsg(tea.Key{Text: text, Code: runes[0]})
}

func keyCode(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code})
}
