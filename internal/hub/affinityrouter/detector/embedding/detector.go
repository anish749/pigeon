package embedding

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/anish749/pigeon/internal/hub/affinityrouter/detector"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/models"
)

// Embedder produces embedding vectors from text.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// CosineDetector implements ConversationShiftDetector using embedding
// cosine similarity. It buffers signals into a sliding window, embeds
// window text via the sidecar, and compares against the previous
// window's embedding to detect topic shifts.
//
// The threshold is self-calibrating: after minCalibration similarity
// observations, it switches from fallbackThreshold to mean - stdMultiplier*std
// computed over all observed similarities for this conversation.
type CosineDetector struct {
	embedder Embedder
	logger   *slog.Logger

	// Self-calibrating threshold using Welford's online algorithm.
	// After minCalibration observations, threshold = mean - stdMultiplier*std.
	fallbackThreshold float64
	stdMultiplier     float64
	minCalibration    int
	stats             RunningStats

	windowSize int
	window     []models.Signal
	prevEmbed  []float32
}

func (d *CosineDetector) Observe(sig models.Signal) bool {
	d.window = append(d.window, sig)

	if len(d.window) < d.windowSize {
		return false
	}

	text := windowText(d.window)

	// Trim window for next call (keep last windowSize-1 signals as overlap).
	defer func() {
		d.window = d.window[1:]
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	emb, err := d.embedder.Embed(ctx, text)
	if err != nil {
		// Embedding failed — trigger reclassification so the LLM classifier
		// handles this batch. prevEmbed is kept for the next successful embed.
		d.logger.Error("embed failed, triggering reclassification", "err", err)
		return true
	}

	// First full window — cache embedding, no comparison yet.
	if d.prevEmbed == nil {
		d.prevEmbed = emb
		return false
	}

	sim := cosineSimilarity(emb, d.prevEmbed)
	d.prevEmbed = emb
	d.stats.Observe(sim)

	threshold := d.currentThreshold()
	shifted := sim < threshold
	if shifted {
		d.logger.Info("topic shift detected",
			"sim", fmt.Sprintf("%.3f", sim),
			"threshold", fmt.Sprintf("%.3f", threshold),
			"calibrated", d.stats.N >= d.minCalibration,
		)
	}
	return shifted
}

// currentThreshold returns the self-calibrating threshold if enough
// observations have been collected, otherwise the fallback.
func (d *CosineDetector) currentThreshold() float64 {
	if d.stats.N < d.minCalibration {
		return d.fallbackThreshold
	}
	return d.stats.Mean - d.stdMultiplier*d.stats.Std()
}

// RunningStats tracks mean and standard deviation incrementally using
// Welford's online algorithm. O(1) per observation, no slice allocation.
type RunningStats struct {
	N    int
	Mean float64
	M2   float64 // sum of squared differences from the running mean
}

// Observe records a new value, updating the running mean and variance.
func (s *RunningStats) Observe(x float64) {
	s.N++
	delta := x - s.Mean
	s.Mean += delta / float64(s.N)
	delta2 := x - s.Mean
	s.M2 += delta * delta2
}

// Std returns the population standard deviation of all observed values.
func (s *RunningStats) Std() float64 {
	if s.N < 2 {
		return 0
	}
	return math.Sqrt(s.M2 / float64(s.N))
}

func cosineSimilarity(a, b []float32) float64 {
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

func windowText(signals []models.Signal) string {
	var text string
	for i, s := range signals {
		if i > 0 {
			text += "\n"
		}
		text += s.Sender + ": " + s.Text
	}
	return text
}

// NewCosineFactory returns a detector.Factory that creates CosineDetectors.
// The embedder and fallbackThreshold are shared across all detectors; each
// detector maintains its own sliding window, previous embedding, and
// self-calibrating threshold that adapts to the conversation's similarity
// distribution after 5 observations.
func NewCosineFactory(embedder Embedder, windowSize int, fallbackThreshold float64, logger *slog.Logger) detector.Factory {
	return func() detector.ConversationShiftDetector {
		return &CosineDetector{
			embedder:          embedder,
			logger:            logger,
			fallbackThreshold: fallbackThreshold,
			stdMultiplier:     1.0,
			minCalibration:    5,
			windowSize:        windowSize,
		}
	}
}
