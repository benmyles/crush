# goal_blocked

Marks the session's active goal as blocked, halting the goal supervision
loop until the user reactivates it with `/goal:resume`.

Use this when meaningful progress is impossible: required access or
credentials are missing, a decision is needed from the user, or repeated
attempts keep failing. Always provide a concise, specific reason. The
user sees the reason in the goal status; do not keep retrying a blocker
the tool has already recorded.

This tool errors when no goal is active.
