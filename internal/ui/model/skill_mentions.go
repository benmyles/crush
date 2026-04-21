package model

import (
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/home"
	"github.com/charmbracelet/crush/internal/skills"
	"github.com/charmbracelet/crush/internal/ui/completions"
	xansi "github.com/charmbracelet/x/ansi"
)

const (
	fileCompletionTrigger  = "@"
	skillCompletionTrigger = "$"
)

func (m *UI) loadSkillCompletions() tea.Cmd {
	return func() tea.Msg {
		return completions.CompletionItemsLoadedMsg{
			Skills: m.skillCompletionValues(),
		}
	}
}

func (m *UI) skillCompletionValues() []completions.SkillCompletionValue {
	cfg := m.com.Config()
	if cfg == nil {
		return nil
	}

	allSkills := skills.DiscoverBuiltin()
	if cfg.Options != nil && len(cfg.Options.SkillsPaths) > 0 {
		paths := make([]string, 0, len(cfg.Options.SkillsPaths))
		for _, path := range cfg.Options.SkillsPaths {
			paths = append(paths, m.expandSkillPath(path))
		}
		allSkills = append(allSkills, skills.Discover(paths)...)
	}

	allSkills = skills.Deduplicate(allSkills)
	if cfg.Options != nil {
		allSkills = skills.Filter(allSkills, cfg.Options.DisabledSkills)
	}
	slices.SortStableFunc(allSkills, func(a, b *skills.Skill) int {
		return strings.Compare(a.Name, b.Name)
	})

	values := make([]completions.SkillCompletionValue, 0, len(allSkills))
	for _, skill := range allSkills {
		values = append(values, completions.SkillCompletionValue{
			Name:        skill.Name,
			Description: skill.Description,
		})
	}
	return values
}

func (m *UI) expandSkillPath(path string) string {
	path = home.Long(path)
	if strings.HasPrefix(path, "$") {
		if resolved, err := m.com.Workspace.Resolver().ResolveValue(path); err == nil {
			path = resolved
		}
	}
	return path
}

func (m *UI) rememberSkillCompletions(values []completions.SkillCompletionValue) {
	names := make(map[string]struct{}, len(values))
	for _, value := range values {
		names[value.Name] = struct{}{}
	}
	m.skillMentionNames = names
}

func (m *UI) refreshSkillMentionNames() {
	m.skillMentionNames = m.skillMentionNameSetFromStates()
}

func (m *UI) skillMentionNameSetFromStates() map[string]struct{} {
	disabled := map[string]struct{}{}
	if cfg := m.com.Config(); cfg != nil && cfg.Options != nil {
		for _, name := range cfg.Options.DisabledSkills {
			disabled[name] = struct{}{}
		}
	}

	names := make(map[string]struct{})
	for _, skill := range cachedBuiltinSkills() {
		if _, ok := disabled[skill.Name]; !ok {
			names[skill.Name] = struct{}{}
		}
	}
	for _, state := range m.skillStates {
		if state.State != skills.StateNormal {
			continue
		}
		name := state.Name
		if name == "" {
			name = filepath.Base(filepath.Dir(state.Path))
		}
		if _, ok := disabled[name]; !ok {
			names[name] = struct{}{}
		}
	}
	return names
}

func (m *UI) renderEditorSkillMentions(view string) string {
	names := m.skillMentionNames
	if names == nil {
		names = m.skillMentionNameSetFromStates()
	}
	return renderRainbowSkillMentions(view, names, m.com.Styles.EditorSkillMention)
}

func renderRainbowSkillMentions(view string, names map[string]struct{}, palette []lipgloss.Style) string {
	if len(names) == 0 || len(palette) == 0 || !strings.Contains(view, "$") {
		return view
	}

	lines := strings.Split(view, "\n")
	for i, line := range lines {
		lines[i] = renderRainbowSkillMentionLine(line, names, palette)
	}
	return strings.Join(lines, "\n")
}

func renderRainbowSkillMentionLine(line string, names map[string]struct{}, palette []lipgloss.Style) string {
	plain := xansi.Strip(line)
	ranges := skillMentionRanges(plain, names)
	if len(ranges) == 0 {
		return line
	}

	styleRanges := make([]lipgloss.Range, 0)
	for _, rng := range ranges {
		for pos := rng[0]; pos < rng[1]; pos++ {
			styleRanges = append(styleRanges, lipgloss.NewRange(pos, pos+1, palette[(pos-rng[0])%len(palette)]))
		}
	}
	return lipgloss.StyleRanges(line, styleRanges...)
}

func skillMentionRanges(line string, names map[string]struct{}) [][2]int {
	var ranges [][2]int
	for i := 0; i < len(line); {
		r, size := utf8.DecodeRuneInString(line[i:])
		if r != '$' {
			i += size
			continue
		}

		if i > 0 {
			prev, _ := utf8.DecodeLastRuneInString(line[:i])
			if isSkillMentionWordRune(prev) || prev == '$' {
				i += size
				continue
			}
		}

		nameStart := i + size
		j := nameStart
		for j < len(line) {
			next, nextSize := utf8.DecodeRuneInString(line[j:])
			if !isSkillNameRune(next) {
				break
			}
			j += nextSize
		}
		if j == nameStart {
			i += size
			continue
		}
		if j < len(line) {
			next, _ := utf8.DecodeRuneInString(line[j:])
			if isSkillMentionWordRune(next) || next == '$' {
				i = j
				continue
			}
		}

		name := line[nameStart:j]
		if _, ok := names[name]; ok {
			start := xansi.StringWidth(line[:i])
			end := start + xansi.StringWidth(line[i:j])
			ranges = append(ranges, [2]int{start, end})
		}
		i = j
	}
	return ranges
}

func isSkillNameRune(r rune) bool {
	return (r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') ||
		r == '-'
}

func isSkillMentionWordRune(r rune) bool {
	return isSkillNameRune(r) || r == '_'
}
