# /// script
# requires-python = ">=3.10"
# dependencies = [
#     "sentence-transformers>=3.0",
#     "numpy",
# ]
# ///
"""
Per-workspace, per-workstream binary evaluation. Runs the same set of
routing variants across multiple workspaces and reports macro-F1
aggregated within each workspace AND a global mean across workspaces.

Tests whether the recommendation (focus+neg(foci) at τ≈0.27) holds
across multiple workspaces — useful for confirming a per-workspace
result generalises rather than reflecting one workspace's structure.
"""

import argparse
import glob
import json
import os
import sys
from collections import Counter, defaultdict, deque
from dataclasses import dataclass
from pathlib import Path

import numpy as np
from sentence_transformers import SentenceTransformer

MODEL_NAME = "all-MiniLM-L6-v2"
WINDOW_SIZE = 5
PIGEON_DATA = Path(os.path.expanduser("~/.local/share/pigeon"))
CONFIG_PATH = Path(__file__).parent / "workstreams.json"


def cos(a, b):
    return float(np.dot(a, b) / (np.linalg.norm(a) * np.linalg.norm(b) + 1e-12))


def cos_to_set(v, mat):
    if mat is None or len(mat) == 0:
        return None
    sims = (mat @ v) / (np.linalg.norm(mat, axis=1) * np.linalg.norm(v) + 1e-12)
    return float(sims.max())


def load_signals(slack_dir, since_prefix="2026-04"):
    workspace_dir = PIGEON_DATA / "slack" / slack_dir
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


# ---------------------------------------------------------------------------
# Filter primitives
# ---------------------------------------------------------------------------

@dataclass
class FilterCfg:
    wsid: str
    v_focus: np.ndarray
    v_neg: np.ndarray = None
    threshold: float = 0.30
    margin: float = 0.0
    min_text_len: int = 0
    neg_aggregator: str = "max"  # "max" or "mean"

    def decide(self, v_msg, msg_text):
        if len(msg_text) < self.min_text_len:
            return False
        s_pos = cos(v_msg, self.v_focus)
        if s_pos < self.threshold:
            return False
        if self.v_neg is not None and len(self.v_neg) > 0:
            sims = (self.v_neg @ v_msg) / (np.linalg.norm(self.v_neg, axis=1) * np.linalg.norm(v_msg) + 1e-12)
            if self.neg_aggregator == "mean":
                s_neg = float(sims.mean())
            else:
                s_neg = float(sims.max())
            if not (s_pos > s_neg + self.margin):
                return False
        return True


def build_filters(workstreams, model, *, threshold, margin=0.0,
                  use_negatives=False, neg_source="foci",
                  positives_by_ws=None, min_text_len=0,
                  neg_top_k=None, neg_aggregator="max"):
    """When neg_top_k is set, keep only the K negatives closest in cosine
    distance to this workstream's focus (i.e., the most-similar other
    workstreams) — others are too far to discriminate."""
    filters = {}
    # Pre-embed all foci so we can pick top-K
    all_focus_embs = {ws["id"]: model.encode(ws["focus"], show_progress_bar=False)
                      for ws in workstreams}
    for ws in workstreams:
        v_focus = all_focus_embs[ws["id"]]
        v_neg = None
        if use_negatives:
            if neg_source == "foci":
                # candidates: list of (other_focus_text, sim_to_v_focus)
                candidates = []
                for other in workstreams:
                    if other["id"] == ws["id"]:
                        continue
                    sim = cos(v_focus, all_focus_embs[other["id"]])
                    candidates.append((other["focus"], sim))
                if neg_top_k is not None:
                    candidates.sort(key=lambda x: -x[1])
                    candidates = candidates[:neg_top_k]
                negs = [c[0] for c in candidates]
            else:  # "messages"
                negs = []
                for other in workstreams:
                    if other["id"] == ws["id"]:
                        continue
                    if positives_by_ws:
                        negs.extend(positives_by_ws.get(other["id"], [])[:2])
            if len(negs) >= 2:
                v_neg = np.array(model.encode(negs, show_progress_bar=False))
        filters[ws["id"]] = FilterCfg(
            wsid=ws["id"], v_focus=v_focus, v_neg=v_neg,
            threshold=threshold, margin=margin, min_text_len=min_text_len,
            neg_aggregator=neg_aggregator,
        )
    return filters


def bootstrap_positives(signals, sig_embs, ws_focus_embs, n=6, threshold=0.45, min_len=40):
    by_ws = defaultdict(list)
    for s, v in zip(signals, sig_embs):
        if len(s["text"]) < min_len:
            continue
        scores = {wid: cos(v, e) for wid, e in ws_focus_embs.items()}
        best = max(scores, key=scores.get)
        if scores[best] >= threshold:
            by_ws[best].append((scores[best], s["text"]))
    out = {}
    for wid, lst in by_ws.items():
        lst.sort(reverse=True)
        out[wid] = [t for _, t in lst[:n]]
    return out


