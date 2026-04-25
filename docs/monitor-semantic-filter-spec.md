# Monitor — Semantic Intent Filter

Specification for a filter that consumes the `pigeon monitor` stream and
forwards only messages that semantically match a described intent.

This document captures a conversational design session. It records only
decisions that were explicitly discussed and agreed on. Items that were
raised but not resolved are listed under Open Questions.

## Problem

`pigeon monitor` streams every message arriving on a platform/account.
Today, scoping it to a project or workstream (e.g. "API work") requires
one of:

- Enumerating channels upfront, plus regex / `jq` filters layered on top.
- Post-hoc keyword matching, which fails on paraphrase and drops short
  thread replies that carry no semantic content on their own.

Neither covers the case where project-relevant discussion appears in DMs
or ad-hoc channels the caller did not enumerate.

A semantic filter lets the caller describe an intent in natural language
and have inbound messages routed based on similarity to that intent.

## Scope & Non-Goals

**In scope**
- A filter tool (working name `semantic_filter`) that reads the JSONL
  event stream emitted by `pigeon monitor` on stdin and emits the subset
  of events that match the intent on stdout.
- A single-invocation CLI — no separate `calibrate` and `watch` verbs.
- Intent defined by the caller, containing topic, positive examples, and
  negative examples.

**Not in scope for this spec**
- Changes to `pigeon monitor` itself. The filter is a separate process in
  a pipeline.
- Multi-intent subscriptions from one invocation.
- Persistent state across runs (each invocation is stateless aside from
  the in-memory window buffer).

## Contract

### Input: intent specification

The intent has three components:

- **topic** — a short natural-language description of what the caller
  is listening for. Single string.
- **positive examples** — ≥ 2 strings representing things that should
  route to this intent. Capture phrasing variance (questions, informal
  asides, concrete artifact names) that the topic paragraph does not.
- **negative examples** — ≥ 2 strings representing things that sit in
  adjacent topic space but should *not* route. They tighten the cone.

Two candidate surfaces for passing this in were discussed, and either is
acceptable; markdown-with-section-headers was rejected as too brittle
(silent parser failure on header typos).

- Repeatable flags: `--topic "..." --positive "..." --positive "..." --negative "..." --negative "..."`
- A schema-validated JSON or YAML file with required keys
  `topic: string`, `positives: [string]`, `negatives: [string]`.

The specific surface is an open question (see below); the content model
is fixed.

### Single-invocation execution

One CLI verb. On startup, the tool emits a **preview frame** summarising
what it will and will not route. It then immediately begins streaming
live events. There is no interactive confirmation step — the caller is
expected to read the preview (the "radio station identifier") and kill
the process if the preview looks wrong.

Motivation: two verbs (`calibrate` then `watch`) create a compliance hole
where the caller can skip calibration. Folding both into one invocation
makes the preview mandatory.

### Stdout

All output is newline-delimited JSON. The first line is the preview; the
rest is the filtered live stream.

```
{"kind":"preview","would_route":[...],"would_skip":[...],"ambiguous":[...]}
{"kind":"system","content":"listening"}
<routed message events, same shape as `pigeon monitor` output>
```

The preview is a single JSON object, not multiple lines — one read, one
parse, complete picture.

`would_route`, `would_skip`, and `ambiguous` are each a list of example
message events drawn from recent traffic (discussed value: last 24h).
They are illustrative, not calibrative.

## Math

### Embeddings, not LLMs

Scoring is done with embeddings rather than per-message LLM
classification. This much was agreed. Nothing else about how
embeddings are produced or which model is used has been agreed.

At startup, the tool computes:

```
v_topic    = embed(topic)
v_pos[i]   = embed(positive_example[i])   for i in 1..P   (P >= 2)
v_neg[j]   = embed(negative_example[j])   for j in 1..N   (N >= 2)
```

### Threshold is derived from the intent alone

No historical corpus is needed to compute the threshold. The geometry of
topic + positives + negatives is sufficient:

```
s_pos_min = min over i of cos(v_pos[i], v_topic)    # tightness of the positive cone
s_neg_max = max over j of cos(v_neg[j], v_topic)    # closest negative approach
threshold = (s_pos_min + s_neg_max) / 2
```

