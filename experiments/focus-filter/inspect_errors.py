# /// script
# requires-python = ">=3.10"
# dependencies = ["sentence-transformers>=3.0", "numpy"]
# ///
"""Inspect filter failures (FN/FP samples) to understand routing errors
on a specific workspace.

Usage:
  uv run experiments/inspect_errors.py --workspace <name>
"""

import argparse
import glob
import json
import os
import sys
from collections import Counter, defaultdict
from pathlib import Path

import numpy as np
from sentence_transformers import SentenceTransformer

PIGEON_DATA = Path(os.path.expanduser("~/.local/share/pigeon"))
CONFIG_PATH = Path(__file__).parent / "workstreams.json"


def cos(a, b):
    return float(np.dot(a, b) / (np.linalg.norm(a) * np.linalg.norm(b) + 1e-12))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--workspace", required=True,
                    help="workspace name (must be a key in workstreams.json)")
    ap.add_argument("--threshold", type=float, default=0.27)
    args = ap.parse_args()

    with open(CONFIG_PATH) as fh:
        config = json.load(fh)
    workstreams = config[args.workspace]["workstreams"]
    ws_by_id = {w["id"]: w for w in workstreams}

    label_path = Path(__file__).parent / f"labels-{args.workspace}.json"
    with open(label_path) as fh:
        labels = json.load(fh)

    print(f"Loading model...", file=sys.stderr)
    model = SentenceTransformer("all-MiniLM-L6-v2")

    # Embed all foci
    focus_embs = {w["id"]: model.encode(w["focus"], show_progress_bar=False)
                  for w in workstreams}

    # Walk labels, for each compute focus similarities, compare to true labels
    n_total = 0
    n_judge_route = 0
    n_filter_route = 0
    n_agree = 0
    fn_examples = []  # judge says yes, filter says no
    fp_examples = []  # filter says yes, judge says no

    for entry in labels.values():
        if entry["labels"] is None:
            continue
        n_total += 1
        text = entry["text"]
        v_msg = model.encode(text, show_progress_bar=False)
        true_set = set(entry["labels"])
        if true_set:
            n_judge_route += 1

        # Compute filter decisions per workstream
        scores = {wid: cos(v_msg, e) for wid, e in focus_embs.items()}
        # Run focus+neg(all-foci,max)@τ=0.27
        pred_set = set()
        for wid in scores:
            s_pos = scores[wid]
            if s_pos < args.threshold:
                continue
            s_neg = max(scores[other] for other in scores if other != wid)
            if s_pos > s_neg:
                pred_set.add(wid)

        if pred_set:
            n_filter_route += 1
        if pred_set == true_set:
            n_agree += 1

        # Collect FP / FN samples
        for wid in true_set - pred_set:
            if len(fn_examples) < 12:
                top3 = sorted(scores.items(), key=lambda x: -x[1])[:3]
                fn_examples.append({
                    "text": text,
                    "true_ws": [ws_by_id[w]["name"] for w in true_set],
                    "true_top3": [(ws_by_id[w]["name"][:30], f"{s:.3f}") for w, s in top3],
                    "missed": ws_by_id[wid]["name"],
                    "missed_score": scores[wid],
                })
        for wid in pred_set - true_set:
            if len(fp_examples) < 12:
                fp_examples.append({
                    "text": text,
                    "predicted": ws_by_id[wid]["name"],
                    "score": scores[wid],
                    "true_labels": [ws_by_id[w]["name"] for w in true_set] or ["(none)"],
                })

    print(f"\nTotal: {n_total}")
    print(f"Judge labels something: {n_judge_route}")
    print(f"Filter routes something: {n_filter_route}")
    print(f"Exact set agreement: {n_agree}")

    print("\n--- FALSE NEGATIVES (judge said yes, filter missed) ---")
    for ex in fn_examples:
        print(f"  text: {ex['text'][:100]!r}")
        print(f"    judge: {ex['true_ws']}")
        print(f"    missed: {ex['missed']} (score {ex['missed_score']:.3f})")
        print(f"    top3 cosines: {ex['true_top3']}")

    print("\n--- FALSE POSITIVES (filter routed, judge said no/different) ---")
    for ex in fp_examples:
        print(f"  text: {ex['text'][:100]!r}")
        print(f"    filter said: {ex['predicted']} (score {ex['score']:.3f})")
        print(f"    judge said: {ex['true_labels']}")


if __name__ == "__main__":
    main()
