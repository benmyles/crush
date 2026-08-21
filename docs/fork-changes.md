# Fork Changes

This document catalogs every change this fork carries on top of upstream
[charmbracelet/crush](https://github.com/charmbracelet/crush). It diverges
from upstream `main` at commit `0f14334f` (2026-08-18, the
`chore: normalize line endings to lf` commit) and adds 49 commits organized
around ten feature areas.

For the deep design rationale behind the largest change, see
[`docs/compaction-plan.md`](./compaction-plan.md).

## Feature areas

### 1. Lossless context compaction engine

The largest change in the fork. Long sessions no longer hit the context
window: when a conversation approaches the model's window, the older part is
compacted into a structured checkpoint plus a deterministic ledger, transcript
map, budgeted verbatim extracts, working-set snapshot, and exact-recovery
note. Raw messages are never thrown away — they stay in the session store and
remain searchable and recoverable.

- **Engine** (`internal/compaction/`): span model, budget governor,
  deterministic ledger + causality index, transcript map, exact-recovery
  note, checkpoint lane with monotonic ID merge and coverage audit
  (`judge`/`checks`/`off` verify modes), three-level escalation guard
  (fail-closed on model error with one transient retry), deterministic
  local extractive lane, working-set snapshot, and optional parallel block
  compaction (off by default). `docs/compaction-plan.md` documents the
  per-area implementation status and deviations from the original design.
- **Trigger**: hard threshold (`window − reserve`) plus a soft, structure-aware
  rubric; synchronous at a step boundary in all cases. `options.compaction`
  covers `reserve-tokens`, `keep-recent-tokens`, `verify`, and more.
- **Compaction model slot**: optional dedicated `models.compaction` model
  (crushrc: `model compaction <provider>/<id>`), picked per dialog, so
  checkpoints can run on a cheaper model than the coding model.
- **UI**: `/compact` command with pulse loader and overview tree, a
  **Compaction Settings** dialog in the model picker, live compaction status
  pills and progress, and collapsed summaries on completion. Compaction and
  summarization output is collapsed by default.
- **Agent tools**: `compact_context` (with soft-threshold gating so early
  requests are declined with a usage report), `recall_grep` (FTS5 search over
  compacted history, grouped by covering summary), `recall_expand`,
  `recall_describe`, and `llm_map` (deterministic parallel map over JSONL).
- **Robustness**: recursive compaction loops prevented; engine failures fall
  back to the legacy single-shot summary rather than aborting the turn.

Commits: `5bdfc1ce`…`5ca940b6` (the `feat(compaction)`/`fix(compaction)`/
`docs: add SOTA compaction engine design plan` series).

### 2. Session goals and global pause/resume

Sessions can be given an objective that the agent supervises itself. After
every turn, the active goal is re-checked with a visible transcript prompt;
the agent can update, complete, or block the goal through dedicated tools.

- `/goal` commands plus `update_goal`, `goal_complete`, and `goal_blocked`
  tools.
- Goal state persists per session in SQLite and survives restarts; checks
  stall after repeated turns without user input.
- A global pause fence stops busy sessions at their next step boundary and
  resumes exactly where they stopped, without duplicating transcript
  messages.
- Goal state, pause state, and commands surface in the sidebar, pills,
  command palette, and window title (goal text + state emoji). `ctrl+x`
  (alongside `ctrl+end`) jumps to the live tail and re-engages follow mode.
- Works over the client/server wire protocol (backend, client proto, server
  endpoints).

Commits: `2f7a1868`, `e9f22ad0`, `c37079ba`, `d655ffac` (partial).

### 3. Agent status updates with periodic reminders

Agents can report mini standup updates via the `status_update` tool
(`done`/`doing`/`next`). Updates are persisted per session, shown
in the UI sidebar, and a reminder loop prods the agent for a fresh update
every couple of minutes of continuous work. The injected reminder also asks
the agent to reconcile its todo list: mark finished work completed and
split or update stale items so the list always reflects the work that
truly remains.

Commits: `61f6491b`.

### 4. Interactive terminal tools (tmux-backed)

A new family of tools lets agents drive interactive programs over a real TTY:
`terminal_start`, `terminal_input`, `terminal_output`, `terminal_resize`, and
`terminal_kill`. Sessions live in a dedicated tmux server pinned to the data
directory, so they persist across agent interruptions and Crush restarts and
can be reconnected by name or listed on demand. Start, input, and kill are
permission-gated.

Commits: `03cf0018`. Backing package: `internal/terminal/`.

### 5. ChatGPT codex backend

Log in with a ChatGPT subscription and use codex models through the same
transport the official client uses: zstd-compressed requests, WebSocket
streaming with SSE fallback, always-on encrypted reasoning content, and
prompt caching. Codex models advertise 500k context with auto-compaction at
372k. Backing packages: `internal/agent/codex`, `internal/oauth/codex`
(+ OAuth dialog), `internal/cmd/{login,logout}` extensions.

Commits: `8db5a67b`.

### 6. Session rewind

With chat focused, select an earlier user message and press `ctrl+r` (or
**Resume From Here** in the command palette) to truncate the session at that
message after confirming. The message text is moved back into the prompt, and
compaction state is reset when its anchors are cut. OpenAPI spec updated to
match.

Commits: `5953b0c4`, `1532f58b`.

### 7. Hooks: SessionStart lifecycle event and hook fragments

- New `SessionStart` lifecycle hook: fires once per session per Crush
  process (covers fresh sessions and `crush --continue` / `--session`
  resumes), informational only — cannot deny, halt, rewrite, or block.
- **Hook fragments**: integrations can register hooks without editing
  `crush.json` or `crushrc` by dropping `*.json` files in the global config
  dir's `hooks/` subdirectory (`~/.config/crush/hooks/` or
  `$CRUSH_GLOBAL_CONFIG/hooks/`). Merged after every other config source, at
  load and reload time, in sorted filename order. Built for the **herdr**
  integration.
