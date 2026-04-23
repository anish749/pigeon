package commands

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
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
func RunMonitor(ctx context.Context, req tailapi.Request, out io.Writer) error {
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
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("daemon returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	bw := bufio.NewWriter(out)
	defer bw.Flush()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), monitorReadBufferSize)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		if _, err := fmt.Fprintln(bw, strings.TrimPrefix(line, "data: ")); err != nil {
			return fmt.Errorf("write frame: %w", err)
		}
		if err := bw.Flush(); err != nil {
			return fmt.Errorf("flush: %w", err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read stream: %w", err)
	}
	return nil
}
