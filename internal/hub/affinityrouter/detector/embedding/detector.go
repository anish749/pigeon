package embedding

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/anish749/pigeon/internal/hub/affinityrouter/detector"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/models"
)

// CosineDetector implements ConversationShiftDetector using embedding
// cosine similarity. It buffers signals into a sliding window, embeds
// window text via the sidecar, and compares against the previous
// window's embedding to detect topic shifts.
type CosineDetector struct {
	client    *Client
	logger    *slog.Logger
	threshold float64

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

	emb, err := d.client.Embed(ctx, text)
	if err != nil {
		d.logger.Warn("embed failed, falling back to no-shift", "err", err)
		return false
	}

	// First full window — cache embedding, no comparison yet.
	if d.prevEmbed == nil {
		d.prevEmbed = emb
		return false
	}

	sim := CosineSimilarity(emb, d.prevEmbed)
	d.prevEmbed = emb

	shifted := sim < d.threshold
	if shifted {
		d.logger.Info("topic shift detected",
			"sim", fmt.Sprintf("%.3f", sim),
			"threshold", fmt.Sprintf("%.3f", d.threshold),
		)
	}
	return shifted
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
// The client and threshold are shared across all detectors; each detector
// maintains its own sliding window and previous embedding.
func NewCosineFactory(client *Client, windowSize int, threshold float64, logger *slog.Logger) detector.Factory {
	return func() detector.ConversationShiftDetector {
		return &CosineDetector{
			client:     client,
			logger:     logger,
			threshold:  threshold,
			windowSize: windowSize,
		}
	}
}
