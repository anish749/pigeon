# Hold the fort

A constrained-autonomy mode the owner enters when stepping away from the
keyboard. The agent stays connected and reactive but its capability surface
shrinks — and within that smaller surface, outbox review is dropped.

This is the inverse of an "auto-approve everything" toggle. Auto-approve
expands what passes through review. Hold the fort shrinks what the agent
will even attempt, then trusts it inside that scope. The failure mode flips
from "agent did something bad and you can't unsend" to "agent didn't do
something you wanted" — recoverable, low blast radius.

## Allowed in hold the fort

- Reply in an existing thread the owner is already part of
- Defer: "I'll get back to you when I'm at my desk" as a graceful fallback
  whenever the agent is uncertain

## Not allowed in hold the fort

- Initiating new outreach to anyone
- Starting new tasks or exploration in response to incoming prompts
- Touching production data or destructive surfaces
- Making commitments beyond the immediate exchange

In normal mode these guards are off — the agent operates with full
capability, with the outbox handling per-message review.

## Entry and exit are explicit, not timed

The owner turns hold the fort on when leaving and off when back. There is
no auto-expiry — the mode is bounded by the owner's presence, not by a
clock. A timer would either fire mid-absence (defeating the purpose) or
after return (redundant).

## Return view is the product

Because the mode constrains what the agent does, the digest on return is
naturally short and predictable — "replied to 3 threads, deferred 1
question, no new conversations started." That digest is the connection
back to authorship and is what makes the mode safe to live with.

## Relationship to other features

Hold the fort is one of three orthogonal axes around outbox review. The
other two are already in flight as separate PRs.

- **Direct trust allowlist (PR #173).** Per-workspace list of recipients
  whose messages bypass outbox review at all times. Permanent,
  recipient-scoped trust. Answers "this person is effectively me, don't
  gate replies to them." Bypass is pre-outbox so these messages do not
  appear in outbox history — they are treated as direct, not as
  auto-approved. Sent messages still land in the JSONL store.

- **Phone review (PR #232).** A second review surface — outbox items
  posted to the owner's bot DM as Block Kit messages with approve /
  feedback buttons. Trust is unchanged; every message still goes through
  review. Only the surface moves. Addresses review *ergonomics*, not
  review *bypass*.

- **Hold the fort (this doc).** Temporary, capability-scoped trust while
  the owner is away. Entered and exited explicitly.

The three compose cleanly because they answer different questions:

- Phone review: *where* you do the reviewing.
- Direct allowlist: *who* permanently bypasses review.
- Hold the fort: *whether* review is needed, and only inside a tiny
  capability scope, while the owner is away.

Hold the fort can be entered or exited from any available surface —
terminal review TUI, CLI, or the phone-review channel itself once that
lands.
