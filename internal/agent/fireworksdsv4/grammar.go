package fireworksdsv4

import (
	"fmt"
	"sort"
	"sync"
)

const grammarGap = `[\t\n\r ]{0,64}`

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

func parameterOpen(name string, stringFlag bool) string {
	return fmt.Sprintf("<%sparameter name=%q string=%q>", dsmlToken, name, fmt.Sprint(stringFlag))
}

func buildParameterRule(script *grammarScript, compiler *schemaGrammar, name string, schema jsonSchema) (string, error) {
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
		rules = append(rules, concat(literal(parameterOpen(name, branch.stringFlag)), branch.valueRule, literal(parameterClose)))
	}
	body, err := choice(rules)
	if err != nil {
		return "", err
	}
	return script.newRule("dsml-parameter-"+name, body), nil
}

func buildParameterSequence(script *grammarScript, compiler *schemaGrammar, schema jsonSchema, gap string) (string, error) {
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
		rule, err := buildParameterRule(script, compiler, name, property)
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

func buildToolRule(script *grammarScript, tool toolSpec, rawText, gap string) (string, error) {
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
		parameters, err := buildParameterSequence(script, compiler, branch, gap)
		if err != nil {
			return "", err
		}
		parameterBranches = append(parameterBranches, parameters)
	}
	parameters, err := choice(parameterBranches)
	if err != nil {
		return "", err
	}
	return script.newRule("dsml-invoke-"+name, concat(literal(fmt.Sprintf("<%sinvoke name=%q>", dsmlToken, name)), gap, parameters, gap, literal(invokeClose))), nil
}

func buildUncachedGrammar(tools []toolSpec, thinkingEnabled bool, mode string) (string, error) {
	script := newGrammarScript()
	gap := script.defineRule("dsml-gap", grammarGap)
	rawText, err := defineTextWithout(script, "dsv4-safe-text", rawStringExcludes)
	if err != nil {
		return "", err
	}
	content := rawText
	if len(tools) > 0 && mode != "none" {
		invokeRules := make([]string, 0, len(tools))
		for _, tool := range tools {
			rule, err := buildToolRule(script, tool, rawText, gap)
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
		block := script.newRule("dsv4-tool-calls-block", concat(literal(toolCallsOpen), gap, batch, literal(toolCallsClose)))
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
		"version": 3, "thinking_enabled": thinkingEnabled, "tool_choice": mode,
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
