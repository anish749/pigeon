package toolgate

import (
	"crypto/rand"
	"encoding/json"
	"sync"
	"time"
)

// Decision is the outcome sent back to the blocking hook handler.
type Decision struct {
	Action  string // "allow", "deny", "ask"
	Reason  string
	Context string
}

// Item is a pending tool-call awaiting human approval.
type Item struct {
	ID        string        `json:"id"`
	SessionID string        `json:"session_id"`
	Input     HookInput     `json:"input"`
	CreatedAt time.Time     `json:"created_at"`
	decision  chan Decision // unexported, size-1 buffered
}

// ToolName returns the tool name from the hook input.
func (it *Item) ToolName() string { return it.Input.ToolName }

// Command extracts a human-readable command string from tool input.
// For Bash: the command field. For Read/Edit/Write: file_path. For others: raw JSON.
func (it *Item) Command() string {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(it.Input.ToolInput, &m); err != nil {
		return string(it.Input.ToolInput)
	}
	switch it.Input.ToolName {
	case "Bash":
		if v, ok := m["command"]; ok {
			var s string
			json.Unmarshal(v, &s)
			return s
		}
	case "Read", "Edit", "Write":
		if v, ok := m["file_path"]; ok {
			var s string
			json.Unmarshal(v, &s)
			return s
		}
	case "Glob":
		if v, ok := m["pattern"]; ok {
			var s string
			json.Unmarshal(v, &s)
			return s
		}
	case "Grep":
		if v, ok := m["pattern"]; ok {
			var s string
			json.Unmarshal(v, &s)
			return s
		}
	}
	return string(it.Input.ToolInput)
}

// Gate is an in-memory registry of pending tool-call approvals.
type Gate struct {
	mu    sync.RWMutex
	items map[string]*Item
	order []string
}

// NewGate creates an empty gate.
func NewGate() *Gate {
	return &Gate{items: make(map[string]*Item)}
}

// Submit registers a new hook input and returns the item (caller blocks on item.decision).
func (g *Gate) Submit(input HookInput) *Item {
	g.mu.Lock()
	defer g.mu.Unlock()
	id := shortID()
	item := &Item{
		ID:        id,
		SessionID: input.SessionID,
		Input:     input,
		CreatedAt: time.Now(),
		decision:  make(chan Decision, 1),
	}
	g.items[id] = item
	g.order = append(g.order, id)
	return item
}

// List returns all pending items in submission order.
func (g *Gate) List() []*Item {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]*Item, 0, len(g.order))
	for _, id := range g.order {
		if item, ok := g.items[id]; ok {
			out = append(out, item)
		}
	}
	return out
}

// Get returns a pending item by ID, or nil.
func (g *Gate) Get(id string) *Item {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.items[id]
}

// Resolve sends a decision to the waiting handler and removes the item.
func (g *Gate) Resolve(id string, d Decision) bool {
	g.mu.Lock()
	item, ok := g.items[id]
	if !ok {
		g.mu.Unlock()
		return false
	}
	delete(g.items, id)
	g.order = removeFromOrder(g.order, id)
	g.mu.Unlock()
	item.decision <- d
	return true
}

// Remove deletes an item without resolving. Returns false if not found.
func (g *Gate) Remove(id string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.items[id]; !ok {
		return false
	}
	delete(g.items, id)
	g.order = removeFromOrder(g.order, id)
	return true
}

func removeFromOrder(order []string, id string) []string {
	for i, v := range order {
		if v == id {
			return append(order[:i], order[i+1:]...)
		}
	}
	return order
}

func shortID() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 6)
	rand.Read(b)
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}
