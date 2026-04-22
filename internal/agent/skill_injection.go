package agent

import (
	"fmt"
	"html"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/skills"
)

var explicitSkillTagPattern = regexp.MustCompile(`\$([A-Za-z0-9]+(?:-[A-Za-z0-9]+)*)`)

func (c *coordinator) attachExplicitSkillContents(prompt string, attachments []message.Attachment) []message.Attachment {
	matched := explicitSkills(prompt, c.activeSkills)
	if len(matched) == 0 {
		return attachments
	}

	result := append([]message.Attachment(nil), attachments...)
	for _, skill := range matched {
		content, err := readSkillContent(skill)
		if err != nil {
			slog.Warn("Failed to attach explicitly referenced skill", "skill", skill.Name, "path", skill.SkillFilePath, "error", err)
			continue
		}
		c.skillTracker.MarkLoaded(skill.Name)
		result = append(result, message.Attachment{
			FilePath: skill.SkillFilePath,
			FileName: filepath.Base(skill.SkillFilePath),
			MimeType: "text/markdown",
			Content:  attachedSkillContent(skill, content),
		})
	}
	return result
}

func explicitSkills(prompt string, activeSkills []*skills.Skill) []*skills.Skill {
	if len(activeSkills) == 0 {
		return nil
	}

	byName := make(map[string]*skills.Skill, len(activeSkills))
	for _, skill := range activeSkills {
		byName[skill.Name] = skill
	}

	names := explicitSkillNames(prompt)
	matched := make([]*skills.Skill, 0, len(names))
	for _, name := range names {
		if skill, ok := byName[name]; ok {
			matched = append(matched, skill)
		}
	}
	return matched
}

func explicitSkillNames(prompt string) []string {
	matches := explicitSkillTagPattern.FindAllStringSubmatchIndex(prompt, -1)
	names := make([]string, 0, len(matches))
	seen := make(map[string]bool, len(matches))
	for _, match := range matches {
		start, end := match[0], match[1]
		nameStart, nameEnd := match[2], match[3]
		if start > 0 && prompt[start-1] == '\\' {
			continue
		}
		if end < len(prompt) && isSkillTagContinuation(prompt[end]) {
			continue
		}
		name := prompt[nameStart:nameEnd]
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

func isSkillTagContinuation(b byte) bool {
	return (b >= 'A' && b <= 'Z') ||
		(b >= 'a' && b <= 'z') ||
		(b >= '0' && b <= '9') ||
		b == '-' ||
		b == '_'
}

func readSkillContent(skill *skills.Skill) ([]byte, error) {
	if strings.HasPrefix(skill.SkillFilePath, skills.BuiltinPrefix) {
		embeddedPath := "builtin/" + strings.TrimPrefix(skill.SkillFilePath, skills.BuiltinPrefix)
		return fs.ReadFile(skills.BuiltinFS(), embeddedPath)
	}
	return os.ReadFile(skill.SkillFilePath)
}

func attachedSkillContent(skill *skills.Skill, content []byte) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "<attached_skill name=%q path=%q>\n", html.EscapeString(skill.Name), html.EscapeString(skill.SkillFilePath))
	b.WriteString("The user explicitly referenced this skill. This is the full SKILL.md content; treat it as already loaded and follow it directly. Do not call the view tool for this skill file again.\n\n")
	b.WriteString(string(content))
	b.WriteString("\n</attached_skill>\n")
	return []byte(b.String())
}
