package detector

import (
	"time"

	"github.com/anish749/pigeon/internal/hub/affinityrouter/models"
)

// BurstGapDetector triggers classification when the time gap between
// consecutive signals exceeds the configured threshold.
type BurstGapDetector struct {
	gap    time.Duration
	lastTs time.Time
	seen   bool
}

func (d *BurstGapDetector) Observe(sig models.Signal) bool {
	if !d.seen {
		d.seen = true
		d.lastTs = sig.Ts
		return false
	}
	shifted := sig.Ts.Sub(d.lastTs) >= d.gap
	d.lastTs = sig.Ts
	return shifted
}

// NewBurstGapFactory returns a Factory that creates BurstGapDetectors
// with the given gap duration.
func NewBurstGapFactory(gap time.Duration) Factory {
	return func() ConversationShiftDetector {
		return &BurstGapDetector{gap: gap}
	}
}
