package dialog

import (
	"image"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/userquestion"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/stretchr/testify/require"
)

func TestUserQuestionDrawReturnsInputCursor(t *testing.T) {
	t.Parallel()

	question := NewUserQuestion(common.DefaultCommon(nil), userquestion.Request{
		Question: "How should Crush handle long output?",
		Choices: []userquestion.Choice{
			{ID: "short", Label: "Short"},
			{ID: "full", Label: "Full"},
		},
	})
	question.input.SetValue("Use the full output")

	scr := uv.NewScreenBuffer(100, 40)
	cur := question.Draw(scr, image.Rect(0, 0, 100, 40))

	require.NotNil(t, cur)
	require.GreaterOrEqual(t, cur.X, 0)
	require.GreaterOrEqual(t, cur.Y, 0)
	require.Less(t, cur.X, 100)
	require.Less(t, cur.Y, 40)
}

func TestUserQuestionAcceptsPaste(t *testing.T) {
	t.Parallel()

	question := NewUserQuestion(common.DefaultCommon(nil), userquestion.Request{
		Question: "How should Crush handle long output?",
		Choices: []userquestion.Choice{
			{ID: "short", Label: "Short"},
			{ID: "full", Label: "Full"},
		},
	})

	action := question.HandleMsg(tea.PasteMsg{Content: "Use the full output"})
	require.Nil(t, action)
	require.Equal(t, "Use the full output", question.input.Value())

	action = question.HandleMsg(tea.KeyPressMsg(tea.Key{Code: 'v', Mod: tea.ModCtrl}))
	_, ok := action.(ActionCmd)
	require.True(t, ok)

	action = question.HandleMsg(tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))
	_, ok = action.(ActionCmd)
	require.True(t, ok)
}

func TestUserQuestionInsertText(t *testing.T) {
	t.Parallel()

	question := NewUserQuestion(common.DefaultCommon(nil), userquestion.Request{
		Question: "How should Crush handle long output?",
	})

	cmd := question.InsertText("Use the full output")
	require.Nil(t, cmd)
	require.Equal(t, "Use the full output", question.input.Value())
}
