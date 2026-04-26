package manager

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/workstream/models"
	"github.com/anish749/pigeon/internal/workstream/store"
)

func newTestManager(t *testing.T) (*Manager, store.Store) {
	t.Helper()
	st := store.NewFS(t.TempDir())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mgr := New(nil, NewStatCollector(), models.Config{ApprovalMode: models.Interactive}, st, logger)
	return mgr, st
}

func TestApproveProposalCreatesWorkstream(t *testing.T) {
	mgr, st := newTestManager(t)
	ts := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	if err := st.PutProposal(&models.Proposal{
		ID:             "p-1",
		Type:           models.ProposalCreate,
		State:          models.ProposalPending,
		SuggestedName:  "Auth Refactor",
		SuggestedFocus: "Migrate session tokens off the legacy middleware.",
		Workspace:      "acme",
		ProposedAt:     ts,
	}); err != nil {
		t.Fatal(err)
	}

	wsID, err := mgr.ApproveProposal(context.Background(), "p-1")
	if err != nil {
		t.Fatalf("ApproveProposal: %v", err)
	}
	if wsID != "ws-auth-refactor" {
		t.Errorf("workstream ID = %q, want ws-auth-refactor", wsID)
	}

	got, ok, err := st.GetWorkstream(wsID)
	if err != nil || !ok {
		t.Fatalf("workstream not found: ok=%v err=%v", ok, err)
	}
	if got.Name != "Auth Refactor" || got.State != models.StateActive || !got.Created.Equal(ts) {
		t.Errorf("workstream = %+v", got)
	}

	p, _, _ := st.GetProposal("p-1")
	if p.State != models.ProposalApproved {
		t.Errorf("proposal state = %q, want approved", p.State)
	}
	if p.ResolvedAt.IsZero() {
		t.Error("ResolvedAt not set")
	}
}

func TestApproveProposalIdempotentOnExistingWorkstream(t *testing.T) {
	mgr, st := newTestManager(t)
	ts := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	if err := st.PutWorkstream(models.Workstream{
		ID:        "ws-auth-refactor",
		Name:      "Auth Refactor",
		Workspace: "acme",
		State:     models.StateActive,
		Focus:     "User-edited focus that must survive.",
		Created:   ts,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutProposal(&models.Proposal{
		ID:             "p-1",
		Type:           models.ProposalCreate,
		State:          models.ProposalPending,
		SuggestedName:  "Auth Refactor",
		SuggestedFocus: "LLM's newer focus that should NOT overwrite.",
		Workspace:      "acme",
		ProposedAt:     ts,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := mgr.ApproveProposal(context.Background(), "p-1"); err != nil {
		t.Fatal(err)
	}
	got, _, _ := st.GetWorkstream("ws-auth-refactor")
	if got.Focus != "User-edited focus that must survive." {
		t.Errorf("user edit was overwritten: focus = %q", got.Focus)
	}
}

func TestApproveProposalRejectsResolved(t *testing.T) {
	mgr, st := newTestManager(t)
	if err := st.PutProposal(&models.Proposal{
		ID:    "p-1",
		Type:  models.ProposalCreate,
		State: models.ProposalApproved,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.ApproveProposal(context.Background(), "p-1"); err == nil {
		t.Error("expected error when approving already-approved proposal")
	}
}

func TestApproveProposalNotFound(t *testing.T) {
	mgr, _ := newTestManager(t)
	if _, err := mgr.ApproveProposal(context.Background(), "p-missing"); err == nil {
		t.Error("expected error for missing proposal")
	}
}

func TestRejectProposal(t *testing.T) {
	mgr, st := newTestManager(t)
	if err := st.PutProposal(&models.Proposal{
		ID:            "p-1",
		Type:          models.ProposalCreate,
		State:         models.ProposalPending,
		SuggestedName: "Auth Refactor",
	}); err != nil {
		t.Fatal(err)
	}

	if err := mgr.RejectProposal(context.Background(), "p-1"); err != nil {
		t.Fatal(err)
	}
	p, _, _ := st.GetProposal("p-1")
	if p.State != models.ProposalRejected {
		t.Errorf("state = %q, want rejected", p.State)
	}
	if p.ResolvedAt.IsZero() {
		t.Error("ResolvedAt not set")
	}

	// No workstream should have been created.
	all, _ := st.ListWorkstreams()
	if len(all) != 0 {
		t.Errorf("workstreams created on reject: %d", len(all))
	}

	// Re-rejecting fails.
	if err := mgr.RejectProposal(context.Background(), "p-1"); err == nil {
		t.Error("expected error when rejecting already-rejected proposal")
	}
}
