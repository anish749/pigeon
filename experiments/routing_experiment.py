# /// script
# requires-python = ">=3.10"
# dependencies = [
#     "sentence-transformers>=3.0",
#     "scikit-learn>=1.3",
#     "numpy>=1.24",
#     "hdbscan>=0.8",
# ]
# ///
"""
Workstream routing experiments.

Reads pigeon signals, embeds them, and tries multiple clustering/routing
strategies to find the best approach for workstream discovery.

Usage: uv run experiments/routing_experiment.py --workspace <name> --since 2026-01-01 --until 2026-04-01
"""

import argparse
import json
import os
import sys
from collections import defaultdict
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional

import hdbscan
import numpy as np
from sentence_transformers import SentenceTransformer
from sklearn.cluster import AgglomerativeClustering, DBSCAN
from sklearn.metrics import silhouette_score


@dataclass
class Signal:
    id: str
    ts: datetime
    sender: str
    text: str
    conversation: str
    account: str
    platform: str
    signal_type: str


def load_signals(data_dir: str, workspace: str, since: str, until: str) -> list[Signal]:
    """Load all signals from pigeon data store for a workspace."""
    signals = []
    base = Path(data_dir)

    since_dt = datetime.fromisoformat(since + "T00:00:00+00:00")
    until_dt = datetime.fromisoformat(until + "T23:59:59+00:00")

    # Scan all platforms
    for platform_dir in base.iterdir():
        if not platform_dir.is_dir() or platform_dir.name.startswith("."):
            continue
        platform = platform_dir.name

        for account_dir in platform_dir.iterdir():
            if not account_dir.is_dir() or account_dir.name.startswith("."):
                continue

            # Match workspace by slug
            acct_slug = account_dir.name.lower()
            ws_lower = workspace.lower()
            # Check if this account belongs to the workspace
            if platform in ("slack", "whatsapp"):
                if acct_slug != ws_lower and not acct_slug.startswith(ws_lower):
                    continue
            elif platform == "gws":
                # GWS accounts are email-based, match by workspace name in email
                if ws_lower not in acct_slug:
                    continue
            elif platform == "linear":
                if acct_slug != ws_lower:
                    continue
            else:
                continue

            if platform in ("slack", "whatsapp"):
                _load_conversations(signals, account_dir, platform, since_dt, until_dt)
            elif platform == "gws":
                _load_gmail(signals, account_dir, since_dt, until_dt)

    signals.sort(key=lambda s: s.ts)
    return signals


def _load_conversations(signals, account_dir, platform, since_dt, until_dt):
    for conv_dir in account_dir.iterdir():
        if not conv_dir.is_dir() or conv_dir.name in ("identity", ".meta.json"):
            continue
        if conv_dir.name.startswith("."):
            continue

        for f in sorted(conv_dir.glob("*.jsonl")):
            if "threads" in str(f):
                continue
            date_str = f.stem
            try:
                file_date = datetime.fromisoformat(date_str + "T00:00:00+00:00")
            except ValueError:
                continue
            if file_date.date() < since_dt.date() or file_date.date() > until_dt.date():
                continue

            for line in f.read_text().strip().split("\n"):
                if not line:
                    continue
                try:
                    m = json.loads(line)
                except json.JSONDecodeError:
                    continue
                if m.get("type") != "msg":
                    continue
                ts_str = m.get("ts", "")
                try:
                    ts = datetime.fromisoformat(ts_str)
                except ValueError:
                    continue
                if ts < since_dt or ts > until_dt:
                    continue
                text = m.get("text", "")
                if not text.strip():
                    continue
                signals.append(Signal(
                    id=m.get("id", ""),
                    ts=ts,
                    sender=m.get("sender", ""),
                    text=text,
                    conversation=conv_dir.name,
                    account=account_dir.name,
                    platform=platform,
                    signal_type=f"{platform}-message",
                ))


