package embedding_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/hub/affinityrouter/detector"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/detector/embedding"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/models"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/reader"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
)

// TestBenchmarkDetectors compares burst-gap vs cosine detectors on real
// conversation data. Run with:
//
//	go test -run TestBenchmarkDetectors -v -timeout 5m ./internal/hub/affinityrouter/detector/embedding/
//
// Requires the embedding sidecar — start it first or skip with -short.
func TestBenchmarkDetectors(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping benchmark: requires embedding sidecar")
	}

	socketPath := filepath.Join(paths.StateDir(), "embed-benchmark.sock")
	client, err := embedding.NewClient(socketPath)
	if err != nil {
		t.Fatalf("start sidecar: %v", err)
	}
	defer client.Close()

	// Read the last 7 days of signals.
	until := time.Now()
	since := until.AddDate(0, 0, -7)

	root := paths.DefaultDataRoot()
	fsStore := store.NewFSStore(root)
	rdr := reader.New(fsStore, root)

	signals, err := rdr.ReadAll(since, until)
	if err != nil {
		t.Fatalf("read signals: %v", err)
	}

	// Group signals by conversation (Slack and WhatsApp only).
	type convEntry struct {
		key  models.ConversationKey
		sigs []models.Signal
	}
	convSignals := make(map[models.ConversationKey][]models.Signal)
	for _, sig := range signals {
		if sig.Type != models.SignalSlackMessage && sig.Type != models.SignalWhatsApp {
			continue
		}
		key := models.ConversationKey{Account: sig.Account, Conversation: sig.Conversation}
		convSignals[key] = append(convSignals[key], sig)
	}

	// Benchmark all conversations with enough messages for meaningful comparison.
	const minMessages = 10
	var active []convEntry
	for key, sigs := range convSignals {
		if len(sigs) >= minMessages {
			active = append(active, convEntry{key, sigs})
		}
	}
	sort.Slice(active, func(i, j int) bool {
		return active[i].key.Conversation < active[j].key.Conversation
	})

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	burstGapFactory := detector.NewBurstGapFactory(90 * time.Minute)
	cosineFactory := embedding.NewCosineFactory(client, logger)

	type result struct {
		name     string
		msgs     int
		gapTrigs int
		cosTrigs int
		both     int
		gapOnly  int
		cosOnly  int
	}
	var results []result
	var totals result

	for _, entry := range active {
		bgDet := burstGapFactory()
		cosDet := cosineFactory()

		var gapIdxs, cosIdxs []int
		for i, sig := range entry.sigs {
			if bgDet.Observe(sig) {
				gapIdxs = append(gapIdxs, i)
			}
			if cosDet.Observe(sig) {
				cosIdxs = append(cosIdxs, i)
			}
		}

		gapSet := make(map[int]bool)
		for _, i := range gapIdxs {
			gapSet[i] = true
		}
		cosSet := make(map[int]bool)
		for _, i := range cosIdxs {
			cosSet[i] = true
		}

		var bothCount, gapOnlyCount, cosOnlyCount int
		for i := range gapSet {
			if cosSet[i] {
				bothCount++
			} else {
				gapOnlyCount++
			}
		}
		for i := range cosSet {
			if !gapSet[i] {
				cosOnlyCount++
			}
		}

		name := fmt.Sprintf("%s/%s", entry.key.Account.Name, entry.key.Conversation)
		r := result{
			name:     name,
			msgs:     len(entry.sigs),
			gapTrigs: len(gapIdxs),
			cosTrigs: len(cosIdxs),
			both:     bothCount,
			gapOnly:  gapOnlyCount,
			cosOnly:  cosOnlyCount,
		}
		results = append(results, r)
		totals.msgs += r.msgs
		totals.gapTrigs += r.gapTrigs
		totals.cosTrigs += r.cosTrigs
		totals.both += r.both
		totals.gapOnly += r.gapOnly
		totals.cosOnly += r.cosOnly
	}

	// Print results.
	t.Logf("\n%-50s %4s %4s %4s %4s %8s %8s", "Conversation", "Msgs", "Gap", "Cos", "Both", "Gap-only", "Cos-only")
	t.Logf("%-50s %4s %4s %4s %4s %8s %8s", "", "", "trig", "trig", "", "(wasted)", "(missed)")
	t.Logf("%s", "------------------------------------------------------------------------------------------------------")
	for _, r := range results {
		t.Logf("%-50s %4d %4d %4d %4d %8d %8d", r.name, r.msgs, r.gapTrigs, r.cosTrigs, r.both, r.gapOnly, r.cosOnly)
	}
	t.Logf("%s", "------------------------------------------------------------------------------------------------------")
	t.Logf("%-50s %4d %4d %4d %4d %8d %8d", "TOTAL", totals.msgs, totals.gapTrigs, totals.cosTrigs, totals.both, totals.gapOnly, totals.cosOnly)

	if totals.gapTrigs > 0 {
		wastePct := float64(totals.gapOnly) / float64(totals.gapTrigs) * 100
		t.Logf("\nBurst-gap waste rate: %d/%d = %.0f%% of reclassifications unnecessary", totals.gapOnly, totals.gapTrigs, wastePct)
	}
	if totals.cosOnly > 0 {
		t.Logf("Burst-gap miss rate: %d topic shifts that burst-gap would not detect", totals.cosOnly)
	}
}

// embedClient is a test helper to verify the sidecar is reachable.
func TestSidecarSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: requires embedding sidecar")
	}

	socketPath := filepath.Join(paths.StateDir(), "embed-smoke.sock")
	client, err := embedding.NewClient(socketPath)
	if err != nil {
		t.Fatalf("start sidecar: %v", err)
	}
	defer client.Close()

	vec, err := client.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vec) == 0 {
		t.Fatal("empty embedding")
	}
	t.Logf("embedding dimensions: %d", len(vec))
}
