Read the screen of an interactive terminal started with terminal_start.

## Reading a screen

`terminal_output` with a terminal_id returns the terminal's current
visible screen with escape sequences stripped. The capture is
positional: blank lines and spacing are meaningful (useful for TUIs
like vim). Output is truncated if very large.

## Waiting for output (expect-lite)

Use `wait_for` to block until text you expect appears, then get the
full history in one call. This is the right way to drive interactive
programs:

- Wait for `password:` before typing a password
- Wait for a shell prompt (e.g. `$ `) before sending the next command
- Wait for an installer question or an error message before acting

Example: `terminal_output terminal_id=crush-abc wait_for="password:" timeout_ms=5000`

`timeout_ms` is required (1-30000 ms, roughly 30 seconds max).

## Waiting is capped at 30 seconds

Every terminal_output call with wait_for waits at most 30 seconds,
then returns whatever the terminal produced in that time. If the text
was not found, the response says so, shows everything captured so far,
and notes that the terminal is still running.

To keep waiting past 30 seconds, call terminal_output again with the
same wait_for and timeout_ms. Each call covers the next 30 seconds of
output, so the agent never blocks without receiving fresh updates.

## Patterns for slow terminals

- Long ssh command on a remote server: send the command with
  terminal_input, then loop terminal_output (wait_for the shell prompt
  or an expected completion marker). Each loop iteration waits at most
  30 seconds, so you get frequent progress updates.
- Slow program boot: after terminal_start, poll terminal_output
  wait_for="ready" (or whatever prompt the program prints) instead of
  guessing how long startup takes.
- Installers, remote builds, or anything with progress inside the
  terminal: wait_for a milestone string (e.g. "done", "error", "100%")
  and re-call until it appears.
- Quick reads: terminal_output without wait_for returns the current
  screen immediately and never waits.

## History vs current screen

By default the current visible pane is returned. Set `history: true`
to include the scrollback buffer, which is useful after running a
command that produced lots of output.

## Listing terminals

Call terminal_output without a terminal_id to list every active
terminal with its ID, current program, and size. Use this to reconnect
after an interruption or restart.
