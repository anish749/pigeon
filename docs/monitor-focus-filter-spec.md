# `pigeon monitor` — focus filter

This document specifies a focus-based filter on the `pigeon monitor`
event stream, enabling per-workstream and per-session subscriptions to
incoming messaging traffic.

It supersedes the earlier multi-vector spec at
`docs/monitor-semantic-filter-spec.md`, which proposed a more complex
algorithm with positives, negatives, geometry-derived thresholds, and
two-stage scoring with a window fallback. Empirical validation
(Section 2) showed that simpler is strictly better under the metrics
that actually matter for fan-in reduction.

---

## 1. The spec

### 1.1 Problem

Without a filter, every workstream-bound Claude Code session must wake
on every incoming message in the workspace. With N workstreams and a
modestly active workspace, this means N agent wake-ups per message and
N agents reading content that is, for most of them, irrelevant.

The filter exists to **reduce fan-in**: route each message only to the
sessions whose workstream focus the message is plausibly related to.
The receiving agent self-filters anything that slipped through — a
false positive at the router costs a few hundred tokens of "this isn't
for me, ignoring", not a wrong action.

This is not a precision-classification problem. It is a fan-in
reduction problem.

### 1.2 Two access patterns

The filter must support two callers, with the same algorithm:

**Pattern A — workstream-driven (long-lived).**
A workstream daemon spawns one filter process per active workstream.
Each filter subscribes to the broadcast bus and emits the subset of
messages relevant to its workstream. Each filter pipes into a Claude
Code session bound to that workstream. Lifecycle: filter lives as long
as the workstream does; updates to focus mean restarting the filter.

**Pattern B — session-driven (ad hoc).**
A live Claude Code session declares "I am working on X right now, send
me everything related." The session is responsible for spawning its
own filter process with its current focus. When the session's focus
changes, it kills the old filter and spawns a new one. The pigeon
daemon does not maintain session-focus state.

Both patterns share the same CLI invocation; only the lifecycle
manager differs.

### 1.3 CLI surface

```
pigeon monitor --focus "<workstream focus prose>"
```

`pigeon monitor` already exists today and emits all events from the
broadcast bus to stdout as newline-delimited JSON. This spec extends it
with one flag:

- **`--focus`** *(string, required when filtering)*. A natural-language
  description of what the caller cares about. For Pattern A this is
  the workstream's focus from `pigeon workstream discover`. For
  Pattern B this is whatever the session declares.

When `--focus` is omitted, `pigeon monitor` behaves as today (no
filtering). When set, only matching messages are emitted.

The cosine threshold is an internal implementation constant tied to
the embedder, not a caller-visible knob. See Section 1.5 for why.

Multi-intent subscription from a single invocation is **out of scope**
for this spec — Pattern A spawns N filter processes, one per intent.

### 1.4 Algorithm

```
setup (once, when filter starts):
  v_focus = embed(focus_text)

per incoming message m:
  v_msg = embed(m.text)
  if cosine(v_msg, v_focus) >= threshold:
      emit(m)
```

That is the entire algorithm. Six lines.

Notes:

- **Multi-route is implicit.** Each filter process is independent.
  The same underlying message arriving on the broadcast bus can pass
  any subset of running filters; that's the desired behavior.
- **Embedder is `all-MiniLM-L6-v2`** (the model the existing
  `embed/sidecar.py` already serves). Stronger embedders were tested
  and did not improve performance on this task; see Section 2.6.
- **No state across messages.** Each message decision is independent.
  Filters can scale, restart, and run in parallel freely.
- **No additional caller inputs.** Specifically, no positives, no
  negatives, no margin, no length gate, no window fallback. Each was
  tested empirically and shown to either reduce recall, increase
  noise wake rate, or both. See Section 2.5.

### 1.5 The threshold

The cosine threshold is hardcoded inside the filter implementation:
**0.22 with `all-MiniLM-L6-v2`** (the model the existing
`embed/sidecar.py` already serves). It is not exposed to callers.

The reasoning:

- **No caller is in a position to choose a cosine value.** A
  workstream daemon spawning per-workstream filters doesn't know what
  cosine the focus prose embeds at; a Claude session declaring
  "I'm working on X" definitely doesn't. Exposing the number turns
  into "everyone passes the default", which means the flag is
  pretending to be a knob it isn't.
