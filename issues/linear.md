# Linear

Pending work and known gaps for Linear support.

1. **Identity service integration for Linear.**
2. **Replicate issue comments into a date-sharded comments log.** Comments currently live only inside the per-issue file. They are a linear log of activity and should also be replicated into a separate comments file organized by date.

See also `linear-storage-protocol.md` for the storage protocol gap that hides issue activity dates from filename-based discovery.