def _load_gmail(signals, account_dir, since_dt, until_dt):
    gmail_dir = account_dir / "gmail"
    if not gmail_dir.exists():
        return
    for f in sorted(gmail_dir.glob("*.jsonl")):
        date_str = f.stem
        try:
            file_date = datetime.fromisoformat(date_str + "T00:00:00+00:00")
        except ValueError:
            continue
        if file_date.date() < since_dt.date() or file_date.date() > until_dt.date():
            continue
        for line in f.read_text(errors="replace").strip().split("\n"):
            if not line:
                continue
            try:
                m = json.loads(line)
            except json.JSONDecodeError:
                continue
            if m.get("type") != "email":
                continue
            ts_str = m.get("ts", "")
            try:
                ts = datetime.fromisoformat(ts_str)
            except ValueError:
                continue
            if ts < since_dt or ts > until_dt:
                continue
            labels = m.get("labels", [])
            if "SPAM" in labels or "CATEGORY_PROMOTIONS" in labels:
                continue
            text = (m.get("subject", "") + " " + m.get("snippet", "")).strip()
            if not text:
                continue
            signals.append(Signal(
                id=m.get("id", ""),
                ts=ts,
                sender=m.get("fromName", m.get("from", "")),
                text=text,
                conversation=m.get("threadId", ""),
                account=account_dir.name,
                platform="gws",
                signal_type="email",
            ))


def embed_signals(model: SentenceTransformer, signals: list[Signal]) -> np.ndarray:
    """Embed all signals using sentence-transformers."""
    texts = []
    for s in signals:
        # Include conversation context in embedding
        text = f"{s.sender}: {s.text}"
        texts.append(text)
    print(f"Embedding {len(texts)} signals...", flush=True)
    embeddings = model.encode(texts, show_progress_bar=True, batch_size=64)
    return embeddings


def build_conversation_embeddings(
    signals: list[Signal], embeddings: np.ndarray, window_size: int = 10
) -> dict[str, list[tuple[np.ndarray, list[int]]]]:
    """Group signals by conversation and create window embeddings."""
    conv_signals = defaultdict(list)
    for i, s in enumerate(signals):
        conv_signals[s.conversation].append(i)

    conv_windows = {}
    for conv, indices in conv_signals.items():
        windows = []
        for start in range(0, len(indices), window_size):
            window_indices = indices[start : start + window_size]
            if len(window_indices) < 3:  # skip tiny windows
                continue
            window_emb = np.mean(embeddings[window_indices], axis=0)
            windows.append((window_emb, window_indices))
        conv_windows[conv] = windows
    return conv_windows


# ─── Experiment 1: HDBSCAN on all signal embeddings ──────────────────

def experiment_hdbscan(signals, embeddings, min_cluster_size=8):
    """Cluster all signals directly with HDBSCAN."""
    # Normalize embeddings for cosine distance via euclidean on unit vectors
    from sklearn.preprocessing import normalize
    normed = normalize(embeddings)
    clusterer = hdbscan.HDBSCAN(
        min_cluster_size=min_cluster_size,
        min_samples=3,
        metric="euclidean",
        cluster_selection_method="eom",
    )
    labels = clusterer.fit_predict(normed)
    n_clusters = len(set(labels)) - (1 if -1 in labels else 0)
    noise = (labels == -1).sum()

    clusters = defaultdict(list)
    for i, label in enumerate(labels):
        clusters[label].append(i)

    return {
        "name": f"HDBSCAN (min_cluster_size={min_cluster_size})",
        "n_clusters": n_clusters,
        "noise_signals": noise,
        "noise_pct": noise / len(signals) * 100,
        "clusters": clusters,
        "labels": labels,
    }


# ─── Experiment 2: HDBSCAN on conversation-window embeddings ─────────

def experiment_hdbscan_windows(signals, embeddings, window_size=10, min_cluster_size=5):
    """Cluster conversation windows (averaged embeddings) with HDBSCAN."""
    conv_windows = build_conversation_embeddings(signals, embeddings, window_size)

    all_window_embs = []
    all_window_meta = []  # (conversation, signal_indices)
    for conv, windows in conv_windows.items():
        for emb, indices in windows:
            all_window_embs.append(emb)
            all_window_meta.append((conv, indices))

    if len(all_window_embs) < 5:
        return {"name": "HDBSCAN-windows", "error": "too few windows"}

    X = np.array(all_window_embs)
    from sklearn.preprocessing import normalize
    X_normed = normalize(X)
    clusterer = hdbscan.HDBSCAN(
        min_cluster_size=min_cluster_size,
        min_samples=2,
        metric="euclidean",
        cluster_selection_method="eom",
    )
    labels = clusterer.fit_predict(X_normed)
    n_clusters = len(set(labels)) - (1 if -1 in labels else 0)

    # Map back to signals
    signal_labels = np.full(len(signals), -1)
    clusters = defaultdict(list)
    for i, label in enumerate(labels):
        conv, indices = all_window_meta[i]
        for idx in indices:
            signal_labels[idx] = label
        clusters[label].extend(indices)

    return {
        "name": f"HDBSCAN-windows (window={window_size}, min_cluster={min_cluster_size})",
        "n_clusters": n_clusters,
        "n_windows": len(all_window_embs),
        "noise_windows": (labels == -1).sum(),
        "clusters": clusters,
        "labels": signal_labels,
    }


