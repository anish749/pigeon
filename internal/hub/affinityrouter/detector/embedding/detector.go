package embedding

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gonum.org/v1/gonum/floats"
	"gonum.org/v1/gonum/stat"

	"github.com/anish749/pigeon/internal/hub/affinityrouter/detector"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/models"
)

// Embedder produces embedding vectors from text.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float64, error)
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

	// Self-calibrating threshold parameters.
	fallbackThreshold float64
	stdMultiplier     float64
	minCalibration    int
	sims              []float64

	windowSize     int
	window         []models.Signal
	prevEmbed      []float64
	prevWindowText string
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
		d.prevWindowText = text
		d.logger.Info("cosine: first window cached",
			"conversation", sig.Conversation,
			"account", sig.Account.Display(),
			"window_text", text,
		)
		return false
	}

	sim := CosineSimilarity(emb, d.prevEmbed)
	prevText := d.prevWindowText
	d.prevEmbed = emb
	d.prevWindowText = text
	d.sims = append(d.sims, sim)

	threshold := d.currentThreshold()
	shifted := sim < threshold
	d.logger.Info("cosine: similarity check",
		"conversation", sig.Conversation,
		"account", sig.Account.Display(),
		"sim", fmt.Sprintf("%.3f", sim),
		"threshold", fmt.Sprintf("%.3f", threshold),
		"shifted", shifted,
		"calibrated", len(d.sims) >= d.minCalibration,
		"observations", len(d.sims),
		"prev_window", prevText,
		"curr_window", text,
	)
	return shifted
}

// currentThreshold returns the self-calibrating threshold if enough
// observations have been collected, otherwise the fallback.
func (d *CosineDetector) currentThreshold() float64 {
	if len(d.sims) < d.minCalibration {
		return d.fallbackThreshold
	}
	mean, std := stat.PopMeanStdDev(d.sims, nil)
	return mean - d.stdMultiplier*std
}

// CosineSimilarity returns the cosine similarity between two vectors
// using gonum's BLAS-optimized Dot and Norm.
func CosineSimilarity(a, b []float64) float64 {
	denom := floats.Norm(a, 2) * floats.Norm(b, 2)
	if denom == 0 {
		return 0
	}
	return floats.Dot(a, b) / denom
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

const (
	defaultWindowSize        = 5
	defaultFallbackThreshold = 0.6
	defaultStdMultiplier     = 1.0
	defaultMinCalibration    = 5
)

// NewCosineFactory returns a detector.Factory that creates CosineDetectors.
// The embedder is shared across all detectors; each detector maintains its
// own sliding window, previous embedding, and self-calibrating threshold
// that adapts to the conversation's similarity distribution.
func NewCosineFactory(embedder Embedder, logger *slog.Logger) detector.Factory {
	return func() detector.ConversationShiftDetector {
		return &CosineDetector{
			embedder:          embedder,
			logger:            logger,
			fallbackThreshold: defaultFallbackThreshold,
			stdMultiplier:     defaultStdMultiplier,
			minCalibration:    defaultMinCalibration,
			windowSize:        defaultWindowSize,
		}
	}
}

