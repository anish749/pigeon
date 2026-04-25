# /// script
# requires-python = ">=3.10"
# dependencies = ["sentence-transformers>=3.0", "numpy"]
# ///
"""
Re-evaluate routing variants against the three metrics that actually
matter for fan-in reduction (not F1):

  recall_relevant       — of (msg, true_workstream) pairs, fraction where
                          the filter routed to that workstream.
                          What % of relevant work reaches the right session.

  wake_rate_noise       — of messages the judge labeled as none-of-the-
                          workstreams, fraction where the filter routed to
                          AT LEAST ONE workstream. Bad — wakes a session
                          for nothing.

  avg_woken_overall     — mean # of workstreams routed-to per message
                          (lower = better fan-in reduction)
  avg_woken_relevant    — same, restricted to messages judge labeled as
                          having ≥1 true workstream
  avg_woken_noise       — same, restricted to messages judge labeled as none

The product question: at what threshold does a given variant land at
acceptable wake-rate-on-noise (say ≤0.30) and what recall does it
achieve there? How many sessions wake per message at that point?

The intent of routing is to AVOID running every workstream's agent on
every message. If a router averages 1.5 sessions woken per message in
a workspace with N workstreams, fan-in has been collapsed by N/1.5.
"""

import argparse
import glob
import json
import os
import sys
from collections import Counter, defaultdict
from dataclasses import dataclass
from pathlib import Path

import numpy as np
from sentence_transformers import SentenceTransformer

MODEL_NAME = "all-MiniLM-L6-v2"
PIGEON_DATA = Path(os.path.expanduser("~/.local/share/pigeon"))
CONFIG_PATH = Path(__file__).parent / "workstreams.json"


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
                    sigs.append({"ts": obj.get("ts", ""), "sender": obj.get("sender", ""),
                                 "text": text, "conversation": conv})
        except OSError:
            continue
    sigs.sort(key=lambda s: s["ts"])
    return sigs


# ---------------------------------------------------------------------------
# Variants — simpler now, no s_pos > s_neg gating
# ---------------------------------------------------------------------------

def filter_focus_only(v_msg, focus_embs, threshold):
    """Pure threshold on focus cosine. Multi-route. The fan-in-reduction
    primitive."""
    return {wid for wid, e in focus_embs.items() if cos(v_msg, e) >= threshold}


def filter_focus_with_neg(v_msg, focus_embs, threshold):
    """Old algorithm: route only if cos to this focus is highest among all
    foci AND >= threshold. Single-best-route."""
    routed = set()
    for wid in focus_embs:
        s_pos = cos(v_msg, focus_embs[wid])
        if s_pos < threshold:
            continue
        s_neg = max(cos(v_msg, focus_embs[other]) for other in focus_embs if other != wid)
        if s_pos > s_neg:
            routed.add(wid)
    return routed


def filter_top_k(v_msg, focus_embs, k):
    """Always route to top-K closest workstreams (no threshold). Captures
    'always wake exactly K agents'."""
    sims = sorted(((cos(v_msg, e), wid) for wid, e in focus_embs.items()), reverse=True)
    return {wid for _, wid in sims[:k]}


# ---------------------------------------------------------------------------
# Metric computation
# ---------------------------------------------------------------------------

@dataclass
class Metrics:
    n_msgs: int
    n_relevant_msgs: int       # judge said ≥1 ws
    n_noise_msgs: int          # judge said none
    n_relevant_pairs: int      # total true (msg, ws) pairs
    pairs_recalled: int        # of those, how many the filter routed
    msgs_routed_anything: int  # routed to ≥1 ws
    noise_routed_anything: int # of noise msgs, routed to ≥1
    sum_woken_overall: int
    sum_woken_relevant: int
    sum_woken_noise: int

    @property
    def recall(self):
        return self.pairs_recalled / max(self.n_relevant_pairs, 1)

    @property
    def wake_rate_noise(self):
        return self.noise_routed_anything / max(self.n_noise_msgs, 1)

    @property
    def avg_woken_overall(self):
        return self.sum_woken_overall / max(self.n_msgs, 1)

    @property
    def avg_woken_relevant(self):
        return self.sum_woken_relevant / max(self.n_relevant_msgs, 1)

    @property
    def avg_woken_noise(self):
        return self.sum_woken_noise / max(self.n_noise_msgs, 1)


