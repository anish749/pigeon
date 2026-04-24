package hub

import (
	"encoding/json"
	"errors"
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
		flusher.Flush() // commit headers immediately so the client learns the stream is up

		// Subscribe BEFORE historical replay so we don't miss any event
		// that lands during replay. Live events are deduped by timestamp
		// against replayStart when draining.
		events, cancel := h.broadcast.Subscribe(filter, tailSubscriberBufferSize)
		defer cancel()

		slog.Info("tail client connected",
			"accounts", len(filter.Accounts), "since", req.Since)

		// Send a minimal "connected" frame. This also forces the first
		// body flush on HTTP/2 and any other transport that holds
		// headers until a body byte lands.
		if err := writeFrame(w, flusher, Event{
			Kind:    EventSystem,
			Ts:      time.Now(),
			Content: "connected",
		}); err != nil {
			slog.Info("tail client disconnected before connect frame", "error", err)
			return
		}

		replayStart := time.Now()
		if !req.Since.IsZero() {
			if err := h.replayHistory(w, flusher, filter, req.Since); err != nil {
				// Replay is best-effort; tell the client what broke, then
				// keep the live stream open so they still get new events.
				slog.Warn("tail: historical replay error", "error", err)
				if werr := writeFrame(w, flusher, Event{
					Kind:    EventSystem,
					Ts:      time.Now(),
					Content: "replay error: " + err.Error(),
				}); werr != nil {
					slog.Info("tail client disconnected during replay error frame", "error", werr)
					return
				}
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
				if err := writeFrame(w, flusher, e); err != nil {
					slog.Info("tail client write failed, closing", "error", err)
					return
				}
			}
		}
	}
}

// replayHistory streams events from disk for the given filter, starting
// at sinceTime. Per-account and per-conversation read failures are
// collected and returned as a single joined error — the caller can
// surface that to the client. Write failures short-circuit the walk.
func (h *Hub) replayHistory(w http.ResponseWriter, flusher http.Flusher, filter Filter, sinceTime time.Time) error {
	since := time.Since(sinceTime)
	if since < 0 {
		since = 0
	}

	accounts := filter.Accounts
	var errs []error
	if len(accounts) == 0 {
		// No filter = every account we know about via store.
		platforms, err := h.store.ListPlatforms()
		if err != nil {
			return fmt.Errorf("list platforms: %w", err)
		}
		for _, p := range platforms {
			names, err := h.store.ListAccounts(p)
			if err != nil {
				errs = append(errs, fmt.Errorf("list accounts for platform %q: %w", p, err))
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
			errs = append(errs, fmt.Errorf("list conversations for %s: %w", acct, err))
			continue
		}
		for _, conv := range convs {
			df, err := h.store.ReadConversation(acct, conv, store.ReadOpts{Since: since})
			if err != nil {
				errs = append(errs, fmt.Errorf("read %s/%s: %w", acct, conv, err))
				continue
			}
			if df == nil {
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
				if werr := writeFrame(w, flusher, e); werr != nil {
					// Client disconnected mid-replay. Return the write
					// error — the handler will stop the stream.
					return fmt.Errorf("write replay frame: %w", werr)
				}
			}
		}
	}
	return errors.Join(errs...)
}

// writeFrame marshals v as a single SSE `data:` frame and flushes it.
// Returns the marshal or write error so the caller can decide to stop.
func writeFrame(w http.ResponseWriter, flusher http.Flusher, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal frame: %w", err)
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return fmt.Errorf("write frame: %w", err)
	}
	flusher.Flush()
	return nil
}