- **The empirical optimum is stable across workspaces.** Across the
  three test workspaces in Section 2, the threshold where
  wake-rate-on-noise drops below 0.30 lives in [0.20, 0.25]. A single
  hardcoded value of 0.22 covers all three within a percentage point
  of recall.
- **If it ever needs retuning, it's a binary change, not an API
  change.** Adjusting the constant in the filter implementation is
  cheap. Plumbing a new flag through every caller's invocation is
  expensive and asks the caller to know things they don't.

A different embedder requires a different constant. With
`BAAI/bge-base-en-v1.5` the natural separation lives near 0.55; with
`intfloat/e5-base-v2` near 0.80. Embedder choice and threshold are
co-determined and live together in the filter implementation.

If cross-workspace variance turns out to bite in production (e.g.,
some workspace's traffic distribution puts the natural knee outside
[0.20, 0.25] for MiniLM), the planned follow-up is **auto-tune at
startup**: sample the last N hours of traffic from the broadcast bus,
compute the cosine distribution to the focus, pick the knee, set the
threshold. Still no caller-visible knob. This is deliberately not in
the v1 surface — the data does not motivate it yet.

### 1.6 Metrics the filter is optimised for

The algorithm is chosen to optimise three metrics, in priority order
within reasonable ranges:

1. **`recall_relevant`** — of (msg, true_workstream) pairs in
   labelled validation data, fraction the filter routed.
2. **`wake_rate_noise`** — of messages labelled as belonging to no
   workstream, fraction that wake at least one filter (i.e. `cos >= τ`
   for at least one workstream's focus).
3. **`woken_per_msg`** — average number of filters that fire per
   message. Below 1.0 means net fan-in reduction; the ratio
   `(workstream_count) / woken_per_msg` is the operational gain.

Operating-point picking heuristic: among thresholds where
`wake_rate_noise <= 0.30`, choose the highest `recall_relevant`. This
is what produced τ = 0.22 as the cross-workspace default.

### 1.7 Preview frame

When a filter starts, before live events flow, it should emit a
single JSON line summarising what it will route. The preview is a
UX surface for the caller to verify the focus prose actually matches
the kind of traffic they expect — i.e. that they spelled the focus
correctly and that it isn't accidentally aligned with an unrelated
chunk of the workspace's content.

```json
{"kind":"preview",
 "would_route_examples":[<up to K recent matching events>],
 "would_skip_examples":[<up to K recent non-matching events>]}
```

The preview consumes recent traffic from the daemon's history (via
the existing replay mechanism) — no special API needed. After the
preview, a `{"kind":"system","content":"listening"}` line marks the
start of live output.

The preview is illustrative, not calibrative. The algorithm has no
calibration step; it's just a threshold.

### 1.8 What is NOT in scope

- Caller-supplied positives or negatives. Adding them does not
  improve the metrics under fan-in framing (Section 2.5).
- Cross-message state (window buffer, thread-aware aggregation).
  The conversation context idea was tested and rejected; it
  consistently lowered recall and raised noise wake rate together.
- Multi-intent subscription from one invocation. Spawn one filter
  per intent.
- Caller-visible threshold tuning. The threshold is an internal
  constant; callers do not pass it.
- Auto-tune from sampled traffic. Plausible follow-up if production
  shows the hardcoded threshold is wrong for some workspace, but
  the validation data does not motivate building it now.
- LLM-based per-message classification. Not tested directly, but
  unnecessary under fan-in framing — the agent on the receiving end
  is the real classifier; the filter only needs to be cheap and
  recall-leaning.

### 1.9 Workstream discovery is upstream

The filter's recall ceiling is bounded by how distinct the
workstream focuses are from each other. If two workstreams' focus
prose embeds at cosine > ~0.7 with each other, the filter cannot
reliably tell them apart and a message about one will route to both.

Improving the filter further is largely an upstream problem in
`pigeon workstream discover`. Three concrete levers, all outside the
scope of this spec but worth flagging:

- **Reject focus proposals** whose nearest existing focus has
  cosine gap < 0.05; ask the LLM to either merge or differentiate.
- **Prompt the LLM** to write focuses that emphasise *what makes
  this workstream different from neighbouring workstreams*, not
  describe each in isolation.
- **Post-process focus prose** to drop shared boilerplate prefixes
  (e.g., if multiple workstreams' focuses begin with the same
  partnership/team name, that token mass dominates the embedding).

