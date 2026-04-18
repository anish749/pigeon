package embedding

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/hub/affinityrouter/models"
)

// --- cosineSimilarity tests ---

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a, b []float32
		want float64
	}{
		{
			name: "identical vectors",
			a:    []float32{1, 2, 3},
			b:    []float32{1, 2, 3},
			want: 1.0,
		},
		{
			name: "opposite vectors",
			a:    []float32{1, 0, 0},
			b:    []float32{-1, 0, 0},
			want: -1.0,
		},
		{
			name: "orthogonal vectors",
			a:    []float32{1, 0, 0},
			b:    []float32{0, 1, 0},
			want: 0.0,
		},
		{
			name: "scaled vectors are identical",
			a:    []float32{1, 2, 3},
			b:    []float32{2, 4, 6},
			want: 1.0,
		},
		{
			name: "45 degree angle",
			a:    []float32{1, 0},
			b:    []float32{1, 1},
			want: 1 / math.Sqrt(2),
		},
		{
			name: "zero vector",
			a:    []float32{0, 0, 0},
			b:    []float32{1, 2, 3},
			want: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cosineSimilarity(tt.a, tt.b)
			if math.Abs(got-tt.want) > 1e-6 {
				t.Errorf("cosineSimilarity(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// --- meanStd tests ---

func TestMeanStd(t *testing.T) {
	mean, std := meanStd([]float64{0.8, 0.8, 0.8, 0.8, 0.8})
	if math.Abs(mean-0.8) > 1e-9 {
		t.Errorf("mean = %v, want 0.8", mean)
	}
	if math.Abs(std) > 1e-9 {
		t.Errorf("std = %v, want 0.0", std)
	}

	mean, std = meanStd([]float64{0.6, 0.8})
	if math.Abs(mean-0.7) > 1e-9 {
		t.Errorf("mean = %v, want 0.7", mean)
	}
	if math.Abs(std-0.1) > 1e-9 {
		t.Errorf("std = %v, want 0.1", std)
	}
}

// --- fakeEmbedder ---

type embedResponse struct {
	vec []float32
	err error
}

type fakeEmbedder struct {
	responses []embedResponse
	idx       int
}

func (f *fakeEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	if f.idx >= len(f.responses) {
		return nil, fmt.Errorf("unexpected embed call %d", f.idx)
	}
	r := f.responses[f.idx]
	f.idx++
	return r.vec, r.err
}

// --- CosineDetector.Observe tests ---

func sig(sender, text string) models.Signal {
	return models.Signal{
		ID:     text,
		Sender: sender,
		Text:   text,
		Ts:     time.Now(),
	}
}

func newTestDetector(embedder Embedder, windowSize int, fallbackThreshold float64) *CosineDetector {
	return &CosineDetector{
		embedder:          embedder,
		logger:            slog.Default(),
		fallbackThreshold: fallbackThreshold,
		stdMultiplier:     1.0,
		minCalibration:    5,
		windowSize:        windowSize,
	}
}

func TestObserve_WindowNotFull(t *testing.T) {
	// Embedder should not be called when window isn't full.
	emb := &fakeEmbedder{}
	d := newTestDetector(emb, 3, 0.5)

	if d.Observe(sig("alice", "hello")) {
		t.Error("expected false for first signal (window not full)")
	}
	if d.Observe(sig("bob", "hi")) {
		t.Error("expected false for second signal (window not full)")
	}
	if emb.idx != 0 {
		t.Errorf("embedder called %d times, want 0", emb.idx)
	}
}

func TestObserve_FirstFullWindow(t *testing.T) {
	// First full window caches embedding, returns false (nothing to compare).
	emb := &fakeEmbedder{
		responses: []embedResponse{
			{vec: []float32{1, 0, 0, 0}},
		},
	}
	d := newTestDetector(emb, 2, 0.5)

	d.Observe(sig("alice", "msg1"))
	shifted := d.Observe(sig("alice", "msg2"))

	if shifted {
		t.Error("expected false for first full window (no previous to compare)")
	}
	if emb.idx != 1 {
		t.Errorf("embedder called %d times, want 1", emb.idx)
	}
}

func TestObserve_NoShift(t *testing.T) {
	// Two very similar embeddings — no shift.
	emb := &fakeEmbedder{
		responses: []embedResponse{
			{vec: []float32{1, 0, 0, 0}},     // first window
			{vec: []float32{0.99, 0.1, 0, 0}}, // second window — very similar
		},
	}
	d := newTestDetector(emb, 2, 0.5)

	d.Observe(sig("alice", "msg1"))
	d.Observe(sig("alice", "msg2")) // first window, cached
	shifted := d.Observe(sig("alice", "msg3")) // second window, compared

	if shifted {
		t.Error("expected no shift for similar embeddings")
	}
}

func TestObserve_ShiftDetected(t *testing.T) {
	// Orthogonal embeddings — clear shift.
	emb := &fakeEmbedder{
		responses: []embedResponse{
			{vec: []float32{1, 0, 0, 0}}, // first window
			{vec: []float32{0, 0, 1, 0}}, // second window — orthogonal
		},
	}
	d := newTestDetector(emb, 2, 0.5)

	d.Observe(sig("alice", "msg1"))
	d.Observe(sig("alice", "msg2")) // first window
	shifted := d.Observe(sig("alice", "msg3")) // second window

	if !shifted {
		t.Error("expected shift for orthogonal embeddings")
	}
}

func TestObserve_EmbedError_TriggersReclassification(t *testing.T) {
	// When embedding fails, Observe returns true to trigger LLM fallback.
	emb := &fakeEmbedder{
		responses: []embedResponse{
			{vec: []float32{1, 0, 0, 0}},               // first window succeeds
			{err: fmt.Errorf("sidecar connection refused")}, // second window fails
			{vec: []float32{0.95, 0.05, 0, 0}},          // third window succeeds, compares against first
		},
	}
	d := newTestDetector(emb, 2, 0.5)

	d.Observe(sig("alice", "msg1"))
	d.Observe(sig("alice", "msg2")) // first window — cached

	shifted := d.Observe(sig("alice", "msg3")) // embed fails
	if !shifted {
		t.Error("expected true when embed fails (trigger reclassification)")
	}

	// prevEmbed should still be the first window's embedding, so the
	// next successful embed compares against it.
	shifted = d.Observe(sig("alice", "msg4")) // succeeds, similar to first
	if shifted {
		t.Error("expected no shift after recovery with similar embedding")
	}
}

func TestObserve_SelfCalibratingThreshold(t *testing.T) {
	// Build up 5+ observations with high similarity (0.95+), then check
	// that a moderate drop (0.7) triggers a shift under calibrated threshold
	// but would not under the fallback threshold of 0.5.

	// We need windowSize=2, so each new signal after the first two produces
	// one embed call and one similarity observation.
	// Total: 1 initial embed + 6 comparison embeds = 7 embed calls.
	highSim := []float32{1, 0, 0, 0}
	slightlyDiff := []float32{0.98, 0.2, 0, 0} // cos with highSim ≈ 0.98
	moderateDiff := []float32{0.6, 0.8, 0, 0}   // cos with highSim ≈ 0.6

	emb := &fakeEmbedder{
		responses: []embedResponse{
			{vec: highSim},       // window 1 — cached
			{vec: slightlyDiff},  // window 2 — sim ≈ 0.98
			{vec: highSim},       // window 3 — sim ≈ 0.98
			{vec: slightlyDiff},  // window 4 — sim ≈ 0.98
			{vec: highSim},       // window 5 — sim ≈ 0.98
			{vec: slightlyDiff},  // window 6 — sim ≈ 0.98 (5 observations, calibrated)
			{vec: moderateDiff},  // window 7 — sim ≈ 0.6 (below calibrated threshold)
		},
	}
	d := newTestDetector(emb, 2, 0.5) // fallback=0.5, so 0.6 would NOT trigger under fallback

	// Fill first window.
	d.Observe(sig("alice", "msg0"))

	// Observe 6 windows to accumulate 5 similarity observations.
	for i := 1; i <= 6; i++ {
		shifted := d.Observe(sig("alice", fmt.Sprintf("msg%d", i)))
		if shifted {
			t.Errorf("unexpected shift at observation %d", i)
		}
	}

	if len(d.sims) != 5 {
		t.Fatalf("expected 5 similarity observations, got %d", len(d.sims))
	}

	// Threshold should now be calibrated: mean(~0.98) - 1*std(~0.0) ≈ 0.98.
	// A similarity of ~0.6 is well below this.
	shifted := d.Observe(sig("alice", "topic_change"))
	if !shifted {
		t.Error("expected shift: similarity ~0.6 is below calibrated threshold ~0.98, " +
			"even though it's above the fallback threshold of 0.5")
	}
}
