package model

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/stretchr/testify/require"
)

// TestEndFollowKeyReengagesFollow verifies both follow shortcuts
// (ctrl+x and ctrl+end) scroll back to the live tail and re-engage
// follow mode after the user scrolled up.
func TestEndFollowKeyReengagesFollow(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		code rune
	}{
		{"ctrl+x", 'x'},
		{"ctrl+end", tea.KeyEnd},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			u := newTestUI()
			u.dialog = dialog.NewOverlay()
			u.keyMap = DefaultKeyMap()
			u.session = &session.Session{ID: "s1"}
			u.focus = uiFocusMain

			// Scrolling up leaves follow mode.
			_ = u.chat.ScrollBy(-20)
			require.False(t, u.chat.Follow(), "scrolling up must leave follow mode")

			keyMsg := tea.KeyPressMsg{Code: tc.code, Mod: tea.ModCtrl}
			require.Equal(t, tc.name, keyMsg.String())

			_ = u.handleKeyPressMsg(keyMsg)
			require.True(t, u.chat.Follow(), "follow shortcut must re-engage follow mode")
			require.True(t, u.chat.AtBottom(), "follow shortcut must scroll back to the bottom")
		})
	}
}
