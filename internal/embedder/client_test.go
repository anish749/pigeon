package embedder

import (
	"context"
	"os"
	"testing"
	"time"
)

var testClient *Client

func TestMain(m *testing.M) {
	var err error
	testClient, err = NewClient()
	if err != nil {
		panic("failed to start embedding sidecar: " + err.Error())
	}
	code := m.Run()
	testClient.Close()
	os.Exit(code)
}

func TestClient_SidecarStartsAndEmbeds(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	emb, err := testClient.Embed(ctx, "hello world")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	if len(emb) == 0 {
		t.Fatal("expected non-empty embedding")
	}

	// all-MiniLM-L6-v2 produces 384-dimensional embeddings.
	if len(emb) != 384 {
		t.Errorf("expected 384 dimensions, got %d", len(emb))
	}
}

func TestClient_DifferentTextsDifferentEmbeddings(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	emb1, err := testClient.Embed(ctx, "the cat sat on the mat")
	if err != nil {
		t.Fatalf("Embed 1: %v", err)
	}

	emb2, err := testClient.Embed(ctx, "quarterly revenue forecast for Q3")
	if err != nil {
		t.Fatalf("Embed 2: %v", err)
	}

	sim := CosineSimilarity(emb1, emb2)
	if sim > 0.5 {
		t.Errorf("unrelated texts should have low similarity, got %.3f", sim)
	}
}

func TestClient_SimilarTextsHighSimilarity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	emb1, err := testClient.Embed(ctx, "deploy the authentication service to production")
	if err != nil {
		t.Fatalf("Embed 1: %v", err)
	}

	emb2, err := testClient.Embed(ctx, "push the auth service to prod")
	if err != nil {
		t.Fatalf("Embed 2: %v", err)
	}

	sim := CosineSimilarity(emb1, emb2)
	if sim < 0.5 {
		t.Errorf("similar texts should have high similarity, got %.3f", sim)
	}
}
