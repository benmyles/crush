package prompt

import "strings"

// NormalizeCriticalInstructions trims whitespace from critical instructions.
func NormalizeCriticalInstructions(instructions string) string {
	return strings.TrimSpace(instructions)
}

// CriticalInstructionsBlock formats critical instructions for system prompts.
func CriticalInstructionsBlock(instructions string) string {
	return criticalInstructionsXML("critical_instructions", instructions)
}

// CriticalInstructionReminderBlock formats critical instructions for user
// message reminders.
func CriticalInstructionReminderBlock(instructions string) string {
	return criticalInstructionsXML("critical_instruction_reminder", instructions)
}

// AppendCriticalInstructionReminder appends a reminder block to user text.
func AppendCriticalInstructionReminder(text string, instructions string) string {
	block := CriticalInstructionReminderBlock(instructions)
	if block == "" {
		return text
	}
	return strings.TrimSpace(text) + "\n\n" + block
}

func criticalInstructionsXML(tag string, instructions string) string {
	instructions = strings.TrimSpace(instructions)
	if instructions == "" {
		return ""
	}

	var b strings.Builder
	b.WriteString("<")
	b.WriteString(tag)
	b.WriteString(">\n")
	b.WriteString("These user-configured critical instructions apply to every agent and override lower-priority guidance.\n\n")
	b.WriteString("<instruction>\n")
	b.WriteString(instructions)
	b.WriteString("\n</instruction>\n")
	b.WriteString("</")
	b.WriteString(tag)
	b.WriteString(">")
	return b.String()
}
