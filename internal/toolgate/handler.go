package toolgate

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// NotifyFunc posts a Slack C&C message for a tool gate item.
// It should be wired to the API server's postToolGateCCMessage.
type NotifyFunc func(ctx context.Context, item *Item) error

// Handler serves the HTTP endpoints for the tool gate.
type Handler struct {
	gate    *Gate
	notify  NotifyFunc
	timeout time.Duration
}

// NewHandler creates a handler with the given gate, notification callback, and timeout.
func NewHandler(gate *Gate, notify NotifyFunc, timeout time.Duration) *Handler {
	if timeout == 0 {
		timeout = 2 * time.Minute
	}
	return &Handler{gate: gate, notify: notify, timeout: timeout}
}

// Gate returns the underlying gate for direct access.
func (h *Handler) Gate() *Gate { return h.gate }

// HandleHook is the main PreToolUse hook endpoint. It blocks until a decision arrives.
// POST /api/hook/pretooluse
func (h *Handler) HandleHook(w http.ResponseWriter, r *http.Request) {
	var input HookInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeHookResponse(w, "ask", "invalid hook input: "+err.Error(), "")
		return
	}

	item := h.gate.Submit(input)
	slog.Info("tool gate item submitted", "id", item.ID, "tool", input.ToolName, "session_id", input.SessionID)

	if h.notify != nil {
		if err := h.notify(r.Context(), item); err != nil {
			slog.Error("tool gate cc notification failed", "id", item.ID, "error", err)
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()

	select {
	case d := <-item.decision:
		slog.Info("tool gate resolved", "id", item.ID, "action", d.Action)
		writeHookResponse(w, d.Action, d.Reason, d.Context)
	case <-ctx.Done():
		h.gate.Remove(item.ID)
		slog.Info("tool gate timed out", "id", item.ID)
		writeHookResponse(w, "ask", "review timed out", "")
	}
}

// HandleList returns all pending tool gate items.
// GET /api/toolgate
func (h *Handler) HandleList(w http.ResponseWriter, r *http.Request) {
	items := h.gate.List()
	if items == nil {
		items = make([]*Item, 0)
	}
	writeJSON(w, http.StatusOK, items)
}

// ActionRequest is the body for POST /api/toolgate/action.
type ActionRequest struct {
	ID      string `json:"id"`
	Action  string `json:"action"` // "allow", "deny", "ask"
	Reason  string `json:"reason,omitempty"`
	Context string `json:"context,omitempty"`
}

// ActionResponse is the response for POST /api/toolgate/action.
type ActionResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// HandleAction resolves a pending tool gate item.
// POST /api/toolgate/action
func (h *Handler) HandleAction(w http.ResponseWriter, r *http.Request) {
	var req ActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ActionResponse{Error: "invalid request"})
		return
	}

	switch req.Action {
	case "allow", "deny", "ask":
	default:
		writeJSON(w, http.StatusBadRequest, ActionResponse{Error: "action must be allow, deny, or ask"})
		return
	}

	if !h.gate.Resolve(req.ID, Decision{Action: req.Action, Reason: req.Reason, Context: req.Context}) {
		writeJSON(w, http.StatusNotFound, ActionResponse{Error: "item not found"})
		return
	}
	writeJSON(w, http.StatusOK, ActionResponse{OK: true})
}

// Get returns a pending item by ID.
func (h *Handler) Get(id string) *Item {
	return h.gate.Get(id)
}

func writeHookResponse(w http.ResponseWriter, decision, reason, ctx string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(NewHookOutput(decision, reason, ctx))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
