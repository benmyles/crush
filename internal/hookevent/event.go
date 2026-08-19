// Package hookevent holds hook event name constants shared between the
// hooks runner and config packages so neither has to import the other
// (config compiles hooks; hooks reads config).
package hookevent

// Hook event names.
const (
	PreToolUse   = "PreToolUse"
	SessionStart = "SessionStart"
)
