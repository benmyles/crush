Start an interactive terminal session and get a terminal ID to control it.

Terminals run inside a dedicated tmux server, not the `bash` tool's
stateless shells. The program gets a real TTY, so interactive programs
like ssh, vim, less, python REPLs, and TUIs work, and the session keeps
running in the background with its state intact.

## What this is for

- Driving an interactive program: ssh sessions, database REPLs, editors
- Any program that needs a TTY or reads keystrokes (the `bash` tool
  cannot do this; its stdio is not a terminal)
- Long-lived sessions you want to survive Crush restarts or reconnect to
  from a later turn

## Sessions persist across restarts

Terminals live in a detached tmux server, so they survive if the agent
is interrupted or restarted. To reconnect, call terminal_start again
with the same `name`, or call terminal_output with no terminal_id to
list active terminals and their IDs.

## Controlling the terminal

- `terminal_input` — type text and press keys
- `terminal_output` — read the screen (optionally wait for text to
  appear, e.g. a prompt)
- `terminal_resize` — change the TTY dimensions
- `terminal_kill` — end the session (use it when you are done)

## When to use and not use

Use terminal_* for interactive sessions. For non-interactive commands
(compiling, tests, file inspection), use `bash` instead: it is cheaper,
sandboxed, and runs without a TTY. Only `ssh` and similar interactive
programs belong in a terminal.

## Notes

- All starts require user approval via the permission prompt.
- Up to 10 terminals can run at once; kill terminals you no longer
  need.
- The command runs through the default shell inside tmux, so quoting
  works like it does in your shell.
- Passwords or secrets you type are visible to the model in screen
  captures; prefer asking the user to handle password prompts, or
  non-interactive auth (keys, agent forwarding) when possible.
