# AGENTS.md

This file provides guidance to AI coding agents when working with code in this repository.

## Build & Run

Run `./pigeon <command>` — the wrapper script auto-builds to `pigeon.bin` (gitignored) and executes.
Do not run `go build -o pigeon` directly; that overwrites the wrapper script.
`./pigeon help` gives everything you need to know about available commands and usage.

## GitHub

When creating pull requests, use the GitHub CLI (`gh pr create`) from the local checkout.
Before committing or opening a PR:
- Run `gofmt` only on changed Go files.
- Stage only files changed for the task; add files explicitly by path, never with broad staging commands.
- Run `go test ./...` and `go vet ./...`.
- Keep the PR description concise: a couple of sentences covering the change, intent, and outcome, not an exhaustive file-by-file list.

## Testing

```bash
go test ./...                           # all tests
go test ./internal/store/...            # one package
go test ./internal/store/... -run TestResolve  # one test
```

Tests require `ripgrep` (`rg`) and `uv` on PATH. CI (.github/workflows/ci.yml) runs `go build ./...` then `go test ./...`.

## Architecture

Pigeon is a local-first messaging bridge for AI agents. It mirrors messaging and workspace history (Slack, WhatsApp, Google Workspace, Linear, Jira) as local files and provides a CLI + MCP channel server for Claude Code.

### Package Layering

```
cmd/pigeon/main.go → cli.Execute()
       ↓
  internal/cli          Cobra command tree; each subcommand in its own file
       ↓
  internal/commands     Business logic behind CLI commands (setup, contacts, list, etc.)
       ↓
  internal/daemon       Daemon process lifecycle; *_manager.go files start per-platform listeners
       ↓
  internal/hub          Routes incoming messages to connected Claude Code sessions via SSE
       ↓
  internal/listener/*   Platform-specific listeners (slack, whatsapp) — write to store, observe identity
  internal/gws/*        Google Workspace pollers (drive, gmail, calendar)
  internal/linear/*     Linear issue poller
  internal/jira/*       Jira issue poller
       ↓
  internal/store        Store interface + JSONL protocol v1 implementation (modelv1/)
  internal/identity     Cross-platform contact identity (Observer/Resolver pattern)
       ↓
  internal/paths        Centralized path resolution — all other packages import paths from here
  internal/read         Read/glob/grep operations using ripgrep
  internal/search       Parsing and summarizing rg output
  internal/workstream   Workstream discovery, routing, replay, and TUI support
```

### Key Subsystems

- **Daemon** (`internal/daemon`): `process.go` manages the daemon lifecycle. Each platform has a `*_manager.go` that instantiates its listener. The daemon serves a Unix socket API and runs the hub.

- **Hub** (`internal/hub`): Routes incoming messages to the right Claude Code session via SSE. One session per account. `handler.go` implements the SSE endpoint.

- **MCP Server** (`internal/mcp/server`): Runs inside Claude Code as a channel server. Connects to the daemon's SSE endpoint and delivers messages as channel notifications.

- **Outbox** (`internal/outbox`): Queues outgoing messages from Claude for human review. `pigeon review` opens a Bubble Tea TUI for approve/reject.

- **Paths** (`internal/paths`): XDG-compliant directory resolution. Env overrides: `PIGEON_CONFIG_DIR`, `PIGEON_DATA_DIR`, `PIGEON_STATE_DIR`. Defaults: `~/.config/pigeon/`, `~/.local/share/pigeon/`, `~/.local/state/pigeon/`.

- **Workspace** (`internal/workspace`): Scopes CLI operations to a subset of accounts (--workspace flag → env → config → all).

- **Identity** (`internal/identity`): Cross-source contact resolution. Listeners report signals via `Observer`; `Reader` provides lookup.

- **Workstream** (`internal/workstream`): Reads recent cross-source signals, discovers ongoing efforts, routes signals to workstreams, and persists workstream/proposal state under the data root's workspace area.

### Reading Data: Store vs Read

