package fireworksdsv4

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

type parameterBranch struct {
	stringFlag bool
	valueRule  string
}

type schemaGrammar struct {
	script         *grammarScript
	root           jsonSchema
	rawTextRule    string
	jsonRules      map[string]string
	rawRules       map[string]string
	jsonWhitespace string
	jsonCharacter  string
}

func newSchemaGrammar(script *grammarScript, root jsonSchema, rawTextRule, prefix string) *schemaGrammar {
	return &schemaGrammar{
		script:         script,
		root:           root,
		rawTextRule:    rawTextRule,
		jsonRules:      make(map[string]string),
		rawRules:       make(map[string]string),
		jsonWhitespace: script.defineRule(prefix+"-json-ws", grammarGap),
		jsonCharacter:  script.defineRule(prefix+"-json-char", `[^"\\\x00-\x1F] | "\\" (["\\/bfnrt] | "u" [0-9a-fA-F]{4})`),
	}
}

func (c *schemaGrammar) objectBranches(schema jsonSchema) ([]jsonSchema, error) {
	branches := unionBranches(schema, c.root)
	for _, branch := range branches {
		resolved := resolveSchema(branch, c.root, nil)
		found := false
		for _, typ := range schemaTypes(resolved) {
			if typ == "object" {
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("tool parameters for DSV4 must resolve to an object schema")
		}
	}
	return branches, nil
}

func (c *schemaGrammar) parameterBranches(schema jsonSchema) ([]parameterBranch, error) {
	var result []parameterBranch
	for _, branch := range unionBranches(schema, c.root) {
		if value, ok := branch["const"]; ok {
			rule, err := c.compileJSON(branch, 0)
			stringFlag := false
			if _, ok := value.(string); ok {
				stringFlag = true
				rule, err = c.compileRawString(branch)
			}
			if err != nil {
				return nil, err
			}
			result = append(result, parameterBranch{stringFlag: stringFlag, valueRule: rule})
			continue
		}
		if values, ok := branch["enum"].([]any); ok {
			var stringsOnly, others []any
			for _, value := range values {
				if _, ok := value.(string); ok {
					stringsOnly = append(stringsOnly, value)
				} else {
					others = append(others, value)
				}
			}
			if len(stringsOnly) > 0 {
				typed := cloneSchema(branch)
				typed["type"], typed["enum"] = "string", stringsOnly
				rule, err := c.compileRawString(typed)
				if err != nil {
					return nil, err
				}
				result = append(result, parameterBranch{stringFlag: true, valueRule: rule})
			}
			if len(others) > 0 {
				typed := cloneSchema(branch)
				typed["enum"] = others
				rule, err := c.compileJSON(typed, 0)
				if err != nil {
					return nil, err
				}
				result = append(result, parameterBranch{valueRule: rule})
			}
			continue
		}
		types := schemaTypes(branch)
		if len(types) == 0 {
			result = append(result, parameterBranch{stringFlag: true, valueRule: c.rawTextRule})
			rule, err := c.compileGenericJSON(0)
			if err != nil {
				return nil, err
			}
			result = append(result, parameterBranch{valueRule: rule})
			continue
		}
		for _, typ := range types {
			typed := cloneSchema(branch)
			typed["type"] = typ
			if typ == "string" {
				rule, err := c.compileRawString(typed)
				if err != nil {
					return nil, err
				}
				result = append(result, parameterBranch{stringFlag: true, valueRule: rule})
			} else {
				rule, err := c.compileJSON(typed, 0)
				if err != nil {
					return nil, err
				}
				result = append(result, parameterBranch{valueRule: rule})
			}
		}
	}
	seen := make(map[string]struct{})
	unique := result[:0]
	for _, branch := range result {
		key := fmt.Sprintf("%t:%s", branch.stringFlag, branch.valueRule)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, branch)
	}
	return unique, nil
}

