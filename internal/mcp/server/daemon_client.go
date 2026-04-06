package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	daemonclient "github.com/anish749/pigeon/internal/daemon/client"
)

// rejectedError is returned when the daemon permanently rejects this session.
// The MCP client should not retry.
type rejectedError struct {
	reason string
}

func (e *rejectedError) Error() string {
	return e.reason
}

type ClaudeChannelNotification struct {
	Content string `json:"content"`
	// Meta must be non-nil (at minimum an empty map). Claude Code ignores
	// channel notifications where meta serializes as null instead of {}.
	Meta map[string]any `json:"meta"`
}

func (n *ClaudeChannelNotification) AsMap() map[string]any {
	return map[string]any{
		"content": n.Content,
		"meta":    n.Meta,
	}
}

func NewClaudeChannelErrorNotification(err error) *ClaudeChannelNotification {
	return &ClaudeChannelNotification{
		Content: "pigeon channel error: " + err.Error(),
		Meta:    map[string]any{},
	}
}

// pigeonDaemonStreamingClient manages the SSE connection to the pigeon daemon and forwards
// incoming messages as MCP channel notifications.
type pigeonDaemonStreamingClient struct {
	client    *daemonclient.PgnHTTPClient
	sessionID string
	cwd       string
	notify    func(notification *ClaudeChannelNotification)
}

// startPigeonDaemonStream connects to the daemon's SSE endpoint and forwards
// incoming messages via notify. Reconnects automatically in a background
// goroutine. The provided context should be long-lived (not request-scoped).
// Returns an error if initial setup fails.
func startPigeonDaemonStream(ctx context.Context, notify func(*ClaudeChannelNotification)) error {
	sessionID := os.Getenv("PIGEON_SESSION_ID")
	if sessionID == "" {
		return fmt.Errorf("PIGEON_SESSION_ID not set — launch via 'pigeon claude' to set it")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	ds := &pigeonDaemonStreamingClient{
		client:    daemonclient.DefaultPgnHTTPClient,
		sessionID: sessionID,
		cwd:       cwd,
		notify:    notify,
	}
	go ds.run(ctx)
	return nil
}

// run connects to the daemon's SSE endpoint and forwards messages.
// Reconnects automatically on transient failures. Stops on permanent rejection.
func (ds *pigeonDaemonStreamingClient) run(ctx context.Context) {
	// Claude Code sends notifications/initialized before its channel notification
	// reader is fully wired up. If we connect to the daemon SSE stream immediately,
	// the hello message arrives within milliseconds and is silently dropped because
	// the client isn't listening yet. A short delay gives the client time to finish
	// initialization. Empirically, 2 seconds is sufficient; without any delay the
	// first notification is consistently lost.
	select {
	case <-time.After(2 * time.Second):
	case <-ctx.Done():
		return
	}

	for {
		err := ds.connect(ctx)
		if ctx.Err() != nil {
			return
		}

		var rejected *rejectedError
		if errors.As(err, &rejected) {
			slog.Error("session rejected by daemon, stopping", "reason", rejected.reason)
			ds.notify(NewClaudeChannelErrorNotification(fmt.Errorf("session rejected by daemon: %w", err)))
			return
		}

		slog.Warn("sse connection lost, reconnecting", "error", err)
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			return
		}
	}
}

func (ds *pigeonDaemonStreamingClient) connect(ctx context.Context) error {
	reqURL := fmt.Sprintf("http://pigeon/api/events?session_id=%s&cwd=%s", url.QueryEscape(ds.sessionID), url.QueryEscape(ds.cwd))

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	resp, err := ds.client.Do(req)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("unexpected status %d (and failed to read body: %w)", resp.StatusCode, err)
		}
		msg := strings.TrimSpace(string(body))
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			return &rejectedError{reason: msg}
		}
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, msg)
	}

	slog.Info("sse connected to daemon")

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		var notification ClaudeChannelNotification
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &notification); err != nil {
			slog.Warn("sse parse error", "error", err)
			ds.notify(NewClaudeChannelErrorNotification(fmt.Errorf("failed to parse daemon event: %w", err)))
			continue
		}

		slog.Info("forwarding notification", "content_len", len(notification.Content), "meta", notification.Meta)
		ds.notify(&notification)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read stream: %w", err)
	}
	return fmt.Errorf("stream closed")
}
