package prompt

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCriticalInstructionsBlockFormatsInstructions(t *testing.T) {
	t.Parallel()

	block := CriticalInstructionsBlock(" cite sources \n\nDo not truncate.")

	require.Contains(t, block, "<critical_instructions>")
	require.Contains(t, block, "<instruction>\ncite sources \n\nDo not truncate.\n</instruction>")
}

func TestAppendCriticalInstructionReminder(t *testing.T) {
	t.Parallel()

	text := AppendCriticalInstructionReminder("Hello", "Use concise answers.")

	require.True(t, strings.HasPrefix(text, "Hello\n\n"))
	require.Contains(t, text, "<critical_instruction_reminder>")
	require.Contains(t, text, "Use concise answers.")
}
