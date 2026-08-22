package fireworksdsv4

import (
	"fmt"
	"sort"
	"sync"
)

const (
	grammarGap         = `[\t\n\r ]*`
	grammarRequiredGap = `[\t\n\r ]+`
)

var rawStringExcludes = []string{
	"<" + dsmlToken,
	"</" + dsmlToken,
	beginToken,
	endToken,
	userToken,
	assistantToken,
	thinkingStart,
	thinkingEnd,
}

type grammarLRU struct {
	mu     sync.Mutex
	values map[string]string
	order  []string
}

var grammars = grammarLRU{values: make(map[string]string)}

func dsmlAttributeRule(name, value, gap string) string {
	quoted, _ := choice([]string{literal(`"` + value + `"`), literal(`'` + value + `'`)})
	return concat(literal(name), gap, literal("="), gap, quoted)
}

func dsmlOpeningTagRule(kind, attributes, gap, requiredGap string) string {
	body := literal("<" + dsmlToken + kind)
	if attributes != "" {
		body = concat(body, requiredGap, attributes)
	}
	return concat(body, gap, literal(">"))
}

func dsmlClosingTagRule(kind, gap string) string {
	return concat(literal("</"+dsmlToken+kind), gap, literal(">"))
}

func invokeOpenRule(name, gap, requiredGap string) string {
	return dsmlOpeningTagRule("invoke", dsmlAttributeRule("name", name, gap), gap, requiredGap)
}

func parameterOpenRule(name string, stringFlag bool, gap, requiredGap string) string {
	nameAttribute := dsmlAttributeRule("name", name, gap)
	stringAttribute := dsmlAttributeRule("string", fmt.Sprint(stringFlag), gap)
	attributes, _ := choice([]string{
		concat(nameAttribute, requiredGap, stringAttribute),
		concat(stringAttribute, requiredGap, nameAttribute),
	})
	return dsmlOpeningTagRule("parameter", attributes, gap, requiredGap)
}

func buildParameterRule(script *grammarScript, compiler *schemaGrammar, name string, schema jsonSchema, gap, requiredGap, parameterClose string) (string, error) {
	if _, err := safeAttributeValue(name, "Tool parameter"); err != nil {
		return "", err
	}
	branches, err := compiler.parameterBranches(schema)
	if err != nil {
		return "", err
	}
	if len(branches) == 0 {
		return "", fmt.Errorf("tool parameter %q has no representable schema branch", name)
	}
	rules := make([]string, 0, len(branches))
	for _, branch := range branches {
		value := branch.valueRule
		if !branch.stringFlag {
			value = concat(compiler.jsonWhitespace, value, compiler.jsonWhitespace)
		}
		rules = append(rules, concat(parameterOpenRule(name, branch.stringFlag, gap, requiredGap), value, parameterClose))
	}
	body, err := choice(rules)
	if err != nil {
		return "", err
	}
	return script.newRule("dsml-parameter-"+name, body), nil
}

