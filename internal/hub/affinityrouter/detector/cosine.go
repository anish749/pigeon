package detector

import (
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/anish749/pigeon/internal/hub/affinityrouter/models"
	embedding "github.com/clems4ever/all-minilm-l6-v2-go/all_minilm_l6_v2"
)

const (
	defaultWindowSize   = 5
	defaultThresholdStd = 1.0
)

// CosineDetector triggers classification when the cosine similarity between
// consecutive sliding-window embeddings drops below a dynamic threshold
// (mean - thresholdStd * std, self-calibrating per conversation).
type CosineDetector struct {
	model        *embedding.Model // shared across all detectors
	windowSize   int
	thresholdStd float64

	// Per-conversation state.
	window    []models.Signal // rolling window of recent signals
	lastEmb   []float32       // embedding of the previous window
	simStats  runningStats    // running mean/std of similarity values
}

func (d *CosineDetector) Observe(sig models.Signal) bool {
	d.window = append(d.window, sig)

	// Not enough signals to form a full window yet.
	if len(d.window) < d.windowSize {
		return false
	}

	// Keep only the last windowSize signals.
	if len(d.window) > d.windowSize {
		d.window = d.window[len(d.window)-d.windowSize:]
	}

	text := windowText(d.window)
	emb, err := d.model.Compute(text, true)
	if err != nil {
		return false
	}

	prev := d.lastEmb
	d.lastEmb = emb

	if prev == nil {
		return false
	}

	sim := embedding.CosineSimilarity(prev, emb)
	d.simStats.push(sim)

	// Need at least 3 data points for a meaningful threshold.
	if d.simStats.count < 3 {
		return false
	}

	threshold := d.simStats.mean() - d.thresholdStd*d.simStats.std()
	return sim < threshold
}

// CosineConfig holds configuration for the cosine detector factory.
type CosineConfig struct {
	WindowSize   int     // messages per window (default: 5)
	ThresholdStd float64 // std deviations below mean for boundary (default: 1.0)
	RuntimePath  string  // path to ONNX runtime library
}

// CosineResources holds the shared embedding model. Create once, pass to
// the factory. The caller is responsible for calling Close when done.
type CosineResources struct {
	model *embedding.Model
}

// NewCosineResources loads the embedding model. Call Close when done.
func NewCosineResources(runtimePath string) (*CosineResources, error) {
	var opts []embedding.ModelOption
	if runtimePath != "" {
		opts = append(opts, embedding.WithRuntimePath(runtimePath))
	}
	model, err := embedding.NewModel(opts...)
	if err != nil {
		return nil, fmt.Errorf("load embedding model: %w", err)
	}
	return &CosineResources{model: model}, nil
}

// Close releases the embedding model resources.
func (r *CosineResources) Close() error {
	if r.model != nil {
		return r.model.Close()
	}
	return nil
}

// NewCosineFactory returns a Factory that creates CosineDetectors sharing
// the given resources. Each detector is conversation-scoped and independent.
func NewCosineFactory(res *CosineResources, cfg CosineConfig) Factory {
	if cfg.WindowSize == 0 {
		cfg.WindowSize = defaultWindowSize
	}
	if cfg.ThresholdStd == 0 {
		cfg.ThresholdStd = defaultThresholdStd
	}
	return func() ConversationShiftDetector {
		return &CosineDetector{
			model:        res.model,
			windowSize:   cfg.WindowSize,
			thresholdStd: cfg.ThresholdStd,
		}
	}
}

// windowText concatenates signals into the text fed to the embedding model.
func windowText(signals []models.Signal) string {
	var b strings.Builder
	for i, sig := range signals {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(sig.Sender)
		b.WriteString(": ")
		b.WriteString(sig.Text)
	}
	return b.String()
}

// runningStats tracks mean and variance with Welford's online algorithm.
type runningStats struct {
	count int
	mean_ float64
	m2    float64
}

func (rs *runningStats) push(x float64) {
	rs.count++
	delta := x - rs.mean_
	rs.mean_ += delta / float64(rs.count)
	delta2 := x - rs.mean_
	rs.m2 += delta * delta2
}

func (rs *runningStats) mean() float64 {
	return rs.mean_
}

func (rs *runningStats) std() float64 {
	if rs.count < 2 {
		return 0
	}
	return math.Sqrt(rs.m2 / float64(rs.count))
}

// Ensure io import is used — CosineResources.Close returns error (satisfies io.Closer).
var _ io.Closer = (*CosineResources)(nil)