func (c *schemaGrammar) compileRawString(schema jsonSchema) (string, error) {
	resolved := resolveSchema(schema, c.root, nil)
	key, err := stableJSON(resolved)
	if err != nil {
		return "", err
	}
	if cached, ok := c.rawRules[key]; ok {
		return cached, nil
	}
	body := ""
	if value, ok := resolved["const"].(string); ok {
		body = literal(value)
	} else if values, ok := resolved["enum"].([]any); ok {
		branches := make([]string, 0, len(values))
		allStrings := true
		for _, value := range values {
			text, ok := value.(string)
			if !ok {
				allStrings = false
				break
			}
			branches = append(branches, literal(text))
		}
		if allStrings {
			body, err = choice(branches)
		}
	}
	if body == "" {
		if pattern, ok := simplePatternRule(resolved["pattern"]); ok {
			body, err = repeat(pattern.characterClass, pattern.minimum, pattern.maximum)
		} else {
			body = c.rawTextRule
		}
	}
	if err != nil {
		return "", err
	}
	rule := c.script.newRule("raw-string-value", body)
	c.rawRules[key] = rule
	return rule, nil
}

func (c *schemaGrammar) compileJSON(schema jsonSchema, depth int) (string, error) {
	if depth > maxJSONDepth {
		return c.compileGenericJSON(maxJSONDepth)
	}
	resolved := resolveSchema(schema, c.root, nil)
	stable, err := stableJSON(resolved)
	if err != nil {
		return "", err
	}
	key := fmt.Sprintf("%d:%s", depth, stable)
	if cached, ok := c.jsonRules[key]; ok {
		return cached, nil
	}
	typ := "value"
	if types := schemaTypes(resolved); len(types) > 0 {
		typ = types[0]
	}
	rule := c.script.reserveRule("json-" + typ)
	c.jsonRules[key] = rule
	body, err := c.compileJSONBody(resolved, depth)
	if err != nil {
		return "", err
	}
	c.script.defineRule(rule, body)
	return rule, nil
}

func (c *schemaGrammar) compileGenericJSON(depth int) (string, error) {
	return c.compileJSON(jsonSchema{}, depth)
}

func jsonLiteral(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("the JSON Schema const/enum contains a non-JSON value: %w", err)
	}
	return literal(string(data)), nil
}

func (c *schemaGrammar) compileJSONBody(schema jsonSchema, depth int) (string, error) {
	if value, ok := schema["const"]; ok {
		return jsonLiteral(value)
	}
	if values, ok := schema["enum"].([]any); ok {
		if len(values) == 0 {
			return "", fmt.Errorf("an empty JSON Schema enum cannot be represented")
		}
		branches := make([]string, 0, len(values))
		for _, value := range values {
			branch, err := jsonLiteral(value)
			if err != nil {
				return "", err
			}
			branches = append(branches, branch)
		}
		return choice(branches)
	}
	branches := unionBranches(schema, c.root)
	if len(branches) > 1 {
		rules := make([]string, 0, len(branches))
		for _, branch := range branches {
			rule, err := c.compileJSON(branch, depth)
			if err != nil {
				return "", err
			}
			rules = append(rules, rule)
		}
		return choice(rules)
	}
	types := schemaTypes(schema)
	if len(types) > 1 {
		var rules []string
		for _, typ := range types {
			typed := cloneSchema(schema)
			typed["type"] = typ
			rule, err := c.compileJSON(typed, depth)
			if err != nil {
				return "", err
			}
			rules = append(rules, rule)
		}
		return choice(rules)
	}
	if len(types) == 0 {
		return c.genericJSONBody(depth)
	}
	switch types[0] {
	case "object":
		return c.objectBody(schema, depth)
	case "array":
		return c.arrayBody(schema, depth)
	case "string":
		return c.jsonStringBody(schema)
	case "integer":
		return c.integerBody(schema)
	case "number":
		return c.numberBody(schema)
	case "boolean":
		return literal("true") + " | " + literal("false"), nil
	case "null":
		return literal("null"), nil
	default:
		return c.genericJSONBody(depth)
	}
}

