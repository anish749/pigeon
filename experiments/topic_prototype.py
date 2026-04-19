"""
Topic segmentation prototype: sliding window embeddings + two approaches.
1. Cosine similarity drop between consecutive windows (boundary detection)
2. HDBSCAN clustering on window embeddings (topic grouping)
"""

import json
import glob
import sys
import time
import numpy as np
from sentence_transformers import SentenceTransformer
from sklearn.metrics.pairwise import cosine_similarity
import hdbscan

WINDOW_SIZE = 5  # messages per window
STRIDE = 2       # overlap between windows

def load_messages(data_dir, date_prefix="2026-04-1"):
    """Load all messages from JSONL files, sorted by timestamp."""
    msgs = []
    for f in sorted(glob.glob(f"{data_dir}/{date_prefix}*.jsonl")):
        with open(f) as fh:
            for line in fh:
                obj = json.loads(line)
                if obj.get("type") == "msg" and obj.get("text"):
                    msgs.append({
                        "ts": obj["ts"],
                        "sender": obj["sender"],
                        "text": obj["text"],
                    })
    return msgs


def make_windows(msgs, window_size, stride):
    """Create sliding windows of messages, concatenating text."""
    windows = []
    for i in range(0, len(msgs) - window_size + 1, stride):
        chunk = msgs[i : i + window_size]
        text = "\n".join(f"{m['sender']}: {m['text']}" for m in chunk)
        windows.append({
            "start_idx": i,
            "end_idx": i + window_size - 1,
            "start_ts": chunk[0]["ts"],
            "end_ts": chunk[-1]["ts"],
            "text": text,
        })
    return windows


def approach_1_cosine_boundaries(embeddings, windows, threshold_std=1.0):
    """Detect topic boundaries via cosine similarity drops between consecutive windows."""
    sims = []
    for i in range(len(embeddings) - 1):
        sim = cosine_similarity([embeddings[i]], [embeddings[i + 1]])[0][0]
        sims.append(sim)

    sims = np.array(sims)
    mean_sim = sims.mean()
    std_sim = sims.std()
    threshold = mean_sim - threshold_std * std_sim

    print("=" * 70)
    print("APPROACH 1: Sliding Window Cosine Similarity (Boundary Detection)")
    print("=" * 70)
    print(f"Windows: {len(windows)}, Mean similarity: {mean_sim:.3f}, "
          f"Std: {std_sim:.3f}, Threshold: {threshold:.3f}")
    print()

    boundaries = []
    for i, sim in enumerate(sims):
        marker = " <-- TOPIC BOUNDARY" if sim < threshold else ""
        if marker:
            boundaries.append(i)
        print(f"  Window {i:2d}→{i+1:2d}  sim={sim:.3f}{marker}")

    print(f"\nDetected {len(boundaries)} topic boundaries")
    print()

    # Show segments
    seg_start = 0
    for seg_idx, b in enumerate(boundaries + [len(windows) - 1]):
        seg_end = b if b in boundaries else b
        win_start = windows[seg_start]
        win_end = windows[min(seg_end, len(windows) - 1)]
        print(f"--- Segment {seg_idx + 1}: windows {seg_start}–{seg_end} "
              f"({win_start['start_ts'][:16]} to {win_end['end_ts'][:16]}) ---")
        # Show first window's text as a preview
        preview = windows[seg_start]["text"][:200].replace("\n", " | ")
        print(f"    Preview: {preview}...")
        print()
        seg_start = b + 1

    return boundaries, sims


