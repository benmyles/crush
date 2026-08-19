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

Example: `terminal_output terminal_id=crush-abc wait_for="password:"`

The default timeout is 10 seconds. If not found in time, the latest
screen is still returned along with a note.

## History vs current screen

By default the current visible pane is returned. Set `history: true`
to include the scrollback buffer, which is useful after running a
command that produced lots of output.

## Listing terminals

Call terminal_output without a terminal_id to list every active
terminal with its ID, current program, and size. Use this to reconnect
after an interruption or restart.