func (c *schemaGrammar) jsonStringBody(schema jsonSchema) (string, error) {
	minimum, _ := optionalNonNegativeInteger(schema["minLength"])
	maximum, hasMaximum := optionalNonNegativeInteger(schema["maxLength"])
	var maximumPtr *int
	if hasMaximum {
		maximumPtr = &maximum
	}
	body := ""
	var err error
	if pattern, ok := simplePatternRule(schema["pattern"]); ok {
		body, err = repeat(pattern.characterClass, pattern.minimum, pattern.maximum)
	} else {
		body, err = repeat(c.jsonCharacter, minimum, maximumPtr)
	}
	if err != nil {
		return "", err
	}
	return concat(literal(`"`), body, literal(`"`)), nil
}

func (c *schemaGrammar) integerBody(schema jsonSchema) (string, error) {
	minimum, hasMinimum := optionalFiniteNumber(schema["minimum"])
	maximum, hasMaximum := optionalFiniteNumber(schema["maximum"])
	if value, ok := optionalFiniteNumber(schema["exclusiveMinimum"]); ok {
		minimum, hasMinimum = math.Floor(value)+1, true
	}
	if value, ok := optionalFiniteNumber(schema["exclusiveMaximum"]); ok {
		maximum, hasMaximum = math.Ceil(value)-1, true
	}
	if hasMinimum && hasMaximum && minimum == math.Trunc(minimum) && maximum == math.Trunc(maximum) && maximum >= minimum && maximum-minimum <= 256 {
		branches := make([]string, 0, int(maximum-minimum)+1)
		for value := int64(minimum); value <= int64(maximum); value++ {
			branches = append(branches, literal(fmt.Sprint(value)))
		}
		return choice(branches)
	}
	if hasMinimum && minimum == 0 && !hasMaximum {
		return literal("0") + " | [1-9] [0-9]*", nil
	}
	if hasMinimum && minimum == 1 && !hasMaximum {
		return "[1-9] [0-9]*", nil
	}
	return literal("-") + "? (" + literal("0") + " | [1-9] [0-9]*)", nil
}

func (c *schemaGrammar) numberBody(schema jsonSchema) (string, error) {
	minimum, hasMinimum := optionalFiniteNumber(schema["minimum"])
	sign := literal("-") + "?"
	if hasMinimum && minimum >= 0 {
		sign = ""
	}
	return concat(sign, "("+literal("0")+" | [1-9] [0-9]*)", optional(concat(literal("."), "[0-9]+")), optional(concat("[eE]", optional("[+-]"), "[0-9]+"))), nil
}

func schemaArray(value any) ([]jsonSchema, bool) {
	values, ok := value.([]any)
	if !ok {
		return nil, false
	}
	result := make([]jsonSchema, 0, len(values))
	for _, value := range values {
		item, ok := schemaMap(value)
		if !ok {
			return nil, false
		}
		result = append(result, item)
	}
	return result, true
}

func (c *schemaGrammar) arrayBody(schema jsonSchema, depth int) (string, error) {
	if items, ok := schemaArray(schema["prefixItems"]); ok {
		var additional jsonSchema
		if schema["items"] != false {
			additional, _ = schemaMap(schema["items"])
			if additional == nil {
				additional = jsonSchema{}
			}
		}
		return c.tupleArrayBody(schema, items, additional, depth)
	}
	if items, ok := schemaArray(schema["items"]); ok {
		var additional jsonSchema
		if schema["additionalItems"] != false {
			additional, _ = schemaMap(schema["additionalItems"])
			if additional == nil {
				additional = jsonSchema{}
			}
		}
		return c.tupleArrayBody(schema, items, additional, depth)
	}
	minimum, _ := optionalNonNegativeInteger(schema["minItems"])
	if schema["items"] == false {
		if minimum > 0 {
			return "", fmt.Errorf("array requires %d items but its items schema is false", minimum)
		}
		return concat(literal("["), c.jsonWhitespace, literal("]")), nil
	}
	itemSchema, _ := schemaMap(schema["items"])
	if itemSchema == nil {
		itemSchema = jsonSchema{}
	}
	item, err := c.compileJSON(itemSchema, depth+1)
	if err != nil {
		return "", err
	}
	maximum := maxDynamicItems
	if configured, ok := optionalNonNegativeInteger(schema["maxItems"]); ok {
		maximum = configured
	}
	if maximum < minimum {
		return "", fmt.Errorf("array maxItems %d is below minItems %d", maximum, minimum)
	}
	if maximum == 0 {
		return concat(literal("["), c.jsonWhitespace, literal("]")), nil
	}
	tail := concat(c.jsonWhitespace, literal(","), c.jsonWhitespace, item)
	maxTail := maximum - 1
	repeated, err := repeat(tail, max(0, minimum-1), &maxTail)
	if err != nil {
		return "", err
	}
	nonempty := concat(literal("["), c.jsonWhitespace, item, repeated, c.jsonWhitespace, literal("]"))
	if minimum == 0 {
		return choice([]string{concat(literal("["), c.jsonWhitespace, literal("]")), nonempty})
	}
	return nonempty, nil
}

