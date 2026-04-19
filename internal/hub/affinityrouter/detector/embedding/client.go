// Package embedding provides a client for the Python embedding sidecar
// and a ConversationShiftDetector that uses cosine similarity drops
// to detect topic shifts.
package embedding

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/anish749/pigeon/internal/paths"
)

// Client manages the Python embedding sidecar process and communicates
// with it over HTTP on a Unix domain socket. Creating a Client starts
// the sidecar; closing it shuts the process down.
type Client struct {
	socketPath string
	httpClient *http.Client
	proc       *os.Process
}

const (
	defaultModelName      = "all-MiniLM-L6-v2"
	defaultStartupTimeout = 2 * time.Minute
)

// NewClient starts the Python embedding sidecar and returns a Client
// connected to it. The caller must call Close to shut down the sidecar.
func NewClient() (*Client, error) {
	socketPath := filepath.Join(paths.StateDir(), fmt.Sprintf("embed-%d.sock", os.Getpid()))
	sidecarScript := filepath.Join(findRepoRoot(), "embed", "sidecar.py")

	proc, err := startSidecar(sidecarScript, socketPath, defaultModelName, defaultStartupTimeout)
	if err != nil {
		return nil, err
	}

	return &Client{
		socketPath: socketPath,
		httpClient: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", socketPath)
				},
			},
		},
		proc: proc,
	}, nil
}

// Close sends SIGTERM to the sidecar process and removes the socket file.
func (c *Client) Close() error {
	var err error
	if c.proc != nil {
		err = c.proc.Signal(os.Interrupt)
	}
	os.Remove(c.socketPath)
	return err
}

// Embed returns the embedding vector for text.
func (c *Client) Embed(ctx context.Context, text string) ([]float64, error) {
	body, err := json.Marshal(struct {
		Text string `json:"text"`
	}{Text: text})
	if err != nil {
		return nil, fmt.Errorf("marshal embed request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://localhost/embed", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create embed request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp struct{ Error string }
		json.NewDecoder(resp.Body).Decode(&errResp)
		return nil, fmt.Errorf("embed sidecar: %s (status %d)", errResp.Error, resp.StatusCode)
	}

	var result struct {
		Embedding []float64 `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode embed response: %w", err)
	}
	return result.Embedding, nil
}

// startSidecar launches the Python sidecar process and blocks until it
// prints READY to stdout, indicating the socket is listening. If the
// sidecar does not become ready within the timeout, the process is killed.
func startSidecar(script, socketPath, modelName string, timeout time.Duration) (*os.Process, error) {
	cmd := exec.Command("uv", "run", script, "--socket", socketPath, "--model", modelName)
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start embed sidecar: %w", err)
	}

	ready := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if scanner.Text() == "READY" {
				close(ready)
				return
			}
		}
	}()

	select {
	case <-ready:
		return cmd.Process, nil
	case <-time.After(timeout):
		cmd.Process.Kill()
		return nil, fmt.Errorf("embed sidecar did not become ready within %s", timeout)
	}
}

// TODO: findRepoRoot walks up from cwd looking for embed/sidecar.py — this is
// fragile when the binary runs from a different directory or is installed
// globally. Make the sidecar script path a config parameter instead.
func findRepoRoot() string {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "embed", "sidecar.py")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "."
		}
		dir = parent
	}
}
