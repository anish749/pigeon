// Package embedding provides a client for the Python embedding sidecar
// and a ConversationShiftDetector that uses cosine similarity drops
// to detect topic shifts.
package embedding

import (
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
)

// Client manages the Python embedding sidecar process and communicates
// with it over HTTP on a Unix domain socket. Creating a Client starts
// the sidecar; closing it shuts the process down.
type Client struct {
	socketPath string
	httpClient *http.Client
	proc       *os.Process
}

// NewClient starts the Python embedding sidecar and returns a Client
// connected to it. The caller must call Close to shut down the sidecar.
func NewClient(socketPath, modelName string) (*Client, error) {
	sidecarScript := filepath.Join(findRepoRoot(), "embed", "sidecar.py")

	cmd := exec.Command("uv", "run", sidecarScript, "--socket", socketPath, "--model", modelName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start embed sidecar: %w", err)
	}

	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socketPath)
			},
		},
	}

	c := &Client{
		socketPath: socketPath,
		httpClient: httpClient,
		proc:       cmd.Process,
	}

	if err := c.waitReady(30 * time.Second); err != nil {
		cmd.Process.Kill()
		return nil, fmt.Errorf("embed sidecar not ready: %w", err)
	}

	return c, nil
}

// Close sends SIGTERM to the sidecar process.
func (c *Client) Close() error {
	if c.proc != nil {
		return c.proc.Signal(os.Interrupt)
	}
	return nil
}

// Embed returns the embedding vector for text.
func (c *Client) Embed(ctx context.Context, text string) ([]float32, error) {
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
		Embedding []float32 `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode embed response: %w", err)
	}
	return result.Embedding, nil
}

func (c *Client) waitReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(c.socketPath); err == nil {
			conn, err := net.DialTimeout("unix", c.socketPath, time.Second)
			if err == nil {
				conn.Close()
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("socket %s not available after %s", c.socketPath, timeout)
}

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
