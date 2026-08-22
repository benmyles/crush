package fireworksdsv4

import (
	"fmt"
)

type anyOrderOptions struct {
	script          *grammarScript
	entryRules      []string
	requiredIndices map[int]struct{}
	minimum         int
	maximum         *int
	separator       string
	label           string
}

func buildAnyOrderMembersRule(options anyOrderOptions) (string, error) {
	count := len(options.entryRules)
	if options.minimum < 0 {
		return "", fmt.Errorf("%s minimum entry count cannot be negative", options.label)
	}
	if options.maximum != nil && *options.maximum < options.minimum {
		return "", fmt.Errorf("%s maximum entry count %d cannot be less than %d", options.label, *options.maximum, options.minimum)
	}
	for index := range options.requiredIndices {
		if index < 0 || index >= count {
			return "", fmt.Errorf("%s required entry index is outside the declared member range", options.label)
		}
	}
	if options.minimum > count {
		return "", fmt.Errorf("%s requires %d entries but declares only %d", options.label, options.minimum, count)
	}
	empty := literal("")
	if count == 0 {
		return empty, nil
	}
	maximum := count
	if options.maximum != nil && *options.maximum < maximum {
		maximum = *options.maximum
	}
	if maximum == 0 {
		return empty, nil
	}

	// Tracking the set of emitted members in a context-free grammar needs a
	// state for every subset and makes the grammar grow exponentially. Keep
	// the wire grammar linear in the schema size instead. The DSML decoder and
	// JSON Schema validator enforce member uniqueness and required fields once
	// the complete value is available.
	entry, err := choice(options.entryRules)
	if err != nil {
		return "", err
	}
	entry = options.script.newRule("any-order-entry", entry)
	maxTail := maximum - 1
	tail, err := repeat(concat(options.separator, entry), max(0, options.minimum-1), &maxTail)
	if err != nil {
		return "", err
	}
	members := concat(entry, tail)
	if options.minimum == 0 {
		members, _ = choice([]string{empty, members})
	}
	return options.script.newRule("any-order-members", members), nil
}
