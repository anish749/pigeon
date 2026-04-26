# /// script
# requires-python = ">=3.10"
# dependencies = ["sentence-transformers>=3.0", "numpy"]
# ///
"""
Compare workstream focus geometry across discover models (haiku, sonnet,
opus) for the same workspaces.

For each (workspace, model), embeds the discovered focuses, computes
pairwise cosines, and reports:
  - workstream count
  - off-diagonal cosine summary (min/median/max)
  - smallest nearest-neighbour gap (the most-confusable pair)

Reads parsed workstream configs from /tmp/ws-<workspace>-<model>.txt
files via the same parser as parse_discover.py.

The premise being tested: does using a stronger model in
`pigeon workstream discover` produce focus prose that is more
separable in embedding space, lifting the routing recall ceiling?
"""

import json
import re
import sys
from pathlib import Path

import numpy as np
from sentence_transformers import SentenceTransformer


WORKSPACES = ["trudy", "igfd", "tubular"]
MODELS = ["haiku", "sonnet", "opus"]
EMBEDDER_NAME = "all-MiniLM-L6-v2"


def parse_discover_text(text):
    match = re.search(r'msg="claude cli response" raw="(.+)"\s*$',
                      text, re.MULTILINE)
    if not match:
        return []
    raw = match.group(1).encode().decode("unicode_escape")
    try:
        env = json.loads(raw)
    except json.JSONDecodeError:
        return []
    result = env.get("result", "").strip()
    result = re.sub(r"^```(?:json)?\s*", "", result)
    result = re.sub(r"\s*```\s*$", "", result)
    try:
        obj = json.loads(result)
    except json.JSONDecodeError:
        return []
    return [{"name": w.get("name", ""), "focus": w.get("focus", "")}
            for w in obj.get("workstreams", [])]


def load_haiku_from_txt():
    """Haiku results from original /tmp/ws-<workspace>.txt files."""
    out = {}
    for ws in WORKSPACES:
        path = Path(f"/tmp/ws-{ws}.txt")
        if not path.exists():
            continue
        out[ws] = parse_discover_text(path.read_text())
    return out


def cos_matrix(M):
    norms = np.linalg.norm(M, axis=1, keepdims=True)
    return (M @ M.T) / (norms @ norms.T + 1e-12)


def analyse(focuses, model):
    if not focuses:
        return None
    n = len(focuses)
    if n < 2:
        return {"n": n, "off_min": float("nan"), "off_med": float("nan"),
                "off_max": float("nan"), "smallest_nn_gap": float("nan"),
                "n_pairs_above_07": 0}
    embs = np.array(model.encode([f["focus"] for f in focuses],
                                  show_progress_bar=False, batch_size=16,
                                  normalize_embeddings=True))
    M = cos_matrix(embs)
    off = M[~np.eye(n, dtype=bool)]
    nn_gaps = []
    for i in range(n):
        row = M[i].copy()
        row[i] = -1
        order = np.argsort(-row)
        nn_gaps.append(float(row[order[0]] - row[order[1]] if len(order) > 1 else 0))
    n_pairs_above_07 = sum(1 for v in off if v > 0.7) // 2
    return {
        "n": n,
        "off_min": float(off.min()),
        "off_med": float(np.median(off)),
        "off_max": float(off.max()),
        "smallest_nn_gap": float(min(nn_gaps)),
        "n_pairs_above_07": n_pairs_above_07,
    }


def main():
    print(f"Loading {EMBEDDER_NAME}...", file=sys.stderr)
    model = SentenceTransformer(EMBEDDER_NAME)

    haiku_data = load_haiku_from_txt()

    by_model = {"haiku": haiku_data, "sonnet": {}, "opus": {}}

    for ws in WORKSPACES:
        for m in ("sonnet", "opus"):
            path = Path(f"/tmp/ws-{ws}-{m}.txt")
            if not path.exists():
                print(f"  missing: {path}", file=sys.stderr)
                continue
            by_model[m][ws] = parse_discover_text(path.read_text())

    print("\n" + "=" * 95)
    print("FOCUS GEOMETRY BY DISCOVER MODEL")
    print("=" * 95)
    print(f"\n  {'workspace':<10s} {'model':<8s} {'n':>3s} "
          f"{'off_min':>8s} {'off_med':>8s} {'off_max':>8s} "
          f"{'min_nn_gap':>11s} {'#pairs>0.7':>11s}")
    for ws in WORKSPACES:
        for m in MODELS:
            data = by_model[m].get(ws)
            if data is None:
                print(f"  {ws:<10s} {m:<8s} (no data)")
                continue
            r = analyse(data, model)
            if r is None:
                print(f"  {ws:<10s} {m:<8s} (no workstreams)")
                continue
            print(f"  {ws:<10s} {m:<8s} {r['n']:>3d} "
                  f"{r['off_min']:>8.3f} {r['off_med']:>8.3f} {r['off_max']:>8.3f} "
                  f"{r['smallest_nn_gap']:>11.3f} {r['n_pairs_above_07']:>11d}")
        print()

    # Save haiku/sonnet/opus configs for downstream use
    out_dir = Path(__file__).parent
    for m in ("haiku", "sonnet", "opus"):
        config_out = {}
        for ws in WORKSPACES:
            data = by_model[m].get(ws, [])
            if not data:
                continue
            config_out[ws] = {
                "slack_dir": {"trudy": "Trudy", "igfd": "ingredifind",
                              "tubular": "tubular"}[ws],
                "merges": {},
                "workstreams": [
                    {
                        "id": f"ws-{re.sub(r'[^a-z0-9]+', '-', w['name'].lower()).strip('-')[:36]}",
                        "name": w["name"],
                        "focus": w["focus"],
                    }
                    for w in data
                ],
            }
        out_path = out_dir / f"workstreams-{m}.json"
        with open(out_path, "w") as fh:
            json.dump(config_out, fh, indent=2)
        print(f"  wrote {out_path}", file=sys.stderr)


if __name__ == "__main__":
    main()
