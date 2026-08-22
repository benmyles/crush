# Crush Development Guide

## Project Overview

Crush is a terminal-based AI coding assistant built in Go by
[Charm](https://charm.land). It connects to LLMs and gives them tools to read,
write, and execute code. It supports multiple providers (Anthropic, OpenAI,
Gemini, Bedrock, Copilot, Hyper, MiniMax, Vercel, and more), integrates with
LSPs for code intelligence, and supports extensibility via MCP servers and
agent skills.

The module path is `github.com/charmbracelet/crush`.

## Architecture

```
main.go                            CLI entry point (cobra via internal/cmd)
.agents/skills/                    Repo-level agent skills (authoring builtin
                                   skills and shell builtins)
internal/
  app/app.go                       Top-level wiring: DB, config, agents, LSP,
                                   MCP, events
  backend/                         Transport-agnostic workspace/session/agent
                                   operations; consumed by the HTTP server
  server/                          HTTP API server speaking the Crush RPC protocol
  client/                          RPC client for talking to a Crush server
  proto/                           RPC types shared by client and server
  workspace/                       Workspace interface for frontends; local and
                                   remote implementations
  cmd/                             CLI commands (root, run, login, logout, models,
                                   stats, logs, projects, server, schema)
  config/
    config.go                      Config struct, context file paths, agent definitions
    load.go                        crushrc and crush.json loading and validation
    provider.go                    Provider configuration and model resolution
  shellconfig/                     Bash-powered config format (crushrc builtins)
  agent/
    agent.go                       SessionAgent: runs LLM conversations per session
    coordinator.go                 Coordinator: manages named agents ("coder",
                                   "task"), tool calls, permissions, and hooks
    hooked_tool.go                 Decorator that runs PreToolUse hooks before
                                   tool execution
    prompts.go                     Loads Go-template system prompts
    prompt/                        Prompt assembly helpers
    templates/                     System prompt templates (coder.md.tpl, task.md.tpl, etc.)
    tools/                         All built-in tools (bash, edit, view, grep,
                                   glob, etc.)
      mcp/                         MCP client integration
    fireworksdsv4/                 DSV4 constrained decoding for Fireworks models
    codex/, hyper/                 Provider-specific integrations
    notify/                        Model notifications
    compaction_support.go          Bridges the compaction engine to the agent
  compaction/                      Automatic context compaction engine
  goal/                            Session goals and the goal supervision loop
  status/                          Agent status updates (standup-style reminders)
  commands/                        User-defined slash command loading
  terminal/                         tmux-backed interactive terminal sessions
  question/                        Blocking user questions via pubsub (the
                                   question tool)
  hooks/                           Hook engine: runs user shell commands on hook
                                   events
    hooks.go                       Decision types, aggregation logic
    runner.go                      Parallel hook execution, timeout, dedup
    input.go                       Stdin payload builder, env vars, stdout
                                   parsing (Crush + Claude Code compat)
  hookevent/                       Hook event name constants shared with hooks
                                   and config
  session/session.go               Session CRUD backed by SQLite
  message/                         Message model and content types
  db/                              SQLite via sqlc, with migrations
    sql/                           Raw SQL queries (consumed by sqlc)
    migrations/                    Schema migrations
  lsp/                             LSP client manager, auto-discovery, on-demand
                                   startup
  ui/                              Bubble Tea v2 TUI (see internal/ui/AGENTS.md)
  permission/                      Tool permission checking and allow-lists
  skills/                           Skill file discovery and loading
  shell/                            Bash command execution with background job
                                   support
  event/                           Telemetry (PostHog)
  pubsub/                          Internal pub/sub for cross-component messaging
  filetracker/                     Tracks files touched per session
  history/                         Prompt history
  swagger/                         Generated OpenAPI spec for the HTTP API
  discover/                        Model discovery (catwalk/litellm)
  projects/                        Project list management
  update/                          Update checker
  oauth/                           OAuth token handling
  herdr/                           herdr terminal multiplexer integration
  clipboard/                       Cross-platform clipboard access
  lock/                            Cross-process advisory file locking
  log/                             slog setup
  csync/                           Concurrent data structures
  diff/                            Unified diff generation
  diffdetect/                      Detects unified-diff markers in text
  dns/                             Termux/Android DNS resolver configuration
  ansiext/, env/, filepathext/,     Small utility packages
  fsext/, format/, home/, stringext/
```

### Key Dependency Roles

- **`charm.land/fantasy`**: LLM provider abstraction layer. Handles protocol
  differences between Anthropic, OpenAI, Gemini, etc. Used in `internal/app`,
  `internal/agent`, and `internal/backend`.
- **`charm.land/bubbletea/v2`**: TUI framework powering the interactive UI.
- **`charm.land/bubbles/v2`**: Reusable TUI components.
- **`charm.land/fang/v2`**: CLI argument parsing in `internal/cmd`.
- **`charm.land/lipgloss/v2`**: Terminal styling.
- **`charm.land/glamour/v2`**: Markdown rendering in the terminal.
- **`charm.land/catwalk`**: Snapshot/golden-file testing for TUI components.
- **`sqlc`**: Generates Go code from SQL queries in `internal/db/sql/`.
- **`charm.land/x/vcr`**: Records and replays provider API cassettes in
  tests.

### Key Patterns

- **Config is a Service**: accessed via `config.Service`, not global state.
- **Tools are self-documenting**: each tool has a `.go` implementation and a
  `.md` or `.md.tpl` description file in `internal/agent/tools/`.
- **System prompts are Go templates**: `internal/agent/templates/*.md.tpl`
  with runtime data injected.
- **Context files**: Crush reads AGENTS.md, CRUSH.md, CLAUDE.md, GEMINI.md
  (and `.local` variants) from the working directory for project-specific
  instructions.
- **HTTP API**: `internal/backend` holds transport-agnostic operations;
  `internal/server` serves the HTTP API while `internal/client` and
  `internal/workspace` provide remote frontends. RPC types live in
  `internal/proto`; the OpenAPI spec is generated into
  `internal/swagger`.
- **Context compaction**: `internal/compaction` compacts long conversations
  into summaries stored in the session store, triggered automatically or
  via the `compact_context` tool.
- **Goals**: `internal/goal` tracks a session objective; the supervision
  loop prods the model between turns until the goal is marked complete or
  blocked via the goal tools.
- **Status updates**: `internal/status` reminds the agent to emit
  standup-style updates through the `status_update` tool.
- **Bash config format**: Crush's primary config format is `crushrc` — a
  Bash script using builtins (`provider`, `model`, `mcp`, `lsp`,
  `permissions`, `hook`, `options`) to define config. `crush.json` is still
  supported but is deprecated in favor of `crushrc` and may be removed in a
  future release. Shell config files are discovered alongside JSON configs
  and deep-merged through the same pipeline. Builtins are registered via
  `shell.RegisterBuiltin` and gated by a `ConfigBuilder` on the context —
  they are no-ops during normal bash tool execution. See
  `internal/shellconfig/`.
- **Persistence**: SQLite + sqlc. All queries live in `internal/db/sql/`,
  generated code in `internal/db/`. Migrations in `internal/db/migrations/`.
- **Pub/sub**: `internal/pubsub` for decoupled communication between agent,
  UI, and services.
- **Hooks**: User-defined shell commands in `crushrc` (or `crush.json`)
  that fire before tool execution. The engine (`internal/hooks/`) is
  independent of fantasy and agent — it takes inputs, runs commands,
  returns decisions. Event name constants live in `internal/hookevent/`.
  The `hookedTool` decorator in `internal/agent/hooked_tool.go` wraps
  tools at the coordinator level. Hooks run before permission checks. See
  `docs/hooks/README.md` for the user-facing protocol.
- **CGO disabled**: builds with `CGO_ENABLED=0` and
  `GOEXPERIMENT=greenteagc`.

## Build/Test/Lint Commands

- **Build**: `go build .`, `go run .`, or `task build`
- **Test**: `task test` (runs `go test -race -failfast ./...`) or
  `go test ./...` (run a single test:
  `go test ./internal/agent/tools -run TestDeleteContentRejectsMultipleMatchesWithoutReplaceAll`)
- **Update Golden Files**: `go test ./... -update` (regenerates `.golden`
  files when test output changes)
  - Update a specific package: `go test ./internal/ui/diffview -update`
- **Record VCR Cassettes**: `task test:record` (re-records provider API
  cassettes in `internal/agent/testdata/`)
- **Lint**: `task lint:fix`
- **Format**: `task fmt` (`gofumpt -w .`)
- **Modernize**: `task modernize` (runs `modernize` which makes code
  simplifications)
- **Dev**: `task dev` (runs with profiling enabled)
- **Generate**: `task sqlc` (SQL query code) and `task swag` (OpenAPI spec)

## Code Style Guidelines

- **Imports**: Use `goimports` formatting, group stdlib, external, internal
  packages.
- **Formatting**: Use gofumpt (stricter than gofmt), enabled in
  golangci-lint.
- **Naming**: Standard Go conventions — PascalCase for exported, camelCase
  for unexported.
- **Types**: Prefer explicit types, use type aliases for clarity (e.g.,
  `type AgentName string`).
- **Error handling**: Return errors explicitly, use `fmt.Errorf` for
  wrapping.
- **Context**: Always pass `context.Context` as first parameter for
  operations.
- **Interfaces**: Define interfaces in consuming packages, keep them small
  and focused.
- **Structs**: Use struct embedding for composition, group related fields.
- **Constants**: Use typed constants with iota for enums, group in const
  blocks.
- **Testing**: Use testify's `require` package, parallel tests with
  `t.Parallel()`, `t.SetEnv()` to set environment variables. Always use
  `t.Tempdir()` when in need of a temporary directory. This directory does
  not need to be removed.
- **JSON tags**: Use snake_case for JSON field names.
- **File permissions**: Use octal notation (0o755, 0o644) for file
  permissions.
- **Log messages**: Log messages must start with a capital letter (e.g.,
  "Failed to save session" not "failed to save session").
  - This is enforced by `task lint:log` which runs as part of `task lint`.
- **Comments**: End comments in periods unless comments are at the end of the
  line.

## Testing with Mock Providers

When writing tests that involve provider configurations, use the mock
providers to avoid API calls:

```go
func TestYourFunction(t *testing.T) {
    // Enable mock providers for testing
    originalUseMock := config.UseMockProviders
    config.UseMockProviders = true
    defer func() {
        config.UseMockProviders = originalUseMock
        config.ResetProviders()
    }()

    // Reset providers to ensure fresh mock data
    config.ResetProviders()

    // Your test code here - providers will now return mock data
    providers := config.Providers()
    // ... test logic
}
```

## Formatting

- ALWAYS format any Go code you write.
  - First, try `gofumpt -w .`.
  - If `gofumpt` is not available, use `goimports`.
  - If `goimports` is not available, use `gofmt`.
  - You can also use `task fmt` to run `gofumpt -w .` on the entire project,
    as long as `gofumpt` is on the `PATH`.

## Comments

- Comments that live on their own lines should start with capital letters and
  end with periods. Wrap comments at 78 columns.

## Committing

- ALWAYS use semantic commits (`fix:`, `feat:`, `chore:`, `refactor:`,
  `docs:`, `sec:`, etc).
- Try to keep commits to one line, not including your attribution. Only use
  multi-line commits when additional context is truly necessary.

## Working on the TUI (UI)

Anytime you need to work on the TUI, read `internal/ui/AGENTS.md` before
starting work.

## Styling System

The styling system lives in `internal/ui/styles/` and is organized into
three layers:

- **`quickstyle.go`**: The stable base theme builder. `quickStyle(opts)`
  constructs a `Styles` struct from `quickStyleOpts` — a palette of
  design tokens (primary, secondary, fgBase, bgBase, success, error, etc.).
  `quickStyle` must be fully token-driven: never hardcode specific
  `charmtone.*` colors here (except Chroma syntax highlighting, which is
  pending tokenization). This lets any theme reuse the base without
  inheriting Charmtone-specific colors.
- **`themes.go`**: Defines concrete themes. Each theme function (e.g.
  `CharmtonePantera`) calls `quickStyle` with its palette, then applies
  theme-specific overrides as needed.
- **`styles.go`**: Defines the `Styles` struct and its documentation —
  the shape of what `quickStyle` produces.

**Adding theme-specific overrides**: When a style genuinely needs a
color that doesn't fit the token model (e.g. the bang prompt uses
Salt/Hazy/Larple), keep `quickStyle` on the closest semantic token and
override only the differing colors in the theme function:

```go
func CharmtonePantera() Styles {
	s := quickStyle(quickStyleOpts{ /* palette */ })

	// Override only the colors that differ from the token defaults.
	s.Editor.PromptBangIconFocused = s.Editor.PromptBangIconFocused.
		Foreground(charmtone.Salt).
		Background(charmtone.Hazy)

	return s
}
```

**Adding a new theme**: Add a function in `themes.go` that returns the
result of `quickStyle` with a `quickStyleOpts` palette (plus any needed
overrides), then wire it into `ThemeForProvider`.
