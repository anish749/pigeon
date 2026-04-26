# Linear: storage protocol hides activity date in line content

Linear data for an issue is stored as a single flat file per issue:
`linear-issues/<acct>/issues/PROJ-123.jsonl`. The file is appended to over
time with a mix of issue-update events and comment events. The "when did
this issue last change" signal lives only inside the JSONL line bodies
(`"updatedAt"`, `"createdAt"`), not in any filename.

Pigeon's discovery contract everywhere else is "filename tells you when":
messaging conversations shard by `YYYY-MM-DD.jsonl`, gmail/calendar do the
same, Drive uses a `drive-meta-YYYY-MM-DD.json` sidecar that gets rewritten
on every change (so even a comment-only update bumps the date filename).
Linear is the one source that violates this contract.

As a result, `read.Glob(dir, since)` cannot discover a recently-updated
issue file by filename — issue files are identifier-named (`PROJ-123.jsonl`),
not date-named, and Glob's date-glob and drive-meta selectors don't match
them. The code path that *would* have found them — content-based
`rg -l "updatedAt":"YYYY-MM-DD"` discovery — is the patch shape we
explicitly chose not to ship; it would couple discovery to JSONL
serialization details (same fragility as the existing thread-file content
scan, see "Thread files cannot be date-filtered without scanning content"
in `bugs.md`) and treat the symptom rather than the cause.

The cause is the storage protocol. Compare to messaging — an event stream
sharded into per-day files inside a per-conversation directory — which is
the natural shape for any append-only event log. Linear is exactly that
(issue events + comment events accumulating over time), but it stores
everything in one flat file with no date-keyed filename anywhere in the
tree.

**Impact.** `pigeon list --since=Nd` silently omits any Linear issue
whose only activity in the window was an update or comment. The issue
file exists on disk, its content has the right `updatedAt` value, and
`paths.Classify` types it correctly — but Glob's discovery never returns
it, so it never reaches the `LatestTs` / `listConvFor` dispatch, so it
never appears in the listing. Same for `pigeon glob --since` and any
future caller that wants "what changed in the last N days."

The fix is at the protocol layer: restructure Linear writes from
`issues/PROJ-123.jsonl` to a per-issue directory of date-sharded files
(`issues/PROJ-123/YYYY-MM-DD.jsonl`), matching the messaging pattern.
This makes Linear discoverable by the existing date-filename selectors
with zero new code in `read.Glob`, retires `paths.IssueFile` in favour
of a sealed-LogFile `LinearDateFile` analogous to `MessagingDateFile`,
and reuses messaging's per-conversation grouping in `listConvFor` for
the per-issue grouping. Trade-off: requires re-syncing all Linear
history into the new layout — acceptable, since the alternative is
either silently-incomplete listings or another content-scan patch in
the read layer.
