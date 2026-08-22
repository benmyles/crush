package fireworksdsv4

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type jsonSchema = map[string]any

func schemaMap(value any) (jsonSchema, bool) {
	result, ok := value.(map[string]any)
	return result, ok
}

func cloneSchema(value jsonSchema) jsonSchema {
	result := make(jsonSchema, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}

func resolvePointer(root jsonSchema, reference string) (jsonSchema, bool) {
	if !strings.HasPrefix(reference, "#/") {
		return nil, false
	}
	var current any = root
	for _, raw := range strings.Split(strings.TrimPrefix(reference, "#/"), "/") {
		key := strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~")
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[key]
		if !ok {
			return nil, false
		}
	}
	result, ok := current.(map[string]any)
	return result, ok
}

func mergeObjectSchemas(schemas []jsonSchema) (jsonSchema, bool) {
	if len(schemas) == 0 {
		return nil, false
	}
	properties := make(map[string]any)
	requiredSet := make(map[string]struct{})
	var additional any
	for _, schema := range schemas {
		for _, typ := range schemaTypes(schema) {
			if typ != "object" {
				return nil, false
			}
		}
		if values, ok := schemaMap(schema["properties"]); ok {
			for key, value := range values {
				properties[key] = value
			}
		}
		for _, name := range stringSlice(schema["required"]) {
			requiredSet[name] = struct{}{}
		}
		if value, ok := schema["additionalProperties"]; ok {
			additional = value
		}
	}
	result := jsonSchema{"type": "object", "properties": properties}
	if len(requiredSet) > 0 {
		required := make([]string, 0, len(requiredSet))
		for name := range requiredSet {
			required = append(required, name)
		}
		result["required"] = required
	}
	if additional != nil {
		result["additionalProperties"] = additional
	}
	return result, true
}

func resolveSchema(schema, root jsonSchema, references map[string]struct{}) jsonSchema {
	resolved := cloneSchema(schema)
	if reference, ok := resolved["$ref"].(string); ok {
		if _, recursive := references[reference]; !recursive {
			if target, found := resolvePointer(root, reference); found {
				next := make(map[string]struct{}, len(references)+1)
				for key := range references {
					next[key] = struct{}{}
				}
				next[reference] = struct{}{}
				base := resolveSchema(target, root, next)
				for key, value := range resolved {
					if key != "$ref" {
						base[key] = value
					}
				}
				resolved = base
			}
		}
	}
	if raw, ok := resolved["allOf"].([]any); ok {
		members := make([]jsonSchema, 0, len(raw))
		for _, value := range raw {
			if item, ok := schemaMap(value); ok {
				members = append(members, resolveSchema(item, root, references))
			}
		}
		if merged, ok := mergeObjectSchemas(members); ok {
			for key, value := range resolved {
				if key != "allOf" {
					merged[key] = value
				}
			}
			resolved = merged
		}
	}
	return resolved
}

func unionBranches(schema, root jsonSchema) []jsonSchema {
	resolved := resolveSchema(schema, root, nil)
	for _, key := range []string{"anyOf", "oneOf"} {
		if values, ok := resolved[key].([]any); ok {
			parent := cloneSchema(resolved)
			delete(parent, key)
			branches := make([]jsonSchema, 0, len(values))
			for _, value := range values {
				if member, ok := schemaMap(value); ok {
					branch := cloneSchema(parent)
					for entryKey, entryValue := range member {
						branch[entryKey] = entryValue
					}
					branches = append(branches, resolveSchema(branch, root, nil))
				}
			}
			return branches
		}
	}
	types := schemaTypes(resolved)
	if len(types) > 1 {
		branches := make([]jsonSchema, 0, len(types))
		for _, typ := range types {
			branch := cloneSchema(resolved)
			branch["type"] = typ
			branches = append(branches, branch)
		}
		return branches
	}
	return []jsonSchema{resolved}
}

func schemaTypes(schema jsonSchema) []string {
	switch value := schema["type"].(type) {
	case string:
		return []string{value}
	case []any:
		var values []string
		for _, item := range value {
			if text, ok := item.(string); ok {
				values = append(values, text)
			}
		}
		return values
	case []string:
		return value
	}
	if _, ok := schema["properties"]; ok {
		return []string{"object"}
	}
	if _, ok := schema["additionalProperties"]; ok {
		return []string{"object"}
	}
	if _, ok := schema["items"]; ok {
		return []string{"array"}
	}
	if _, ok := schema["prefixItems"]; ok {
		return []string{"array"}
	}
	if value, ok := schema["const"]; ok {
		return []string{jsonValueType(value)}
	}
	if values, ok := schema["enum"].([]any); ok && len(values) > 0 {
		seen := make(map[string]struct{})
		var types []string
		for _, value := range values {
			typ := jsonValueType(value)
			if _, ok := seen[typ]; !ok {
				seen[typ] = struct{}{}
				types = append(types, typ)
			}
		}
		return types
	}
	return nil
}

func jsonValueType(value any) string {
	if value == nil {
		return "null"
	}
	switch value := value.(type) {
	case string:
		return "string"
	case bool:
		return "boolean"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	case json.Number:
		if _, err := strconv.ParseInt(string(value), 10, 64); err == nil {
			return "integer"
		}
		return "number"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return "integer"
	default:
		return "number"
	}
}

func stringSlice(value any) []string {
	switch values := value.(type) {
	case []string:
		return values
	case []any:
		result := make([]string, 0, len(values))
		for _, value := range values {
			if text, ok := value.(string); ok {
				result = append(result, text)
			}
		}
		return result
	default:
		return nil
	}
}

func requiredNames(schema jsonSchema) map[string]struct{} {
	result := make(map[string]struct{})
	for _, name := range stringSlice(schema["required"]) {
		result[name] = struct{}{}
	}
	return result
}

type patternGrammar struct {
	characterClass string
	minimum        int
	maximum        *int
}

var simplePattern = regexp.MustCompile(`^\^(\[[^\]]+\])(?:(?:\{(\d+)(?:,(\d*))?\})|([+*?]))\$$`)

func simplePatternRule(value any) (patternGrammar, bool) {
	pattern, ok := value.(string)
	if !ok {
		return patternGrammar{}, false
	}
	match := simplePattern.FindStringSubmatch(pattern)
	if match == nil {
		return patternGrammar{}, false
	}
	minimum := 0
	var maximum *int
	if match[2] != "" {
		minimum, _ = strconv.Atoi(match[2])
		if !strings.Contains(match[0], ",") {
			value := minimum
			maximum = &value
		} else if match[3] != "" {
			value, _ := strconv.Atoi(match[3])
			maximum = &value
		}
	}
	switch match[4] {
	case "+":
		minimum = 1
	case "?":
		value := 1
		maximum = &value
	case "*":
		minimum = 0
	}
	return patternGrammar{characterClass: match[1], minimum: minimum, maximum: maximum}, true
}

var safeAttribute = regexp.MustCompile(`^[A-Za-z0-9_.:/-]+$`)

func safeAttributeValue(value, label string) (string, error) {
	if !safeAttribute.MatchString(value) {
		return "", fmt.Errorf("%s %q cannot be represented in a DSML attribute", label, value)
	}
	return value, nil
}

func optionalNonNegativeInteger(value any) (int, bool) {
	switch number := value.(type) {
	case int:
		return number, number >= 0
	case int64:
		return int(number), number >= 0
	case float64:
		integer := int(number)
		return integer, number >= 0 && number == float64(integer)
	case json.Number:
		integer, err := strconv.Atoi(string(number))
		return integer, err == nil && integer >= 0
	default:
		return 0, false
	}
}

func optionalFiniteNumber(value any) (float64, bool) {
	switch number := value.(type) {
	case int:
		return float64(number), true
	case int64:
		return float64(number), true
	case float64:
		return number, true
	case json.Number:
		result, err := strconv.ParseFloat(string(number), 64)
		return result, err == nil
	default:
		return 0, false
	}
}
