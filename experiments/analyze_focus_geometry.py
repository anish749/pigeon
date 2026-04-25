# /// script
# requires-python = ">=3.10"
# dependencies = ["sentence-transformers>=3.0", "numpy"]
# ///
"""
Analyze whether workstream focuses are sufficiently distinct in
embedding space, per workspace, across embedders.

For each workspace × embedder:
  - Embed all focuses
  - Compute the full pairwise cosine matrix
  - Report:
      - Mean / median / min / max off-diagonal cosine
      - Top-5 most similar focus pairs (highest off-diagonal cosines)
      - Each focus's nearest neighbor and the gap to the next-nearest
        (small gap = the embedder can't reliably tell them apart)

Interpretation:
  - cos < 0.5 between two focuses → easily distinguishable
  - 0.5 < cos < 0.7 → close, but the right side of clear
  - cos > 0.7 → confusable; embedding-only filter will struggle to
    route messages to one without the other
"""

import json
import os
import sys
from pathlib import Path

import numpy as np
from sentence_transformers import SentenceTransformer

CONFIG_PATH = Path(__file__).parent / "workstreams.json"
EMBEDDERS = [
    ("all-MiniLM-L6-v2", "", ""),
    ("BAAI/bge-base-en-v1.5", "", ""),
    ("intfloat/e5-base-v2", "passage: ", "query: "),
]


def cos_matrix(M):
    """Pairwise cosine similarity matrix for rows of M."""
    norms = np.linalg.norm(M, axis=1, keepdims=True)
    return (M @ M.T) / (norms @ norms.T + 1e-12)


def analyze_workspace(workspace_name, ws_config, model, model_name, focus_prefix):
    workstreams = ws_config["workstreams"]
    names = [w["name"] for w in workstreams]
    texts = [focus_prefix + w["focus"] for w in workstreams]
    embs = model.encode(texts, show_progress_bar=False, batch_size=16,
                         normalize_embeddings=True)
    embs = np.array(embs)

    M = cos_matrix(embs)
    n = len(workstreams)

    # Off-diagonal cosines
    off = M[~np.eye(n, dtype=bool)]
    off_sorted = np.sort(off)
    summary = {
        "n_workstreams": n,
        "off_min": float(off_sorted[0]),
        "off_p25": float(off_sorted[len(off_sorted)//4]),
        "off_median": float(off_sorted[len(off_sorted)//2]),
        "off_p75": float(off_sorted[3*len(off_sorted)//4]),
        "off_max": float(off_sorted[-1]),
        "off_mean": float(off.mean()),
    }

    # Top-K most similar pairs (off-diagonal)
    pairs = []
    for i in range(n):
        for j in range(i+1, n):
            pairs.append((float(M[i, j]), names[i], names[j]))
    pairs.sort(reverse=True)

    # Per-focus nearest-neighbor gap
    nn_info = []
    for i in range(n):
        row = M[i].copy()
        row[i] = -1  # exclude self
        order = np.argsort(-row)
        nn_idx = order[0]
        next_idx = order[1]
        gap = float(row[nn_idx] - row[next_idx])
        nn_info.append({
            "focus": names[i],
            "nn": names[nn_idx],
            "nn_cos": float(row[nn_idx]),
            "gap_to_2nd": gap,
        })
    return summary, pairs, nn_info


def main():
    with open(CONFIG_PATH) as fh:
        config = json.load(fh)

    workspaces = sorted(config.keys())

    for model_name, focus_prefix, _ in EMBEDDERS:
        print(f"\n{'#'*100}")
        print(f"# EMBEDDER: {model_name}")
        print(f"{'#'*100}", file=sys.stderr)
        model = SentenceTransformer(model_name)

        for ws_name in workspaces:
            ws_config = config[ws_name]
            summary, pairs, nn_info = analyze_workspace(
                ws_name, ws_config, model, model_name, focus_prefix)

            print(f"\n--- {ws_name}  (n={summary['n_workstreams']} workstreams) ---")
            print(f"  off-diagonal cosine: "
                  f"min={summary['off_min']:.3f}  "
                  f"p25={summary['off_p25']:.3f}  "
                  f"med={summary['off_median']:.3f}  "
                  f"p75={summary['off_p75']:.3f}  "
                  f"max={summary['off_max']:.3f}  "
                  f"mean={summary['off_mean']:.3f}")

            print(f"  top-5 most similar focus pairs:")
            for sim, a, b in pairs[:5]:
                print(f"    {sim:.3f}  {a[:38]:<38s}  ↔  {b}")

            # Per-focus: who's their nearest neighbor and what's the
            # gap to the next-nearest? Small gap = ambiguous.
            print(f"  per-focus nearest-neighbor gap (smallest first — most confusable):")
            nn_info.sort(key=lambda x: x["gap_to_2nd"])
            for entry in nn_info[:6]:
                print(f"    gap={entry['gap_to_2nd']:.3f}  "
                      f"nn={entry['nn_cos']:.3f}  "
                      f"{entry['focus'][:36]:<36s}  →  {entry['nn']}")


if __name__ == "__main__":
    main()
