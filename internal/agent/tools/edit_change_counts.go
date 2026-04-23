package tools

import "strings"

func editChangeCounts(oldContent, newContent string) (int, int) {
	oldLines := splitEditLines(oldContent)
	newLines := splitEditLines(newContent)

	prefix := 0
	for prefix < len(oldLines) && prefix < len(newLines) && oldLines[prefix] == newLines[prefix] {
		prefix++
	}

	suffix := 0
	for suffix < len(oldLines)-prefix && suffix < len(newLines)-prefix {
		oldLine := oldLines[len(oldLines)-1-suffix]
		newLine := newLines[len(newLines)-1-suffix]
		if oldLine != newLine {
			break
		}
		suffix++
	}

	return len(newLines) - prefix - suffix, len(oldLines) - prefix - suffix
}

func splitEditLines(content string) []string {
	if content == "" {
		return nil
	}

	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
