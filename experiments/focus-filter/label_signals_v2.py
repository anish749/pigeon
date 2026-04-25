# /// script
# requires-python = ">=3.10"
# dependencies = []
# ///
"""
Label a stratified sample of messages with workstream membership using
Claude Haiku as the judge.

Reads workstream config from experiments/workstreams.json (keyed by
workspace name), loads signals from the corresponding Slack directory,
samples stratified messages, asks Claude which workstream(s) each
belongs to, and caches the labels to experiments/labels-<workspace>.json.

Usage:
  uv run experiments/label_signals_v2.py --workspace <name>

The workspace name must be a key in experiments/workstreams.json (see
experiments/workstreams.example.json for the schema).
"""

import argparse
import glob
import json
import os
import random
import subprocess
import sys
from collections import defaultdict, deque
from pathlib import Path

PIGEON_DATA = Path(os.path.expanduser("~/.local/share/pigeon"))
CONFIG_PATH = Path(__file__).parent / "workstreams.json"


def load_signals(slack_dir_name: str, since_prefix: str = "2026-04"):
    workspace_dir = PIGEON_DATA / "slack" / slack_dir_name
    sigs = []
    for f in sorted(glob.glob(str(workspace_dir / "*" / "*.jsonl"))):
        if since_prefix not in f:
            continue
        conv = Path(f).parent.name
        try:
            with open(f) as fh:
                for line in fh:
                    try:
                        obj = json.loads(line)
                    except json.JSONDecodeError:
                        continue
                    if obj.get("type") != "msg":
                        continue
                    text = (obj.get("text") or "").strip()
                    if not text:
                        continue
                    sigs.append({
                        "ts": obj.get("ts", ""),
                        "sender": obj.get("sender", ""),
                        "text": text,
                        "conversation": conv,
                    })
        except OSError:
            continue
    sigs.sort(key=lambda s: s["ts"])
    return sigs


def stratified_sample(signals, n_short=70, n_med=70, n_long=70, seed=42):
    short = [s for s in signals if len(s["text"]) < 25]
    medium = [s for s in signals if 25 <= len(s["text"]) < 100]
    longs = [s for s in signals if len(s["text"]) >= 100]
    rng = random.Random(seed)
    return (rng.sample(short, min(n_short, len(short)))
            + rng.sample(medium, min(n_med, len(medium)))
            + rng.sample(longs, min(n_long, len(longs))))


def build_context_map(signals, window=3):
    bufs = defaultdict(lambda: deque(maxlen=window))
    contexts = {}
    for i, s in enumerate(signals):
        contexts[(s["ts"], s["text"], s["conversation"])] = list(bufs[s["conversation"]])
        bufs[s["conversation"]].append(s)
    return contexts


def build_prompt(workspace, workstreams, signal, context):
    ws_lines = "\n".join(f"  {w['id']}: {w['name']} — {w['focus']}"
                         for w in workstreams)
    ctx_lines = "\n".join(f"  [{c['sender']}]: {c['text']}" for c in context) or "  (none)"
    valid_ids = ", ".join(w["id"] for w in workstreams)
    return f"""Workstreams in {workspace} workspace:
{ws_lines}

Recent conversation context (last 3 messages in {signal['conversation']}):
{ctx_lines}

CURRENT MESSAGE:
  [{signal['sender']}]: {signal['text']}

Question: Which workstream(s) does the CURRENT MESSAGE belong to?

Rules:
- A short reply ("yes", "ok", ":+1:", "morning?") belongs to a workstream ONLY IF the surrounding context is clearly about that workstream's specific work.
- Generic logistics ("running late", "want to grab coffee", "happy birthday") belong to NO workstream unless they're about workstream-specific work.
- A message can belong to multiple workstreams if it genuinely spans them.
- Default: if uncertain or it's just general chat, return empty array.

Return ONLY this JSON, nothing else:
{{"workstreams": ["ws-id-1", "ws-id-2"]}}

Valid IDs: {valid_ids}
Empty array means "none of the above"."""


