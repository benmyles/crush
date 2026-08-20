# update_goal

Sets or updates the session's active goal. The goal is the objective the
agent autonomously supervises: while it is active, the agent is prodded to
verify completion after every turn and expected to keep working until the
goal is complete.

Use this tool when the user asks the agent to take on a goal, when the
real objective turns out to be larger or different than the current goal,
or when sub-work is finished and the goal should be narrowed to what
remains.

The goal text should be outcome-focused, phrased in the imperative, and
express what "done" means (e.g. "Make every unit test pass without
skipping any").
