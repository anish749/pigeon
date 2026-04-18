# Workstream Routing Protocol

How incoming signals get routed to the right workstream, and how workstreams
emerge, evolve, and resolve over time.

## The Problem

A person working across multiple messaging platforms generates hundreds of
signals per day. These signals belong to different ongoing efforts — but they
arrive interleaved, scattered across channels, DMs, email threads, and tools.

An AI agent assisting this person needs to understand which signals belong
together. Without grouping, every incoming message is noise. With grouping,
the agent has context: the history of the effort, the people involved, the
current state, and what's being decided.

## Core Concepts

### Signal

An atomic incoming event: a Slack message, an email, a calendar invite, an
issue tracker update, a document comment. Each signal has a timestamp,
sender, content, and source (platform, account, conversation).

### Workstream

A coherent ongoing effort that accumulates signals over time. Not a channel,
not a thread, not a project board — a workstream cuts across all of those.
It represents the answer to: "what do I need to know to continue this work?"

A workstream has:
- A **focus** — a short description of what the effort is about, updated as
  the workstream evolves.
- A **set of conversations** that have contributed signals.
- A **set of participants** involved.
- A **lifecycle state**: active, dormant, or resolved.

### Conversation Affinity

The binding between a conversation and a workstream. When a DM with a
colleague has been routing to the same workstream for weeks, that affinity is
strong and durable. New messages in that DM are assumed to continue the same
workstream unless the content clearly diverges.

### Workspace

An organizational boundary — a Slack workspace, a company's email domain.
Workstreams do not cross workspace boundaries. Each workspace has its own
set of workstreams, its own default stream, and its own base context.

## How It Works

### The Default Stream

Every workspace has a default workstream. All signals start here. There are
no "unrouted" signals — everything always belongs to something. The default
stream is the general catch-all.

### Workstream Creation Is Always a Split

A new workstream is never created from nothing. It emerges when the system
detects a coherent topic forming within an existing stream (usually the
default) and proposes splitting it out. The user confirms, and the relevant
signals are reclassified into the new workstream.

This means the system starts simple (one stream per workspace) and gets more
precise over time as it learns what you're working on.

### Two-Speed Routing

Routing has a fast path and a slow path.

**Fast path (no LLM, handles ~80% of signals):** When a message arrives in a
conversation that already has an affinity to a workstream, the message
inherits that affinity. No classification needed. This handles the majority
of traffic — especially short messages like "ok", "sounds good", "call?" that
have no semantic content on their own but clearly belong to whatever the
conversation is currently about.

**Slow path (LLM, handles ~20%):** When there is no existing affinity, or
when the batch classifier runs, an LLM compares the buffered messages against
the focus descriptions of all active workstreams and decides: do these belong
to an existing workstream, or is this a new topic?

### Batch Classification

The LLM is not called on every message. Signals accumulate in a per-
conversation buffer, and the batch classifier runs when a threshold is
reached — for example, every 5-10 messages or every 30 minutes, whichever
comes later.

This batching is important for three reasons:
1. **Cost**: LLM calls are expensive. Batching reduces them by ~10x.
2. **Context**: A single message like "can you check?" is unclassifiable.
   A batch of 8 messages gives enough context to determine the topic.
3. **Stability**: Classifying every message independently would cause
   thrashing — the affinity would flip-flop on ambiguous messages.

### Affinity Is Durable, Not Temporal

A conversation's affinity to a workstream does not expire after a time gap.
If a DM with a colleague has been about a feature build for three months,
a week of silence does not reset that affinity. The next message is still
assumed to be about the feature build.

The reclassification trigger is **content mismatch**, not time. If the
colleague sends a message about a completely different topic, the batch
classifier will detect the divergence and update the affinity.

This is a key insight from observing real messaging patterns: workstreams
run for weeks or months with multi-day gaps between bursts of activity. A
time-based reset would incorrectly break affinity on every gap.

### Cross-Channel Radiation

Workstreams are often born when a person starts writing about the same topic
in multiple conversations. The typical pattern:

1. A topic comes up in a DM.
2. The person escalates it to a channel.
3. They pull in another colleague via a different DM.
4. They post an update in a status channel.

Each of these conversations, individually, might look like general chatter.
But the same person writing about the same thing across multiple
conversations in a short time window is a strong signal that a workstream
exists. The batch classifier detects this by comparing buffered messages
across conversations for the same user.

### Focus Descriptions Evolve

