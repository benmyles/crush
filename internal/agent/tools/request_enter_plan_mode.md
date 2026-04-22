Request entering plan mode, then stop the current turn.

<usage>
- Use this when the user asks for a large, risky, ambiguous, or multi-step task
  that would benefit from an explicit plan before implementation.
- Include `prompt` with the exact task to restart in plan mode whenever the
  user already gave the task. The UI will queue that prompt in plan mode.
- Leave `prompt` empty only when the user explicitly asked to enable plan mode
  without starting work yet.
- After calling this tool, do not continue in the current turn.
</usage>
