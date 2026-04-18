// Package embedding provides a client for the Python embedding sidecar
// and a ConversationShiftDetector that uses cosine similarity drops
// to detect topic shifts.
package embedding

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
)

// Client communicates with the Python embedding sidecar over a Unix socket.
type Client struct {
	socketPath string
}

// NewClient returns a Client that connects to the sidecar at socketPath.
// It does not hold a persistent connection — each call dials, sends, and closes.
func NewClient(socketPath string) *Client {
	return &Client{socketPath: socketPath}
}

type embedRequest struct {
	Text          string    `json:"text"`
	PrevEmbedding []float32 `json:"prev_embedding,omitempty"`
}

type embedResponse struct {
	Embedding []float32 `json:"embedding"`
	Sim       *float64  `json:"sim,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// Embed returns the embedding vector for text.
func (c *Client) Embed(ctx context.Context, text string) ([]float32, error) {
	resp, err := c.call(ctx, embedRequest{Text: text})
	if err != nil {
		return nil, err
	}
	return resp.Embedding, nil
}

// Compare embeds text and returns cosine similarity against prevEmbedding.
func (c *Client) Compare(ctx context.Context, text string, prevEmbedding []float32) (embedding []float32, similarity float64, err error) {
	resp, err := c.call(ctx, embedRequest{Text: text, PrevEmbedding: prevEmbedding})
	if err != nil {
		return nil, 0, err
	}
	if resp.Sim == nil {
		return resp.Embedding, 0, fmt.Errorf("sidecar returned no similarity")
	}
	return resp.Embedding, *resp.Sim, nil
}

func (c *Client) call(ctx context.Context, req embedRequest) (embedResponse, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", c.socketPath)
	if err != nil {
		return embedResponse{}, fmt.Errorf("dial embed sidecar: %w", err)
	}
	defer conn.Close()

	data, err := json.Marshal(req)
	if err != nil {
		return embedResponse{}, fmt.Errorf("marshal embed request: %w", err)
	}
	data = append(data, '\n')

	if _, err := conn.Write(data); err != nil {
		return embedResponse{}, fmt.Errorf("write embed request: %w", err)
	}

	// Signal we're done writing so the sidecar sees EOF after the newline.
	if uc, ok := conn.(*net.UnixConn); ok {
		uc.CloseWrite()
	}

	var resp embedResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return embedResponse{}, fmt.Errorf("decode embed response: %w", err)
	}
	if resp.Error != "" {
		return embedResponse{}, fmt.Errorf("embed sidecar: %s", resp.Error)
	}
	return resp, nil
}