# ─── Experiment 3: Agglomerative clustering with distance threshold ──

def experiment_agglomerative(signals, embeddings, distance_threshold=0.5, min_cluster_size=5):
    """Agglomerative clustering with cosine distance threshold."""
    from sklearn.metrics.pairwise import cosine_distances
    dist_matrix = cosine_distances(embeddings)

    clusterer = AgglomerativeClustering(
        n_clusters=None,
        distance_threshold=distance_threshold,
        metric="precomputed",
        linkage="average",
    )
    labels = clusterer.fit_predict(dist_matrix)

    # Filter small clusters to noise
    cluster_counts = defaultdict(int)
    for l in labels:
        cluster_counts[l] += 1

    filtered_labels = np.array([
        l if cluster_counts[l] >= min_cluster_size else -1
        for l in labels
    ])

    n_clusters = len(set(filtered_labels)) - (1 if -1 in filtered_labels else 0)
    noise = (filtered_labels == -1).sum()

    clusters = defaultdict(list)
    for i, label in enumerate(filtered_labels):
        clusters[label].append(i)

    return {
        "name": f"Agglomerative (threshold={distance_threshold}, min_size={min_cluster_size})",
        "n_clusters": n_clusters,
        "noise_signals": noise,
        "noise_pct": noise / len(signals) * 100,
        "clusters": clusters,
        "labels": filtered_labels,
    }


# ─── Experiment 4: Conversation-first clustering ─────────────────────

def experiment_conversation_first(signals, embeddings, sim_threshold=0.65):
    """
    Treat each conversation as a unit. Compute centroid embedding per
    conversation, then cluster conversations by centroid similarity.
    This respects the insight that conversations are natural boundaries.
    """
    conv_indices = defaultdict(list)
    for i, s in enumerate(signals):
        conv_indices[s.conversation].append(i)

    # Compute centroid for each conversation
    conv_names = []
    conv_centroids = []
    conv_idx_map = {}
    for conv, indices in conv_indices.items():
        if len(indices) < 3:  # skip tiny conversations
            continue
        centroid = np.mean(embeddings[indices], axis=0)
        conv_idx_map[conv] = indices
        conv_names.append(conv)
        conv_centroids.append(centroid)

    if len(conv_centroids) < 3:
        return {"name": "Conversation-first", "error": "too few conversations"}

    X = np.array(conv_centroids)

    # Cluster conversations by centroid similarity
    from sklearn.metrics.pairwise import cosine_distances
    dist = cosine_distances(X)

    clusterer = AgglomerativeClustering(
        n_clusters=None,
        distance_threshold=1 - sim_threshold,
        metric="precomputed",
        linkage="average",
    )
    conv_labels = clusterer.fit_predict(dist)

    # Map conversation clusters back to signals
    signal_labels = np.full(len(signals), -1)
    clusters = defaultdict(list)
    conv_cluster_map = {}
    for i, label in enumerate(conv_labels):
        conv = conv_names[i]
        conv_cluster_map[conv] = label
        for idx in conv_idx_map[conv]:
            signal_labels[idx] = label
            clusters[label].append(idx)

    n_clusters = len(set(conv_labels))

    return {
        "name": f"Conversation-first (sim_threshold={sim_threshold})",
        "n_clusters": n_clusters,
        "conversations_clustered": len(conv_names),
        "conv_cluster_map": conv_cluster_map,
        "clusters": clusters,
        "labels": signal_labels,
    }


# ─── Result formatting ───────────────────────────────────────────────

