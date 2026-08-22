package fireworksdsv4

const (
	// Name is the bundled provider's config and Fantasy identifier.
	Name = "fireworks-dsv4"
	// DisplayName is the provider label shown in model selection UI.
	DisplayName = "Fireworks DSV4 (Constrained)"

	beginToken        = "<｜begin▁of▁sentence｜>"
	endToken          = "<｜end▁of▁sentence｜>"
	userToken         = "<｜User｜>"
	assistantToken    = "<｜Assistant｜>"
	thinkingStart     = "<think>"
	thinkingEnd       = "</think>"
	dsmlToken         = "｜DSML｜"
	toolCallsOpen     = "<" + dsmlToken + "tool_calls>"
	toolCallsClose    = "</" + dsmlToken + "tool_calls>"
	invokeClose       = "</" + dsmlToken + "invoke>"
	parameterClose    = "</" + dsmlToken + "parameter>"
	chatToolName      = "send_chat_message"
	maxToolCalls      = 5
	maxDynamicMembers = 32
	maxDynamicItems   = 64
	maxJSONDepth      = 8
	maxGrammarEntries = 128
	maxErrorBodyBytes = 8_192
)

const highReasoningGuidance = "Reasoning Effort: Absolute maximum with no shortcuts permitted.\n" +
	"You MUST be very thorough in your thinking and comprehensively decompose the problem to resolve the root cause, rigorously stress-testing your logic against all potential paths, edge cases, and adversarial scenarios.\n" +
	"Explicitly write out your entire deliberation process, documenting every intermediate step, considered alternative, and rejected hypothesis to ensure absolutely no assumption is left unchecked.\n\n"

const maxReasoningGuidance = "Reasoning Effort: Beyond maximum — exhaustive, relentless, and uncompromising.\n" +
	"You MUST reason with the utmost depth and rigor, leaving absolutely nothing to chance: exhaustively decompose the problem into its most fundamental components, trace every causal chain to its root, and resolve the underlying cause rather than any surface symptom.\n" +
	"Do not stop reasoning until you have independently verified the solution from multiple angles and are certain that no assumption remains unchecked and no error remains undiscovered.\n\n"
