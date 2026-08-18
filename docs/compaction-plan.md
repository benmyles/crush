# Crush Compaction Engine — Design Plan

A plan for a new compaction engine for Crush that takes the best of **LCM**
(Lossless Context Management, Voltropy 2026), **ShiftUp's pi-shiftup-compaction**
engine, and the **2026 research literature**, grounded in Crush's existing
Go / SQLite / fantasy / pubsub architecture.

Status: **implemented** in `internal/compaction` (Phases 1–3 and the retrieval /
operator parts of Phase 4). See "Implementation status" below for what shipped,
where the implementation deliberately deviates from this design, and what is
deferred. Sections marked *Status:* further down record the per-feature outcome.

---

## Implementation status

| Area | Status |
| --- | --- |
| Immutable store + summary DAG (`compaction_summaries`, `parent_ids`, `covered_message_ids`, `first_retained_message_id`) | Shipped. Nodes are `leaf`; each new node parents the previous active node (a chain), and each compaction covers only the span since the previous node's retained cut (incremental). Condensed nodes are not built. |
| Span model, budget governor, deterministic ledger + causality index, transcript map, exact-recovery note | Shipped (`span.go`, `budget.go`, `ledger.go`, `transcript.go`). Anchors are **session-absolute ordinals** in the summary-free message list (not rowids); `recall_grep` prints the same ordinal. |
| Checkpoint lane with monotonic ID merge, validation, coverage audit (judge) | Shipped (`checkpoint.go`, `engine.go`). Verify modes: `judge` (model audit) or `checks`/`off` (structural validation only). |
| Three-level escalation guard | Shipped (`escalation.go`) with a twist: model **errors fail closed** (one transient retry, then abort); the deterministic level is reached only when both model levels fail to *converge*. Truncated level-1 output is retried once at 1.6× with a retry note. |
| Extractive lane (golden spans, line anchors, older-lane decay) | Shipped as a **deterministic local compressor** bounded by the budget governor's extracts allotment; no LLM relevance pass. |
| Working-set snapshot | Shipped (`workingset.go`, cwd-bounded, secret-like names skipped). Git snapshot: **not shipped**. |
| Fail-closed pipeline | Shipped for the engine. If the engine fails, `Summarize` falls back to the legacy single-shot summary (logged) rather than aborting the turn. |
| Trigger: hard threshold `window − reserve`, soft threshold + structure-aware rubric | Shipped (`trigger.go`, `agent.go`). Compaction is **synchronous** at a step boundary in all cases (no async swap-in yet); `keep_recent`/`reserve` are clamped to the window for small models. |
| Agent-initiated `compact_context` | Shipped as a request flag honored at the next step boundary. |
| Parallel block compaction | Shipped (`parallel.go`), fail-closed, off by default (`parallel_block_threshold: 0`). |
| `recall_grep`, `recall_expand` (sub-agents only), `recall_describe` | Shipped. `recall_grep` phrase-quotes the pattern (FTS5) with a `LIKE` fallback and groups hits by covering summary. |
| `llm_map` | Shipped (permission-gated output write, cwd-relative paths). `agentic_map`: **not shipped**. |
| Scope-reduction invariant for nested sub-agents | **Not shipped** (the task agent does not receive the `agent` tool, so the check would be dead code). |
| Optional embedding index + `recall_query` | **Deferred** (no schema, no tool). |
| Large-file references + exploration summaries | **Deferred** (no schema). |
| Pubsub compaction lifecycle events / TUI footer | **Deferred**. The composed summary is stored as a summary message so the chat renders it. |
| Configurable summarizer model / reasoning | Shipped as the optional `models.compaction` slot (`model compaction <provider>/<id> [--reasoning-effort …]` in crushrc, the **Compaction** slot of the TUI model picker, or `models.compaction` in `crush.json`). Unset → the active large model. |
| Config (`options.compaction`, `option compaction …` in crushrc) | Shipped; see §6.4. Also editable in the TUI: the **Compaction Settings** dialog (command palette) edits every `options.compaction` key and the compaction model's reasoning/thinking, and the model picker (`ctrl+l`) has a **Compaction** slot with a "Same as the large model" default. |

---

## 1. Where Crush is today

*(This section describes Crush before the engine landed. The single-shot path
described here still exists as the fallback used when the engine is disabled
or fails.)*

Crush's current compaction is a single-shot LLM summarization in
`internal/agent/agent.go` (`Summarize`):

- **Trigger**: when remaining context drops to a threshold — `20_000` tokens for
  models with `ContextWindow > 200_000`, otherwise `20%` of the window
  (`largeContextWindowThreshold` / `largeContextWindowBuffer` /
  `smallContextWindowRatio`).
- **Produce**: stream one assistant message using `templates/summary.md`, marked
  `IsSummaryMessage: true`.
- **Consume**: `getSessionMessages` keeps only the summary message onward
  (`msgs = msgs[summaryMsgIndex:]`, retyped to `User`). Everything before the
  summary is **dropped from context**.
- **Persistence**: the dropped messages stay in SQLite (`internal/db`), but
  nothing indexes or retrieves them. `sessions.summary_message_id` and
  `messages.is_summary_message` are the only compaction-related schema.
- **Config**: a single `disable_auto_summarize` bool and per-model
  `ContextWindow`. No reserve/keep-recent, no budget, no escalation, no
  retrieval, no DAG, no deterministic facts, no validation, no retry.

What's missing vs. SOTA: lossless retrievability, a summary DAG, guaranteed
convergence, deterministic (model-free) memory, a budget governor, checkpoint
validation/merge, exact transcript recovery, parallel/block compaction,
operator-level recursion, adaptive timing, and on-demand retrieval.

The good news: **Crush already has the perfect substrate.** SQLite gives
transactional writes, foreign-key integrity, and FTS5 full-text search for free.
Messages are already append-only and never mutated — the Immutable Store already
exists; it just isn't used as one. The coordinator already has sub-agents
("coder"/"task") with isolated context. `fantasy` already abstracts every
provider. `pubsub` already decouples agent ↔ UI. So the work is mostly *adding*
an engine on top of storage that already has the right shape.

