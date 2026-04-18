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

	proc, err := startSidecar(sidecarScript, socketPath, modelName)
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

// startSidecar launches the Python sidecar process and blocks until it
// prints READY to stdout, indicating the socket is listening.
func startSidecar(script, socketPath, modelName string) (*os.Process, error) {
	cmd := exec.Command("uv", "run", script, "--socket", socketPath, "--model", modelName)
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start embed sidecar: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		if scanner.Text() == "READY" {
			return cmd.Process, nil
		}
	}

	cmd.Process.Kill()
	return nil, fmt.Errorf("embed sidecar exited without becoming ready")
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
