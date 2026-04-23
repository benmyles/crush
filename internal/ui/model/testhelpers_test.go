package model

import tea "charm.land/bubbletea/v2"

func collectCommandMessages(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	switch msg := msg.(type) {
	case tea.BatchMsg:
		var msgs []tea.Msg
		for _, batched := range msg {
			msgs = append(msgs, collectCommandMessages(batched)...)
		}
		return msgs
	default:
		return []tea.Msg{msg}
	}
}
