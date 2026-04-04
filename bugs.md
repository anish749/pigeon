# Bugs / Tech Debt

## CLI commands registered via init()

All cobra commands in `internal/cli/` use `init()` to register themselves with `rootCmd`. This means commands are added implicitly at package load time with no explicit control over ordering or dependencies. Should be refactored to explicit registration (e.g. a `RegisterCommands()` function called from the entry point).

Files affected: every `internal/cli/*.go` file that calls `rootCmd.AddCommand()` in `init()`.

## Session file locking pattern improvement over daemon flock

The daemon's device locking (`internal/daemon/flock.go`) uses a bare `LockDevice()` that returns an `*os.File` and relies on the caller to hold the lock and close the file at the right time. Load and save operations are separate functions with comments saying "caller should hold the lock" — nothing enforces it.

The session file management (`internal/claude/session.go`) improves on this with `SessionFile` — a struct that acquires the flock on `OpenSession()`, holds it for all read/write operations, and releases on `Close()`. The caller can't access session data without going through the struct, so the lock is always held during operations.

Consider refactoring `internal/daemon/flock.go` to follow the same pattern if device file access grows beyond the current single lock-and-forget usage.

## Account name representation is ambiguous (slug vs display name vs lowercase)

Account names flow through the system in multiple forms with no clear convention:
- **Slack config**: original casing (`"Coding With Anish"`)
- **Session files**: lowercased (`"coding with anish"`)
- **Session file names**: slugified (`"slack-coding-with-anish.yaml"`)
- **Listener callbacks**: original casing from config (`"Coding With Anish"`)
- **Hub channel keys**: lowercased at `Route()` call site
- **Store data directories**: original casing (`~/.local/share/pigeon/slack/Coding With Anish/`)

This makes it unclear at any given call site which form you're working with. The hub works around it with `strings.ToLower()` in `Route()`, but the mismatch between data dir casing and session file casing could cause read failures when the hub tries to read messages from disk.

**Fix:** Define named types or at minimum clear naming conventions (e.g. `accountSlug`, `accountDisplay`, `accountDir`) so the code is self-documenting about which representation is expected. Normalize consistently at system boundaries.

Files affected: `internal/hub/hub.go`, `internal/claude/session.go`, `internal/listener/slack/listener.go`, `internal/store/store.go`.

## Centralize log file management

The daemon and MCP server configure logging independently:
- `internal/commands/daemon.go` sets up slog with lumberjack writing to `daemon.log`
- `cmd/pigeon-mcp/main.go` sets up slog with lumberjack writing to `mcp.log`
- Both duplicate the stateDir resolution and lumberjack config

Should have a shared logging setup function that takes a log file name and returns a configured slog handler, so all processes use consistent log rotation settings and directory resolution.
