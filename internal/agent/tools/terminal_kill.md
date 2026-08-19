Terminate an interactive terminal started with terminal_start.

Kill sessions when you are done with them; up to 10 terminals can run
at once. Terminal sessions persist in tmux even after the agent
continues or restarts, so anything left running keeps consuming
resources until killed.

Set `all: true` to kill every active Crush terminal in one call, e.g.
when finishing a task that used several ssh sessions.