There are two sibling subsystems for reading message data, at the same level of the dependency tree. Higher-level code (cli, commands, hub) picks whichever is appropriate for the task.

```
cli/commands/hub
    ├── store (structured)     — parse JSONL, resolve reactions/edits, return typed Go structs
    └── read  (file-oriented)  — discover and search files via ripgrep, return paths or raw bytes
```

**`internal/store`** — Structured, conversation-aware reads. `Store.ReadConversation()` loads JSONL date/thread files, parses each line into typed `modelv1` structs (`MsgLine`, `ReactLine`, `EditLine`, etc.), applies compaction (dedup, edit/delete reconciliation), and resolves reactions onto their parent messages. Returns `ResolvedDateFile` with fully materialized messages. Used by `pigeon read` (via `commands.RunRead`) and the hub for formatting messages to send to Claude. The JSONL format is designed to be greppable — tests in `store/grep_test.go` and `store/rg_test.go` verify this by running actual `rg` commands against written files.

**`internal/read`** — File discovery and content search via ripgrep. Main entry points:
- `read.Glob(dir, since)` — returns typed `paths.DataFile` values. With `since == 0`, it finds JSONL files plus Drive content. With `since > 0`, it finds date-named JSONL files, thread files filtered by content (`rg -l` for timestamp prefixes), and Drive content resolved through sibling `drive-meta-*.json` files.
- `read.Grep(dir, opts)` — content search. When `since > 0`, pre-computes the file list via `Glob` then passes it to `rg` as positional args. When `since == 0`, lets `rg` walk the tree with glob filters (faster).
- `read.GlobFiles(dir, globs)` — raw glob with custom patterns, no date logic.

Both are scoped by `read.SearchDirs(workspace, platform, account)` which resolves workspace boundaries and returns the directories to operate on.

**Who uses what:**
- `pigeon read` → `store.ReadConversation()` — needs parsed, resolved messages for human-readable output
- `pigeon list --since` → `read.Glob()` → extracts conversation paths and last-line timestamps from matched files
- `pigeon list` (no --since) → `store.ListConversations()` — directory enumeration
- `pigeon glob` → `read.Glob()` / `read.GlobFiles()` — file discovery for agents
- `pigeon grep` → `read.Grep()` — content search for agents
- Hub → `store.ReadConversation()` — formats last N messages for Claude delivery

### Data Flow

1. Listeners receive platform events → write JSONL lines via `store.Append()`
2. Pollers receive GWS/Linear/Jira updates → write JSONL/content files through the store and typed paths
3. Hub picks up new messages → pushes to connected MCP sessions via SSE
4. Claude Code receives channel notifications → can read/search/reply via CLI
5. Outgoing replies go to the outbox → human approves via `pigeon review` → sent via `api.Send()`

## Error Handling

Errors are the caller's decision — always propagate them, never hide them.

1. **Propagate, don't swallow.** If a function returns an error, the caller must handle or
   propagate it. Never `_ = someFunc()`. Logging inside a loop is still swallowing — the
   callee reports errors; the caller decides what to do.

2. **Collect errors in loops.** Partial failures are still failures. When processing multiple
   items (messages, channels, contacts), collect errors and return them:
   ```go
   var errs []error
   for _, msg := range msgs {
       if err := store.Write(msg); err != nil {
           errs = append(errs, err)
       }
   }
   return errors.Join(errs...)
   ```

3. **Wrap with context.** Every error return should say what failed and why — this codebase
   already does this well with `fmt.Errorf("verb noun: %w", err)`.

4. **Never hide errors behind defaults.** Don't return `&User{}`, `0`, or `[]Item{}` on error
   — the caller can't distinguish "no data" from "fetch failed". Always return the error.
   Exceptions: explicit `OrDefault` functions, or first-run cases where a missing file is
   expected (e.g. `loadCursors` returning empty map when cursor file doesn't exist yet).

5. **Skip useless nil checks.** Ranging over a nil slice is safe in Go — don't guard it.
   Only nil-check pointers and interfaces that can actually be nil.
