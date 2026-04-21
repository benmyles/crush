Send input to a running interactive background shell by ID.

<usage>
- Provide the shell ID returned from a background bash execution.
- Use input for the exact text to send.
- Set press_enter=true when answering a prompt or submitting a line.
</usage>

<features>
- Continue commands that are waiting for interactive input.
- Works with terminal-backed jobs that report support for input.
- Use job_output after sending input to inspect the updated output.
</features>

<tips>
- Do not include a trailing newline in input when press_enter=true.
- Use an empty input with press_enter=true to press Enter without text.
- If the job does not support input, explain the limitation to the user.
</tips>
