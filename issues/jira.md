# Jira

Things to do for Jira support.

1. **Poller and manager validation.** Complete validation of the poller and the manager — whether files are getting collected/connected and whether the data is actually present.
2. **Workspace integration for Jira accounts.**
3. **Maintenance compaction.**
4. **Identity service integration for Jira.**
5. **Read protocol integration for Jira.**
6. **`pigeon setup` command for Jira** — setup validation and integration.
7. **Replicate issue comments into a date-sharded comments log.** Comments currently live only inside the per-issue file. They are a linear log of activity and should also be replicated into a separate comments file organized by date.
