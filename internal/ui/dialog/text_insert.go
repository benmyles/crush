package dialog

import "charm.land/bubbles/v2/textinput"

func insertIntoTextInput(input *textinput.Model, text string) {
	value := []rune(input.Value())
	pos := input.Position()
	if pos < 0 {
		pos = 0
	}
	if pos > len(value) {
		pos = len(value)
	}
	insert := []rune(text)
	updated := make([]rune, 0, len(value)+len(insert))
	updated = append(updated, value[:pos]...)
	updated = append(updated, insert...)
	updated = append(updated, value[pos:]...)
	input.SetValue(string(updated))
	input.SetCursor(pos + len(insert))
}
