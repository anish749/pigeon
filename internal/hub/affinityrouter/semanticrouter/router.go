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

	"github.com/anish749/pigeon/internal/hub/affinityrouter/detector/embedding"
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
	embedder    Embedder
	threshold   float64
	logger      *slog.Logger
	workstreams []WorkstreamEmbedding
	defaultWSID string
}

// New creates a semantic router.
//
// threshold controls the minimum cosine similarity for routing a signal
// to a workstream. Typical values: 0.3-0.5. Lower = more multi-routing,
// higher = more signals land in default.
//
// defaultWSID is the workstream ID used when no workstream matches above
// the threshold.
func New(embedder Embedder, threshold float64, defaultWSID string, logger *slog.Logger) *Router {
	return &Router{
		embedder:    embedder,
		threshold:   threshold,
		defaultWSID: defaultWSID,
		logger:      logger,
	}
}

// LoadWorkstreams computes and caches focus embeddings for each workstream.
// Call this after discovery or when workstreams change.
func (r *Router) LoadWorkstreams(ctx context.Context, workstreams []models.Workstream) error {
	r.workstreams = nil
	for _, ws := range workstreams {
		if ws.IsDefault() || ws.Focus == "" {
			r.logger.Warn("skipping workstream", "name", ws.Name, "id", ws.ID, "reason_default", ws.IsDefault(), "reason_empty_focus", ws.Focus == "")
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

// Route computes the embedding for a signal and compares it against all
// workstream focus embeddings. Returns a routing decision with all
// workstreams above the similarity threshold, or the default workstream
// if none match. Each signal is routed independently — no state, no caching.
func (r *Router) Route(ctx context.Context, sig models.Signal) (models.RoutingDecision, error) {
	text := sig.Sender + ": " + sig.Text
	emb, err := r.embedder.Embed(ctx, text)
	if err != nil {
		return models.RoutingDecision{}, fmt.Errorf("embed signal: %w", err)
	}

	var matched []string
	scores := make(map[string]float64, len(r.workstreams))

	for _, ws := range r.workstreams {
		sim := embedding.CosineSimilarity(emb, ws.Embedding)
		scores[ws.Workstream.ID] = sim
		if sim >= r.threshold {
			matched = append(matched, ws.Workstream.ID)
		}
	}

	if len(matched) == 0 {
		matched = []string{r.defaultWSID}
	}

	decision := models.RoutingDecision{
		SignalID:      sig.ID,
		WorkstreamIDs: matched,
		Ts:            sig.Ts,
	}

	r.logger.Info("routed signal", "sender", sig.Sender, "text", sig.Text, "matched", decision.WorkstreamIDs, "scores", scores)

	return decision, nil
}
