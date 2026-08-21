# set_terminal_title

Sets the terminal window title to a curated 2-4 word phrase describing
what you are working on right now. The title replaces the default
prompt-based title until you clear it or the user sends a new message.

Use it:

- When you start a new task or pick up a different one mid-session.
- When the current task changes meaningfully and the old title no
  longer applies.
- When work completes, clear it by passing an empty `title` string.

Craft titles as terse, lowercase, present-tense phrases (e.g. "fixing
deploy pipeline", "migrating auth queries", "writing database tests").
Never include secrets, tokens, or sensitive values in the title.
