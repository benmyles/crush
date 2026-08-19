Send keystrokes to an interactive terminal started with terminal_start.

## How to drive a terminal

The typical loop is:

1. terminal_start to open a session (e.g. `ssh user@host`)
2. terminal_input to type text or press keys
3. terminal_output to wait for and read the result

terminal_input sends input AND returns the updated screen by default,
so a single call both acts and shows the effect.

## Sending control keys

`text` is typed literally (great for shell commands). Then `keys` can
press one or more control keys, in tmux key-name syntax:

- `"enter"`, `"tab"`, `"esc"`, `"backspace"`, `"delete"`, `"space"`
- `"up"`, `"down"`, `"left"`, `"right"`, `"home"`, `"end"`, `"pgup"`,
  `"pgdown"`
- `"f1"` through `"f12"`
- `"ctrl-a"` through `"ctrl-z"` (e.g. `"ctrl-c"`, `"ctrl-d"`)

`enter` is a convenience flag equivalent to adding `"enter"` to keys.
To confirm a prompt: `{"text": "yes", "enter": true}`.

## Rapid sequences

For multi-step interactions where the screen in-between does not
matter, send each step with `read_back: false`, then read once with
terminal_output. For example: send password, wait for prompt, send a
command.

## Notes

- Sending input requires user approval via the permission prompt unless
  the session is already approved.
- Wait for the program to respond before sending more input. Use
  terminal_output's `wait_for` to wait for prompts like `password:` or
  a shell prompt.