func (c *schemaGrammar) tupleArrayBody(schema jsonSchema, prefix []jsonSchema, additional jsonSchema, depth int) (string, error) {
	minimum, _ := optionalNonNegativeInteger(schema["minItems"])
	maximum := len(prefix)
	if additional != nil {
		maximum = max(len(prefix), maxDynamicItems)
	}
	if configured, ok := optionalNonNegativeInteger(schema["maxItems"]); ok && configured < maximum {
		maximum = configured
	}
	if maximum < minimum {
		return "", fmt.Errorf("tuple maximum item count %d cannot be less than %d", maximum, minimum)
	}
	prefixRules := make([]string, 0, len(prefix))
	for _, item := range prefix {
		rule, err := c.compileJSON(item, depth+1)
		if err != nil {
			return "", err
		}
		prefixRules = append(prefixRules, rule)
	}
	additionalRule := ""
	if additional != nil {
		var err error
		additionalRule, err = c.compileJSON(additional, depth+1)
		if err != nil {
			return "", err
		}
	}
	separator := concat(c.jsonWhitespace, literal(","), c.jsonWhitespace)
	var branches []string
	for length := minimum; length <= maximum; length++ {
		members := make([]string, length)
		for i := range length {
			if i < len(prefixRules) {
				members[i] = prefixRules[i]
			} else {
				members[i] = additionalRule
			}
		}
		branches = append(branches, concat(literal("["), c.jsonWhitespace, strings.Join(members, " "+separator+" "), c.jsonWhitespace, literal("]")))
	}
	return choice(branches)
}

