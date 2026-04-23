Get stdout/stderr from a background shell by ID; set wait=true to wait for completion.

<usage>
- Provide the shell ID returned from a background bash execution
- Returns the current stdout and stderr output
- Indicates whether the shell has completed execution
- Set wait=true to wait until the shell completes, the request context is done,
  or an interactive shell is idle and waiting for input
</usage>

<features>
- View output from running background processes
- Check if background process has completed
- Get cumulative output from process start
- Optionally wait for process completion
- Returns early for input-capable jobs that appear to be waiting for interaction
</features>

<tips>
- Use this to monitor long-running processes
- Check the 'done' status to see if process completed
- Can be called multiple times to view incremental output
- Use wait=true when you need the final output and exit status
- Prefer job_output with wait=true over wait with seconds only, since it returns as soon as the job completes instead of blocking for a fixed duration
- If the returned status is running and the job accepts input, use job_input to
  respond to prompts instead of calling job_output with wait=true again
</tips>