---

## 2. Sources studied

### 2.1 LCM (Voltropy, arXiv:2605.04050)

Core ideas worth taking:

- **Dual-state memory**: an *Immutable Store* (every message verbatim, never
  modified) separate from an *Active Context* (recent raw messages + precomputed
  summary nodes). Summary nodes are materialized views; the immutable history is
  the sole source of truth.
- **Hierarchical summary DAG**: leaf summaries (direct summary of a message span)
  + condensed summaries (summary of summaries), with provenance/foreign keys to
  prevent orphaned context. Stored in a transactional backend with indexed
  full-text search.
- **Three-level escalation** (guaranteed convergence): `preserve_details` →
  `bullet_points` (at half the target) → `deterministic truncate` (512 tokens, no
  LLM). If a level doesn't reduce token count, escalate. No "compaction failure."
- **Deterministic retrievability**: when the engine compacts, it *programmatically*
  inserts the IDs of the summarized content alongside each summary (a
  post-processing step, independent of model output). Any earlier message is
  always recoverable via `lcm_expand`/`lcm_grep`/`lcm_describe`, regardless of how
  many compactions have occurred.
- **Zero-cost continuity**: below a soft threshold `τ_soft`, the store is a passive
  logger and the user sees raw model latency. Compaction runs async and swaps in
  atomically between turns. Only the hard threshold `τ_hard` blocks.
- **Large-file handling**: files above a token threshold are stored as a path
  reference + a type-aware *Exploration Summary* (schema/shape for JSON/CSV/SQL,
  signatures/hierarchies for code, LLM summary for text). File IDs propagate
  through the summary DAG so the model never loses awareness of a file.
- **Operator-level recursion**: `llm_map` / `agentic_map` — apply a prompt to every
  item in a JSONL file in parallel, schema-validated, with retries, file-based
  I/O for context isolation. Moves control flow from the stochastic model layer to
  the deterministic engine.
- **Scope-reduction invariant** for sub-agents: a sub-agent spawning another must
  declare `delegated_scope` + `kept_work`; if it would delegate everything, the
  call is rejected. Well-founded recursion, no depth limit needed.

### 2.2 ShiftUp pi-shiftup-compaction

Core ideas worth taking (these are the most production-hardened pieces):

- **Five-form memory** per compaction: (1) self-addressed structured checkpoint,
  (2) deterministic session ledger + transcript map, (3) labeled byte-exact
  extracts, (4) working-set snapshot, (5) exact recovery index into the canonical
  session JSONL.
- **Span model** (`serialize.ts`): the compacted history rendered as labeled,
  line-anchored blocks (user / thinking / text / tool calls / results) with
  head+tail truncation, multi-line tool args, and superseded-read marking. Both
  summary lanes consume the same faithful record.
- **Budget governor** (`budget.ts`): sizes every part from the *consumer* model's
  context window (allowance = `min(fraction·window, maxSummaryTokens, headroom·0.5)`),
  then allocates across checkpoint / extracts / ledger / map / restore. No fixed
  ratios, no default output cap.
- **Checkpoint lane** (`checkpoint.ts`): self-addressed prompt with stable item IDs
  (`[C1]` constraints, `[D1]` decisions, `[X1]` dead ends, `[Q1]` questions);
  section validation (required: Goal & User Intent, Progress, Next Action);
  one retry at a larger output budget on truncation; **monotonic ID merge** across
  compactions (dropped IDs are re-inserted under their section and reported as
  drift; `resolved:` items stay); coverage audit (judge mode) that appends
  verbatim ledger facts the judge found missing.
- **Extractive lane** (`morph-lane.ts`): byte-exact compression with golden spans
  (`<keepContext>` around user messages, turn conclusions, error tails, tool-call
  headers), a goal-conditioned `query` built *only* from user text (never the
  checkpoint), re-labeling + line pointers on the output, and an older-lane
  re-compression at a decayed ratio so older history decays gracefully.
- **Fail-closed**: any mandatory lane fails → cancel, save nothing, no host
  fallback, no degraded/partial summary. Per-lane transient retry with exponential
  backoff (5s → 5min, up to 24h); non-transient errors (quota/4xx) not retried.
- **Exact transcript recovery**: the append-only session file is canonical and
  never rewritten; compacted entries are indexed to stable physical line numbers;
  the recovery note gives absolute path, exact ranges, entry ids, and ready-to-run
  `nl`/`sed`/`rg`/`jq` commands.
- **Deterministic facts**: user instructions verbatim, files touched, commands run,
  error signatures, git state — extracted without a model and never left to the
  summarizer alone.

### 2.3 2026 research (what to integrate)

- **SelfCompact / Self-Sum (training-free adaptive timing)**: a lightweight rubric
  that fires compaction at *closed reasoning units* (sub-task resolved, trajectory
  converging) and suppresses it mid-derivation or when stuck. Matches/exceeds
  fixed-interval compaction at 30–70% lower token cost. **Take**: add a
  structure-aware trigger on top of the token threshold.
- **Parallel Context Compaction**: block-based parallel summarization gives the
  operator fine-grained, predictable control over summary volume via block count;
  empirically summary output is input/prompt-invariant, so prompt engineering is
  an unreliable volume knob. **Take**: parallelize the checkpoint lane over blocks.
- **ACE (reversibility / elasticizer)**: keep both raw + abstract per step and
  assign each an elastic type (raw / abstract / drop) per decision step; reversible
  retrieval beats irreversible summarization on the queries that turn on discarded
  material. **Take**: never discard raws; the active context is a *view* over them.
- **Rate-distortion survey**: "reversibility usually matters more than any scoring
  trick"; under repeated *irreversible* summarization, error grows super-linearly,
  while retrieval-backed memory stays flat. **Take**: design for reversibility
  first, lossy compression second.
