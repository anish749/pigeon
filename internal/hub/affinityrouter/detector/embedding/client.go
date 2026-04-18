// Package embedding provides a client for the Python embedding sidecar
// and a ConversationShiftDetector that uses cosine similarity drops
// to detect topic shifts.
package embedding

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Client manages the Python embedding sidecar process and communicates
// with it over a Unix domain socket. Creating a Client starts the sidecar;
// closing it shuts the process down.
type Client struct {
	socketPath string
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

	c := &Client{
		socketPath: socketPath,
		proc:       cmd.Process,
	}

	// Wait for the socket to appear.
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
	req := struct {
		Text string `json:"text"`
	}{Text: text}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal embed request: %w", err)
	}
	data = append(data, '\n')

	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", c.socketPath)
	if err != nil {
		return nil, fmt.Errorf("dial embed sidecar: %w", err)
	}
	defer conn.Close()

	if _, err := conn.Write(data); err != nil {
		return nil, fmt.Errorf("write embed request: %w", err)
	}
	if uc, ok := conn.(*net.UnixConn); ok {
		uc.CloseWrite()
	}

	var resp struct {
		Embedding []float32 `json:"embedding"`
		Error     string    `json:"error,omitempty"`
	}
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode embed response: %w", err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("embed sidecar: %s", resp.Error)
	}
	return resp.Embedding, nil
}

// CosineSimilarity returns the cosine similarity between two vectors.
func CosineSimilarity(a, b []float32) float64 {
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}

func (c *Client) waitReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(c.socketPath); err == nil {
			// Socket file exists — try a test connection.
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
	// Walk up from executable or cwd to find embed/sidecar.py.
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
