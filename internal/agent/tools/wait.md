Wait for a number of seconds or until a background shell job completes.

<usage>
- Set shell_id to wait until a background job completes (preferred over seconds-only, since it returns as soon as the job finishes).
- Set both shell_id and seconds to wait for the job with a timeout.
- Set seconds without shell_id to sleep for that duration.
- Use job_output after waiting when you need stdout or stderr.
</usage>
