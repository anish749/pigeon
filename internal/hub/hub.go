// Package hub routes incoming messages from platform listeners to connected
// MCP sessions. The daemon creates a single Hub and passes it to listeners.
// MCP shim processes register themselves as sessions when they connect.
package hub

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)



// Session represents a connected MCP shim process.
type Session struct {
	// ClaudeCodePID is the process ID of the Claude Code process that
	// spawned the MCP shim. Used as the unique session identifier.
	ClaudeCodePID int
	// CWD is the working directory of the Claude Code session.
	CWD string
	// Send delivers a message to this session. Provided by the transport
	// layer (e.g. SSE) when the session registers.
	Send func(ctx context.Context, incoming IncomingMsg) error
}

// Hub manages active MCP sessions and routes incoming messages to them.
type Hub struct {
	mu       sync.RWMutex
	sessions map[int]*Session // ClaudeCodePID → session
}

// New creates an empty Hub.
func New() *Hub {
	return &Hub{
		sessions: make(map[int]*Session),
	}
}

// Register adds a session to the hub. Returns an error if the PID is already taken.
func (h *Hub) Register(s *Session) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, exists := h.sessions[s.ClaudeCodePID]; exists {
		return fmt.Errorf("session for Claude Code PID %d already registered", s.ClaudeCodePID)
	}
	h.sessions[s.ClaudeCodePID] = s
	slog.Info("session registered", "claude_code_pid", s.ClaudeCodePID, "cwd", s.CWD)
	return nil
}

// Unregister removes a session from the hub.
func (h *Hub) Unregister(pid int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if s, exists := h.sessions[pid]; exists {
		delete(h.sessions, pid)
		slog.Info("session unregistered", "claude_code_pid", pid, "cwd", s.CWD)
	}
}

// Route delivers an incoming message to the first available session.
// If no session is connected, the message is dropped with a warning.
func (h *Hub) Route(ctx context.Context, incoming IncomingMsg) error {
	h.mu.RLock()
	target := h.pickSession()
	h.mu.RUnlock()

	if target == nil {
		// TODO: start a new Claude Code session and register it, instead of dropping.
		slog.Warn("no session available, dropping message", "incoming", incoming)
		return nil
	}
	return target.Send(ctx, incoming)
}

// Sessions returns the number of active sessions.
func (h *Hub) Sessions() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.sessions)
}

// pickSession returns the first session found. Caller must hold at least a read lock.
func (h *Hub) pickSession() *Session {
	for _, s := range h.sessions {
		return s
	}
	return nil
}