If `s_pos_min < s_neg_max`, the intent is **self-contradictory** — a
positive example is closer to a negative example than to the topic. The
tool refuses to start and reports which examples collide. This is a
pre-flight check; no live traffic is consumed.

### Per-message decision (two-stage)

For each inbound message event `m` with text `m.text`:

**Stage 1 — single-message scoring:**

```
v_m   = embed(m.text)
s_pos = max( cos(v_m, v_topic),  max over i of cos(v_m, v_pos[i]) )
s_neg = max over j of cos(v_m, v_neg[j])
```

Route `m` if `s_pos >= threshold` **and** `s_pos > s_neg`.

(Whether a separation *margin* is added to the second condition —
`s_pos > s_neg + margin` — was discussed at one point but not carried
through after the threshold-from-geometry decision. Listed under Open
Questions.)

**Stage 2 — conversation window fallback:**

If stage 1 rejects, embed a concatenation of the last *W* messages in
`m.conversation` (including `m` itself) and re-score using the same rule.

```
v_win = embed(concat(last_W_messages[m.conversation]))
(apply same s_pos / s_neg / route rule to v_win)
```

If stage 2 passes, route `m`.

Motivation: individual messages like "sounds good" or "yes" have no
semantic content on their own but are perfectly meaningful in context.
Embedding the surrounding window captures that context. The fallback
runs only when stage 1 rejects.

### Per-conversation window buffer

The tool keeps an in-memory ring buffer of the last *W* messages per
conversation (discussed default: W = 5). New messages push oldest out.
Stage 2 reads from this buffer. Lookup is O(1).

## Hidden vs exposed configuration

Agreed: the user-facing CLI surface should be minimal. Only the intent
components are required inputs.

- **Window size W** — hidden default (5).
- **Threshold** — not exposed at all. Computed from the intent. The
  caller does not set it.

What the caller always sees: the intent they wrote, and the preview
frame with the derived threshold and examples. That is the contract.

## Startup sequence

1. Parse intent (topic, positives, negatives). Validate arity (P ≥ 2,
   N ≥ 2). Fail fast on structural errors.
2. Embed topic, positives, negatives.
3. Compute `s_pos_min`, `s_neg_max`, `threshold`.
4. If `s_pos_min < s_neg_max`, abort with a clear error listing the
   conflicting pair(s). Do not start streaming.
5. Sample recent traffic (discussed: last 24h from the account's JSONL
   files or from `pigeon monitor --since=24h`) for the preview examples.
   If no recent traffic exists, note that in the preview and continue.
6. Emit the preview JSON object as the first stdout line.
7. Emit `{"kind":"system","content":"listening"}`.
8. Begin consuming the live event stream, applying the two-stage
   decision per message, forwarding matches to stdout.

## Example (verbatim from the design session)

An intent that describes "routing messages by semantic similarity,
using embeddings, handling short thread replies via a context window":

```
topic:
  Semantic routing of streaming chat messages. Using embedding models
  (not LLMs) to compare incoming messages against a described intent
  and forward ones above a cosine-similarity threshold. Pairing
  single-message scoring with a fallback that embeds a rolling window
  of the last few messages in the same conversation, so low-signal
  replies like "sounds good" still route correctly when the surrounding
  thread is on-topic. Calibration concerns: threshold tuning, intent
  specificity, concept drift, comparison against channel-glob and
  keyword filters in tools like pigeon monitor.

positives:
  - "how should we handle thread replies that don't have enough text on
     their own to match an intent?"
  - "what cosine threshold should we start with, 0.4 or 0.5?"
  - "embeddings are basically free at this volume, it's fractions of a
     cent per day"
  - "the intent description needs to include pigeon-specific names or
     it'll match any ML-routing chatter"

negatives:
  - "routing HTTP traffic between microservices"
  - "semantic search over documents in a vector DB"
  - "LLM-based classification of customer support tickets"
```

This is the artifact shape, not a prescription for how it is serialised
(see Open Questions on surface).

## Why this shape

- **Embedding, not LLM**: deterministic enough to be auditable (cosine
  similarity is a number), cheap enough to ignore cost concerns,
  latency low enough that a per-message call fits into a monitor path.
