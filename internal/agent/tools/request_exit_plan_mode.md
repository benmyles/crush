Request leaving plan mode, then stop the current turn.

<usage>
- Use this when the current turn is in plan mode but the user clearly asked you
  to proceed with implementation or otherwise continue outside plan mode.
- Include `prompt` with the exact continuation task to run outside plan mode.
  The UI will queue that prompt after disabling plan mode.
- Leave `prompt` empty only when the user explicitly asked to disable plan mode
  without starting implementation yet.
- Do not use this to bypass required plan review. If the user wants a plan,
  submit or revise the plan with `submit_plan`.
- After calling this tool, do not continue in the current turn.
</usage>
