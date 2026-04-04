package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/anish/claude-msg-utils/internal/hub"
)

// daemonStream manages the SSE connection to the pigeon daemon and forwards
// incoming messages as MCP channel notifications.
type daemonStream struct {
	socketPath string
	sessionID  string
	cwd        string
	notify     func(incoming hub.IncomingMsg) error
}

// startDaemonStream connects to the daemon's SSE endpoint and forwards
// incoming messages via notify. Reconnects automatically in a background
// goroutine. Returns an error if initial setup fails.
func startDaemonStream(ctx context.Context, socketPath string, notify func(hub.IncomingMsg) error) error {
	sessionID := os.Getenv("PIGEON_SESSION_ID")
	if sessionID == "" {
		return fmt.Errorf("PIGEON_SESSION_ID not set — launch via 'pigeon claude' to set it")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	ds := &daemonStream{
		socketPath: socketPath,
		sessionID:  sessionID,
		cwd:        cwd,
		notify:     notify,
	}
	go ds.run(ctx)
	return nil
}

// run connects to the daemon's SSE endpoint and forwards messages.
// Reconnects automatically on failure. Blocks until ctx is cancelled.
func (ds *daemonStream) run(ctx context.Context) {
	for {
		err := ds.connect(ctx)
		if ctx.Err() != nil {
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

func (ds *daemonStream) connect(ctx context.Context) error {
	reqURL := fmt.Sprintf("http://pigeon/api/events?session_id=%s&cwd=%s", url.QueryEscape(ds.sessionID), url.QueryEscape(ds.cwd))

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", ds.socketPath)
			},
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	slog.Info("sse connected to daemon", "session_id", ds.sessionID, "cwd", ds.cwd)

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		var incoming hub.IncomingMsg
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &incoming); err != nil {
			slog.Warn("sse parse error", "error", err)
			continue
		}

		if err := ds.notify(incoming); err != nil {
			slog.Error("channel notification failed", "error", err)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read stream: %w", err)
	}
	return fmt.Errorf("stream closed")
}
