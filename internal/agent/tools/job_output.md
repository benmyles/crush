Get stdout/stderr from a background shell by ID.

## Polling

Call without wait (or wait=false) to get the output collected so far;
the response includes the shell's status (running/completed). Use this
for servers and watchers that run indefinitely.

## Waiting

Set wait=true to block until the shell completes, then get all of its
output in one call. Waiting is capped at 30 seconds: if the shell is
still running when the cap is hit, the response returns everything
captured so far and says the shell is still running.

To keep waiting past 30 seconds, call job_output again with the same
shell_id. Each call covers the next 30 seconds of output, so you never
block without receiving fresh updates.

`timeout_ms` controls the wait (default 30000, max 30000). job_kill
terminates a background shell.

## Patterns for long runs

- Quick listing, fast build, or small check: plain `bash` with no
  background flags; it returns when done (anything over 30 seconds
  auto-backgrounds anyway).
- Long test suite or any command that may run longer than 30 seconds:
  start it with `bash` run_in_background=true, then call job_output
  with wait=true. Each wait returns after at most 30 seconds with
  everything captured so far; repeat with the same shell_id until the
  status becomes "completed", then read the exit code.
- Long-lived servers or watchers: poll job_output without wait to see
  the current output whenever you need it, and job_kill when done.
