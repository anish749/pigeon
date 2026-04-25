# Focus-filter validation harnesses

Harnesses used to validate the focus-filter design in
`docs/monitor-focus-filter-spec.md`. The spec records the conclusions;
this directory is the apparatus that produced them.

## Setup

Each script is a self-contained `uv` script with inline dependencies.
No shared install needed — `uv run experiments/<script>.py` will set up
the environment on first use.

Two pieces of state the scripts read:

1. **`workstreams.json`** — workspace-keyed config: per-workspace Slack
   directory name, optional ID merges, and a list of workstreams with
   `{id, name, focus}`. See `workstreams.example.json` for the schema.
   The actual file is gitignored — it contains workspace-specific data.
2. **`labels-<workspace>.json`** — per-workspace Claude-judge labels,
   produced by `label_signals_v2.py`. Also gitignored.

Pigeon data is read from the standard pigeon path under
`~/.local/share/pigeon/slack/<dir>/...` — one of the harnesses' inputs
is whatever the local pigeon daemon has synced.

## Workflow

```
# 1. Discover workstreams (uses pigeon CLI + Claude). Output is the
#    raw input you copy into workstreams.json by hand.
pigeon workstream discover --workspace <name> --since 2026-04-01

# 2. Sample messages and label them with the Claude judge.
uv run experiments/label_signals_v2.py --workspace <name>

# 3. Inspect focus-prose geometry — check whether the discovered
#    workstreams are sufficiently distinct from each other.
uv run experiments/analyze_focus_geometry.py

# 4. Sweep routing variants and operating points.
uv run experiments/evaluate_recall_wake.py
uv run experiments/evaluate_multi_workspace.py    # F1-based, earlier framing
uv run experiments/test_embedders.py              # MiniLM vs bge vs e5

# 5. Inspect specific failure cases.
uv run experiments/inspect_errors.py --workspace <name>
```

## What each script measures

- **`label_signals_v2.py`** — produces ground-truth labels by sending
  each sampled message + 3 messages of conversation context to Claude
  Haiku and asking which workstreams (if any) it belongs to.
- **`analyze_focus_geometry.py`** — pairwise cosine matrix over each
  workspace's focus prose, across MiniLM / bge / e5. Identifies pairs
  the embedder cannot distinguish.
- **`evaluate_recall_wake.py`** — sweeps thresholds and reports
  recall, wake-rate-on-noise, and average sessions woken per message.
  These are the metrics the spec optimises for.
- **`evaluate_multi_workspace.py`** — older F1-based sweep over
  routing variants; superseded by the recall-wake harness for picking
  operating points but useful as a sanity check.
- **`test_embedders.py`** — same algorithm against three embedders to
  confirm `all-MiniLM-L6-v2` is a reasonable default.
- **`inspect_errors.py`** — pulls FN/FP samples from the labels for a
  given workspace so the failure mode can be read directly.

## Things this directory does NOT contain

- The labels themselves (`labels-*.json`) — gitignored.
- The real `workstreams.json` — gitignored. See `.example.json`.
- Pigeon message data — read from `~/.local/share/pigeon/slack/`.
