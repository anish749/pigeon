package hub

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/store/modelv1"
	"github.com/anish749/pigeon/internal/tailapi"
)

// tailSubscriberBufferSize is the event-channel capacity for a single
// tail client. Sized large enough to absorb short bursts while the
// client is reading; slow consumers beyond this point see drops logged
// by Broadcast.Publish.
const tailSubscriberBufferSize = 128

// TailHandler returns an http.HandlerFunc that serves the stateless
// /api/tail SSE endpoint. Each `data:` frame is one Event.
//
// The request payload (accounts filter, since timestamp) is decoded
// from query parameters via tailapi.Decode — see internal/tailapi for
// the wire contract.
//
// No cursor, no session binding, no server-side state.
func (h *Hub) TailHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		req, err := tailapi.Decode(r.URL.Query())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		filter := Filter{Accounts: req.Accounts}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		// Subscribe BEFORE historical replay so we don't miss any event
		// that lands during replay. We dedupe by timestamp against
		// replayStart when draining the live buffer.
		events, cancel := h.broadcast.Subscribe(filter, tailSubscriberBufferSize)
		defer cancel()

		slog.Info("tail client connected",
			"accounts", len(filter.Accounts), "since", req.Since)

		// Announce stream is up so the client knows setup worked.
		writeFrame(w, flusher, map[string]any{
			"kind":    "system",
			"content": fmt.Sprintf("pigeon tail connected (accounts=%d, since=%s)", len(filter.Accounts), req.Since.Format(time.RFC3339)),
			"ts":      time.Now(),
		})

		replayStart := time.Now()
		if !req.Since.IsZero() {
			if err := h.replayHistory(w, flusher, filter, req.Since); err != nil {
				slog.Warn("tail: historical replay error", "error", err)
			}
		}

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				slog.Info("tail client disconnected")
				return
			case e, ok := <-events:
				if !ok {
					return
				}
				// Skip events that predate the replay cutover — they
				// were already streamed by replayHistory.
				if !req.Since.IsZero() && e.Ts.Before(replayStart) {
					continue
				}
				writeFrame(w, flusher, e)
			}
		}
	}
}

// replayHistory streams events from disk for the given filter, starting
// at sinceTime. Uses ReadConversation per account+conversation.
func (h *Hub) replayHistory(w http.ResponseWriter, flusher http.Flusher, filter Filter, sinceTime time.Time) error {
	since := time.Since(sinceTime)
	if since < 0 {
		since = 0
	}

	accounts := filter.Accounts
	if len(accounts) == 0 {
		// No filter = every account we know about via store.
		platforms, err := h.store.ListPlatforms()
		if err != nil {
			return fmt.Errorf("list platforms: %w", err)
		}
		for _, p := range platforms {
			names, err := h.store.ListAccounts(p)
			if err != nil {
				slog.Warn("tail replay: list accounts failed", "platform", p, "error", err)
				continue
			}
			for _, n := range names {
				accounts = append(accounts, account.New(p, n))
			}
		}
	}

	for _, acct := range accounts {
		convs, err := h.store.ListConversations(acct)
		if err != nil {
			slog.Warn("tail replay: list conversations failed", "account", acct, "error", err)
			continue
		}
		for _, conv := range convs {
			df, err := h.store.ReadConversation(acct, conv, store.ReadOpts{Since: since})
			if err != nil || df == nil {
				if err != nil {
					slog.Warn("tail replay: read conversation failed",
						"account", acct, "conversation", conv, "error", err)
				}
				continue
			}
			for _, m := range df.Messages {
				e := Event{
					Kind:         EventMessage,
					Ts:           m.Ts,
					Acct:         acct,
					Platform:     acct.Platform,
					Account:      acct.Name,
					Conversation: conv,
					Content:      strings.Join(modelv1.FormatMsg(m, time.Local), "\n"),
					MsgID:        m.ID,
				}
				writeFrame(w, flusher, e)
			}
		}
	}
	return nil
}

func writeFrame(w http.ResponseWriter, flusher http.Flusher, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		slog.Error("tail: marshal frame failed", "error", err)
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}
