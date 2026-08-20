# goal_complete

Marks the session's active goal as complete. Use this only when every
part of the goal has genuinely been finished and verified.

This is the single source of truth that stops the goal supervision loop:
after completion, no further goal checks run. Provide a short summary of
what was accomplished so the user can review it with `/goal:show`.

This tool errors when no goal is active; do not call it to "clear" a goal
without completing it.