These are quantifiable gates that `discover` can apply before
proposing a workstream as a routing target.

---

## 2. How this spec was reached

This section records the experimental path from the original
multi-vector spec to the simpler shape above. All numbers reproduce
from `experiments/focus-filter/` in the validation worktree.

### 2.1 The validation problem

The original spec proposed a per-message filter with:
- topic embedding + ≥2 positives + ≥2 negatives
- geometry-derived threshold = (s_pos_min + s_neg_max) / 2
- self-contradiction pre-flight check
- two-stage scoring: single-message stage 1, conversation-window
  fallback when stage 1 rejected
- the rule: route if `s_pos >= threshold` and `s_pos > s_neg`

The validation question was whether each of those components paid for
its complexity, and whether the algorithm worked on real workspace
data.

### 2.2 Validation setup

Three messaging workspaces were used, anonymised here as **X, Y, Z**:

| workspace | role     | active workstreams (after merges) | dominant content |
|-----------|----------|----------------------------------:|------------------|
| X         | small    | 4                                 | crisply-separated product/feature workstreams |
| Y         | medium   | 11                                | many narrowly-overlapping workstreams within one product domain |
| Z         | medium   | 12                                | mixed engineering + data-partnership workstreams within one product |

Workstreams were obtained from `pigeon workstream discover --workspace
<name>` over a roughly 2–3 week message window. Initial discovery
proposed 4 / 13 / 16 workstreams respectively; some workstreams were
identified as semantic duplicates and merged before evaluation (see
Section 2.7).

