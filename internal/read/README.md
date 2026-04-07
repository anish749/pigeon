# read — file discovery and content search

The `read` package provides two retrieval tools over pigeon's JSONL data tree,
backed by ripgrep (`rg`). Ripgrep is required.

## Tools

| Tool | CLI command | Purpose | Returns |
|------|-------------|---------|---------|
| **Glob** | `pigeon glob` | File discovery — find data files | Absolute file paths, most recent first |
| **Grep** | `pigeon grep` (aliases: `rg`, `search`) | Content search — find content in files | Raw rg output, or file paths (`-l`), or counts (`-c`) |

Read (`pigeon read`) is a separate tool that renders messages from a single
conversation. It is not part of this package.

## Options

| Flag | Glob | Grep | Meaning |
|------|------|------|---------|
| `--since` | ✓ | ✓ | Only files/messages within this time window |
| `--platform` | ✓ | ✓ | Scope to a platform directory |
| `--account` | ✓ | ✓ | Scope to an account directory |
| `-q / --query` | — | ✓ (required) | Ripgrep search pattern |
| `-l` | — | ✓ | File paths only (no content) |
| `-c` | — | ✓ | Match count per file |
| `-i` | — | ✓ | Case insensitive |
| `-F` | — | ✓ | Literal string match (no regex) |
| `-C N` | — | ✓ | Lines of context around matches |

## --since strategy

The `--since` flag uses the same logic in both tools:

### Date files (YYYY-MM-DD.jsonl)

Filtered by filename. Date globs are generated arithmetically from the cutoff
date through today (UTC). For `--since=3d` on 2026-04-07:

```
rg --files --glob 2026-04-05.jsonl --glob 2026-04-06.jsonl --glob 2026-04-07.jsonl
```

No filesystem walk needed — dates are computed, not discovered.

### Thread files (threads/*.jsonl)

Thread filenames are opaque identifiers, not dates. Filtering by filename or
mtime is not reliable (compaction rewrites files). Instead, thread files are
filtered by content using `rg -l` with timestamp prefix patterns:

```
rg -l --glob '**/threads/*.jsonl' -e '"ts":"2026-04-05' -e '"ts":"2026-04-06' -e '"ts":"2026-04-07'
```

This finds threads containing messages within the window without parsing files
in Go. One rg call, returns only matching file paths.

For Grep with `--since`, thread files are included via the `**/threads/*.jsonl`
glob. The caller (CLI layer) post-filters thread results by message timestamp.

## Implementation

| File | Contents |
|------|----------|
| `rg.go` | Shared rg invocation helpers, date glob generation, thread pattern generation |
| `glob.go` | `Glob(dir, since)` — file discovery |
| `grep.go` | `Grep(dir, GrepOpts)` — content search |
| `duration.go` | `ParseDuration(s)` — duration parsing with "d" (days) support |

## Data layout

```
<data-dir>/
├── <platform>/
│   └── <account>/
│       └── <conversation>/
│           ├── YYYY-MM-DD.jsonl        # date files (one per day, UTC)
│           └── threads/
│               └── <id>.jsonl          # thread files (one per thread)
```

All files are JSONL. Each line is a JSON object with a `ts` field (ISO 8601).
