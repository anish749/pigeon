## Outbox Review Flow

The outbox review TUI runs as a separate process from the daemon. Three
processes collaborate to get a message from intent to delivery:

```
Process 1: CLI or Claude session        Process 2: DAEMON                    Process 3: TUI (pigeon review)
────────────────────────────────        ─────────────────                    ──────────────────────────────

pigeon send slack -c '#eng' -m "hi"
  │
  POST /api/send ──────────────────→  handleSend()
  (Unix socket HTTP)                    validates, defaults Via
                                        │
                                      outbox.Submit(sessionID, payload)
                                        stores SendRequest as json.RawMessage
                                        │
  ←─────────────────────────────────  returns {OutboxID: "abc123"}
prints "Submitted for review"
exits
                                                                             pigeon review starts
                                                                               │
                                      GET /api/outbox  ←───────────────────  fetchItems() polls every 1s
                                      handleOutboxList()                       │
                                        lists items from outbox                │
                                        resolves display names ←── daemon      │
                                          has the Slack resolvers              │
                                        returns []OutboxListItem               │
                                      ─────────────────────────→             receives enriched items
                                                                             renders with resolved names
                                                                               │
                                                                             user presses 'a' to approve
                                      POST /api/outbox/action ←─────────── {id, action: "approve"}
                                      handler.approve()
                                        unmarshals payload → SendRequest
                                        executeSend() → Slack/WhatsApp API
                                        removes from outbox
                                        notifies Claude session via SSE
                                      ─────────────────────────→             shows "✓ Approved"
```

### Key design principle

- **The daemon is the intelligent process.** It holds platform connections,
  resolvers, and the outbox. CLI and TUI are dumb clients that talk to
  the daemon over Unix socket HTTP.