For each workspace, **210 messages** were sampled stratified by
length (70 short < 25 chars, 70 medium 25–100, 70 long ≥ 100). Each
sampled message, plus its last 3 messages of conversation context,
was sent to **Claude Haiku as a judge**, asked which workstream(s) it
belonged to (multi-label, "none" allowed). Labels were cached. The
prompt explicitly handled short replies (acks belong to a workstream
only if the surrounding context is clearly that workstream's work)
and generic logistics ("running late", scheduling) as belonging to
no workstream.

Total labelled signals: 630. Labels distribution per workspace:

| workspace | labelled | judge-says-relevant | judge-says-noise | total (msg, ws) pairs |
|-----------|---------:|--------------------:|-----------------:|----------------------:|
| X         | 255      | 84                  | 171              | 88                    |
| Y         | 210      | 106                 | 104              | 124                   |
| Z         | 210      | 102                 | 108              | 107                   |

### 2.3 Metrics reframing

Earlier rounds of the validation used macro-F1 across workstreams as
the optimisation target. This was wrong. F1 punishes false positives
and false negatives equally, but for the actual product:

- A false positive at the filter (routing an irrelevant message to a
  workstream's session) costs a few hundred tokens of agent triage.
  The agent reads it, decides it's not relevant, moves on.
- A false negative (not routing a relevant message) means the
  workstream's agent never sees that piece of work. The cost is
  unbounded — depending on what the message contained.
- Wake rate on noise is the actual fleet-wide cost: each wake of an
  agent on a message that has nothing to do with its workstream is
  capacity that could have run something useful.

Under this framing the right metrics are recall, wake-rate-on-noise,
and average sessions woken per message (Section 1.6). F1 is
discarded.

This reframing alone changed which algorithm wins. With the F1
target, an algorithm that adds a `s_pos > s_neg` rule on top of the
focus-cosine threshold scored marginally higher (precision lift
exceeded recall loss in some workspaces). Under fan-in metrics, the
same rule strictly loses — recall drops 12–20% and the only
"benefit" is reducing already-low woken-per-message even further,
which is not a benefit, since the fleet-wide wake on noise was
already acceptable.

### 2.4 The selected algorithm vs alternatives

The candidate algorithms tested under fan-in metrics:

- `focus-only`: route if cos(msg, focus) ≥ τ. Multi-route across
  filter instances.
- `focus + s_pos > s_neg`: same threshold, plus require focus to be
  the closest workstream focus to the message.
- `top-K`: always route to the K nearest workstreams, no threshold.

Operating points across workspaces (selected for `wake_rate_noise ≤
0.30`):

| workspace | algorithm | τ or K | recall | wake/noise | woken/msg | fan-in × |
|-----------|-----------|-------:|-------:|----------:|----------:|---------:|
| X         | focus-only | 0.20 | **0.67** | 0.29       | 0.97      | **4.1×** |
| X         | focus + s_pos>s_neg | 0.20 | 0.56 | 0.29 | 0.43      | 9.4×     |
| X         | top-K K=2 | —      | 0.82   | 1.00       | 2.00      | 2.0×     |
| Y         | focus-only | 0.25 | **0.35** | 0.11       | 0.90      | **12.2×**|
| Y         | focus + s_pos>s_neg | 0.25 | 0.18 | 0.11 | 0.32      | 34.0×    |
| Y         | top-K K=2 | —      | 0.45   | 1.00       | 2.00      | 5.5×     |
| Z         | focus-only | 0.25 | **0.41** | 0.23       | 1.19      | **10.1×**|
| Z         | focus + s_pos>s_neg | 0.25 | 0.32 | 0.23 | 0.40      | 30.0×    |
| Z         | top-K K=2 | —      | 0.46   | 1.00       | 2.00      | 6.0×     |

`focus-only` dominates at every workspace's chosen operating point.

`focus + s_pos>s_neg` reduces woken-per-message but at the cost of
12–17 percentage points of recall. The reduction is irrelevant —
woken-per-message is already < 1.5 with focus-only, so cutting it
further has no operational benefit. The recall loss is real lost work.

`top-K` always wakes K agents per message including on every noise
message (wake_rate_noise = 1.0 by construction). It hits higher recall
than focus-only at K=2 on workspace X, but pays double wake on every
noise message. Worse on every workspace.

### 2.5 Components that were rejected

Each component of the original spec was tested as an opt-in addition
to focus-only and rejected based on data:

- **Positives** (`max(cos(msg, focus), max_i cos(msg, pos_i))`).
  Tested in earlier rounds under F1: routing volume rose, but precision
  dropped more than recall rose. Mechanism: bootstrapped positives sit
  near the focus already, so adding them widens the cone toward the
  variance in those examples — pulling in nearby other-workstream
  messages. Under fan-in metrics this hurts both metrics.

- **Negatives** (`s_pos > max_i cos(msg, neg_i)`). Tested in two
  flavours: (a) negatives = positives bootstrapped from other
  workstreams' high-confidence messages, (b) negatives = focus prose
  of other workstreams directly. Both narrow the cone further than
  focus+threshold alone, lowering recall. Under F1 they were marginally
  helpful on the small workspace (X); under fan-in metrics they are
  strictly worse, because the recall loss comes with no compensating
  reduction in fleet wakes (each filter wakes only on its own focus
  match, regardless of negatives).

- **Self-contradiction pre-flight** (refuse to start if `s_pos_min <
  s_neg_max`). Without positives, this check has nothing to compute.
  With positives, the check false-fired on cleanly-separated intents
  in earlier rounds when bootstrapped positives were in a different
  register/length distribution from chat.

- **Margin** in the `s_pos > s_neg + margin` rule. Sweeping margin ∈
  {0.00, 0.02, 0.05, 0.10}, F1 dropped monotonically; under fan-in
  metrics the same trend holds — margin > 0 strictly reduces recall.

- **Two-stage with conversation-window fallback** (concat last-W
  messages and re-score). Tested with concat, mean-of-embeddings, and
  length-gated variants. Every flavour rescued some short replies
  ("yes", "sounds good") but propagated a much larger volume of
  noise wakes, because the surrounding conversation context attracts
  the embedding regardless of the current message's actual content.
  Net effect: wake_rate_noise jumped 0.10–0.20 absolute, recall lifted
  3–5 percentage points. Strictly bad under fan-in metrics.

- **Length gating** (skip messages with text shorter than N chars).
  Untested directly under fan-in metrics. Under F1 it was neutral or
  slightly negative. Under fan-in framing, it can only reduce recall
  (skipped messages cannot be routed); the reduction in wakes is
  small because short messages mostly already fail the cosine
  threshold. Skipped.

- **Geometry-derived threshold** ((s_pos_min + s_neg_max) / 2). With
  realistic positives, the formula lands at 0.41–0.49 on this data
  and stays brittle: it's dominated by the worst-phrased positive
  example. Scaling by 0.7–0.85 recovers usable thresholds, suggesting
  the formula structure is roughly right but the constant is wrong.
  Replaced with a flat tunable threshold parameter.

### 2.6 Embedder comparison

Three embedders were tested on the same labelled set and the same
algorithm (`focus-cos ≥ τ`, multi-route):

| embedder                    | natural τ band | mean micro-F1 (3 ws) |
|-----------------------------|---------------:|---------------------:|
| `all-MiniLM-L6-v2` (384-dim)|   0.20–0.25    | 0.350                |
| `BAAI/bge-base-en-v1.5` (768-dim) | 0.55–0.60 | 0.265              |
| `intfloat/e5-base-v2` (768-dim)   | 0.75–0.80 | 0.243              |

bge and e5 are stronger embedders trained for retrieval, but they
compress all chat-like text into a small high-cosine region of the
unit sphere. The relative gap between focus prose and a relevant
chat message is no larger than with MiniLM, and the absolute working
range is narrower. MiniLM-L6-v2 is a strict win on this task and is
what the existing `embed/sidecar.py` already serves; no model swap
needed.

### 2.7 Why two regimes appear (and what to do upstream)

Workspace X with 4 well-separated workstreams routed at recall 0.67.
Workspaces Y and Z with 11–12 narrowly overlapping workstreams
routed at recall 0.35–0.41. The gap is not about algorithm choice
or embedder; it is about the **separability of the workstream focuses
themselves**.

Pairwise cosine analysis on the focus prose alone (no messages
involved) confirms this:

| workspace | min off-diagonal cos | median | max | smallest NN-gap |
|-----------|---------------------:|-------:|----:|----------------:|
| X         | 0.55                 | 0.61   | 0.65 | 0.000          |
| Y         | 0.50                 | 0.58   | 0.70 | 0.002          |
| Z         | 0.43                 | 0.60   | 0.75 | 0.001          |

A pair of focuses with cosine > 0.7 cannot be distinguished by any
threshold — messages about one will route to both. Six pairs of
discovered workstreams across Y and Z were identified as semantic
duplicates and merged before final evaluation. The merges fixed the
specific high-cosine pairs but several inter-workstream cosines in
the 0.65–0.70 range remained — these were genuinely related but
distinct workstreams (e.g., two views of the same data partnership
where one team receives data and another sends data). The remaining
recall ceiling reflects this irreducible overlap.

The implication for the filter spec is that **further routing-quality
work belongs in `pigeon workstream discover`**, not in the filter
algorithm. Specifically:

- A discover-time gate that rejects focus proposals where the
  nearest existing focus has cosine gap < 0.05.
- An LLM prompt change to make discover write focuses that emphasise
  difference rather than describe each workstream in isolation.
- A post-processing pass that detects shared boilerplate prefixes
  across focuses (e.g., all focuses starting with the same team name
  or partnership phrase) and rewrites them.

These are quantifiable gates with measurable outputs, and they live
upstream of where this spec operates.

### 2.8 Caveats

- 210 labels per workspace × 3 workspaces. Per-workspace recall
  numbers have CIs of roughly ±0.05.
- Single LLM judge (Claude Haiku). Larger workspaces had 11–12
  workstreams to choose from; judge labelling is the noisiest part of
  the experiment.
- Stratified sampling oversamples short messages relative to the
  natural stream distribution. The recall numbers may be
  pessimistic if short-message density is lower in production.
- Recall is computed on (msg, true_workstream) pairs. A message
  labelled as belonging to W1 AND W2 where the filter routed only to
  W1 counts as 50% recall, even though under "the agent self-filters"
  framing routing-to-at-least-one is partially acceptable. The
  numbers are the conservative reading.
- Threshold default 0.22 is the cross-workspace centre point. Per-
  workspace tuning to {0.20, 0.25, 0.25} would lift recall by a few
  percentage points each, at the cost of caller complexity.

### 2.9 Files used in the validation

All under `experiments/focus-filter/`:

- `evaluate_recall_wake.py` — the final metric harness
- `evaluate_multi_workspace.py` — earlier F1-based sweep
- `analyze_focus_geometry.py` — focus-prose pairwise analysis used to
  identify mergeable workstreams
- `test_embedders.py` — comparison across MiniLM, bge, e5
- `label_signals_v2.py` — judge-labelling pipeline
- `workstreams.json` (gitignored) — workstream config + post-merge
  remap; see `workstreams.example.json` for the schema
- `labels-{X,Y,Z}.json` (gitignored) — Claude Haiku judge labels
