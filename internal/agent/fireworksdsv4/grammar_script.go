package fireworksdsv4

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

var unsafeRuleName = regexp.MustCompile(`[^0-9A-Za-z-]+`)

type grammarScript struct {
	order  []string
	rules  map[string]string
	counts map[string]int
}

func newGrammarScript() *grammarScript {
	return &grammarScript{rules: make(map[string]string), counts: make(map[string]int)}
}

func safeRuleName(name string) string {
	name = strings.ReplaceAll(name, "_", "-")
	name = unsafeRuleName.ReplaceAllString(name, "-")
	name = strings.ToLower(strings.Trim(name, "-"))
	if name == "" {
		return "rule"
	}
	if name[0] >= '0' && name[0] <= '9' {
		return "rule-" + name
	}
	return name
}

func (s *grammarScript) defineRule(name, body string) string {
	name = safeRuleName(name)
	if _, ok := s.rules[name]; !ok {
		s.order = append(s.order, name)
	}
	s.rules[name] = body
	return name
}

func (s *grammarScript) newRule(hint, body string) string {
	hint = safeRuleName(hint)
	count := s.counts[hint]
	name := hint
	if count > 0 {
		name = fmt.Sprintf("%s-%d", hint, count)
	}
	for {
		if _, exists := s.rules[name]; !exists {
			break
		}
		count++
		name = fmt.Sprintf("%s-%d", hint, count)
	}
	s.counts[hint] = count + 1
	s.order = append(s.order, name)
	s.rules[name] = body
	return name
}

func (s *grammarScript) reserveRule(hint string) string {
	return s.newRule(hint, literal(""))
}

func (s *grammarScript) render() string {
	names := append([]string{"root"}, s.order...)
	seen := make(map[string]struct{}, len(names))
	lines := make([]string, 0, len(names))
	for _, name := range names {
		if _, duplicate := seen[name]; duplicate {
			continue
		}
		seen[name] = struct{}{}
		if body, ok := s.rules[name]; ok {
			lines = append(lines, name+" ::= "+body)
		}
	}
	return strings.Join(lines, "\n")
}

func literal(text string) string {
	data, _ := json.Marshal(text)
	return string(data)
}

func concat(parts ...string) string {
	values := parts[:0]
	for _, part := range parts {
		if part != "" {
			values = append(values, part)
		}
	}
	return strings.Join(values, " ")
}

func choice(parts []string) (string, error) {
	parts = slices.Compact(parts)
	unique := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		unique = append(unique, part)
	}
	if len(unique) == 0 {
		return "", fmt.Errorf("grammar choice needs at least one branch")
	}
	if len(unique) == 1 {
		return unique[0], nil
	}
	for i := range unique {
		unique[i] = "(" + unique[i] + ")"
	}
	return "(" + strings.Join(unique, " | ") + ")", nil
}

func optional(part string) string { return "(" + part + ")?" }

func repeat(part string, minimum int, maximum *int) (string, error) {
	if minimum < 0 || maximum != nil && *maximum < minimum {
		return "", fmt.Errorf("invalid grammar repetition %d..%v", minimum, maximum)
	}
	if maximum != nil && minimum == 0 && *maximum == 0 {
		return literal(""), nil
	}
	if maximum != nil && minimum == 1 && *maximum == 1 {
		return part, nil
	}
	if maximum == nil && minimum == 0 {
		return "(" + part + ")*", nil
	}
	if maximum == nil && minimum == 1 {
		return "(" + part + ")+", nil
	}
	ending := ""
	if maximum != nil {
		ending = fmt.Sprint(*maximum)
	}
	return fmt.Sprintf("(%s){%d,%s}", part, minimum, ending), nil
}

func escapeCharacterClass(character rune) string {
	switch character {
	case '\\':
		return `\\`
	case ']':
		return `\]`
	case '^':
		return `\^`
	case '-':
		return `\-`
	case '\n':
		return `\n`
	case '\r':
		return `\r`
	case '\t':
		return `\t`
	default:
		return string(character)
	}
}

func nextPrefix(prefix string, character rune, prefixes []string) string {
	candidate := prefix + string(character)
	best := ""
	for _, possible := range prefixes {
		if len(possible) > len(best) && strings.HasSuffix(candidate, possible) {
			best = possible
		}
	}
	return best
}

func defineTextWithout(script *grammarScript, hint string, forbiddenValues []string) (string, error) {
	forbiddenSet := make(map[string]struct{}, len(forbiddenValues))
	var forbidden []string
	for _, value := range forbiddenValues {
		if value == "" {
			continue
		}
		if _, exists := forbiddenSet[value]; exists {
			continue
		}
		forbiddenSet[value] = struct{}{}
		forbidden = append(forbidden, value)
	}
	if len(forbidden) == 0 {
		return "", fmt.Errorf("a sentinel-safe text rule needs at least one forbidden value")
	}
	prefixSet := map[string]struct{}{"": {}}
	for _, value := range forbidden {
		runes := []rune(value)
		for i := 1; i < len(runes); i++ {
			prefixSet[string(runes[:i])] = struct{}{}
		}
	}
	prefixes := make([]string, 0, len(prefixSet))
	for prefix := range prefixSet {
		prefixes = append(prefixes, prefix)
	}
	sort.Slice(prefixes, func(i, j int) bool {
		left, right := utf8.RuneCountInString(prefixes[i]), utf8.RuneCountInString(prefixes[j])
		if left != right {
			return left < right
		}
		return prefixes[i] < prefixes[j]
	})
	stateNames := make(map[string]string, len(prefixes))
	for i, prefix := range prefixes {
		stateNames[prefix] = script.reserveRule(fmt.Sprintf("%s-state-%d", hint, i))
	}
	characterSet := make(map[rune]struct{})
	for _, value := range forbidden {
		for _, character := range value {
			characterSet[character] = struct{}{}
		}
	}
	characters := make([]rune, 0, len(characterSet))
	for character := range characterSet {
		characters = append(characters, character)
	}
	sort.Slice(characters, func(i, j int) bool { return characters[i] < characters[j] })

	for _, prefix := range prefixes {
		branches := []string{literal("")}
		var excluded []rune
		transitions := make(map[string][]rune)
		for _, character := range characters {
			candidate := prefix + string(character)
			blocked := false
			for _, value := range forbidden {
				if strings.HasSuffix(candidate, value) {
					blocked = true
					break
				}
			}
			if blocked {
				excluded = append(excluded, character)
				continue
			}
			destination := nextPrefix(prefix, character, prefixes)
			if destination == "" {
				continue
			}
			excluded = append(excluded, character)
			transitions[destination] = append(transitions[destination], character)
		}
		var class strings.Builder
		for _, character := range excluded {
			class.WriteString(escapeCharacterClass(character))
		}
		branches = append(branches, concat("[^"+class.String()+"]", stateNames[""]))
		destinations := make([]string, 0, len(transitions))
		for destination := range transitions {
			destinations = append(destinations, destination)
		}
		sort.Strings(destinations)
		for _, destination := range destinations {
			transitionCharacters := transitions[destination]
			token := ""
			if len(transitionCharacters) == 1 {
				token = literal(string(transitionCharacters[0]))
			} else {
				var value strings.Builder
				for _, character := range transitionCharacters {
					value.WriteString(escapeCharacterClass(character))
				}
				token = "[" + value.String() + "]"
			}
			branches = append(branches, concat(token, stateNames[destination]))
		}
		body, err := choice(branches)
		if err != nil {
			return "", err
		}
		script.defineRule(stateNames[prefix], body)
	}
	return stateNames[""], nil
}

func stableJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
