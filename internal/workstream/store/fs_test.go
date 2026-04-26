package store

import (
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/workstream/models"
)

func TestFSRoundTrip(t *testing.T) {
	s := NewFS(t.TempDir())
	ts := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)

	t.Run("workstreams", func(t *testing.T) {
		ws := models.Workstream{ID: "ws-alpha", Name: "Alpha", State: models.StateActive, Focus: "alpha focus", Created: ts}
		if err := s.PutWorkstream(ws); err != nil {
			t.Fatal(err)
		}
		got, ok, err := s.GetWorkstream("ws-alpha")
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatal("expected to find ws-alpha")
		}
		if got.Name != "Alpha" || got.Focus != "alpha focus" {
			t.Errorf("got %+v", got)
		}

		// Update existing.
		ws.Focus = "updated focus"
		if err := s.PutWorkstream(ws); err != nil {
			t.Fatal(err)
		}
		got, _, _ = s.GetWorkstream("ws-alpha")
		if got.Focus != "updated focus" {
			t.Errorf("focus not updated: %q", got.Focus)
		}

		// List.
		all, err := s.ListWorkstreams()
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 1 {
			t.Errorf("got %d workstreams, want 1", len(all))
		}

		// Not found.
		_, ok, err = s.GetWorkstream("ws-missing")
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Error("expected not found")
		}
	})

	t.Run("proposals", func(t *testing.T) {
		seq, err := s.NextProposalSeq()
		if err != nil {
			t.Fatal(err)
		}
		if seq != 1 {
			t.Errorf("got seq %d, want 1", seq)
		}

		p := &models.Proposal{ID: "p-1", Type: models.ProposalCreate, State: models.ProposalApproved, SuggestedName: "Alpha", ProposedAt: ts}
		if err := s.PutProposal(p); err != nil {
			t.Fatal(err)
		}
		all, err := s.ListProposals()
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 1 || all[0].SuggestedName != "Alpha" {
			t.Errorf("got %+v", all)
		}

		// Update existing.
		p.State = models.ProposalRejected
		if err := s.PutProposal(p); err != nil {
			t.Fatal(err)
		}
		all, _ = s.ListProposals()
		if all[0].State != models.ProposalRejected {
			t.Error("state not updated")
		}

		// Lookup by ID.
		got, ok, err := s.GetProposal("p-1")
		if err != nil {
			t.Fatal(err)
		}
		if !ok || got.SuggestedName != "Alpha" {
			t.Errorf("GetProposal: ok=%v got=%+v", ok, got)
		}
		_, ok, err = s.GetProposal("p-missing")
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Error("expected not found")
		}
	})

	t.Run("empty_store", func(t *testing.T) {
		empty := NewFS(t.TempDir())
		ws, err := empty.ListWorkstreams()
		if err != nil {
			t.Fatal(err)
		}
		if len(ws) != 0 {
			t.Errorf("expected empty, got %d", len(ws))
		}
		props, err := empty.ListProposals()
		if err != nil {
			t.Fatal(err)
		}
		if len(props) != 0 {
			t.Errorf("expected empty, got %d", len(props))
		}
	})
}
