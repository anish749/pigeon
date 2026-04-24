package commands

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	daemonclient "github.com/anish749/pigeon/internal/daemon/client"
	"github.com/anish749/pigeon/internal/tailapi"
)

// monitorReadBufferSize caps the SSE scanner buffer. A single `data:`
// frame for a slack message can exceed the default 64 KiB bufio limit
// once rich blocks and metadata are included, so we size up.
const monitorReadBufferSize = 1024 * 1024

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

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), monitorReadBufferSize)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		if _, err := fmt.Fprintln(out, strings.TrimPrefix(line, "data: ")); err != nil {
			return fmt.Errorf("write frame to out: %w", err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read stream: %w", err)
	}
	return nil
}
