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
// cosine similarity. It buffers the last N signals into a sliding window,
// embeds the window text via the sidecar, and compares against the
// previous window's embedding to detect topic shifts.
type CosineDetector struct {
	client    *Client
	logger    *slog.Logger
	threshold float64

	windowSize int
	window     []models.Signal    // current sliding window of signals
	prevEmbed  []float32          // embedding of the previous full window
}

func (d *CosineDetector) Observe(sig models.Signal) bool {
	d.window = append(d.window, sig)

	if len(d.window) < d.windowSize {
		return false
	}

	// Build window text.
	text := windowText(d.window)

	// Trim window for next call (keep last windowSize-1 signals as overlap).
	defer func() {
		d.window = d.window[1:]
	}()

	// First full window — embed and cache, no comparison yet.
	if d.prevEmbed == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		emb, err := d.client.Embed(ctx, text)
		if err != nil {
			d.logger.Warn("embed failed, falling back to no-shift", "err", err)
			return false
		}
		d.prevEmbed = emb
		return false
	}

	// Compare against previous window.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	emb, sim, err := d.client.Compare(ctx, text, d.prevEmbed)
	if err != nil {
		d.logger.Warn("compare failed, falling back to no-shift", "err", err)
		return false
	}
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
