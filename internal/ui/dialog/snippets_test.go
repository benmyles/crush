package dialog

import (
	"image"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/ui/common"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestSnippetsSearchMatchesBodyText(t *testing.T) {
	t.Parallel()

	snippets := NewSnippets(common.DefaultCommon(nil), []ScopedSnippet{
		{
			Scope: config.ScopeGlobal,
			Index: 0,
			Snippet: config.Snippet{
				Title: "Review",
				Body:  "Check correctness and tests.",
			},
		},
		{
			Scope: config.ScopeWorkspace,
			Index: 0,
			Snippet: config.Snippet{
				Title: "Deploy",
				Body:  "Include rollback instructions.",
			},
		},
	})

	action := snippets.HandleMsg(tea.PasteMsg{Content: "rollback"})
	require.Nil(t, action)
	require.Len(t, snippets.list.FilteredItems(), 1)

	action = snippets.HandleMsg(keyCode(tea.KeyEnter))
	selected, ok := action.(ActionSnippetSelected)
	require.True(t, ok)
	require.Equal(t, "Deploy", selected.Snippet.Title)
}

func TestSnippetEditorSavesInlineSnippet(t *testing.T) {
	t.Parallel()

	editor := NewSnippetEditor(common.DefaultCommon(nil), config.ScopeWorkspace, -1, config.Snippet{})
	action := editor.HandleMsg(tea.PasteMsg{Content: "Review checklist"})
	require.Nil(t, action)

	action = editor.HandleMsg(keyCode(tea.KeyEnter))
	require.Nil(t, action)
	require.Equal(t, snippetEditorFocusBody, editor.focus)

	action = editor.HandleMsg(tea.PasteMsg{Content: "Check correctness and missing tests."})
	require.Nil(t, action)

	action = editor.HandleMsg(keyCode(tea.KeyTab))
	require.Nil(t, action)
	require.Equal(t, snippetEditorFocusScope, editor.focus)

	action = editor.HandleMsg(keyCode(tea.KeySpace))
	require.Nil(t, action)
	require.Equal(t, config.ScopeGlobal, editor.scope)

	action = editor.HandleMsg(keyCode(tea.KeyTab))
	require.Nil(t, action)
	require.Equal(t, snippetEditorFocusActions, editor.focus)

	action = editor.HandleMsg(keyCode(tea.KeyEnter))
	saved, ok := action.(ActionSnippetSaved)
	require.True(t, ok)
	require.Equal(t, config.ScopeWorkspace, saved.OriginalScope)
	require.Equal(t, config.ScopeGlobal, saved.Scope)
	require.Equal(t, "Review checklist", saved.Snippet.Title)
	require.Equal(t, "Check correctness and missing tests.", saved.Snippet.Body)
}

func TestSnippetEditorInsertTextTargetsBody(t *testing.T) {
	t.Parallel()

	editor := NewSnippetEditor(common.DefaultCommon(nil), config.ScopeWorkspace, -1, config.Snippet{
		Title: "Combined",
		Body:  "Before ",
	})

	cmd := editor.InsertText("after")
	require.Nil(t, cmd)
	require.Equal(t, snippetEditorFocusBody, editor.focus)
	require.Equal(t, "Before after", editor.body.Value())
}

func TestSnippetEditorTitleCursorIsOnInputLine(t *testing.T) {
	t.Parallel()

	editor := NewSnippetEditor(common.DefaultCommon(nil), config.ScopeWorkspace, -1, config.Snippet{})
	scr := uv.NewScreenBuffer(120, 50)
	cur := editor.Draw(scr, image.Rect(0, 0, 120, 50))

	require.NotNil(t, cur)
	require.Contains(t, cursorLine(t, scr.String(), cur), "Snippet title")
}

func cursorLine(t *testing.T, view string, cur *tea.Cursor) string {
	t.Helper()

	lines := strings.Split(ansi.Strip(view), "\n")
	require.GreaterOrEqual(t, cur.Y, 0)
	require.Less(t, cur.Y, len(lines))
	return lines[cur.Y]
}
