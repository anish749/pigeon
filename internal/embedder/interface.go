package embedder

import "context"

// Embedder embeds text and manages the underlying sidecar lifecycle.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float64, error)
	Close() error
}
