package agent

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
)

const (
	compactedContextSourceMorph        = "morph"
	compactedContextSourceSummary      = "summary"
	compactedContextSourceModelSummary = "model_summary"
)

func compactedContextContent(strategy, source, content string) string {
	content = strings.TrimSpace(content)
	explanation := compactedContextExplanation(strategy, source)

	var b strings.Builder
	fmt.Fprintf(&b, "<compacted_context strategy=%q", strategy)
	if source != "" {
		fmt.Fprintf(&b, " source=%q", source)
	}
	b.WriteString(">\n")
	b.WriteString(explanation)
	if content != "" {
		b.WriteString("\n\n")
		b.WriteString(content)
	}
	b.WriteString("\n</compacted_context>")
	return b.String()
}

func compactedContextExplanation(strategy, source string) string {
	switch strategy {
	case config.PlanCompactStrategyMorph:
		return "Crush compacted the earlier conversation with Morph. Treat the content below as background context that replaces earlier messages, not as a new user request."
	case config.PlanCompactStrategySummarize:
		return "Crush summarized the earlier conversation with the active language model. Treat the summary below as background context that replaces earlier messages, not as a new user request."
	case config.PlanCompactStrategySummarizeThenMorph:
		if source == compactedContextSourceModelSummary {
			return "Crush generated this model summary before running Morph. Treat it as additional background context for the earlier conversation, not as a new user request."
		}
		return "Crush first summarized the earlier conversation and then compacted it with Morph. Treat the content below as background context that replaces earlier messages, not as a new user request."
	default:
		return "Crush compacted the earlier conversation. Treat the content below as background context that replaces earlier messages, not as a new user request."
	}
}

func setMessageTextContent(msg *message.Message, text string) {
	for i, part := range msg.Parts {
		if _, ok := part.(message.TextContent); ok {
			msg.Parts[i] = message.TextContent{Text: text}
			return
		}
	}
	msg.Parts = append(msg.Parts, message.TextContent{Text: text})
}