- **CoACT (action-preserving observation compression)**: a compressed observation
  should induce the *same next action* as the raw one; NAP is a practical signal.
  **Take**: use next-action preservation as a validation signal for tool-result
  compression.
- **AMA-Bench (causality graph + tool-augmented retrieval)**: similarity-based
  retrieval is insufficient for agent memory; a causality graph of
  action→result→state transitions plus tool-augmented retrieval beats RAG by
  ~11pts. **Take**: index causality (tool-call → result → file-changed edges), not
  just text.
- **AgentMemBench (EKV dense retrieval at long range)**: at long horizons, recency
  windows, summaries, and entity graphs collapse (Recall@5 ≤ 0.005); only dense
  embedding retrieval scales (0.573). Compression-with-provenance is the runner-up.
  **Take**: add an optional embedding index over leaf messages as a complementary
  retrieval pathway (LCM explicitly notes this as an unimplemented extension).
- **ACM (lossless + agent-initiated)**: `manage_context` (compress + offload raws
  to external store, summary gets a unique id mapping to raws) + `query_memory`
  (retrieve on demand). **Take**: expose compaction as an agent-callable tool too,
  not only an engine trigger.
- **Cat / SWE-Compressor (context as a tool)**: a structured context workspace —
  stable task semantics, condensed long-term memory, high-fidelity short-term
  interactions. **Take**: the checkpoint's "Goal & User Intent" + ledger are the
  stable layer; recent raw messages are the high-fidelity layer.
- **Context Compaction Theory**: generation (arbitrary bounded message) can need
  strictly less budget than selection (subset); both reduce to one-way
  communication complexity. **Take**: keep generation (LLM checkpoint) as primary,
  but use deterministic selection (ledger/extracts) for facts that must be exact.
- **Recursive Models theory**: recursion (call/return sub-agents) exponentially
  reduces required context vs. single-context summarization; the minimal
  call/return model is already optimal. **Take**: lean on the coordinator's
  sub-agents for context isolation rather than cramming everything into one
  context.

---

## 3. Design principles (non-negotiable)

1. **Reversibility first.** Raw messages are never deleted or mutated. Every
   compaction is a *new view* over the immutable store. Lossy compression is a
   performance optimization on top of lossless storage, never a replacement.
2. **Deterministic facts stay deterministic.** User instructions, files, commands,
   errors, git state, and causality edges are extracted in Go, never by an LLM.
3. **Fail closed.** A failed mandatory lane cancels the compaction and saves
   nothing. Never fall back to a degraded/host summary that could mislead.
4. **Zero-cost continuity.** Below the soft threshold, no work. Short sessions pay
   nothing.
5. **Guaranteed convergence.** Every compaction produces a strictly smaller active
   context, escalating to a deterministic fallback if the model won't compress.
