# /// script
# requires-python = ">=3.10"
# dependencies = ["sentence-transformers>=3.0", "numpy"]
# ///
"""
Three-way comparison: haiku vs sonnet vs opus as the discover model.

For each (workspace, model), loads:
  - workstreams-<model>.json    — discover output (parsed)
  - labels-<workspace>-<model>.json — Claude judge labels for that
                                      workstream set

Runs the recall-wake metric on each combination, reports operating
points side by side.

Question being answered: does using a stronger model in
`pigeon workstream discover` actually lift the routing recall ceiling?
"""

import glob
import json
import os
import sys
from collections import defaultdict
from dataclasses import dataclass
from pathlib import Path

import numpy as np
from sentence_transformers import SentenceTransformer

MODEL_NAME = "all-MiniLM-L6-v2"
PIGEON_DATA = Path(os.path.expanduser("~/.local/share/pigeon"))
HERE = Path(__file__).parent

WORKSPACES = ["trudy", "igfd", "tubular"]
DISCOVER_MODELS = ["haiku", "sonnet", "opus"]
THRESHOLDS = [0.10, 0.14, 0.18, 0.20, 0.22, 0.25, 0.27, 0.30, 0.35]


def cos(a, b):
    return float(np.dot(a, b) / (np.linalg.norm(a) * np.linalg.norm(b) + 1e-12))


def load_signals(slack_dir):
    sigs = []
    for f in sorted(glob.glob(str(PIGEON_DATA / "slack" / slack_dir / "*" / "*.jsonl"))):
        if "2026-04" not in f:
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
                    sigs.append({"ts": obj.get("ts", ""),
                                 "sender": obj.get("sender", ""),
                                 "text": text, "conversation": conv})
        except OSError:
            continue
    sigs.sort(key=lambda s: s["ts"])
    return sigs


@dataclass
class Metrics:
    recall: float
    wake_noise: float
    woken_per_msg: float
    n_workstreams: int


def evaluate_one(workspace, model_name, st_model):
    config_path = HERE / f"workstreams-{model_name}.json"
    labels_path = HERE / f"labels-{workspace}-{model_name}.json"
    if not config_path.exists() or not labels_path.exists():
        return None
    with open(config_path) as fh:
        config = json.load(fh)
    if workspace not in config:
        return None
    workstreams = config[workspace]["workstreams"]
    n_ws = len(workstreams)
    with open(labels_path) as fh:
        labels = json.load(fh)

    focus_embs = {ws["id"]: st_model.encode(ws["focus"], show_progress_bar=False,
                                              normalize_embeddings=False)
                  for ws in workstreams}

    signals = load_signals(config[workspace]["slack_dir"])
    sig_embs = np.array(st_model.encode([s["text"] for s in signals],
                                          show_progress_bar=False, batch_size=64))
    sigs_by_key = {(s["ts"], s["conversation"], s["sender"], s["text"]): i
                    for i, s in enumerate(signals)}

    out = {}
    for thr in THRESHOLDS:
        n_msgs = n_relevant = n_noise = 0
        n_pairs = pairs_recalled = 0
        sum_woken = noise_routed_anything = 0
        for entry in labels.values():
            if entry["labels"] is None:
                continue
            key = (entry["ts"], entry["conversation"], entry["sender"], entry["text"])
            idx = sigs_by_key.get(key)
            if idx is None:
                continue
            n_msgs += 1
            v_msg = sig_embs[idx]
            true_set = set(entry["labels"])
            routed = {wid for wid, e in focus_embs.items() if cos(v_msg, e) >= thr}
            sum_woken += len(routed)
            if true_set:
                n_relevant += 1
                n_pairs += len(true_set)
                pairs_recalled += len(true_set & routed)
            else:
                n_noise += 1
                if routed:
                    noise_routed_anything += 1
        out[thr] = Metrics(
            recall=pairs_recalled / max(n_pairs, 1),
            wake_noise=noise_routed_anything / max(n_noise, 1),
            woken_per_msg=sum_woken / max(n_msgs, 1),
            n_workstreams=n_ws,
        )
    return out


def main():
    print(f"Loading {MODEL_NAME}...", file=sys.stderr)
    model = SentenceTransformer(MODEL_NAME)

    all_results = {}
    for ws in WORKSPACES:
        all_results[ws] = {}
        for m in DISCOVER_MODELS:
            r = evaluate_one(ws, m, model)
            if r is None:
                print(f"  [{ws}/{m}] missing config or labels — skipping",
                      file=sys.stderr)
                continue
            all_results[ws][m] = r
            n_ws = next(iter(r.values())).n_workstreams
            print(f"  [{ws}/{m}] evaluated ({n_ws} workstreams)",
                  file=sys.stderr)

    # Side-by-side: best operating point per (workspace, model) where
    # wake_noise <= 0.30, picking highest recall.
    print("\n" + "=" * 105)
    print("BEST OPERATING POINT  (wake_rate_noise <= 0.30, pick max recall)")
    print("=" * 105)
    print(f"\n  {'workspace':<10s} {'model':<8s} {'#ws':>4s} {'τ':>6s} "
          f"{'recall':>8s} {'wake/n':>8s} {'woken/m':>9s} {'fan-in':>8s}")
    for ws in WORKSPACES:
        for m in DISCOVER_MODELS:
            r = all_results[ws].get(m)
            if r is None:
                continue
            best = None
            for thr, metrics in sorted(r.items()):
                if metrics.wake_noise <= 0.30:
                    if best is None or metrics.recall > best[1].recall:
                        best = (thr, metrics)
            if best is None:
                # No threshold satisfies — pick the strictest (lowest noise)
                best = min(r.items(), key=lambda x: x[1].wake_noise)
            thr, metrics = best
            n_ws = metrics.n_workstreams
            fan_in = n_ws / max(metrics.woken_per_msg, 0.001)
            print(f"  {ws:<10s} {m:<8s} {n_ws:>4d} {thr:>6.2f} "
                  f"{metrics.recall:>8.3f} {metrics.wake_noise:>8.3f} "
                  f"{metrics.woken_per_msg:>9.2f} {fan_in:>7.1f}x")
        print()

    # Curves
    print("\n" + "=" * 105)
    print("FULL CURVES  (recall and wake_noise across thresholds)")
    print("=" * 105)
    for ws in WORKSPACES:
        print(f"\n  {ws}:")
        print(f"    {'τ':>6s}", end="")
        for m in DISCOVER_MODELS:
            print(f" {m+' R':>10s} {m+' W/N':>10s}", end="")
        print()
        for thr in THRESHOLDS:
            print(f"    {thr:>6.2f}", end="")
            for m in DISCOVER_MODELS:
                r = all_results[ws].get(m)
                if r is None:
                    print(f" {'-':>10s} {'-':>10s}", end="")
                else:
                    metrics = r[thr]
                    print(f" {metrics.recall:>10.3f} {metrics.wake_noise:>10.3f}",
                          end="")
            print()


if __name__ == "__main__":
    main()
