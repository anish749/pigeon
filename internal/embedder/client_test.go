package embedder

import (
	"context"
	"testing"
	"time"
)

func newTestClient(t *testing.T) *Client {
	t.Helper()
	client, err := NewClient()
	if err != nil {
		t.Fatalf("start embedding sidecar: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

func TestClient(t *testing.T) {
	client := newTestClient(t)

	t.Run("embed returns 384 dimensions", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		emb, err := client.Embed(ctx, "hello world")
		if err != nil {
			t.Fatalf("Embed: %v", err)
		}
		if len(emb) != 384 {
			t.Errorf("expected 384 dimensions, got %d", len(emb))
		}
	})

	t.Run("similarity", func(t *testing.T) {
		tests := []struct {
			name   string
			a, b   string
			minSim float64
			maxSim float64
		}{
			{
				name:   "similar texts have high similarity",
				a:      "deploy the authentication service to production",
				b:      "push the auth service to prod",
				minSim: 0.5,
				maxSim: 1.0,
			},
			{
				name:   "unrelated texts have low similarity",
				a:      "the cat sat on the mat",
				b:      "quarterly revenue forecast for Q3",
				minSim: -1.0,
				maxSim: 0.5,
			},
			{
				name:   "identical texts have similarity near 1",
				a:      "fix the login bug in the auth module",
				b:      "fix the login bug in the auth module",
				minSim: 0.99,
				maxSim: 1.0,
			},
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				embA, err := client.Embed(ctx, tt.a)
				if err != nil {
					t.Fatalf("Embed(%q): %v", tt.a, err)
				}
				embB, err := client.Embed(ctx, tt.b)
				if err != nil {
					t.Fatalf("Embed(%q): %v", tt.b, err)
				}
				sim := CosineSimilarity(embA, embB)
				if sim < tt.minSim || sim > tt.maxSim {
					t.Errorf("CosineSimilarity = %.3f, want [%.2f, %.2f]", sim, tt.minSim, tt.maxSim)
				}
			})
		}
	})
}
