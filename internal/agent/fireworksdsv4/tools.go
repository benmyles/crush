package fireworksdsv4

import (
	"encoding/json"
	"fmt"
	"slices"

	"charm.land/fantasy"
	jsonvalidator "github.com/kaptinlin/jsonschema"
)

type toolSpec struct {
	Name        string
	Description string
	Schema      jsonSchema
	Validator   *jsonvalidator.Schema
	Synthetic   bool
}

type preparedTools struct {
	tools        []toolSpec
	chatRequired bool
	grammarMode  string
}

func compileTool(name, description string, schema jsonSchema, synthetic bool) (toolSpec, error) {
	data, err := json.Marshal(schema)
	if err != nil {
		return toolSpec{}, fmt.Errorf("marshaling schema for DSV4 tool %q: %w", name, err)
	}
	validator, err := jsonvalidator.NewCompiler().Compile(data)
	if err != nil {
		return toolSpec{}, fmt.Errorf("compiling schema for DSV4 tool %q: %w", name, err)
	}
	return toolSpec{Name: name, Description: description, Schema: schema, Validator: validator, Synthetic: synthetic}, nil
}

func functionTool(value fantasy.Tool) (fantasy.FunctionTool, bool) {
	switch tool := value.(type) {
	case fantasy.FunctionTool:
		return tool, true
	case *fantasy.FunctionTool:
		return *tool, true
	default:
		return fantasy.FunctionTool{}, false
	}
}

func prepareToolSpecs(call fantasy.Call) (preparedTools, error) {
	choiceValue := fantasy.ToolChoiceAuto
	if call.ToolChoice != nil {
		choiceValue = *call.ToolChoice
	}
	if choiceValue == fantasy.ToolChoiceNone {
		return preparedTools{grammarMode: "none"}, nil
	}

	tools := make([]toolSpec, 0, len(call.Tools)+1)
	for _, value := range call.Tools {
		tool, ok := functionTool(value)
		if !ok {
			return preparedTools{}, fmt.Errorf("fireworks DSV4 supports only function tools; %q is provider-defined", value.GetName())
		}
		if tool.Name == chatToolName {
			return preparedTools{}, fmt.Errorf("fireworks DSV4 reserves tool name %q for assistant chat messages", chatToolName)
		}
		if _, err := safeAttributeValue(tool.Name, "Tool name"); err != nil {
			return preparedTools{}, err
		}
		schema := tool.InputSchema
		if schema == nil {
			schema = jsonSchema{"type": "object"}
		}
		spec, err := compileTool(tool.Name, tool.Description, schema, false)
		if err != nil {
			return preparedTools{}, err
		}
		tools = append(tools, spec)
	}

	if choiceValue != fantasy.ToolChoiceAuto && choiceValue != fantasy.ToolChoiceRequired {
		name := string(choiceValue)
		index := slices.IndexFunc(tools, func(tool toolSpec) bool { return tool.Name == name })
		if index < 0 {
			return preparedTools{}, fmt.Errorf("required DSV4 tool %q is unavailable", name)
		}
		return preparedTools{tools: []toolSpec{tools[index]}, grammarMode: "required"}, nil
	}
	if len(tools) == 0 {
		if choiceValue == fantasy.ToolChoiceRequired {
			return preparedTools{}, fmt.Errorf("required DSV4 tool output needs at least one tool")
		}
		return preparedTools{grammarMode: "auto"}, nil
	}

	chatSchema := jsonSchema{
		"type": "object",
		"properties": map[string]any{
			"message": map[string]any{
				"type":        "string",
				"description": "User-visible Markdown message. Send a concise progress update while work continues, or the complete answer when work is done.",
			},
		},
		"required":             []string{"message"},
		"additionalProperties": false,
	}
	chat, err := compileTool(chatToolName, "Send a user-visible chat message. Batch this call with a work tool to post an update and continue. Call it by itself only when the task is complete.", chatSchema, true)
	if err != nil {
		return preparedTools{}, err
	}
	return preparedTools{tools: append(tools, chat), chatRequired: true, grammarMode: "required"}, nil
}

func (p preparedTools) byName() map[string]toolSpec {
	result := make(map[string]toolSpec, len(p.tools))
	for _, tool := range p.tools {
		result[tool.Name] = tool
	}
	return result
}
