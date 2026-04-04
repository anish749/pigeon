package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
)

// IncomingMsg is the JSON payload sent over SSE to MCP shim processes.
type IncomingMsg struct {
	Platform     string `json:"platform"`     // "whatsapp" or "slack"
	Account      string `json:"account"`      // phone number or workspace
	Conversation string `json:"conversation"` // conversation directory name or channel name
	Sender       string `json:"sender"`       // display name of the person who sent the message
	Text         string `json:"text"`         // message text
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
		pidStr := r.URL.Query().Get("pid")
		cwd, err := url.QueryUnescape(r.URL.Query().Get("cwd"))
		if err != nil {
			http.Error(w, "invalid cwd encoding: "+err.Error(), http.StatusBadRequest)
			return
		}

		if pidStr == "" || cwd == "" {
			http.Error(w, "pid and cwd query params are required", http.StatusBadRequest)
			return
		}

		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			http.Error(w, "pid must be an integer", http.StatusBadRequest)
			return
		}

		// Channel for delivering messages to this SSE connection.
		msgCh := make(chan IncomingMsg, 64)

		session := &Session{
			ClaudeCodePID: pid,
			CWD:           cwd,
			Send: func(ctx context.Context, incoming IncomingMsg) error {
				select {
				case msgCh <- incoming:
					return nil
				default:
					return fmt.Errorf("session %d: send buffer full", pid)
				}
			},
		}

		if err := h.Register(session); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		defer h.Unregister(pid)

		slog.Info("sse client connected", "claude_code_pid", pid, "cwd", cwd)

		// Set SSE headers.
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher.Flush()

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				slog.Info("sse client disconnected", "claude_code_pid", pid)
				return
			case evt := <-msgCh:
				data, err := json.Marshal(evt)
				if err != nil {
					slog.Error("sse marshal failed", "error", err)
					continue
				}
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
		}
	}
}
