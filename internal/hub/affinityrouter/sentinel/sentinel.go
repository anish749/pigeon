// Package sentinel detects topic shifts in conversation buffers using
// cosine similarity between sliding window embeddings.
//
// It acts as a cheap, local check that decides whether the LLM classifier
// needs to be invoked. When the similarity between consecutive windows
// drops below a dynamic threshold, the sentinel signals a topic shift.
package sentinel

import (
	"fmt"
	"math"
	"strings"

	"github.com/anish749/pigeon/internal/hub/affinityrouter/models"
	embedding "github.com/clems4ever/all-minilm-l6-v2-go/all_minilm_l6_v2"
)

// DefaultWindowSize is the number of messages per sliding window.
const DefaultWindowSize = 5

// DefaultThresholdStd is the number of standard deviations below the mean
// cosine similarity that marks a topic boundary.
const DefaultThresholdStd = 1.0

// Sentinel detects topic shifts using embedding cosine similarity.
type Sentinel struct {
	model        *embedding.Model
	windowSize   int
	thresholdStd float64

	// Per-conversation embedding history: last window embedding.
	lastEmbeddings map[models.ConversationKey][]float32

	// Running similarity stats per conversation for dynamic thresholding.
	simStats map[models.ConversationKey]*runningStats

	stats Stats
}

// Stats tracks sentinel activity.
type Stats struct {
	TopicShiftsDetected int // times cosine drop triggered classification
	EmbeddingsComputed  int // total embedding calls
	SimilarityChecks    int // total similarity comparisons
}

// Config holds sentinel configuration.
type Config struct {
	WindowSize   int     // messages per window (default: 5)
	ThresholdStd float64 // std deviations below mean for boundary (default: 1.0)
	RuntimePath  string  // path to ONNX runtime library
}

// New creates a Sentinel with the given config.
// The ONNX runtime library must be available at RuntimePath.
func New(cfg Config) (*Sentinel, error) {
	if cfg.WindowSize == 0 {
		cfg.WindowSize = DefaultWindowSize
	}
	if cfg.ThresholdStd == 0 {
		cfg.ThresholdStd = DefaultThresholdStd
	}

	var opts []embedding.ModelOption
	if cfg.RuntimePath != "" {
		opts = append(opts, embedding.WithRuntimePath(cfg.RuntimePath))
	}

	model, err := embedding.NewModel(opts...)
	if err != nil {
		return nil, fmt.Errorf("load embedding model: %w", err)
	}

	return &Sentinel{
		model:          model,
		windowSize:     cfg.WindowSize,
		thresholdStd:   cfg.ThresholdStd,
		lastEmbeddings: make(map[models.ConversationKey][]float32),
		simStats:       make(map[models.ConversationKey]*runningStats),
	}, nil
}

// Close releases the embedding model resources.
func (s *Sentinel) Close() error {
	return s.model.Close()
}

// TopicShifted checks whether the current buffer shows a topic shift
// compared to the previous window for this conversation.
//
// It assembles the last windowSize signals into a text window, embeds it,
// and compares against the previous embedding for the same conversation.
// Returns true if the cosine similarity drops below the dynamic threshold.
//
// On the first call for a conversation (no prior embedding), it stores
// the embedding and returns false.
func (s *Sentinel) TopicShifted(key models.ConversationKey, signals []models.Signal) bool {
	if len(signals) < s.windowSize {
		return false
	}

	// Take the last windowSize signals as the current window.
	window := signals[len(signals)-s.windowSize:]
	text := windowText(window)

	emb, err := s.model.Compute(text, true)
	if err != nil {
		// On embedding failure, don't block routing — just skip the check.
		return false
	}
	s.stats.EmbeddingsComputed++

	prev, hasPrev := s.lastEmbeddings[key]

	// Always update the stored embedding.
	s.lastEmbeddings[key] = emb

	if !hasPrev {
		// First window for this conversation — nothing to compare.
		return false
	}

	sim := embedding.CosineSimilarity(prev, emb)
	s.stats.SimilarityChecks++

	// Update running stats for this conversation.
	rs, ok := s.simStats[key]
	if !ok {
		rs = &runningStats{}
		s.simStats[key] = rs
	}
	rs.push(sim)

	// Need at least 3 data points for a meaningful threshold.
	if rs.count < 3 {
		return false
	}

	threshold := rs.mean() - s.thresholdStd*rs.std()
	if sim < threshold {
		s.stats.TopicShiftsDetected++
		return true
	}
	return false
}

// Stats returns sentinel statistics.
func (s *Sentinel) Stats() Stats {
	return s.stats
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
// This avoids storing all values while giving accurate running statistics.
type runningStats struct {
	count int
	mean_ float64
	m2    float64 // sum of squared differences from the mean
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