# ---------------------------------------------------------------------------
# Variants
# ---------------------------------------------------------------------------

@dataclass
class Variant:
    name: str
    filters: dict


def build_variants(workstreams, model, positives_by_ws):
    variants = []

    # Focus only
    for thr in (0.20, 0.22, 0.25, 0.27, 0.30):
        f = build_filters(workstreams, model, threshold=thr)
        variants.append(Variant(f"focus-only@τ={thr:.2f}", f))

    # Focus + all-foci negatives (max aggregator) — what we tried before
    for thr in (0.25, 0.27, 0.30):
        f = build_filters(workstreams, model, threshold=thr, margin=0.0,
                          use_negatives=True, neg_source="foci")
        variants.append(Variant(f"focus+neg(all-foci,max)@τ={thr:.2f}", f))

    # Focus + top-K closest other foci as negatives
    for k in (1, 2, 3, 4):
        for thr in (0.25, 0.27, 0.30):
            f = build_filters(workstreams, model, threshold=thr, margin=0.0,
                              use_negatives=True, neg_source="foci",
                              neg_top_k=k)
            variants.append(Variant(f"focus+neg(top-{k}-foci,max)@τ={thr:.2f}", f))

    # Focus + foci negatives with MEAN aggregator (smooths over many)
    for thr in (0.25, 0.27, 0.30):
        f = build_filters(workstreams, model, threshold=thr, margin=0.0,
                          use_negatives=True, neg_source="foci",
                          neg_aggregator="mean")
        variants.append(Variant(f"focus+neg(all-foci,mean)@τ={thr:.2f}", f))

    return variants


# ---------------------------------------------------------------------------
# Evaluation
# ---------------------------------------------------------------------------

def f_score(tp, fp, fn):
    p = tp / (tp + fp) if (tp + fp) else 0.0
    r = tp / (tp + fn) if (tp + fn) else 0.0
    f1 = 2 * p * r / (p + r) if (p + r) else 0.0
    return p, r, f1


