## Outbox Review Flow

The TUI review screen (`pigeon review`) is a separate process from the
daemon. Two processes collaborate:

```
Process 1: DAEMON (pigeon daemon _run)    Process 2: TUI (pigeon review)
──────────────────────────────────────    ──────────────────────────────

                                          Bubble Tea app starts
                                            │
GET /api/outbox  ←─────────────────────── fetchItems() polls every 1s
handleOutboxList()                          │
  lists items from outbox                   │
  enriches with resolved display names      │
  returns []OutboxListItem                  │
─────────────────────────────────────→    renders items with names
                                            │
                                          user presses 'a' to approve
POST /api/outbox/action ←──────────────── {id, action: "approve"}
handler.approve()
  unmarshals payload → SendRequest
  dispatchSend() → Slack/WhatsApp API
  removes from outbox
  notifies Claude session via SSE
─────────────────────────────────────→    shows "✓ Approved"
```

All IPC is HTTP over a Unix domain socket. The send request enters the
outbox via `POST /api/send` from any client (CLI, Claude session). The
TUI polls the daemon to display pending items and posts actions back.

### Key design principle

**The daemon is the intelligent process.** It holds platform connections,
resolvers, and the outbox. The CLI and TUI are stateless clients — they
display what the daemon tells them and post actions back.
