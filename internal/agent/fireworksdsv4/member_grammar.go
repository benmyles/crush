package fireworksdsv4

import (
	"fmt"
	"math/bits"
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
	if count > maxExactMembers {
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

	requiredMask := 0
	for index := range options.requiredIndices {
		requiredMask |= 1 << index
	}
	root := options.script.newRule("any-order-members", empty)
	states := make(map[int]*string)
	canStop := func(mask int) bool {
		return bits.OnesCount(uint(mask)) >= options.minimum && mask&requiredMask == requiredMask
	}
	var buildState func(int) (*string, error)
	buildState = func(mask int) (*string, error) {
		if state, ok := states[mask]; ok {
			return state, nil
		}
		name := root
		if mask != 0 {
			name = fmt.Sprintf("%s-%x", root, mask)
		}
		states[mask] = &name
		var branches []string
		for index, entry := range options.entryRules {
			bit := 1 << index
			if mask&bit != 0 {
				continue
			}
			nextMask := mask | bit
			var tails []string
			if canStop(nextMask) {
				tails = append(tails, empty)
			}
			if bits.OnesCount(uint(nextMask)) < maximum {
				next, err := buildState(nextMask)
				if err != nil {
					return nil, err
				}
				if next != nil {
					tails = append(tails, concat(options.separator, *next))
				}
			}
			if len(tails) == 0 {
				continue
			}
			tail, _ := choice(tails)
			if len(tails) > 1 {
				tail = "(" + tail + ")"
			}
			if tail == empty {
				branches = append(branches, entry)
			} else {
				branches = append(branches, concat(entry, tail))
			}
		}
		if len(branches) == 0 {
			states[mask] = nil
			return nil, nil
		}
		body, err := choice(branches)
		if err != nil {
			return nil, err
		}
		if mask == 0 && canStop(mask) {
			body, _ = choice([]string{empty, body})
		}
		options.script.defineRule(name, body)
		return &name, nil
	}
	state, err := buildState(0)
	if err != nil {
		return "", err
	}
	if state == nil {
		return "", fmt.Errorf("%s cannot satisfy its required members within the maximum entry count", options.label)
	}
	return root, nil
}
