package router

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/workstream/models"
)

// mockEmbedder returns pre-configured embeddings for known texts.
type mockEmbedder struct {
	embeddings map[string][]float64
}

func (m *mockEmbedder) Embed(_ context.Context, text string) ([]float64, error) {
	if emb, ok := m.embeddings[text]; ok {
		return emb, nil
	}
	return make([]float64, 3), nil
}

func (m *mockEmbedder) Close() error { return nil }

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestRoute_MatchesSingleWorkstream(t *testing.T) {
	embedder := &mockEmbedder{embeddings: map[string][]float64{
		"Deploy the auth service":    {0.9, 0.1, 0.0}, // workstream focus
		"alice: pushed the auth fix": {0.85, 0.15, 0.0},
		"bob: lunch?":                {0.0, 0.0, 1.0},
	}}

	r := New(embedder, 0.4, "_default", testLogger())
	err := r.LoadWorkstreams(context.Background(), []models.Workstream{
		{ID: "ws-auth", Name: "Auth Deploy", Focus: "Deploy the auth service"},
	})
	if err != nil {
		t.Fatal(err)
	}

	sig := models.Signal{
		ID: "1", Ts: time.Now(), Sender: "alice", Text: "pushed the auth fix",
		Account: account.New("slack", "test"),
	}
	decision, err := r.Route(context.Background(), sig)
	if err != nil {
		t.Fatal(err)
	}

	if len(decision.WorkstreamIDs) != 1 || decision.WorkstreamIDs[0] != "ws-auth" {
		t.Errorf("expected [ws-auth], got %v", decision.WorkstreamIDs)
	}
}

func TestRoute_FallsBackToDefault(t *testing.T) {
	embedder := &mockEmbedder{embeddings: map[string][]float64{
		"Deploy the auth service": {0.9, 0.1, 0.0},
		"bob: lunch?":             {0.0, 0.0, 1.0},
	}}

	r := New(embedder, 0.4, "_default_test", testLogger())
	err := r.LoadWorkstreams(context.Background(), []models.Workstream{
		{ID: "ws-auth", Name: "Auth Deploy", Focus: "Deploy the auth service"},
	})
	if err != nil {
		t.Fatal(err)
	}

	sig := models.Signal{
		ID: "2", Ts: time.Now(), Sender: "bob", Text: "lunch?",
		Account: account.New("slack", "test"),
	}
	decision, err := r.Route(context.Background(), sig)
	if err != nil {
		t.Fatal(err)
	}

	if len(decision.WorkstreamIDs) != 1 || decision.WorkstreamIDs[0] != "_default_test" {
		t.Errorf("expected [_default_test], got %v", decision.WorkstreamIDs)
	}
}

func TestRoute_MultiRoutes(t *testing.T) {
	// Signal is similar to both workstreams.
	embedder := &mockEmbedder{embeddings: map[string][]float64{
		"Deploy the auth service":            {0.9, 0.1, 0.0},
		"Migrate the database schema":        {0.1, 0.9, 0.0},
		"alice: auth migration needs schema": {0.6, 0.6, 0.0},
	}}

	r := New(embedder, 0.4, "_default", testLogger())
	err := r.LoadWorkstreams(context.Background(), []models.Workstream{
		{ID: "ws-auth", Name: "Auth Deploy", Focus: "Deploy the auth service"},
		{ID: "ws-db", Name: "DB Migration", Focus: "Migrate the database schema"},
	})
	if err != nil {
		t.Fatal(err)
	}

	sig := models.Signal{
		ID: "3", Ts: time.Now(), Sender: "alice", Text: "auth migration needs schema",
		Account: account.New("slack", "test"),
	}
	decision, err := r.Route(context.Background(), sig)
	if err != nil {
		t.Fatal(err)
	}

	if len(decision.WorkstreamIDs) != 2 {
		t.Errorf("expected 2 workstreams, got %v", decision.WorkstreamIDs)
	}
}

func TestRoute_SignalIDAndTimestamp(t *testing.T) {
	embedder := &mockEmbedder{embeddings: map[string][]float64{
		"Deploy the auth service": {1.0, 0.0, 0.0},
	}}

	r := New(embedder, 0.4, "_default", testLogger())
	_ = r.LoadWorkstreams(context.Background(), []models.Workstream{
		{ID: "ws-auth", Name: "Auth", Focus: "Deploy the auth service"},
	})

	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	sig := models.Signal{ID: "sig-42", Ts: now, Sender: "x", Text: "y", Account: account.New("slack", "test")}
	decision, err := r.Route(context.Background(), sig)
	if err != nil {
		t.Fatal(err)
	}

	if decision.SignalID != "sig-42" {
		t.Errorf("expected signal ID sig-42, got %s", decision.SignalID)
	}
	if !decision.Ts.Equal(now) {
		t.Errorf("expected ts %v, got %v", now, decision.Ts)
	}
}

func TestLoadWorkstreams_SkipsDefaultAndEmptyFocus(t *testing.T) {
	embedder := &mockEmbedder{embeddings: map[string][]float64{
		"real focus": {1.0, 0.0, 0.0},
	}}

	r := New(embedder, 0.4, "_default_ws", testLogger())
	err := r.LoadWorkstreams(context.Background(), []models.Workstream{
		{ID: "_default_ws", Name: "General", Workspace: "ws", Focus: "catch-all"},
		{ID: "ws-empty", Name: "Empty", Focus: ""},
		{ID: "ws-real", Name: "Real", Focus: "real focus"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(r.workstreams) != 1 {
		t.Errorf("expected 1 loaded workstream, got %d", len(r.workstreams))
	}
	if r.workstreams[0].Workstream.ID != "ws-real" {
		t.Errorf("expected ws-real, got %s", r.workstreams[0].Workstream.ID)
	}
}
