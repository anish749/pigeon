package manager

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/workspace"
	"github.com/anish749/pigeon/internal/workstream/models"
	"github.com/anish749/pigeon/internal/workstream/store"
)

func newTestManager(t *testing.T) (*Manager, store.Store) {
	t.Helper()
	st := store.NewFS(t.TempDir())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mgr := New(nil, NewStatCollector(), models.Config{ApprovalMode: models.Interactive}, st, nil, logger)
	return mgr, st
}

func TestApproveProposalCreatesWorkstreamAndDeletesProposal(t *testing.T) {
	mgr, st := newTestManager(t)
	ts := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	if err := st.PutProposal(&models.Proposal{
		ID:             "p-1",
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

	// Proposal removed from queue.
	if _, ok, _ := st.GetProposal("p-1"); ok {
		t.Error("expected proposal deleted after approval")
	}
}

func TestApproveProposalConflictsWithExistingWorkstream(t *testing.T) {
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
		SuggestedName:  "Auth Refactor",
		SuggestedFocus: "LLM's newer focus that should NOT overwrite.",
		Workspace:      "acme",
		ProposedAt:     ts,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := mgr.ApproveProposal(context.Background(), "p-1"); err == nil {
		t.Fatal("expected error on slug conflict, got nil")
	}

	// Existing workstream untouched.
	got, _, _ := st.GetWorkstream("ws-auth-refactor")
	if got.Focus != "User-edited focus that must survive." {
		t.Errorf("user edit was overwritten: focus = %q", got.Focus)
	}

	// Proposal still in queue — caller must reject explicitly.
	if _, ok, _ := st.GetProposal("p-1"); !ok {
		t.Error("proposal removed despite conflict; should remain for caller to reject")
	}
}

func TestApproveProposalNotFound(t *testing.T) {
	mgr, _ := newTestManager(t)
	if _, err := mgr.ApproveProposal(context.Background(), "p-missing"); err == nil {
		t.Error("expected error for missing proposal")
	}
}

func TestApproveProposalDoubleApproveFails(t *testing.T) {
	mgr, st := newTestManager(t)
	ts := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	if err := st.PutProposal(&models.Proposal{
		ID:             "p-1",
		SuggestedName:  "Auth Refactor",
		SuggestedFocus: "first focus",
		Workspace:      "acme",
		ProposedAt:     ts,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.ApproveProposal(context.Background(), "p-1"); err != nil {
		t.Fatal(err)
	}
	// Second approval surfaces as not-found because the proposal was deleted.
	if _, err := mgr.ApproveProposal(context.Background(), "p-1"); err == nil {
		t.Error("expected error on second approval")
	}
}

func TestRejectProposalDeletes(t *testing.T) {
	mgr, st := newTestManager(t)
	if err := st.PutProposal(&models.Proposal{
		ID:            "p-1",
		SuggestedName: "Auth Refactor",
	}); err != nil {
		t.Fatal(err)
	}

	if err := mgr.RejectProposal(context.Background(), "p-1"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := st.GetProposal("p-1"); ok {
		t.Error("expected proposal deleted after rejection")
	}

	// No workstream should have been created.
	all, _ := st.ListWorkstreams()
	if len(all) != 0 {
		t.Errorf("workstreams created on reject: %d", len(all))
	}

	// Re-rejecting surfaces as not-found.
	if err := mgr.RejectProposal(context.Background(), "p-1"); err == nil {
		t.Error("expected error when rejecting already-deleted proposal")
	}
}

// fakeReader is a Reader stub. Records the most recent ReadAccounts
// call and returns the configured signals/err.
type fakeReader struct {
	signals []models.Signal
	err     error

	gotAccounts []account.Account
	gotSince    time.Time
	gotUntil    time.Time
}

func (r *fakeReader) ReadAccounts(accounts []account.Account, since, until time.Time) ([]models.Signal, error) {
	r.gotAccounts = accounts
	r.gotSince = since
	r.gotUntil = until
	return r.signals, r.err
}

func newDiscoverManager(t *testing.T, reader Reader) (*Manager, store.Store) {
	t.Helper()
	st := store.NewFS(t.TempDir())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := models.Config{
		ApprovalMode: models.AutoApprove,
		Workspace: workspace.Workspace{
			Name:     "acme",
			Accounts: []account.Account{account.New("slack", "acme")},
		},
	}
	mgr := New(nil, NewStatCollector(), cfg, st, reader, logger)
	return mgr, st
}

func TestDiscoverEmptyWorkspaceReturnsNilNoError(t *testing.T) {
	r := &fakeReader{} // no signals, no err
	mgr, st := newDiscoverManager(t, r)

	got, err := mgr.Discover(context.Background(), time.Time{}, time.Now())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil discovered, got %+v", got)
	}

	// Reader was called with the workspace's accounts.
	if len(r.gotAccounts) != 1 || r.gotAccounts[0].Platform != "slack" {
		t.Errorf("reader saw accounts %+v, want slack/acme", r.gotAccounts)
	}

	// No default workstream should have been created when there are no signals —
	// EnsureDefaultWorkstream uses signals[0].Ts and is therefore skipped.
	def, ok, _ := st.GetWorkstream(models.DefaultWorkstreamID("acme"))
	if ok {
		t.Errorf("default workstream created on empty discover: %+v", def)
	}
}

func TestDiscoverReaderErrorPropagates(t *testing.T) {
	want := errors.New("disk on fire")
	r := &fakeReader{err: want}
	mgr, _ := newDiscoverManager(t, r)

	_, err := mgr.Discover(context.Background(), time.Time{}, time.Now())
	if err == nil {
		t.Fatal("expected reader error to propagate")
	}
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want chain to include %v", err, want)
	}
}

func TestDiscoverWithoutReaderConfigured(t *testing.T) {
	mgr, _ := newDiscoverManager(t, nil)
	_, err := mgr.Discover(context.Background(), time.Time{}, time.Now())
	if err == nil {
		t.Fatal("expected error when reader is nil")
	}
}

func TestDiscoverPassesWindowThrough(t *testing.T) {
	r := &fakeReader{} // empty signals — reader gets called and we exit cleanly
	mgr, _ := newDiscoverManager(t, r)

	since := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	if _, err := mgr.Discover(context.Background(), since, until); err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if !r.gotSince.Equal(since) || !r.gotUntil.Equal(until) {
		t.Errorf("reader saw window [%s, %s], want [%s, %s]",
			r.gotSince, r.gotUntil, since, until)
	}
}