- New `internal/hookevent` package for shared event-name constants.

Commits: `aff69396`, `a23fbed3`. Docs: `docs/hooks/README.md`.

### 8. Thirty-second cap on blocking tool calls

Nothing the agent runs can stall for more than 30 seconds: terminal captures
and waits, `job_output` waits, and foreground bash commands (auto-background
at 30 seconds) all yield with whatever output was collected. Tool docs spell
out the caps and polling patterns for long runs (remote ssh, long test
suites).

Commits: `9940f437`.

### 9. Queued prompt editing and removal

Queued prompts carry a stable queue ID and the expanded queue pill gains a
cursor: enter pulls a prompt back into the composer for editing and
resending, `x` removes it. A new DELETE endpoint backs removal and publishes
a cancelled `RunComplete` for removed items, so `crush run` callers never
hang.

Commits: `3c2f03db`.

### 10. Skills in the command menu

Skills appear in the command palette under a divider with their own section,
match on any typing substring, and insert a `skill:<name>=<location>`
reference into the composer on enter. Skill chips render in the UI.

Commits: `e5b3c10b`, `0271ad47`.

### 11. Live write progress and window title polish

- Live write progress driven by streamed tool input (`5ca940b6`).
- Terminal window title reflects session and busy state; when a goal is
  active it shows goal text with a state glyph instead of status
  placeholders (`44a86d11`, `c37079ba`). State glyphs are plain unicode
  characters rather than emoji, and the `set_terminal_title` tool lets
  the agent curate a 2-4 word title for the current task.
- All in-flight UI indicators (tool rows, nested agent runs, thinking,
  compacting) render the shared received-data meter
  (`1k [====......] 1,024 chars`) instead of cycling spinner animations.
- Queued prompts can be edited or removed without navigating the pills
  panel: `ctrl+e` recalls the selected queued prompt into the composer,
  `ctrl+q` removes it, and both appear in the commands (ctrl+p) panel and
  the pills hint.
- Sub-agents can be disabled with `options.disable_subagents` (crushrc:
  `options subagents <bool>`) or the commands (ctrl+p) panel; the coder
  prompt drops the agent-tool guidance when disabled.
- The coder prompt prefers the tmux-backed terminal tools over the bash
  tool for interactive sessions and warns against terminal_output
  `wait_for` strings that match the typed command echo (use computed
  completion markers instead).

### 12. Live sub-agent progress dock

While one or more sub-agents (the `agent` task tool or `agentic_fetch`)
are running, a full-width dock appears between the chat and the editor.
It shows one row per running sub-agent with a live view of what each is
doing: kind tag, current nested tool, the shared received-data meter fed
by streamed tool input, and elapsed time. Finished rows show `✓ done` and
linger a few seconds before the dock collapses.

- `tab` cycles editor → chat → dock → editor while the dock is visible;
  `up`/`down` (or clicking) selects an active agent.
- `m` opens an inline compose row on the selected agent: typing a message
  and pressing enter delivers it to the running sub-agent at its next step
  boundary, where the turn continues with the new instruction and keeps
  its tool context. Delivery rides a new
  `POST /v1/workspaces/{id}/agent/sessions/{sid}/message` endpoint end to
  end (backend, client proto, workspace layer).
- `x` cancels the selected sub-agent run; `esc` returns focus to the
  chat.
- Lifecycle is driven by `subagent_started`/`subagent_finished`
  notifications plus live nested-tool events; the coordinator now keeps a
  registry of live child sessions so cancel, busy probes, and interim
  messages reach sub-agent runs that live on their own SessionAgent
  instances.

## Configuration surface

New config touches beyond upstream:

- `options.compaction.*` — engine toggle, token budgets, verify mode,
  parallel block threshold (crushrc: `option compaction <key> <value>`).
- `models.compaction` — dedicated compaction model slot
  (crushrc: `model compaction <provider>/<id>`).
- `options.disable_subagents` — removes the agent tool from the coder's
  allowed tools (crushrc: `options subagents <bool>`, default enabled).
- `models` / `model picker` compaction slot in the TUI settings dialog.
- Goals, pause state, and status updates persist via three new SQLite
  migrations:
  `20260818000000_add_compaction_engine.sql`,
  `20260819000000_add_goals.sql`,
  `20260819000001_add_status_updates.sql`.
- Hook fragments directory: `$CRUSH_GLOBAL_CONFIG/hooks/*.json`.

## Tooling

- **`install.sh`**: builds the fork and installs it as `~/bin/crush` via a
  `.crush-next` swap (`2f890c7a`).
- **Vendored dependencies**: the entire module graph is vendored under
  `vendor/` (notably `mvdan.cc/sh/v3` for terminal-tool command parsing), so
  transport behavior can be tuned without waiting on upstream releases
  (`8db5a67b`).
- **`.gitattributes`**: normalize line endings to LF (`0f14334f`, inherited
  from upstream).

## New agent tools summary

| Tool | Purpose |
| --- | --- |
| `compact_context` | Request compaction at a natural milestone; declines below the soft threshold with a usage report |
| `recall_grep` / `recall_expand` / `recall_describe` | Search and expand compacted session history |
| `llm_map` | Deterministic parallel map over a JSONL dataset |
| `update_goal` / `goal_complete` / `goal_blocked` | Session goal lifecycle |
| `status_update` | Mini standup updates surfaced in the sidebar |
| `set_terminal_title` | Curated 2-4 word terminal window title for the current task |
| `terminal_start` / `terminal_input` / `terminal_output` / `terminal_resize` / `terminal_kill` | tmux-backed interactive terminal control |