- **Multi-vector intent**: real chat varies more than prose does. A
  single long paragraph produces one vector that averages everything
  including phrasing noise. Positives give the embedding more anchors
  in the kinds of sentences chat actually produces.
- **Negatives do the work a background corpus would otherwise do**:
  they define "close but wrong" without requiring the tool to sample
  and label history. The threshold falls out of the geometry.
- **Window fallback is a targeted fix for thread replies**, not a
  general smoothing mechanism. It only fires when the single-message
  score fails, so it does not muddy the base case.
- **Preview as first stdout line** means the caller cannot start
  listening to a mis-specified intent without noticing. The radio-
  station-identifier framing was the chosen mental model.
- **Threshold derivation from geometry** avoids a corpus dependency
  and removes a knob from the user-facing surface. The intent itself
  is the only thing the caller tunes.

## Failure modes explicitly handled

- **Intent is self-contradictory** (`s_pos_min < s_neg_max`): pre-
  flight abort.
- **No recent traffic for preview**: preview emits empty example
  lists with a note; streaming proceeds normally.
- **Short thread reply with no content**: stage 1 fails, stage 2
  succeeds via the conversation window.

## Failure modes discussed and acknowledged, not fully designed

- **Threshold calibration uncertainty** when the positive cone and
  negative edge are close. Surfaced as thin separation in the preview;
  caller can tighten the intent and re-run.
- **Intent specificity** — vague intents produce noisy routing;
  specific intents produce focused routing. This is a caller
  responsibility, not a tool guarantee. The preview makes it visible.
- **Window pivot lag** — if a conversation pivots topics mid-stream,
  the window-based score lags the pivot by a few messages (trailing
  false positives after the switch, leading false negatives before it).
  Acknowledged as tolerable.
- **Concept drift** — new sub-topics emerging within a workstream
  require the intent to be updated and the process restarted. No
  in-process intent reload is specified.
- **Polysemy** — a word like "API" maps to many topics; the caller
  must phrase the intent richly enough for the embedding to
  disambiguate. The negatives are the lever here.

## Open Questions

These were raised in the design session but not resolved. They need to
be settled before implementation.

1. **Intent surface** — repeatable CLI flags vs a JSON/YAML file with
   a schema. Both were discussed and both were judged acceptable over
   markdown-with-headers; no preference was chosen.
2. **Separation margin** in the routing rule — earlier in the
   discussion a `margin` term (example value 0.05) appeared in
   `s_pos > s_neg + margin`. The later collapse to "threshold from
   geometry alone" did not explicitly carry this forward. Needs an
   explicit yes/no: is the rule `s_pos > s_neg` or `s_pos > s_neg + margin`?
3. **Precision/recall preference knob** — the idea of a "strict" vs
   "loose" dial was raised as something the tool cannot guess. Not
   agreed whether to include it or omit it.
4. **Preview sample window** — last 24h was mentioned. Not fixed as
   a default; not specified whether it is configurable.
5. **Preview sample source** — reading from the account's JSONL
   store directly vs invoking `pigeon monitor --since=24h` was not
   decided.
6. **Preview sample size K** — how many examples in each of
   `would_route`, `would_skip`, `ambiguous` is not fixed.
7. **Window buffer seeding at startup** — when the process starts,
   are the per-conversation buffers cold (empty, filling as live
   traffic arrives) or are they warm-started from recent JSONL? The
   stage-2 fallback quality depends on this.
8. **Live event output schema** — are routed events passed through
   unchanged from `pigeon monitor`, or is the filter expected to
   annotate them with `s_pos`, `s_neg`, `via_window` flags? Not
   discussed.
9. **Intent hash algorithm** — the preview includes an `intent_sha`
   identifier so the caller knows which intent is in force, but the
   specific hashing function and what inputs are covered (topic
   only, or topic + positives + negatives + version) was not
   specified.
10. **Embedding cache / dedup** — no decision on whether identical
    incoming messages are re-embedded each time or cached.
11. **Where the filter lives** — a standalone binary, a subcommand
    of `pigeon`, or a library — not decided.
12. **Multi-intent** — running multiple intents from one invocation
    was called out as out-of-scope for this spec but not discussed
    further.