A workstream's focus description is not static. It gets periodically
refreshed by an LLM that reviews recent signals and rewrites the description
to reflect the current state.

The focus serves two purposes:
1. **Routing**: The batch classifier compares incoming signals against focus
   descriptions to decide where they belong.
2. **Context**: When an agent subscribes to a workstream, the focus gives it
   an instant summary of what's happening.

### Merging and Splitting

**Merge**: Two workstreams that keep receiving signals from the same
conversations, with overlapping participants and similar focus descriptions,
may be the same effort. The system proposes merging them. The user confirms.

**Split**: A workstream where the conversations start diverging into two
distinct topics may need to split. The system detects this when the batch
classifier consistently flags signals from some conversations as mismatched.

Both operations are proposed by the system and confirmed by the user. The
system never merges or splits autonomously.

### Resolution

A workstream resolves when its effort completes — a PR merges, a contract is
signed, an incident closes. The system detects resolution heuristically:
explicit completion language, declining signal volume, or the user manually
marking it resolved. Resolved workstreams are no longer active routing
targets, but their context is preserved.

## Why This Works

### Conversations Are Natural Boundaries

Most of the time, a conversation (DM or channel) is about one thing at a
time. A DM about a feature build stays about that feature build for weeks.
Conversation affinity handles the bulk of routing without any LLM.

The exceptions — channels with mixed topics, DMs where someone changes
subject — are handled by the batch classifier. But they're the minority.

### The Person Is the Bridge

In any multi-channel workstream, one person carries the context across
conversations. They discuss a problem in a DM, escalate it to a channel,
ask for help in another DM, post an update elsewhere. The same person
writing about the same topic in different conversations is the strongest
signal that these conversations are connected.

### Short Messages Inherit Context

In real messaging data, roughly 40% of messages are under 50 characters:
"ok", "yes", "call?", "sounds good". These are semantically meaningless in
isolation but perfectly meaningful in context. Conversation affinity handles
them naturally — they inherit the workstream of their conversation.

### Workstreams Have Long Lifespans

Real workstreams run for weeks or months, not hours. A feature build might
span 90 days. A sales deal might have signals every few days for months.
This is why affinity is durable: the default assumption is continuity. Only
a clear content shift triggers reclassification.

## Example: An Incident That Radiates

A routine infrastructure operation goes wrong:

**14:00** — An engineer DMs a colleague: "The database upgrade is blocking
the batch pipeline. Do you know the timeline?" The DM has no prior affinity.
The message goes to the default stream.

**14:15** — The colleague replies with a rollout plan. Still default stream,
but now the buffer has 2 substantive messages about database upgrades.

**14:30** — The colleague posts in #oncall: "Heads up — the DB upgrade has
hit a snag affecting report generation." Different conversation, same topic,
same person. The batch classifier notices the cross-channel pattern and
proposes a new workstream.

**14:35** — The colleague DMs a product manager: "Can you check if the
customer reports look right?" Matches the new workstream's focus. Gets
routed there. Affinity set.

**14:40** — The PM replies: "Let me look." Fast path — inherits the DM's
affinity. No LLM needed.

Over 40 minutes, the workstream grew from 0 to 6 signals across 3
conversations. Future messages in any of these conversations route via the
fast path.

## Example: A Long-Running Build

Two engineers work on a feature for three months:

**Month 1**: All signals in a single DM. After enough messages, the batch
classifier creates a workstream. Affinity is set: this DM routes here.

**Month 2**: Signals spread to additional channels as more people get
involved — design reviews, dependencies, QA. Each new conversation gets
affiliated as signals route there. The focus updates to reflect the current
phase.

**Month 3**: Testing, deployment, customer rollout. The focus reflects
production readiness.

**Day 90**: "Shipped." The system detects resolution language and proposes
marking the workstream resolved.

Throughout, the original DM maintained its affinity. Multi-day gaps never
broke it. The affinity was content-based, not time-based.

## Validation

The routing model can be validated by replaying historical data:

1. Read all stored signals in chronological order.
2. Feed them through the routing model.
3. Compare discovered workstreams against known ground truth.

Metrics:
- **Discovery rate**: Did the system find the real workstreams?
- **Detection latency**: How many signals before a workstream was proposed?
- **Routing precision**: Were signals routed to the correct workstream?
- **Routing recall**: Were relevant signals captured, or left in default?
- **Affinity accuracy**: Do conversation-workstream bindings match reality?

The replay is repeatable, so the model can be tuned iteratively.
