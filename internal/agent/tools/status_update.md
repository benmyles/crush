# status_update

Records a structured status update for the session: what you recently
did, what you are doing now, what you will do next, and any blockers.
Treat this like a mini standup report shown to the user in the sidebar.

Use it:

- When the user enables status updates and you start or finish a
  meaningful piece of work.
- Roughly every couple of minutes during long-running work.
- When you finish a turn that accomplished something.
- When a reminder asks for an update.

Keep each field concise (a sentence or two). Use resume-style phrases
for `done` (e.g. "Added the caching layer"), present tense for `doing`
(e.g. "Writing migration tests"), and short intents for `next`. Put
anything blocking progress in `blockers`; omit the field entirely when
nothing blocks you.