def evaluate_workspace(workspace, ws_config, variants_for_workspace, model, labels,
                       min_positives_for_macro=3):
    """Evaluate every variant on this workspace's labels. Reports both
    macro-F1 (only over workstreams with >= min_positives_for_macro true
    positives) and micro-F1 (pooled TP/FP/FN across all workstreams)."""
    workstreams = ws_config["workstreams"]
    signals = load_signals(ws_config["slack_dir"])
    plain_texts = [s["text"] for s in signals]
    sig_embs = np.array(model.encode(plain_texts, show_progress_bar=False, batch_size=64))
    signals_by_key = {(s["ts"], s["conversation"], s["sender"], s["text"]): i
                      for i, s in enumerate(signals)}

    out = {}
    for variant in variants_for_workspace:
        per_ws_tp = Counter(); per_ws_fp = Counter(); per_ws_fn = Counter()
        total_tp = total_fp = total_fn = 0
        for entry in labels.values():
            if entry["labels"] is None:
                continue
            key = (entry["ts"], entry["conversation"], entry["sender"], entry["text"])
            if key not in signals_by_key:
                continue
            idx = signals_by_key[key]
            v_msg = sig_embs[idx]
            true_set = set(entry["labels"])
            for ws in workstreams:
                wid = ws["id"]
                f = variant.filters[wid]
                routed = f.decide(v_msg, entry["text"])
                in_true = wid in true_set
                if routed and in_true:
                    per_ws_tp[wid] += 1; total_tp += 1
                elif routed:
                    per_ws_fp[wid] += 1; total_fp += 1
                elif in_true:
                    per_ws_fn[wid] += 1; total_fn += 1
        # macro: only include workstreams with enough positives
        macro_p = macro_r = macro_f1 = 0
        n_present = 0
        for ws in workstreams:
            wid = ws["id"]
            t, f, n_ = per_ws_tp.get(wid, 0), per_ws_fp.get(wid, 0), per_ws_fn.get(wid, 0)
            n_pos = t + n_
            if n_pos < min_positives_for_macro:
                continue
            p, r, f1 = f_score(t, f, n_)
            macro_p += p; macro_r += r; macro_f1 += f1
            n_present += 1
        n_present = max(n_present, 1)
        # micro: pooled
        mp, mr, mf1 = f_score(total_tp, total_fp, total_fn)
        out[variant.name] = {
            "macro_p": macro_p / n_present,
            "macro_r": macro_r / n_present,
            "macro_f1": macro_f1 / n_present,
            "micro_p": mp,
            "micro_r": mr,
            "micro_f1": mf1,
            "n_eligible_ws": n_present,
        }
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--workspaces", nargs="+", default=None,
                    help="workspace names (keys in workstreams.json); default: all configured")
    args = ap.parse_args()

    with open(CONFIG_PATH) as fh:
        config = json.load(fh)

    workspaces = args.workspaces or sorted(config.keys())

    print(f"Loading model {MODEL_NAME}...", file=sys.stderr)
    model = SentenceTransformer(MODEL_NAME)

    by_workspace = {}
    for ws_name in workspaces:
        ws_config = config[ws_name]
        label_paths = [Path(__file__).parent / f"labels-{ws_name}.json"]
        labels = None
        used_path = None
        for p in label_paths:
            if p and p.exists():
                with open(p) as fh:
                    labels = json.load(fh)
                used_path = p
                break
        if labels is None:
            print(f"\n--- {ws_name}: no labels found, skipping", file=sys.stderr)
            continue

        # Apply workstream-merge remap to labels (in memory; original
        # files unchanged). Drop dups after remap.
        merges = ws_config.get("merges", {})
        if merges:
            for entry in labels.values():
                if entry["labels"] is None:
                    continue
                remapped = []
                seen = set()
                for old_id in entry["labels"]:
                    new_id = merges.get(old_id, old_id)
                    if new_id not in seen:
                        seen.add(new_id)
                        remapped.append(new_id)
                entry["labels"] = remapped
            print(f"  applied {len(merges)} merges to labels", file=sys.stderr)

        n_eval = sum(1 for e in labels.values() if e["labels"] is not None)
        print(f"\n--- {ws_name}: {n_eval} labeled signals from {used_path.name}",
              file=sys.stderr)

        # Bootstrap positives for the messages-based negative variant
        signals = load_signals(ws_config["slack_dir"])
        router_texts = [f"{s['sender']}: {s['text']}" for s in signals]
        sig_embs_router = np.array(model.encode(router_texts, show_progress_bar=False, batch_size=64))
        ws_focus_embs = {ws["id"]: model.encode(ws["focus"], show_progress_bar=False)
                         for ws in ws_config["workstreams"]}
        positives = bootstrap_positives(signals, sig_embs_router, ws_focus_embs)

        variants = build_variants(ws_config["workstreams"], model, positives)

        # Run
        workspace_results = evaluate_workspace(ws_name, ws_config, variants, model, labels)
        by_workspace[ws_name] = workspace_results

    def report_metric(metric_key, label):
        print("\n" + "=" * 100)
        print(f"PER-WORKSPACE {label}")
        print("=" * 100)
        variant_names = sorted({n for d in by_workspace.values() for n in d.keys()})
        headers = ["variant"] + list(by_workspace.keys()) + ["mean"]
        print(f"  {headers[0]:<44s}" + "".join(f"  {h:>10s}" for h in headers[1:]))
        rows = []
        for v in variant_names:
            row_f1s = []
            for ws in by_workspace.keys():
                metrics = by_workspace[ws].get(v)
                row_f1s.append(metrics[metric_key] if metrics else None)
            mean_f1 = (sum(x for x in row_f1s if x is not None) /
                       max(sum(1 for x in row_f1s if x is not None), 1))
            rows.append((v, row_f1s, mean_f1))
        rows.sort(key=lambda r: -r[2])
        for v, f1s, mean in rows:
            cells = [f"{x:.3f}" if x is not None else "  -  " for x in f1s]
            print(f"  {v:<44s}" + "".join(f"  {c:>10s}" for c in cells) + f"  {mean:.3f}")

    report_metric("macro_f1", "MACRO-F1 (workstreams with ≥3 positives)")
    report_metric("micro_f1", "MICRO-F1 (pooled TP/FP/FN)")

    # Per-workspace winners (by micro-F1, more stable)
    print("\n" + "=" * 100)
    print("PER-WORKSPACE TOP 5  (by micro-F1)")
    print("=" * 100)
    for ws_name, results in by_workspace.items():
        print(f"\n  {ws_name}  (eligible workstreams = {next(iter(results.values()))['n_eligible_ws']}):")
        ranked = sorted(results.items(), key=lambda x: -x[1]["micro_f1"])
        for name, m in ranked[:5]:
            print(f"    micro-F1={m['micro_f1']:.3f}  P={m['micro_p']:.3f}  R={m['micro_r']:.3f}  "
                  f"macro-F1={m['macro_f1']:.3f}  {name}")


if __name__ == "__main__":
    main()