def approach_2_hdbscan(embeddings, windows, min_cluster_size=3):
    """Cluster windows into topics using HDBSCAN."""
    clusterer = hdbscan.HDBSCAN(
        min_cluster_size=min_cluster_size,
        min_samples=2,
        metric="euclidean",
    )
    labels = clusterer.fit_predict(embeddings)

    print("=" * 70)
    print("APPROACH 2: HDBSCAN Clustering (Topic Grouping)")
    print("=" * 70)

    n_clusters = len(set(labels) - {-1})
    n_noise = (labels == -1).sum()
    print(f"Windows: {len(windows)}, Clusters found: {n_clusters}, "
          f"Noise points: {n_noise}")
    print()

    for cluster_id in sorted(set(labels)):
        label = f"NOISE" if cluster_id == -1 else f"Topic {cluster_id + 1}"
        idxs = [i for i, l in enumerate(labels) if l == cluster_id]
        print(f"--- {label} ({len(idxs)} windows) ---")
        for i in idxs[:3]:  # show up to 3 windows per cluster
            preview = windows[i]["text"][:150].replace("\n", " | ")
            print(f"  Window {i:2d} [{windows[i]['start_ts'][:16]}]: {preview}...")
        if len(idxs) > 3:
            print(f"  ... and {len(idxs) - 3} more windows")
        print()

    return labels


def main():
    if len(sys.argv) < 2:
        print("usage: topic_prototype.py <data_dir>")
        sys.exit(1)
    data_dir = sys.argv[1]

    print(f"Loading messages from: {data_dir}")
    msgs = load_messages(data_dir)
    print(f"Total messages: {len(msgs)}")

    if len(msgs) < WINDOW_SIZE:
        print("Not enough messages for windowing")
        return

    windows = make_windows(msgs, WINDOW_SIZE, STRIDE)
    print(f"Windows (size={WINDOW_SIZE}, stride={STRIDE}): {len(windows)}")
    print()

    print("Loading embedding model (all-MiniLM-L6-v2)...")
    t0 = time.perf_counter()
    model = SentenceTransformer("all-MiniLM-L6-v2")
    t_load = time.perf_counter() - t0
    print(f"  Model load: {t_load*1000:.1f}ms")

    texts = [w["text"] for w in windows]
    print("Embedding windows...")
    t0 = time.perf_counter()
    embeddings = model.encode(texts, show_progress_bar=False)
    t_embed = time.perf_counter() - t0
    print(f"  Embedding {len(texts)} windows: {t_embed*1000:.1f}ms ({t_embed/len(texts)*1000:.1f}ms per window)")
    print(f"Embedding shape: {embeddings.shape}")
    print()

    t0 = time.perf_counter()
    approach_1_cosine_boundaries(embeddings, windows)
    t_a1 = time.perf_counter() - t0

    print("\n")

    t0 = time.perf_counter()
    approach_2_hdbscan(embeddings, windows)
    t_a2 = time.perf_counter() - t0

    print()
    print("=" * 70)
    print("TIMING SUMMARY")
    print("=" * 70)
    print(f"  Model load:              {t_load*1000:8.1f}ms")
    print(f"  Embedding ({len(texts)} windows):    {t_embed*1000:8.1f}ms  ({t_embed/len(texts)*1000:.1f}ms/window)")
    print(f"  Approach 1 (cosine):     {t_a1*1000:8.1f}ms")
    print(f"  Approach 2 (HDBSCAN):    {t_a2*1000:8.1f}ms")
    print(f"  Total (excl. model load):{(t_embed+t_a1+t_a2)*1000:8.1f}ms")
    print()
    print("=" * 70)
    print("PARAMETERS")
    print("=" * 70)
    print(f"  Shared:")
    print(f"    model:          all-MiniLM-L6-v2 (384 dims)")
    print(f"    window_size:    {WINDOW_SIZE} messages per window")
    print(f"    stride:         {STRIDE} messages between window starts")
    print(f"    total messages: {len(msgs)}")
    print(f"    total windows:  {len(windows)}")
    print(f"  Approach 1 (Cosine Boundary Detection):")
    print(f"    threshold_std:  1.0 (boundary = similarity < mean - 1.0 * std)")
    print(f"  Approach 2 (HDBSCAN):")
    print(f"    min_cluster_size: 3 (minimum windows to form a topic)")
    print(f"    min_samples:      2 (how conservative — higher = fewer clusters)")
    print(f"    metric:           euclidean")


if __name__ == "__main__":
    main()