func (c *schemaGrammar) objectBody(schema jsonSchema, depth int) (string, error) {
	declared, _ := schemaMap(schema["properties"])
	_, hasProperties := schema["properties"]
	_, hasAdditional := schema["additionalProperties"]
	if !hasProperties && !hasAdditional {
		return c.genericObjectBody(depth)
	}
	if declared == nil {
		declared = jsonSchema{}
	}
	keys := make([]string, 0, len(declared))
	for key := range declared {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	required := requiredNames(schema)
	for name := range required {
		if _, ok := declared[name]; !ok {
			return "", fmt.Errorf("object schema requires unknown properties: %s", name)
		}
	}
	propertyRules := make([]string, 0, len(keys))
	requiredIndices := make(map[int]struct{})
	for index, name := range keys {
		property, ok := schemaMap(declared[name])
		if !ok {
			return "", fmt.Errorf("property %s does not have a JSON Schema", name)
		}
		rule, err := c.compileJSON(property, depth+1)
		if err != nil {
			return "", err
		}
		propertyRules = append(propertyRules, c.script.newRule("json-property-"+name, concat(literal(fmt.Sprintf("%q", name)), c.jsonWhitespace, literal(":"), c.jsonWhitespace, rule)))
		if _, ok := required[name]; ok {
			requiredIndices[index] = struct{}{}
		}
	}
	minimum := len(required)
	if value, ok := optionalNonNegativeInteger(schema["minProperties"]); ok && value > minimum {
		minimum = value
	}
	var maximum *int
	if value, ok := optionalNonNegativeInteger(schema["maxProperties"]); ok {
		maximum = &value
	}
	separator := concat(c.jsonWhitespace, literal(","), c.jsonWhitespace)
	var dynamic jsonSchema
	if schema["additionalProperties"] == true {
		dynamic = jsonSchema{}
	} else {
		dynamic, _ = schemaMap(schema["additionalProperties"])
	}
	var members string
	var err error
	if dynamic != nil {
		members, err = c.dynamicObjectMembers(propertyRules, dynamic, minimum, maximum, depth)
	} else {
		members, err = buildAnyOrderMembersRule(anyOrderOptions{script: c.script, entryRules: propertyRules, requiredIndices: requiredIndices, minimum: minimum, maximum: maximum, separator: separator, label: "Object schema"})
	}
	if err != nil {
		return "", err
	}
	return concat(literal("{"), c.jsonWhitespace, members, c.jsonWhitespace, literal("}")), nil
}

func (c *schemaGrammar) dynamicObjectMembers(declared []string, dynamic jsonSchema, minimum int, maximum *int, depth int) (string, error) {
	name, err := c.jsonStringBody(jsonSchema{"type": "string"})
	if err != nil {
		return "", err
	}
	value, err := c.compileJSON(dynamic, depth+1)
	if err != nil {
		return "", err
	}
	dynamicMember := c.script.newRule("json-dynamic-member", concat(name, c.jsonWhitespace, literal(":"), c.jsonWhitespace, value))
	entries := append(append([]string(nil), declared...), dynamicMember)
	entry, err := choice(entries)
	if err != nil {
		return "", err
	}
	entry = c.script.newRule("json-object-member", entry)
	resolvedMaximum := len(declared) + maxDynamicMembers
	if maximum != nil && *maximum < resolvedMaximum {
		resolvedMaximum = *maximum
	}
	if resolvedMaximum < minimum {
		return "", fmt.Errorf("object maximum entry count %d cannot be less than %d", resolvedMaximum, minimum)
	}
	if resolvedMaximum == 0 {
		return literal(""), nil
	}
	separator := concat(c.jsonWhitespace, literal(","), c.jsonWhitespace)
	maxTail := resolvedMaximum - 1
	tail, err := repeat(concat(separator, entry), max(0, minimum-1), &maxTail)
	if err != nil {
		return "", err
	}
	members := concat(entry, tail)
	if minimum == 0 {
		members, _ = choice([]string{literal(""), members})
	}
	return c.script.newRule("json-object-members", members), nil
}

func (c *schemaGrammar) genericJSONBody(depth int) (string, error) {
	stringRule, err := c.jsonStringBody(jsonSchema{"type": "string"})
	if err != nil {
		return "", err
	}
	numberRule, _ := c.numberBody(jsonSchema{"type": "number"})
	scalars := []string{stringRule, numberRule, literal("true"), literal("false"), literal("null")}
	if depth >= maxJSONDepth {
		return choice(scalars)
	}
	objectRule, err := c.genericObjectBody(depth)
	if err != nil {
		return "", err
	}
	arrayRule, err := c.arrayBody(jsonSchema{}, depth)
	if err != nil {
		return "", err
	}
	return choice(append(scalars, objectRule, arrayRule))
}

func (c *schemaGrammar) genericObjectBody(depth int) (string, error) {
	name, err := c.jsonStringBody(jsonSchema{"type": "string"})
	if err != nil {
		return "", err
	}
	value, err := c.compileGenericJSON(depth + 1)
	if err != nil {
		return "", err
	}
	member := c.script.newRule(fmt.Sprintf("json-generic-member-%d", depth), concat(name, c.jsonWhitespace, literal(":"), c.jsonWhitespace, value))
	tail := concat(c.jsonWhitespace, literal(","), c.jsonWhitespace, member)
	maxTail := maxDynamicMembers - 1
	repeated, err := repeat(tail, 0, &maxTail)
	if err != nil {
		return "", err
	}
	nonempty := concat(literal("{"), c.jsonWhitespace, member, repeated, c.jsonWhitespace, literal("}"))
	return choice([]string{concat(literal("{"), c.jsonWhitespace, literal("}")), nonempty})
}
