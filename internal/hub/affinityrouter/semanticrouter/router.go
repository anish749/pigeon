// Package semanticrouter routes signals to workstreams using embedding
// cosine similarity. Each signal is independently compared against every
// workstream's focus embedding, and routed to all workstreams above the
// similarity threshold. No caching, no batching, no affinity state.
//
// Workstream discovery and creation is handled separately (user-triggered).
// This package only handles the routing decision.
package semanticrouter

import (
	"context"
	"fmt"
	"log/slog"
	"math"

	"github.com/anish749/pigeon/internal/hub/affinityrouter/models"
)

// Embedder produces embedding vectors from text.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float64, error)
}

// WorkstreamEmbedding pairs a workstream with its pre-computed focus embedding.
type WorkstreamEmbedding struct {
	Workstream models.Workstream
	Embedding  []float64
}

// Router routes signals to workstreams by comparing signal embeddings
// against workstream focus embeddings.
type Router struct {
	embedder   Embedder
	threshold  float64
	logger     *slog.Logger
	workstreams []WorkstreamEmbedding
}

// New creates a semantic router.
//
// threshold controls the minimum cosine similarity for routing a signal
// to a workstream. Typical values: 0.3-0.5. Lower = more multi-routing,
// higher = more signals land in default.
func New(embedder Embedder, threshold float64, logger *slog.Logger) *Router {
	return &Router{
		embedder:  embedder,
		threshold: threshold,
		logger:    logger,
	}
}

// LoadWorkstreams computes and caches focus embeddings for each workstream.
// Call this after discovery or when workstreams change.
func (r *Router) LoadWorkstreams(ctx context.Context, workstreams []models.Workstream) error {
	r.workstreams = nil
	for _, ws := range workstreams {
		if ws.IsDefault() || ws.Focus == "" {
			continue
		}
		emb, err := r.embedder.Embed(ctx, ws.Focus)
		if err != nil {
			return fmt.Errorf("embed focus for %q: %w", ws.Name, err)
		}
		r.workstreams = append(r.workstreams, WorkstreamEmbedding{
			Workstream: ws,
			Embedding:  emb,
		})
		r.logger.Info("embedded workstream focus", "name", ws.Name, "id", ws.ID, "dims", len(emb))
	}
	return nil
}

// RouteResult holds the routing decision for a single signal.
type RouteResult struct {
	// WorkstreamIDs lists all workstreams the signal routes to.
	// Empty means the signal goes to the default stream.
	WorkstreamIDs []string

	// Scores maps workstream ID to cosine similarity score.
	Scores map[string]float64
}

// Route computes the embedding for a signal and compares it against all
// workstream focus embeddings. Returns the workstreams above the similarity
// threshold. Each signal is routed independently — no state, no caching.
func (r *Router) Route(ctx context.Context, sig models.Signal) (*RouteResult, error) {
	text := sig.Sender + ": " + sig.Text
	emb, err := r.embedder.Embed(ctx, text)
	if err != nil {
		return nil, fmt.Errorf("embed signal: %w", err)
	}

	result := &RouteResult{
		Scores: make(map[string]float64, len(r.workstreams)),
	}

	for _, ws := range r.workstreams {
		sim := cosineSimilarity(emb, ws.Embedding)
		result.Scores[ws.Workstream.ID] = sim
		if sim >= r.threshold {
			result.WorkstreamIDs = append(result.WorkstreamIDs, ws.Workstream.ID)
		}
	}

	r.logger.Info("routed signal",
		"sender", sig.Sender,
		"text", sig.Text,
		"matched", result.WorkstreamIDs,
		"scores", result.Scores,
	)

	return result, nil
}

// WorkstreamCount returns the number of loaded workstreams.
func (r *Router) WorkstreamCount() int {
	return len(r.workstreams)
}

func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}
