package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
)

// SendFunc executes a send from a raw JSON payload (the stored SendRequest).
// Returns true on success, or false with an error message.
type SendFunc func(ctx context.Context, payload json.RawMessage) (ok bool, errMsg string)

// NotifyFunc sends a text notification to a Claude session by session ID.
type NotifyFunc func(sessionID, text string) error

// Handler provides HTTP handlers for outbox review operations.
type Handler struct {
	outbox *Outbox
	send   SendFunc
	notify NotifyFunc
}

// NewHandler creates an outbox Handler wired to the given send and notify callbacks.
func NewHandler(ob *Outbox, send SendFunc, notify NotifyFunc) *Handler {
	return &Handler{outbox: ob, send: send, notify: notify}
}

// ActionRequest is the payload for POST /api/outbox/action.
type ActionRequest struct {
	ID     string `json:"id"`
	Action string `json:"action"` // "approve", "feedback", "dismiss", or "set_via"
	Note   string `json:"note,omitempty"`
	Via    string `json:"via,omitempty"`
}

// ActionResponse is the response for POST /api/outbox/action.
type ActionResponse struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	Warning string `json:"warning,omitempty"`
}

// Get returns a single outbox item by ID, or nil if not found.
func (h *Handler) Get(id string) *Item {
	return h.outbox.Get(id)
}

// Approve sends the item, removes it from the outbox, and notifies the
// originating session. Returns (true, "") on success, or (false, errMsg)
// if the send failed.
func (h *Handler) Approve(ctx context.Context, item *Item) (bool, string) {
	ok, errMsg := h.send(ctx, item.Payload)
	h.outbox.Remove(item.ID)

	if !ok {
		slog.Error("outbox: send failed on approve", "id", item.ID, "session_id", item.SessionID, "error", errMsg)
		notifyMsg := fmt.Sprintf("[outbox] Send failed (ID: %s): %s", item.ID, errMsg)
		if err := h.notify(item.SessionID, notifyMsg); err != nil {
			slog.Error("outbox: failed to notify session of send error", "id", item.ID, "session_id", item.SessionID, "error", err)
		}
		return false, errMsg
	}

	msg := fmt.Sprintf("[outbox] Approved and sent (ID: %s)", item.ID)
	if err := h.notify(item.SessionID, msg); err != nil {
		slog.Error("outbox: failed to notify session of approval", "id", item.ID, "session_id", item.SessionID, "error", err)
	}
	slog.Info("outbox item approved and sent", "id", item.ID, "session_id", item.SessionID)
	return true, ""
}

// Feedback delivers a note to the originating session and removes the item
// from the outbox.
func (h *Handler) Feedback(item *Item, note string) error {
	if note == "" {
		return fmt.Errorf("note is required for feedback")
	}
	if item.SessionID == "" {
		return fmt.Errorf("item has no session to deliver feedback to")
	}

	msg := fmt.Sprintf("[outbox] Feedback on message %s: %s", item.ID, note)
	if err := h.notify(item.SessionID, msg); err != nil {
		slog.Error("outbox: failed to notify session of feedback", "id", item.ID, "session_id", item.SessionID, "error", err)
		return fmt.Errorf("session not connected — feedback not delivered, item kept in outbox")
	}

	h.outbox.Remove(item.ID)
	slog.Info("outbox feedback delivered", "id", item.ID, "session_id", item.SessionID)
	return nil
}

// HandleList returns all pending outbox items as JSON.
func (h *Handler) HandleList(w http.ResponseWriter, r *http.Request) {
	items := h.outbox.List()
	if items == nil {
		items = []*Item{}
	}
	writeJSON(w, http.StatusOK, items)
}

// HandleAction routes approve/feedback actions on a specific outbox item.
func (h *Handler) HandleAction(w http.ResponseWriter, r *http.Request) {
	var req ActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ActionResponse{Error: "invalid JSON: " + err.Error()})
		return
	}

	item := h.outbox.Get(req.ID)
	if item == nil {
		writeJSON(w, http.StatusNotFound, ActionResponse{Error: "item not found"})
		return
	}

	switch req.Action {
	case "approve":
		h.approve(w, r, item)
	case "feedback":
		h.feedback(w, item, req.Note)
	case "dismiss":
		h.dismiss(w, item)
	case "set_via":
		h.setVia(w, item, req.Via)
	default:
		writeJSON(w, http.StatusBadRequest, ActionResponse{Error: "action must be 'approve', 'feedback', 'dismiss', or 'set_via'"})
	}
}

func (h *Handler) approve(w http.ResponseWriter, r *http.Request, item *Item) {
	ok, errMsg := h.Approve(r.Context(), item)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, ActionResponse{Error: errMsg})
		return
	}
	writeJSON(w, http.StatusOK, ActionResponse{OK: true})
}

func (h *Handler) feedback(w http.ResponseWriter, item *Item, note string) {
	if err := h.Feedback(item, note); err != nil {
		writeJSON(w, http.StatusBadRequest, ActionResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, ActionResponse{OK: true})
}

func (h *Handler) dismiss(w http.ResponseWriter, item *Item) {
	h.outbox.Remove(item.ID)

	msg := fmt.Sprintf("[outbox] Dismissed (ID: %s)", item.ID)
	if item.SessionID != "" {
		if err := h.notify(item.SessionID, msg); err != nil {
			slog.Error("outbox: failed to notify session of dismissal", "id", item.ID, "session_id", item.SessionID, "error", err)
		}
	}

	slog.Info("outbox item dismissed", "id", item.ID, "session_id", item.SessionID)
	writeJSON(w, http.StatusOK, ActionResponse{OK: true})
}

func (h *Handler) setVia(w http.ResponseWriter, item *Item, via string) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(item.Payload, &payload); err != nil {
		writeJSON(w, http.StatusInternalServerError, ActionResponse{Error: "parse payload: " + err.Error()})
		return
	}
	viaJSON, err := json.Marshal(via)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ActionResponse{Error: "marshal via: " + err.Error()})
		return
	}
	payload["via"] = viaJSON
	updated, err := json.Marshal(payload)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ActionResponse{Error: "marshal payload: " + err.Error()})
		return
	}
	h.outbox.UpdatePayload(item.ID, updated)
	slog.Info("outbox item via updated", "id", item.ID, "via", via)
	writeJSON(w, http.StatusOK, ActionResponse{OK: true})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
