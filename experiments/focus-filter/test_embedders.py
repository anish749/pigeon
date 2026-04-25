# /// script
# requires-python = ">=3.10"
# dependencies = ["sentence-transformers>=3.0", "numpy"]
# ///
"""Compare embedders on the per-workstream binary task across workspaces.

Tests:
  - all-MiniLM-L6-v2 (384 dim, current sidecar)
  - BAAI/bge-base-en-v1.5 (768 dim, stronger retrieval embedder)
  - intfloat/e5-base-v2 (768 dim, similar class)

For each, runs `focus + neg(all-foci, max)` against every workspace
configured in workstreams.json with a corresponding labels-*.json file,
sweeping cosine thresholds.
"""

import argparse
import glob
import json
import os
import sys
import time
from collections import Counter
from pathlib import Path

import numpy as np
from sentence_transformers import SentenceTransformer

PIGEON_DATA = Path(os.path.expanduser("~/.local/share/pigeon"))
CONFIG_PATH = Path(__file__).parent / "workstreams.json"

EMBEDDERS = [
    "all-MiniLM-L6-v2",
    "BAAI/bge-base-en-v1.5",
    "intfloat/e5-base-v2",
]


def cos_to_each(v, mat):
    return (mat @ v) / (np.linalg.norm(mat, axis=1) * np.linalg.norm(v) + 1e-12)


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
                    sigs.append({"ts": obj.get("ts", ""), "sender": obj.get("sender", ""),
                                 "text": text, "conversation": conv})
        except OSError:
            continue
    sigs.sort(key=lambda s: s["ts"])
    return sigs


def evaluate(workspace_name, ws_config, model, threshold, model_name):
    workstreams = ws_config["workstreams"]
    label_path = Path(__file__).parent / f"labels-{workspace_name}.json"
    with open(label_path) as fh:
        labels = json.load(fh)

    # E5 expects "query: " / "passage: " prefixes; bge prefers no prefix
    if "e5-" in model_name:
        focus_prefix = "passage: "
        msg_prefix = "query: "
    else:
        focus_prefix = ""
        msg_prefix = ""

    focus_texts = [focus_prefix + ws["focus"] for ws in workstreams]
    focus_embs = model.encode(focus_texts, show_progress_bar=False, batch_size=32, normalize_embeddings=True)
    focus_emb_by_id = {ws["id"]: focus_embs[i] for i, ws in enumerate(workstreams)}

    signals = load_signals(ws_config["slack_dir"])
    msg_texts = [msg_prefix + s["text"] for s in signals]
    msg_embs = model.encode(msg_texts, show_progress_bar=False, batch_size=64, normalize_embeddings=True)
    sigs_by_key = {(s["ts"], s["conversation"], s["sender"], s["text"]): i
                   for i, s in enumerate(signals)}

    per_ws_tp = Counter(); per_ws_fp = Counter(); per_ws_fn = Counter()
    total_tp = total_fp = total_fn = 0

    # Pre-assemble negative matrices per workstream (all other foci)
    neg_mats = {}
    for ws in workstreams:
        neg_rows = [focus_emb_by_id[other["id"]] for other in workstreams if other["id"] != ws["id"]]
        neg_mats[ws["id"]] = np.array(neg_rows)

    for entry in labels.values():
        if entry["labels"] is None:
            continue
        key = (entry["ts"], entry["conversation"], entry["sender"], entry["text"])
        if key not in sigs_by_key:
            continue
        v_msg = msg_embs[sigs_by_key[key]]
        true_set = set(entry["labels"])
        for ws in workstreams:
            wid = ws["id"]
            v_focus = focus_emb_by_id[wid]
            s_pos = float(np.dot(v_msg, v_focus))  # already normalized
            routed = False
            if s_pos >= threshold:
                s_neg_arr = neg_mats[wid] @ v_msg
                s_neg = float(s_neg_arr.max())
                if s_pos > s_neg:
                    routed = True
            in_true = wid in true_set
            if routed and in_true:
                per_ws_tp[wid] += 1; total_tp += 1
            elif routed:
                per_ws_fp[wid] += 1; total_fp += 1
            elif in_true:
                per_ws_fn[wid] += 1; total_fn += 1

    p = total_tp / max(total_tp + total_fp, 1)
    r = total_tp / max(total_tp + total_fn, 1)
    f1 = 2 * p * r / (p + r) if (p + r) else 0.0
    # Per-workstream macro on those with >= 3 positives
    macro_f1 = 0; n = 0
    for ws in workstreams:
        wid = ws["id"]
        t, f, fn = per_ws_tp.get(wid, 0), per_ws_fp.get(wid, 0), per_ws_fn.get(wid, 0)
        if t + fn < 3:
            continue
        p2 = t / max(t + f, 1); r2 = t / max(t + fn, 1)
        f12 = 2 * p2 * r2 / (p2 + r2) if (p2 + r2) else 0.0
        macro_f1 += f12; n += 1
    return {
        "micro_p": p, "micro_r": r, "micro_f1": f1,
        "macro_f1": macro_f1 / max(n, 1), "n_eligible": n,
    }


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--threshold", type=float, default=0.27)
    args = ap.parse_args()

    with open(CONFIG_PATH) as fh:
        config = json.load(fh)

    results = {}
    for model_name in EMBEDDERS:
        print(f"\n=== Loading {model_name} ===", file=sys.stderr)
        t0 = time.time()
        model = SentenceTransformer(model_name)
        print(f"  loaded in {time.time()-t0:.1f}s", file=sys.stderr)
        # Different models cluster cosines differently; sweep thresholds.
        # bge/e5 with normalized embeddings produce sims in roughly the same
        # range as MiniLM but with different absolute means; we'll sweep.
        threshold_sweep = [0.20, 0.25, 0.30, 0.35, 0.40, 0.45, 0.50, 0.55, 0.60, 0.65, 0.70]
        # Workspaces with a corresponding labels-<ws>.json file alongside
        # this script.
        labelled_workspaces = [
            ws for ws in config
            if (Path(__file__).parent / f"labels-{ws}.json").exists()
        ]
        results[model_name] = {}
        for thr in threshold_sweep:
            ws_results = {}
            for ws_name in labelled_workspaces:
                ws_results[ws_name] = evaluate(ws_name, config[ws_name], model, thr, model_name)
            results[model_name][thr] = ws_results

    # Report
    print("\n" + "=" * 100)
    print("EMBEDDER COMPARISON  (focus + neg(all-foci,max), no margin)")
    print("=" * 100)
    header_cells = ["embedder", "threshold"] + [f"{w} mF1" for w in labelled_workspaces] + ["mean"]
    print("\n  " + " ".join(f"{c:>10s}" for c in header_cells))
    rows = []
    for model_name, by_thr in results.items():
        for thr, ws_results in by_thr.items():
            f1s = [ws_results[w]["micro_f1"] for w in labelled_workspaces]
            mean = sum(f1s) / max(len(f1s), 1)
            rows.append((model_name, thr, f1s, mean))
    rows.sort(key=lambda r: -r[3])
    for model_name, thr, f1s, mean in rows[:30]:
        cells = [f"{x:.3f}" for x in f1s]
        print(f"  {model_name:<28s} {thr:>9.2f}  " + "  ".join(f"{c:>10s}" for c in cells)
              + f"  {mean:>7.3f}")


if __name__ == "__main__":
    main()