def evaluate(variant_fn, focus_embs, labels, signals_by_key, sig_embs):
    m = Metrics(0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
    for entry in labels.values():
        if entry["labels"] is None:
            continue
        key = (entry["ts"], entry["conversation"], entry["sender"], entry["text"])
        idx = signals_by_key.get(key)
        if idx is None:
            continue
        m.n_msgs += 1
        true_set = set(entry["labels"])
        routed = variant_fn(sig_embs[idx], focus_embs)
        m.sum_woken_overall += len(routed)
        if true_set:
            m.n_relevant_msgs += 1
            m.n_relevant_pairs += len(true_set)
            m.pairs_recalled += len(true_set & routed)
            m.sum_woken_relevant += len(routed)
        else:
            m.n_noise_msgs += 1
            m.sum_woken_noise += len(routed)
            if routed:
                m.noise_routed_anything += 1
        if routed:
            m.msgs_routed_anything += 1
    return m


# ---------------------------------------------------------------------------
# Main — sweep thresholds, report curves per workspace
# ---------------------------------------------------------------------------

def apply_merges(labels, merges):
    if not merges:
        return labels
    for entry in labels.values():
        if entry["labels"] is None:
            continue
        seen = []
        for old in entry["labels"]:
            new = merges.get(old, old)
            if new not in seen:
                seen.append(new)
        entry["labels"] = seen
    return labels


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

    THRESHOLDS = [0.10, 0.12, 0.14, 0.16, 0.18, 0.20, 0.22, 0.25, 0.27, 0.30, 0.35]

    for ws_name in workspaces:
        ws_config = config[ws_name]
        label_path = Path(__file__).parent / f"labels-{ws_name}.json"
        if not label_path.exists():
            continue
        with open(label_path) as fh:
            labels = json.load(fh)
        labels = apply_merges(labels, ws_config.get("merges", {}))

        print(f"\n{'='*100}")
        n_total_workstreams = len(ws_config["workstreams"])
        print(f"WORKSPACE: {ws_name}   ({n_total_workstreams} workstreams)")
        print(f"{'='*100}")

        # Embed everything
        focus_embs = {ws["id"]: model.encode(ws["focus"], show_progress_bar=False)
                      for ws in ws_config["workstreams"]}
        signals = load_signals(ws_config["slack_dir"])
        sig_embs = np.array(model.encode([s["text"] for s in signals],
                                          show_progress_bar=False, batch_size=64))
        signals_by_key = {(s["ts"], s["conversation"], s["sender"], s["text"]): i
                          for i, s in enumerate(signals)}

        # Distribution of true labels in this workspace
        n_eval = sum(1 for e in labels.values() if e["labels"] is not None)
        n_relevant = sum(1 for e in labels.values()
                         if e["labels"] is not None and e["labels"])
        n_noise = n_eval - n_relevant
        n_pairs = sum(len(e["labels"]) for e in labels.values()
                      if e["labels"] is not None and e["labels"])
        avg_true_per_relevant = n_pairs / max(n_relevant, 1)
        print(f"\n  labeled signals: {n_eval}   relevant: {n_relevant}   noise: {n_noise}")
        print(f"  total (msg,ws) pairs to recall: {n_pairs}   avg ws/relevant-msg: {avg_true_per_relevant:.2f}")

        # focus-only sweep — the candidate algorithm for fan-in reduction
        print(f"\n  ALGORITHM: focus-only multi-route  (cos(msg, focus) >= τ)")
        print(f"  {'τ':>5s} {'recall':>7s} {'wake/noise':>11s} {'woken/msg':>10s} {'woken/rel':>10s} {'woken/noise':>11s} {'fan-in×':>8s}")
        for thr in THRESHOLDS:
            m = evaluate(lambda v, fe: filter_focus_only(v, fe, thr),
                         focus_embs, labels, signals_by_key, sig_embs)
            fan_in = n_total_workstreams / max(m.avg_woken_overall, 0.001)
            print(f"  {thr:>5.2f} {m.recall:>7.3f} {m.wake_rate_noise:>11.3f} "
                  f"{m.avg_woken_overall:>10.2f} {m.avg_woken_relevant:>10.2f} "
                  f"{m.avg_woken_noise:>11.2f} {fan_in:>7.1f}x")

        # focus + neg (single-best-route) — the old "F1-optimised" algorithm
        print(f"\n  COMPARISON: focus+neg single-best  (s_pos>=τ AND s_pos > max_other s)")
        print(f"  {'τ':>5s} {'recall':>7s} {'wake/noise':>11s} {'woken/msg':>10s} {'woken/rel':>10s} {'woken/noise':>11s} {'fan-in×':>8s}")
        for thr in [0.20, 0.25, 0.27, 0.30]:
            m = evaluate(lambda v, fe: filter_focus_with_neg(v, fe, thr),
                         focus_embs, labels, signals_by_key, sig_embs)
            fan_in = n_total_workstreams / max(m.avg_woken_overall, 0.001)
            print(f"  {thr:>5.2f} {m.recall:>7.3f} {m.wake_rate_noise:>11.3f} "
                  f"{m.avg_woken_overall:>10.2f} {m.avg_woken_relevant:>10.2f} "
                  f"{m.avg_woken_noise:>11.2f} {fan_in:>7.1f}x")

        # top-K (always wake exactly K) — degenerate baseline
        print(f"\n  COMPARISON: top-K (always wake K nearest workstreams, no threshold)")
        print(f"  {'k':>5s} {'recall':>7s} {'wake/noise':>11s} {'woken/msg':>10s} {'woken/rel':>10s} {'woken/noise':>11s} {'fan-in×':>8s}")
        for k in [1, 2, 3]:
            m = evaluate(lambda v, fe: filter_top_k(v, fe, k),
                         focus_embs, labels, signals_by_key, sig_embs)
            fan_in = n_total_workstreams / max(m.avg_woken_overall, 0.001)
            print(f"  {k:>5d} {m.recall:>7.3f} {m.wake_rate_noise:>11.3f} "
                  f"{m.avg_woken_overall:>10.2f} {m.avg_woken_relevant:>10.2f} "
                  f"{m.avg_woken_noise:>11.2f} {fan_in:>7.1f}x")


if __name__ == "__main__":
    main()
