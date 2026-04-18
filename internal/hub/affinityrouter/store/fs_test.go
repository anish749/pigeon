package store

import (
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/models"
)

func TestFSRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := NewFS(dir)

	ts := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)

	t.Run("workstreams", func(t *testing.T) {
		want := map[string]models.Workstream{
			"ws-alpha": {ID: "ws-alpha", Name: "Alpha", State: models.StateActive, Focus: "alpha focus", Created: ts},
			"ws-beta":  {ID: "ws-beta", Name: "Beta", State: models.StateDormant, Focus: "beta focus", Created: ts},
		}
		if err := s.SaveWorkstreams(want); err != nil {
			t.Fatal(err)
		}
		got, err := s.LoadWorkstreams()
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(want) {
			t.Fatalf("got %d workstreams, want %d", len(got), len(want))
		}
		for id, w := range want {
			g := got[id]
			if g.Name != w.Name || g.State != w.State || g.Focus != w.Focus {
				t.Errorf("workstream %s mismatch: got %+v", id, g)
			}
		}
	})

	t.Run("affinities", func(t *testing.T) {
		key := models.ConversationKey{
			Account:      account.New("slack", "test-workspace"),
			Conversation: "C123",
		}
		want := map[models.ConversationKey][]models.AffinityEntry{
			key: {
				{WorkstreamID: "ws-alpha", Strength: 5, LastSignal: ts},
				{WorkstreamID: "ws-beta", Strength: 2, LastSignal: ts},
			},
		}
		if err := s.SaveAffinities(want); err != nil {
			t.Fatal(err)
		}
		got, err := s.LoadAffinities()
		if err != nil {
			t.Fatal(err)
		}
		entries := got[key]
		if len(entries) != 2 {
			t.Fatalf("got %d entries, want 2", len(entries))
		}
		if entries[0].WorkstreamID != "ws-alpha" && entries[1].WorkstreamID != "ws-alpha" {
			t.Error("missing ws-alpha entry")
		}
	})

	t.Run("proposals", func(t *testing.T) {
		want := []*models.Proposal{
			{ID: "p-1", Type: models.ProposalCreate, State: models.ProposalApproved, SuggestedName: "Alpha", ProposedAt: ts},
			{ID: "p-2", Type: models.ProposalCreate, State: models.ProposalPending, SuggestedName: "Gamma", ProposedAt: ts},
		}
		if err := s.SaveProposals(want, 2); err != nil {
			t.Fatal(err)
		}
		got, seq, err := s.LoadProposals()
		if err != nil {
			t.Fatal(err)
		}
		if seq != 2 {
			t.Errorf("got seq %d, want 2", seq)
		}
		if len(got) != 2 {
			t.Fatalf("got %d proposals, want 2", len(got))
		}
		if got[0].SuggestedName != "Alpha" || got[1].SuggestedName != "Gamma" {
			t.Error("proposal names mismatch")
		}
	})

	t.Run("load_missing_files", func(t *testing.T) {
		empty := NewFS(t.TempDir())
		ws, err := empty.LoadWorkstreams()
		if err != nil {
			t.Fatal(err)
		}
		if len(ws) != 0 {
			t.Errorf("expected empty workstreams, got %d", len(ws))
		}
		aff, err := empty.LoadAffinities()
		if err != nil {
			t.Fatal(err)
		}
		if len(aff) != 0 {
			t.Errorf("expected empty affinities, got %d", len(aff))
		}
		props, seq, err := empty.LoadProposals()
		if err != nil {
			t.Fatal(err)
		}
		if len(props) != 0 || seq != 0 {
			t.Errorf("expected empty proposals, got %d with seq %d", len(props), seq)
		}
	})
}