6. **Engine-owned memory.** The model never has to invent a memory strategy; it
   gets stable IDs and retrieval tools. (LCM's "structured control flow" stance.)
7. **Fit Crush, don't fork it.** Use SQLite + sqlc, fantasy providers, pubsub, the
   coordinator's sub-agents, and the existing message/session models. New code
   lives in a new `internal/compaction` package and a few new tools.

---

## 4. Architecture

```
                         ┌──────────────────────────────────────────┐
                         │              Immutable Store              │
                         │  (SQLite: messages, tool calls, results,  │
                         │   usage, thinking — already append-only)  │
                         └─────────────────────┬────────────────────┘
                               read / index only │ never mutated
                                                 ▼
┌──────────────────────────────────────────────────────────────────────┐
│                        compaction.Engine                             │
│                                                                      │
│  Trigger ──► Span ──► Budget ──► Ledger/Map ──► Checkpoint ──►       │
│              model     governor   (det, Go)      lane (LLM)          │
│                                                                      │
│       ──► Extracts lane ──► Verify ──► Compose ──► Persist summary    │
│          (byte-exact)      (judge)     (5-form)   node + line index  │
│                                                                      │
│  Escalation guard wraps every LLM lane (3 levels, det. fallback)     │
└────────────────────┬─────────────────────────────────────────────────┘
                     │ summary node + retained tail
                     ▼
              ┌──────────────┐        agent-callable tools (read-only):
              │ Active Context│ ◄────  recall_grep, recall_expand,
              │ (view)        │        recall_describe, recall_query,
              └──────────────┘        compact_context (agent-initiated)
```

### 4.1 Dual-state memory (from LCM + ACE)

- **Immutable Store** = the existing `messages` / tool-call / tool-result tables.
  Already append-only. No change to how messages are written.
- **Active Context** = what `preparePrompt` sends to the model: a sequence of
  *summary nodes* followed by a *retained tail* of recent raw messages. This
  replaces today's "slice from `SummaryMessageID`".
- **Summary nodes are derived views**, persisted in new tables (§5), with
  provenance back to the message spans they cover. A summary can always be
  replaced by the originals it points at.

### 4.2 Hierarchical summary DAG in SQLite (from LCM)

New tables (§5): `compaction_summaries` (leaf + condensed), with
`parent_summary_ids` (foreign keys) and `covered_message_ids` (the span). On
re-compaction, a new condensed summary node parents the previous leaf nodes,
keeping the DAG acyclic and queryable. This gives multi-resolution navigation:
the model sees condensed summaries; `recall_expand` drills down to leaves; leaves
drill down to raw messages.

### 4.3 Five-form summary composition (from ShiftUp)

Each saved summary node's text is composed, in order, with character offsets
recorded for later re-extraction:

1. **Compaction preamble** — operating rules for the resumed agent.
2. **Structured checkpoint** — self-addressed, stable-item-ID sections (Goal &
   User Intent, Constraints, Environment, Progress, Decisions, Dead Ends, Open
   Questions, Working Set, Critical Context, Next Action), monotonic ID merge
   with the previous checkpoint, coverage audit patch.
3. **Deterministic session ledger** — user instructions verbatim, files touched
   (read/edit/write counts), commands run, error signatures, git state. Extracted
   in Go from the span.
4. **Transcript map** — per-turn line ranges, preview, tool-call/error counts,
   files — so the agent can jump to `sed -n 'L,Lp'`.
5. **Labeled extracts** — byte-exact lines kept by the extractive lane,
   re-labeled by speaker with physical line pointers; older history re-compressed
   at a decayed ratio.
6. **Working-set snapshot** — current content of the most-recently-modified files
   (secret-like names skipped, cwd-bounded, budgeted).
7. **Exact recovery note** — the canonical session file path, exact compacted
   line ranges, entry ids, metadata inventory, and a shell cheat sheet.

### 4.4 Span model (from ShiftUp, ported to Go)

`internal/compaction/span.go`: render the compacted message span as labeled,
line-anchored blocks (`user` / `assistantThinking` / `assistantText` /
`assistantToolCalls` / `toolResult`) with head+tail truncation, multi-line tool
arguments, and **superseded-read marking** (a later read of the same path+offset
marks the earlier result stale). Both the checkpoint and extractive lanes consume
the same `SpanModel`. Line anchors come from the message store's physical row
order (stable, like Pi's JSONL line numbers).

### 4.5 Budget governor (from ShiftUp, adapted)

`internal/compaction/budget.go`: `planBudget(consumerContextWindow,
reserveTokens, keepRecentTokens, systemPromptTokens, budgetFraction,
maxSummaryTokens, minSummaryTokens, summarizerWindow, features) → BudgetPlan` with
per-part token targets (checkpoint / extracts / ledger / map / restore). Derived
from the active model's `ContextWindow` from config, not fixed ratios. Reasoning
tokens share the output cap on completions-style providers.

### 4.6 Checkpoint lane (from ShiftUp, with LCM escalation)

`internal/compaction/checkpoint.go`:

- Self-addressed system prompt; user turn = previous checkpoint (stripped of file
  lists) + recovered spans + line-anchored transcript + section instructions.
- Stable item IDs per section; `mergeCheckpoints(prev, next)` re-inserts silently
  dropped IDs and reports drift; `resolved:` items stay.
- `validateCheckpoint` (required sections present, not truncated before Next
  Action); one retry at `1.6×` output budget.
- **LCM three-level escalation wraps the call**: if the validated checkpoint still
  exceeds its token target, retry in `bullet_points` mode at half the target; if
  that still fails, fall back to a deterministic truncation of the ledger + recent
  turns (no LLM). Guaranteed convergence.
- Coverage audit (judge mode): build probes from the ledger (user instructions,
  errors, commands, modified files), ask the summarizer what's missing, append
  verbatim facts that are absent. Optional via config.

The summarizer is a **separate, configurable model** (default: a cheap/fast model
like Gemini Flash at max reasoning, or follow the active model). Resolved through
`config.Service` + fantasy, never by calling host compaction as a fallback.

*Status:* shipped as the optional `models.compaction` slot, resolved by
`config` like `large`/`small` (validated against the provider catalog, catalog
defaults for max tokens / reasoning effort, dropped with a warning when
invalid) and built by the coordinator alongside the other slots. When set, all
compaction-lane calls (checkpoint, retries, judge, parallel blocks) run on it
with its own provider options, system-prompt prefix, auth refresh, context
window, and pricing; the summary node records that model. When unset, the
active large model is used. The default is "follow the large model", not a
built-in cheap model.

### 4.7 Extractive lane (from ShiftUp)

`internal/compaction/extracts.go`: byte-exact, line-anchored compression. Golden
spans force-kept (`<keepContext>`): user messages, each turn's final assistant
statement, error tails, tool-call headers. Goal-conditioned `query` built only
from user-authored text (never the checkpoint). Re-label output by speaker and
re-attach transcript line pointers. Older-lane re-compression at
`morphDecay·ratio`. 

Crush doesn't ship Morph, so the extractive lane uses a **local extractive
compressor**: deterministic head/tail retention per block plus an optional
LLM-assisted relevance pass at a tight ratio (the budget governor's target). This
keeps the byte-exact, line-anchored, golden-span design without an external
service dependency. The LLM-assisted pass is itself wrapped by the escalation
guard.

*Status:* shipped as the deterministic compressor only. Golden spans are kept
first (per-block and total caps), then the remaining character budget
(`plan.Extracts.TargetTokens`) is allocated over non-golden blocks with recency
weighting; blocks that do not fit are dropped. The older lane re-truncates the
previous compaction's extracts to `OlderLaneTokens` (a quarter of the extracts
allotment) so older history decays instead of nesting. No LLM relevance pass.

### 4.8 Escalation guard (from LCM)

`internal/compaction/escalation.go`: wraps any LLM lane.

```
level 1: preserve_details  → LLM summary at target T
level 2: bullet_points     → LLM summary at T/2   (only if level 1 ≥ input)
level 3: deterministic     → Go truncation, 512 tokens, no LLM (only if level 2 ≥ input)
```

Returns as soon as a level produces `tokens(out) < tokens(in)`. Level 3 guarantees
the engine never gets stuck on a model that won't compress.

*Status:* shipped with stricter fail-closed semantics: a model *error*
(including cancellation, auth, 4xx) aborts the compaction after at most one
transient retry — it never falls through to level 3. Level 1 accepts output that
is smaller than the input, not truncated, and within 1.25× the target; a
truncated attempt is retried once at 1.6× the output budget with a retry note.
Level 2 accepts within 0.75× the target. Level 3 (deterministic) is used only
when both model levels fail to converge, and it receives the rendered ledger.

### 4.9 Deterministic ledger + causality graph (from ShiftUp + AMA-Bench)

`internal/compaction/ledger.go`: extract in Go — user instructions verbatim,
injected messages, assistant turn conclusions, files (read/edit/write), commands,
error signatures (deduped by signature), git snapshot (read-only via the shell
package). 

**New from AMA-Bench**: a lightweight *causality index* — for each tool call,
record `(callId, tool, args-hash, resultError, filesChanged)` edges. Stored in a
`compaction_causality` table and surfaced in the ledger as "T4 bash → changed
foo.go; T7 edit foo.go → error: …". This is the structured memory that
similarity-only RAG misses, and it's cheap to build deterministically.

*Status:* shipped except the git snapshot (deferred). Tracked file operations
are `view` (read), `edit`/`multiedit`/`lsp_rename`/`lsp_replace_symbol` (edit),
`write` (write); commands come from `bash`.

### 4.10 Retrieval tools (from LCM + ACM, read-only, agent-callable)

New tools in `internal/agent/tools/`, all backed by SQLite + FTS5, all marked
read-only so they skip permission prompts for trusted repos:

- **`recall_grep(pattern, summary_id?)`** — regex/FTS5 over the immutable store,
  results grouped by the summary node that covers them, paginated.
- **`recall_expand(summary_id)`** — expand a summary node to its children (sub-
  summaries or raw messages). Restricted to sub-agents (the "task" agent) to
  prevent the main loop from flooding its own context (LCM invariant).
- **`recall_describe(id)`** — metadata for a summary or file id: kind, token
  count, parents, covered range, exploration summary.
- **`recall_query(text)`** — semantic retrieval over leaf messages (optional,
  requires an embedding index; see §4.12). The ACM `query_memory` analog.
- **`compact_context(instructions?)`** — agent-initiated compaction (from ACM +
  Cat). Lets the model compact at a closed reasoning unit instead of waiting for
  the token threshold.

Each tool's `.md` description tells the model *when* to use it (e.g. "before
asking the user to repeat prior context, `recall_grep` the transcript").

*Status:* `recall_grep`, `recall_expand`, `recall_describe`, and
`compact_context` shipped. `recall_grep` phrase-quotes the pattern for FTS5
(paths and punctuation are safe) and falls back to `LIKE`; each hit shows the
message's seq (the same session ordinal used in the ledger and recovery note)
and the summary that covers it, so `recall_expand` has a discoverable input.
`recall_query` is deferred with the embedding index. All four are registered
through the normal tool plumbing (`allToolNames`, allow-lists, hooks); the
recall tools are read-only, `compact_context` and `llm_map` are not given to
read-only sub-agents. `compact_context` sets a per-session request flag that the
run honors at the next step boundary (calling the summarizer from inside a tool
call would collide with the in-flight run).

### 4.11 Adaptive, structure-aware trigger (from SelfCompact + LCM)

`internal/compaction/trigger.go`:

- **Hard threshold** `τ_hard` (blocks to compact): `contextWindow - reserveTokens`.
- **Soft threshold** `τ_soft` (async compaction between turns): a configurable
  fraction (default ~0.7·window) or a per-model token trigger.
- **Structure-aware rubric** (training-free, from SelfCompact): at `turn_end`,
  if above `τ_soft`, run a *cheap* rubric check (a Haiku/Flash-class call or a
  deterministic heuristic) that fires compaction when a sub-task has resolved or
  the trajectory is converging, and suppresses it mid-derivation or when stuck.
  The deterministic heuristic version: fire after a tool batch completes with no
  errors and the last assistant turn ended with a final answer; suppress while a
  multi-step tool sequence is mid-flight.
- **Agent-initiated**: `compact_context` can fire early at a natural milestone.

This addresses the research finding that *when* to compact matters as much as
*how*, and that fixed thresholds discard partial results mid-derivation.

*Status:* shipped with the deterministic rubric. The hard threshold is
`window − reserve_tokens` when the engine is enabled (legacy constants
otherwise); the rubric is consulted only above the soft threshold. Compaction
is currently synchronous at a step boundary in every case — the "async between
turns" swap-in is deferred. `keep_recent_tokens` and `reserve_tokens` are
clamped to `window/4` and `window/8` so small-window models cannot end up
with nothing to compact while over threshold; when a single turn exceeds the
retained budget it is split (its prefix is compacted as the In-Flight Turn, the
suffix stays in context, aligned so tool calls and results stay paired).

### 4.12 Optional embedding index (from AgentMemBench EKV)

`internal/compaction/embeddings.go` (gated behind config, off by default):
compute embeddings for each leaf message on compaction (using a configurable
embedding model via fantasy), store vectors in a `compaction_embeddings` table
(or a sidecar SQLite-vec / vec extension). `recall_query` does cosine retrieval.
This is the complementary pathway LCM explicitly left unimplemented and that
AgentMemBench showed is the only thing that scales to long-range recall. Keep it
optional — regex + DAG traversal is the default and is sufficient for most
sessions.

*Status:* deferred (no schema, no config key, no tool).

### 4.13 Large-file handling (from LCM)

`internal/compaction/files.go`: when a tool result includes file content above a
token threshold (default ~25k tokens), the engine stores a *file reference*
(path, mime, token count, exploration summary) instead of the raw content in the
active context. Exploration summaries are produced by a type-aware dispatcher:
structured (JSON/CSV/SQL) → schema/shape; code → signatures/hierarchies (reuse
the LSP manager Crush already has); text → short LLM summary. File IDs propagate
through the summary DAG so the model never loses awareness of a file it saw.

*Status:* deferred (no schema, no config key).

### 4.14 Operator-level recursion (from LCM)

Two new tools, building on the coordinator's existing sub-agent infrastructure:

- **`llm_map(input_path, prompt, output_schema, concurrency)`** — apply a prompt
  to every JSONL item in parallel, schema-validated, with retries, results to an
  output JSONL file. Pure function per item, no side effects. Uses a worker pool.
- **`agentic_map(input_path, prompt, output_schema, read_only)`** — spawn a full
  sub-agent (the "task" agent) per item, with tools, multi-step reasoning.

Both use file-based I/O for context isolation (the dataset never enters the
parent context). The scope-reduction invariant (§4.15) guards delegation.

These directly address Crush's long-context aggregation cases (e.g. "refactor
every handler in this dir") that today would overflow one context.

*Status:* `llm_map` shipped (worker pool, best-effort schema check with one
retry, JSONL in/out resolved against the working directory, output write gated
by the permission service). `agentic_map` deferred.

### 4.15 Scope-reduction invariant for sub-agents (from LCM)

In the coordinator's sub-agent dispatch (`internal/agent/coordinator.go`), when a
*non-root* agent spawns another sub-agent, require `delegated_scope` +
`kept_work`. If the caller can't articulate what it's keeping, reject the call and
instruct it to do the work directly. Read-only exploration agents and parallel
`sibling` tasks are exempt. This gives well-founded recursion without an arbitrary
depth limit, reusing the coordinator that already exists.

*Status:* not shipped. Crush's task agent does not receive the `agent` tool, so
nested delegation cannot happen today and the check would be dead code; revisit
if sub-agents gain the ability to spawn sub-agents.

### 4.16 Exact transcript recovery (from ShiftUp)

`internal/compaction/transcript.go`: the session's messages table is canonical and
never rewritten. On compaction, index the compacted message IDs to their stable
rowids / physical order. Persist the index in `compaction_summaries.covered_range`
and a `compaction_transcript_index` table. The recovery note (part of every
summary) gives the session id, exact rowid ranges, entry ids, and ready-to-run
`crush` CLI / SQL snippets to inspect the raw history. Because Crush uses SQLite
rather than a JSONL file, the "line numbers" are rowids, but the principle is
identical.

*Status:* shipped with one change: the anchor ("seq") is the message's
**session-absolute 1-based ordinal in the summary-free message list** (ordered
by `created_at, rowid`), not the raw rowid, so it is stable, human-readable,
and identical in the ledger, transcript map, recovery note, `recall_grep`, and
`recall_describe`. The authoritative cut for the active context is the
`first_retained_message_id` stored on the summary node; ordinals are labels.
The recovery note lists the session id, the summary id, exact seq ranges,
first/last compacted message ids, and the recall tools (no shell snippets).

### 4.17 Fail-closed + retry (from ShiftUp)

`internal/compaction/pipeline.go` orchestrates the lanes. Every mandatory lane
(checkpoint, extracts, optional verify) must succeed before composition. Any
failure → `cancel`, save nothing, no fallback. Per-lane transient retry
(exponential backoff, 5s→5min, capped at a configurable deadline); non-transient
errors (quota, 4xx) not retried. Aborts propagate immediately. Sanitized,
actionable error notices via `pubsub` (reusing the existing event bus) and the
notify package.

*Status:* the engine (`engine.go`) is fail-closed with a single short transient
retry per model call; there is no `pipeline.go` and no long backoff schedule.
If the engine fails, `sessionAgent.Summarize` logs the error and falls back to
the legacy single-shot summary so the turn still recovers. Persistence of a
summary node (row, causality edges, session pointer) is transactional. No
pubsub notices yet.

### 4.18 Parallel block compaction (from Parallel Context Compaction)

For very large spans (e.g. overflow recovery where a single tool result blew past
the threshold), split the checkpoint lane into N independent block summaries
(block size from the budget governor), summarize in parallel, then a single
merge/condense pass. This gives predictable summary volume (block count is the
knob) and higher throughput, addressing the finding that single-pass
summarization attends poorly over 96k+ tokens. Gated by a span-size threshold.

*Status:* shipped (`parallel.go`), fail-closed (any block error aborts), history
blocks only; off by default (`parallel_block_threshold: 0`).

---

## 5. Schema changes (SQLite migrations)

As shipped in `internal/db/migrations/20260818000000_add_compaction_engine.sql`
(queries in `internal/db/sql/compaction.sql`, generated via sqlc):

```sql
-- compaction_summaries: the summary DAG (one node per compaction)
CREATE TABLE compaction_summaries (
  id                        TEXT PRIMARY KEY,           -- uuid, cited in the recovery note
  session_id                TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  parent_ids                TEXT NOT NULL DEFAULT '[]', -- JSON array: the previous active node
  covered_start             INTEGER,                    -- first compacted seq (session ordinal)
  covered_end               INTEGER,                    -- last compacted seq (session ordinal)
  first_retained_message_id TEXT,                       -- authoritative cut for the active context
  kind                      TEXT NOT NULL CHECK (kind IN ('leaf', 'condensed')),
  level                     INTEGER NOT NULL DEFAULT 0, -- escalation level that produced it
  summary_text              TEXT NOT NULL,              -- the composed five-form entry
  layout                    TEXT NOT NULL DEFAULT '{}', -- JSON char offsets per part
  checkpoint                TEXT,                       -- isolated structured checkpoint
  token_count               INTEGER NOT NULL DEFAULT 0,
  model_provider            TEXT,
  model_id                  TEXT,
  reasoning                 TEXT,
  covered_message_ids       TEXT NOT NULL DEFAULT '[]', -- JSON: raw message ids this node replaces
  created_at                INTEGER NOT NULL
);
CREATE INDEX idx_compaction_summaries_session ON compaction_summaries(session_id, created_at);

-- compaction_causality: action → result → state edges (deterministic)
CREATE TABLE compaction_causality (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  summary_id    TEXT REFERENCES compaction_summaries(id) ON DELETE SET NULL,
  session_id    TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  turn          INTEGER NOT NULL,
  tool_call_id  TEXT,
  tool          TEXT NOT NULL,
  args_hash     TEXT,
  is_error      INTEGER NOT NULL DEFAULT 0,
  files_changed TEXT NOT NULL DEFAULT '[]',              -- JSON array of paths
  created_at    INTEGER NOT NULL
);

-- FTS5 over the immutable message store for recall_grep. External-content
-- table: the FTS column MUST be named `parts` to match messages.parts; the
-- index is backfilled with the 'rebuild' command and kept in sync by
-- insert/update/delete triggers.
CREATE VIRTUAL TABLE messages_fts USING fts5(
  parts, content='messages', content_rowid='rowid', tokenize='unicode61'
);
INSERT INTO messages_fts(messages_fts) VALUES ('rebuild');
```

`sessions` gains `active_summary_id` (the DAG node currently in context).
`messages.is_summary_message` stays for back-compat and for the chat UI: every
compaction also stores the composed entry as a summary *message*, which
`ActiveContext` excludes from the retained tail. `ListMessagesBySession` orders
by `(created_at, rowid)` so session ordinals are deterministic.

Not shipped (see status table): `compaction_file_refs`, `compaction_embeddings`,
per-session `reserve_tokens`/`keep_recent_tokens` columns.

---

## 6. Integration into Crush

### 6.1 New package

`internal/compaction/` — `engine.go`, `span.go`, `budget.go`, `ledger.go`,
`checkpoint.go`, `extracts.go`, `escalation.go`, `transcript.go`, `files.go`,
`trigger.go`, `embeddings.go` (optional), `pipeline.go`. Pure orchestration with
injected `fantasy` completion + `db.Queries` boundaries, mirroring ShiftUp's
testable module split.

### 6.2 Replace `Summarize`

`internal/agent/agent.go`:

- The token-threshold check calls the new `compaction.Engine` instead of
  `a.Summarize`.
- `Summarize` becomes a thin wrapper / is deprecated; the engine owns prompt
  construction, validation, retry, and composition.
- `getSessionMessages` changes from "slice from `SummaryMessageID`" to "build
  active context from the summary DAG + retained tail" (§6.3).

*Status:* shipped. `sessionAgent.Summarize` runs `compactWithEngine` when the
engine is enabled and falls back to the legacy summary if the engine fails.
`compactWithEngine` builds the span from the raw (summary-free) message list,
starting at the previous node's `first_retained_message_id`, and splits the
retained tail at a turn boundary (or mid-turn with a turn prefix when the last
turn is oversized). The compaction's own model calls carry the session's
provider options, auth refresh, and headers, and are accounted against the
session's usage/cost.

### 6.3 Active-context assembly

New `internal/compaction/active.go` (or in `engine.go`): given a session, load the
current condensed summary node(s) from `compaction_summaries`, then the retained
tail of raw messages (rowid > `covered_end` of the newest summary, up to
`keepRecentTokens`). Return them as `fantasy.Message`s for `preparePrompt`. This
is the Active Context view.

*Status:* shipped as `Engine.ActiveContext` (`engine.go`): the active node's
`summary_text` (as a synthetic user message) followed by every raw non-summary
message from `first_retained_message_id` onward (falling back to the
`covered_end` ordinal for rows without the id).

### 6.4 Config

`internal/config/compaction.go` — `Options.Compaction *CompactionConfig`
(`options.compaction` in `crush.json`, `option compaction <key> <value>` in
`crushrc`), as shipped:

```go
type CompactionConfig struct {
    Enabled                   *bool    // default true; false = legacy single-shot summary
    ReserveTokens             int64    // default 16384 (hard threshold = window − reserve)
    KeepRecentTokens          int64    // default 20000 (retained verbatim)
    SoftThresholdFraction     float64  // default 0.7 (rubric consulted above this)
    BudgetFraction            float64  // default 0.15
    MaxSummaryTokens          int64    // default 48000
    MinSummaryTokens          int64    // default 6000
    Verify                    string   // "judge" | "checks" | "off", default "judge"
    Ledger                    *bool    // default true
    TranscriptMap             *bool    // default true
    WorkingSetFiles           int      // default 3; 0 disables
    WorkingSetMaxCharsPerFile int      // default 12000
    ExtractsDecay             *float64 // default 0.5; 0 = no older lane; <0 = no extracts
    ParallelBlockThreshold    int64    // default 0 (disabled)
}
```

Booleans and `extracts_decay` are pointers so a partial block does not
silently disable a feature (`nil` = default, explicit `false`/`0` respected).
`disable_auto_summarize` still works and is honored only when no
`options.compaction` block is present; an explicit `enabled` wins.
`keep_recent_tokens` and `reserve_tokens` are clamped to `window/4` and
`window/8` at runtime. The summarizer model is not part of this block: it is
the optional `models.compaction` slot (§4.6), so it uses the same
`SelectedModel` shape, catalog validation, and crushrc `model` builtin as
`large`/`small`. Not shipped: `git_snapshot`, `embeddings`,
`large_file_threshold`.

### 6.5 Tools

`internal/agent/tools/recall/` — `grep.go`, `expand.go`, `describe.go`,
`query.go`, `compact.go` (agent-initiated). Each with a `.md` description.
`internal/agent/tools/llmmap/` — `map.go` (`llm_map`), `agentic_map.go`. Registered
through the existing tool registry; permissions set read-only where applicable.

*Status:* shipped as `internal/agent/tools/recall.go`, `compact_context.go`, and
`map.go` (descriptions inline), built inside `coordinator.buildTools` so
`allowed_tools`/`disabled_tools`, hooks, and the read-only sub-agent list
apply. `recall_query` and `agentic_map` are deferred.

### 6.6 Coordinator

`internal/agent/coordinator.go`: enforce the scope-reduction invariant on
non-root sub-agent spawn (require `delegated_scope`/`kept_work` on the task tool's
args when the caller is itself a sub-agent).

*Status:* not shipped (see §4.15).

### 6.7 Pubsub / UI

Publish compaction lifecycle events on the existing `pubsub` broker (`compaction:
{phase: requested|started|finished, outcome, lane?}`) so the TUI can show a
compaction footer (like ShiftUp's `🧠⬆️` cycle) and so other components (e.g. a
future goal extension) can coordinate around the abort/continue boundary.

*Status:* deferred. Today the composed entry is stored as a summary message
(rendered by the chat like the legacy summary) and merge/escalation outcomes are
logged.

### 6.8 DB / sqlc

Add the §5 SQL files, regenerate `internal/db/`, add new `db.Queries` methods.
Migrations are additive and safe (no rewrite of existing message data).

---

## 7. Phased delivery

**Phase 1 — Lossless foundation (the biggest win, no new external deps).**
- New `internal/compaction` package: span model, budget governor, deterministic
  ledger + transcript map, exact transcript recovery (rowid-indexed).
- Replace `Summarize` with the engine producing a *single* structured checkpoint
  + ledger + recovery note; keep raws in SQLite, index them.
- Three-level escalation guard around the checkpoint lane.
- `recall_grep` + `recall_expand` + `recall_describe` tools.
- Config + migrations.
- *Outcome*: Crush goes from "drop everything before the summary" to lossless,
  retrievable, deterministic-fact-backed compaction. This alone closes most of the
  gap to SOTA.

**Phase 2 — Two-lane + validation.**
- Extractive lane (local compressor + optional LLM relevance pass) with golden
  spans and line pointers.
- Checkpoint monotonic ID merge + coverage audit (judge mode).
- Working-set snapshot.
- Fail-closed pipeline with per-lane retry.
- *Outcome*: parity with ShiftUp's five-form memory, in-process.

**Phase 3 — Adaptive + parallel.**
- Structure-aware trigger (rubric) on top of the token threshold.
- Parallel block compaction for large spans.
- Agent-initiated `compact_context`.
- *Outcome*: compaction fires at the right time, not just at the token cliff.

**Phase 4 — Operators + retrieval.**
- `llm_map` / `agentic_map` tools with the scope-reduction invariant.
- Optional embedding index + `recall_query`.
- Large-file handling + exploration summaries (reuse LSP manager).
- *Outcome*: LCM's operator-level recursion and long-range dense retrieval.

Each phase is independently shippable and independently testable. Phase 1 is the
priority.

*Status:* Phases 1–3 shipped. Phase 4: `llm_map` shipped; `agentic_map`, the
scope-reduction invariant, the embedding index + `recall_query`, and large-file
handling are deferred (see the status table at the top).

---

## 8. Testing strategy

- **Pure-module tests** (testify `require`, `t.Parallel()`): span model, budget
  governor, ledger extraction, checkpoint merge/drift, escalation levels,
  transcript indexing. Mirror ShiftUp's `test/*.test.ts` contracts in Go.
- **Golden files**: checkpoint prompt construction, summary composition layout —
  use `catwalk`/`-update` where rendering is involved.
- **Invariant tests** (the ShiftUp AGENTS.md invariants, ported): fail-closed on
  every mandatory-lane failure; monotonic IDs survive; raws never mutated;
  deterministic facts present; escalation guarantees convergence.
- **Mock providers**: use `config.UseMockProviders` for any test that exercises
  the fantasy completion path, per the AGENTS.md testing guide.
- **Bench harness** (from the research): a small replay benchmark that measures
  compaction error accumulation under *repeated* compaction (the rate-distortion
  survey's least-measured failure). Reuse a synthetic long session, compact N
  times, assert recall of early facts via `recall_grep`. This is the test that
  would catch the super-linear error growth the literature warns about.

*Status:* pure-module tests, engine tests against a real SQLite store (happy
path, fail-closed, second compaction anchors, extracts budget), a
`compactWithEngine`-level two-compaction test, trigger-threshold tests, split
tests, recall-tool tests, config-resolution tests, and a populated-DB migration
test are in place. Golden files and the repeated-compaction bench are not.

---

## 9. What we deliberately do *not* take

- **RL-trained compaction** (CompactionRL/SUPO/Self-Sum): requires model
  fine-tuning; Crush is a provider-agnostic host, not a trainer. The
  training-free rubric (SelfCompact) gives the timing gains without it.
- **Learned pruners** (SWE-Pruner 0.6B, RepoDistill, CoACT compressor): shipping
  a sidecar model is out of scope for a Go binary with `CGO_ENABLED=0`. The
  deterministic + LLM-checkpoint approach covers the same ground without a
  model dependency. We *do* borrow CoACT's *signal* (next-action preservation)
  as a validation heuristic where cheap.
- **Embedded PostgreSQL** (LCM's reference backend): Crush is SQLite-based and
  SQLite satisfies all three required properties (transactional, FK integrity,
  FTS5). No new DB.
- **An external Morph dependency**: the extractive lane is local; no third-party
  compaction service required.

---

## 10. Why this is SOTA for Crush

It combines the two best production designs (LCM's deterministic architecture +
ShiftUp's five-form memory) with the 2026 literature's most actionable findings
(adaptive timing, reversibility, parallel blocks, causality indexing, dense
retrieval) — and lands them on a substrate (SQLite + fantasy + sub-agents) that
Crush already has. The result:

- **Lossless**: every message retrievable forever via `recall_*` (LCM + ACE +
  rate-distortion).
- **Deterministic facts**: ledger, map, causality — never trust the summarizer
  for what Go can extract (ShiftUp + AMA-Bench).
- **Guaranteed convergence**: three-level escalation, no compaction failure (LCM).
- **Zero-cost short sessions**: below `τ_soft`, nothing runs (LCM).
- **Right-time compaction**: rubric-gated, agent-initiable (SelfCompact + ACM).
- **Validated memory**: checkpoint sections + monotonic IDs + coverage audit
  (ShiftUp).
- **Operator-level recursion**: `llm_map`/`agentic_map` for unbounded datasets
  (LCM).
- **Long-range retrieval**: optional dense embeddings (AgentMemBench).
- **Fits Crush**: Go, SQLite, sqlc, fantasy, pubsub, coordinator — no new
  language, DB, or required external service.