def call_claude(prompt, model="haiku"):
    result = subprocess.run(
        ["claude", "-p", "--model", model, "--output-format", "json",
         "--no-session-persistence", "--tools", "", "--setting-sources", "",
         "--", prompt],
        capture_output=True, text=True,
    )
    if result.returncode != 0:
        raise RuntimeError(f"claude failed: {result.stderr[:200]}")
    env = json.loads(result.stdout)
    if env.get("is_error"):
        raise RuntimeError(f"claude error: {env.get('result')}")
    return env.get("result", "")


def parse_labels(text, valid_ids):
    text = text.strip()
    if text.startswith("```"):
        lines = text.split("\n")
        text = "\n".join(l for l in lines if not l.startswith("```"))
    start = text.find("{")
    end = text.rfind("}")
    if start < 0 or end < 0:
        return None
    try:
        obj = json.loads(text[start:end+1])
        return [l for l in obj.get("workstreams", []) if l in valid_ids]
    except json.JSONDecodeError:
        return None


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--workspace", required=True,
                    help="workspace name (must be a key in workstreams.json)")
    ap.add_argument("--n-short", type=int, default=70)
    ap.add_argument("--n-medium", type=int, default=70)
    ap.add_argument("--n-long", type=int, default=70)
    ap.add_argument("--model", default="haiku")
    args = ap.parse_args()

    with open(CONFIG_PATH) as fh:
        config = json.load(fh)
    if args.workspace not in config:
        print(f"workspace {args.workspace} not in {CONFIG_PATH}", file=sys.stderr)
        sys.exit(1)
    ws_config = config[args.workspace]
    workstreams = ws_config["workstreams"]
    valid_ids = {w["id"] for w in workstreams}

    cache_path = Path(__file__).parent / f"labels-{args.workspace}.json"
    cache = {}
    if cache_path.exists():
        with open(cache_path) as fh:
            cache = json.load(fh)
        print(f"Loaded {len(cache)} cached labels from {cache_path}", file=sys.stderr)

    signals = load_signals(ws_config["slack_dir"])
    print(f"Loaded {len(signals)} signals", file=sys.stderr)
    contexts = build_context_map(signals, window=3)
    sample = stratified_sample(signals, args.n_short, args.n_medium, args.n_long)
    print(f"Stratified sample: {len(sample)} signals", file=sys.stderr)

    for i, s in enumerate(sample):
        key = f"{s['ts']}|{s['conversation']}|{s['sender']}"
        if key in cache:
            continue
        ctx = contexts.get((s["ts"], s["text"], s["conversation"]), [])
        prompt = build_prompt(args.workspace, workstreams, s, ctx)
        try:
            raw = call_claude(prompt, args.model)
            labels = parse_labels(raw, valid_ids)
            if labels is None:
                print(f"  [{i+1}/{len(sample)}] PARSE FAIL: {raw[:100]}", file=sys.stderr)
                labels = []
        except Exception as e:
            print(f"  [{i+1}/{len(sample)}] ERROR: {e}", file=sys.stderr)
            labels = None
        cache[key] = {
            "ts": s["ts"], "sender": s["sender"], "text": s["text"],
            "conversation": s["conversation"], "labels": labels,
        }
        if (i + 1) % 5 == 0 or i == len(sample) - 1:
            with open(cache_path, "w") as fh:
                json.dump(cache, fh, indent=2)
            print(f"  [{i+1}/{len(sample)}] cached", file=sys.stderr)

    with open(cache_path, "w") as fh:
        json.dump(cache, fh, indent=2)
    print(f"\nDone. {len(cache)} labels in {cache_path}", file=sys.stderr)

    counts = defaultdict(int)
    for entry in cache.values():
        if entry["labels"] is None:
            counts["error"] += 1
            continue
        if not entry["labels"]:
            counts["none"] += 1
        else:
            for l in entry["labels"]:
                counts[l] += 1
    print("\nLabel distribution:", file=sys.stderr)
    for k, v in sorted(counts.items(), key=lambda x: -x[1]):
        print(f"  {k}: {v}", file=sys.stderr)


if __name__ == "__main__":
    main()