func buildParameterSequence(script *grammarScript, compiler *schemaGrammar, schema jsonSchema, gap, requiredGap, parameterClose string) (string, error) {
	properties, _ := schemaMap(schema["properties"])
	if properties == nil {
		properties = jsonSchema{}
	}
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	required := requiredNames(schema)
	for name := range required {
		if _, ok := properties[name]; !ok {
			return "", fmt.Errorf("tool schema requires unknown parameters: %s", name)
		}
	}
	rules := make([]string, 0, len(keys))
	requiredIndices := make(map[int]struct{})
	for index, name := range keys {
		property, ok := schemaMap(properties[name])
		if !ok {
			return "", fmt.Errorf("tool parameter %q does not have a JSON Schema", name)
		}
		rule, err := buildParameterRule(script, compiler, name, property, gap, requiredGap, parameterClose)
		if err != nil {
			return "", err
		}
		rules = append(rules, rule)
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
	return buildAnyOrderMembersRule(anyOrderOptions{
		script: script, entryRules: rules, requiredIndices: requiredIndices,
		minimum: minimum, maximum: maximum, separator: gap, label: "Tool parameter schema",
	})
}

func buildToolRule(script *grammarScript, tool toolSpec, rawText, gap, requiredGap, parameterClose, invokeClose string) (string, error) {
	name, err := safeAttributeValue(tool.Name, "Tool name")
	if err != nil {
		return "", err
	}
	compiler := newSchemaGrammar(script, tool.Schema, rawText, "tool-"+name)
	branches, err := compiler.objectBranches(tool.Schema)
	if err != nil {
		return "", err
	}
	parameterBranches := make([]string, 0, len(branches))
	for _, branch := range branches {
		parameters, err := buildParameterSequence(script, compiler, branch, gap, requiredGap, parameterClose)
		if err != nil {
			return "", err
		}
		parameterBranches = append(parameterBranches, parameters)
	}
	parameters, err := choice(parameterBranches)
	if err != nil {
		return "", err
	}
	return script.newRule("dsml-invoke-"+name, concat(invokeOpenRule(name, gap, requiredGap), gap, parameters, gap, invokeClose)), nil
}

func buildUncachedGrammar(tools []toolSpec, thinkingEnabled bool, mode string) (string, error) {
	script := newGrammarScript()
	gap := script.defineRule("dsml-gap", grammarGap)
	requiredGap := script.defineRule("dsml-required-gap", grammarRequiredGap)
	parameterClose := script.defineRule("dsml-parameter-close", dsmlClosingTagRule("parameter", gap))
	invokeClose := script.defineRule("dsml-invoke-close", dsmlClosingTagRule("invoke", gap))
	toolCallsOpen := script.defineRule("dsml-tool-calls-open", dsmlOpeningTagRule("tool_calls", "", gap, requiredGap))
	toolCallsClose := script.defineRule("dsml-tool-calls-close", dsmlClosingTagRule("tool_calls", gap))
	rawText, err := defineTextWithout(script, "dsv4-safe-text", rawStringExcludes)
	if err != nil {
		return "", err
	}
	content := rawText
	if len(tools) > 0 && mode != "none" {
		invokeRules := make([]string, 0, len(tools))
		for _, tool := range tools {
			rule, err := buildToolRule(script, tool, rawText, gap, requiredGap, parameterClose, invokeClose)
			if err != nil {
				return "", err
			}
			invokeRules = append(invokeRules, rule)
		}
		invokeBody, err := choice(invokeRules)
		if err != nil {
			return "", err
		}
		invoke := script.newRule("dsml-invoke", invokeBody)
		maxCalls := maxToolCalls
		batchBody, err := repeat(concat(invoke, gap), 1, &maxCalls)
		if err != nil {
			return "", err
		}
		batch := script.newRule("dsml-invoke-batch", batchBody)
		block := script.newRule("dsv4-tool-calls-block", concat(toolCallsOpen, gap, batch, toolCallsClose))
		toolOutput := script.newRule("dsv4-tool-output", concat(gap, block, gap))
		if mode == "required" {
			content = toolOutput
		} else {
			body, _ := choice([]string{toolOutput, rawText})
			content = script.newRule("dsv4-tool-or-text", body)
		}
	} else if mode == "required" {
		return "", fmt.Errorf("required DSV4 tool output needs at least one tool")
	}
	if thinkingEnabled {
		reasoning, err := defineTextWithout(script, "dsv4-reasoning", []string{thinkingEnd})
		if err != nil {
			return "", err
		}
		content = script.newRule("dsv4-completion-with-thinking", concat(reasoning, literal(thinkingEnd), gap, content))
	}
	script.defineRule("root", content)
	return script.render(), nil
}

func grammarKey(tools []toolSpec, thinkingEnabled bool, mode string) (string, error) {
	values := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		values = append(values, map[string]any{"name": tool.Name, "parameters": tool.Schema})
	}
	return stableJSON(map[string]any{
		"version": 4, "thinking_enabled": thinkingEnabled, "tool_choice": mode,
		"max_tool_calls": maxToolCalls, "tools": values,
	})
}

func buildGrammar(tools []toolSpec, thinkingEnabled bool, mode string) (string, error) {
	key, err := grammarKey(tools, thinkingEnabled, mode)
	if err != nil {
		return "", err
	}
	grammars.mu.Lock()
	if cached, ok := grammars.values[key]; ok {
		for index, item := range grammars.order {
			if item == key {
				grammars.order = append(append(grammars.order[:index], grammars.order[index+1:]...), key)
				break
			}
		}
		grammars.mu.Unlock()
		return cached, nil
	}
	grammars.mu.Unlock()

	grammar, err := buildUncachedGrammar(tools, thinkingEnabled, mode)
	if err != nil {
		return "", err
	}
	grammars.mu.Lock()
	if cached, ok := grammars.values[key]; ok {
		grammars.mu.Unlock()
		return cached, nil
	}
	grammars.values[key] = grammar
	grammars.order = append(grammars.order, key)
	for len(grammars.order) > maxGrammarEntries {
		delete(grammars.values, grammars.order[0])
		grammars.order = grammars.order[1:]
	}
	grammars.mu.Unlock()
	return grammar, nil
}
