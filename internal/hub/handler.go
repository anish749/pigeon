package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	mcpserver "github.com/anish749/pigeon/internal/mcp/server"
)

// IncomingMsg is the JSON payload sent over SSE to MCP shim processes.
type IncomingMsg struct {
	Platform     string   `json:"platform"`            // "whatsapp" or "slack"
	Account      string   `json:"account"`             // phone number or workspace
	Conversation string   `json:"conversation"`        // conversation directory name or channel name
	UserID       string   `json:"user_id,omitempty"`   // Slack user ID for DM conversations
	MsgLines     []string `json:"msg_lines"`           // raw lines from store, e.g. "[2026-04-04 14:00:00 +02:00] Alice: hey"
}

var _ NotificationMsg = (*IncomingMsg)(nil)

func (i *IncomingMsg) Content() string {
	return strings.Join(i.MsgLines, "\n")
}

func (i *IncomingMsg) Meta() map[string]any {
	m := map[string]any{
		"platform":     i.Platform,
		"account":      i.Account,
		"conversation": i.Conversation,
	}
	if i.UserID != "" {
		m["user_id"] = i.UserID
	}
	return m
}

// SSEHandler returns an http.HandlerFunc that serves the SSE endpoint for
// MCP shim processes. Each connection registers a session with the hub
// and streams messages until disconnect.
func (h *Hub) SSEHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		// Parse required query params.
		sessionID := r.URL.Query().Get("session_id")
		cwd, err := url.QueryUnescape(r.URL.Query().Get("cwd"))
		if err != nil {
			http.Error(w, "invalid cwd encoding: "+err.Error(), http.StatusBadRequest)
			return
		}

		if sessionID == "" || cwd == "" {
			http.Error(w, "session_id and cwd query params are required", http.StatusBadRequest)
			return
		}

		// Channel for delivering messages to this SSE connection.
		msgCh := make(chan mcpserver.ClaudeChannelNotification, 64)
		ready := make(chan struct{})

		session := &Session{
			SessionID: sessionID,
			CWD:       cwd,
			Ready:     ready,
			Send: func(ctx context.Context, notificationMsg NotificationMsg) error {
				notification := mcpserver.ClaudeChannelNotification{
					Content: notificationMsg.Content(),
					Meta:    notificationMsg.Meta(),
				}
				select {
				case msgCh <- notification:
					return nil
				default:
					return fmt.Errorf("session %s: send buffer full", sessionID)
				}
			},
		}

		if err := h.Register(session); err != nil {
			var regErr *RegistrationError
			if errors.As(err, &regErr) {
				http.Error(w, regErr.Error(), regErr.StatusCode)
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}
		defer h.Unregister(sessionID)

		slog.Info("sse client connected", "session_id", sessionID, "cwd", cwd)

		// Set SSE headers.
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher.Flush()

		ctx := r.Context()
		close(ready) // Signal that the SSE event loop is ready to deliver messages.
		for {
			select {
			case <-ctx.Done():
				slog.Info("sse client disconnected", "session_id", sessionID)
				return
			case evt := <-msgCh:
				data, err := json.Marshal(evt)
				if err != nil {
					slog.Error("sse marshal failed", "error", err)
					continue
				}
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
				slog.Info("sse event flushed", "session_id", sessionID, "bytes", len(data))
			}
		}
	}
}
