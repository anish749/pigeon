package commands

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	daemonclient "github.com/anish749/pigeon/internal/daemon/client"
	"github.com/anish749/pigeon/internal/tailapi"
)

// RunMonitor opens an SSE stream to the daemon's /api/tail endpoint and
// writes each JSON frame to out as a single line. Blocks until the
// server closes the connection or ctx is cancelled.
//
// out is *os.File rather than io.Writer: os.File has no Go-level buffering,
// so each Fprintln is a direct write syscall — events are never held in a
// Go buffer before reaching the pipe consumer.
func RunMonitor(ctx context.Context, req tailapi.Request, out *os.File) error {
	q, err := req.Encode()
	if err != nil {
		return fmt.Errorf("encode tail request: %w", err)
	}

	reqURL := "http://pigeon/api/tail"
	if encoded := q.Encode(); encoded != "" {
		reqURL += "?" + encoded
	}

	httpReq, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := daemonclient.DefaultPgnHTTPClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("daemon returned %d (failed to read body: %w)", resp.StatusCode, readErr)
		}
		return fmt.Errorf("daemon returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	// bufio.Reader.ReadString grows its return allocation to whatever the
	// line actually is — no fixed cap to tune, no OOM guard beyond the OS.
	// Internal 4 KiB read buffer just batches syscalls; not a latency source.
	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			trimmed := strings.TrimRight(line, "\r\n")
			if payload, ok := strings.CutPrefix(trimmed, "data: "); ok {
				if _, werr := fmt.Fprintln(out, payload); werr != nil {
					return fmt.Errorf("write frame to out: %w", werr)
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("read stream: %w", err)
		}
	}
}