def print_experiment_result(result, signals):
    """Print a formatted experiment result."""
    print(f"\n{'='*60}")
    print(f"  {result['name']}")
    print(f"{'='*60}")

    if "error" in result:
        print(f"  ERROR: {result['error']}")
        return

    print(f"  Clusters: {result['n_clusters']}")
    if "noise_signals" in result:
        print(f"  Noise: {result['noise_signals']} signals ({result['noise_pct']:.1f}%)")
    if "n_windows" in result:
        print(f"  Windows: {result['n_windows']} (noise: {result.get('noise_windows', 0)})")
    if "conversations_clustered" in result:
        print(f"  Conversations clustered: {result['conversations_clustered']}")

    clusters = result.get("clusters", {})
    for label in sorted(clusters.keys()):
        if label == -1:
            continue
        indices = clusters[label]
        if len(indices) == 0:
            continue

        # Summarize the cluster
        convs = defaultdict(int)
        senders = defaultdict(int)
        sample_texts = []
        for idx in indices:
            s = signals[idx]
            convs[s.conversation] += 1
            senders[s.sender] += 1
            if len(sample_texts) < 5 and len(s.text) > 20:
                sample_texts.append(f"    [{s.sender}] {s.text[:80]}")

        top_convs = sorted(convs.items(), key=lambda x: -x[1])[:5]
        top_senders = sorted(senders.items(), key=lambda x: -x[1])[:5]

        print(f"\n  Cluster {label}: {len(indices)} signals")
        print(f"    Conversations: {', '.join(f'{c}({n})' for c, n in top_convs)}")
        print(f"    Senders: {', '.join(f'{s}({n})' for s, n in top_senders)}")
        print(f"    Sample messages:")
        for t in sample_texts:
            print(t)


def main():
    parser = argparse.ArgumentParser(description="Workstream routing experiments")
    parser.add_argument("--data-dir", default=os.path.expanduser("~/.local/share/pigeon"))
    parser.add_argument("--workspace", required=True)
    parser.add_argument("--since", required=True, help="YYYY-MM-DD")
    parser.add_argument("--until", required=True, help="YYYY-MM-DD")
    parser.add_argument("--model", default="all-MiniLM-L6-v2")
    args = parser.parse_args()

    # Load signals
    print(f"Loading signals for workspace '{args.workspace}'...")
    signals = load_signals(args.data_dir, args.workspace, args.since, args.until)
    print(f"Loaded {len(signals)} signals from {len(set(s.conversation for s in signals))} conversations")
    print(f"Date range: {signals[0].ts.date()} to {signals[-1].ts.date()}")
    print(f"Platforms: {set(s.platform for s in signals)}")

    # Signal type breakdown
    type_counts = defaultdict(int)
    for s in signals:
        type_counts[s.signal_type] += 1
    for t, c in sorted(type_counts.items()):
        print(f"  {t}: {c}")

    # Conversation breakdown
    conv_counts = defaultdict(int)
    for s in signals:
        conv_counts[s.conversation] += 1
    print(f"\nTop conversations:")
    for conv, count in sorted(conv_counts.items(), key=lambda x: -x[1])[:15]:
        print(f"  {count:>4}  {conv}")

    # Load embedding model
    print(f"\nLoading model: {args.model}")
    model = SentenceTransformer(args.model)

    # Embed all signals
    embeddings = embed_signals(model, signals)
    print(f"Embeddings shape: {embeddings.shape}")

    # ─── Run experiments ──────────────────────────────────────────────

    print("\n" + "=" * 60)
    print("  RUNNING EXPERIMENTS")
    print("=" * 60)

    # Experiment 1: HDBSCAN with different min_cluster_size
    for min_size in [5, 8, 12, 15]:
        result = experiment_hdbscan(signals, embeddings, min_cluster_size=min_size)
        print_experiment_result(result, signals)

    # Experiment 2: HDBSCAN on conversation windows
    for window_size in [5, 10, 15]:
        result = experiment_hdbscan_windows(signals, embeddings, window_size=window_size)
        print_experiment_result(result, signals)

    # Experiment 3: Agglomerative with different thresholds
    for threshold in [0.3, 0.4, 0.5, 0.6]:
        result = experiment_agglomerative(signals, embeddings, distance_threshold=threshold)
        print_experiment_result(result, signals)

    # Experiment 4: Conversation-first with different similarity thresholds
    for sim_thresh in [0.5, 0.6, 0.65, 0.7, 0.75]:
        result = experiment_conversation_first(signals, embeddings, sim_threshold=sim_thresh)
        print_experiment_result(result, signals)


if __name__ == "__main__":
    main()
