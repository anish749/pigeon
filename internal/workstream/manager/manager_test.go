package manager

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/workspace"
	"github.com/anish749/pigeon/internal/workstream/discovery"
	"github.com/anish749/pigeon/internal/workstream/models"
	"github.com/anish749/pigeon/internal/workstream/store"
)

func newTestManager(t *testing.T) (*Manager, store.Store) {
	t.Helper()
	st := store.NewFS(t.TempDir())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mgr := New(nil, &fakeSignalReader{}, NewStatCollector(), models.Config{ApprovalMode: models.Interactive}, st, logger)
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

type fakeSignalReader struct {
	gotAccounts []account.Account
	gotSince    time.Time
	gotUntil    time.Time
	signals     []models.Signal
	err         error
}

func (f *fakeSignalReader) ReadAccounts(accounts []account.Account, since, until time.Time) ([]models.Signal, error) {
	f.gotAccounts = append([]account.Account(nil), accounts...)
	f.gotSince = since
	f.gotUntil = until
	return append([]models.Signal(nil), f.signals...), f.err
}

type fakeDiscovery struct {
	gotSignals []models.Signal
	discovered []discovery.DiscoveredWorkstream
	err        error
}

func (f *fakeDiscovery) Discover(_ context.Context, signals []models.Signal) ([]discovery.DiscoveredWorkstream, error) {
	f.gotSignals = append([]models.Signal(nil), signals...)
	return append([]discovery.DiscoveredWorkstream(nil), f.discovered...), f.err
}

func TestDiscoverAndProposeReadsWorkspaceSignals(t *testing.T) {
	st := store.NewFS(t.TempDir())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	since := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC)
	firstSignal := since.Add(2 * time.Hour)
	accounts := []account.Account{
		account.NewFromSlug("slack", "acme"),
		account.NewFromSlug("gws", "anish"),
	}
	reader := &fakeSignalReader{signals: []models.Signal{
		{ID: "s1", Account: accounts[0], Ts: firstSignal, Text: "auth migration"},
		{ID: "s2", Account: accounts[1], Ts: firstSignal.Add(time.Hour), Text: "session token review"},
	}}
	disc := &fakeDiscovery{discovered: []discovery.DiscoveredWorkstream{{
		Name:  "Auth Refactor",
		Focus: "Migrate auth sessions.",
	}}}
	mgr := New(nil, reader, NewStatCollector(), models.Config{
		ApprovalMode: models.AutoApprove,
		Workspace: workspace.Workspace{
			Name:     "acme",
			Accounts: accounts,
		},
	}, st, logger)
	mgr.disc = disc

	discovered, err := mgr.DiscoverAndPropose(context.Background(), since, until)
	if err != nil {
		t.Fatalf("DiscoverAndPropose: %v", err)
	}
	if len(discovered) != 1 || discovered[0].Name != "Auth Refactor" {
		t.Fatalf("discovered = %+v", discovered)
	}
	if !reflect.DeepEqual(reader.gotAccounts, accounts) {
		t.Errorf("reader accounts = %+v, want %+v", reader.gotAccounts, accounts)
	}
	if !reader.gotSince.Equal(since) || !reader.gotUntil.Equal(until) {
		t.Errorf("reader range = %s to %s, want %s to %s", reader.gotSince, reader.gotUntil, since, until)
	}
	if !reflect.DeepEqual(disc.gotSignals, reader.signals) {
		t.Errorf("discovery signals = %+v, want %+v", disc.gotSignals, reader.signals)
	}

	defaultWS, ok, err := st.GetWorkstream(models.DefaultWorkstreamID("acme"))
	if err != nil || !ok {
		t.Fatalf("default workstream not created: ok=%v err=%v", ok, err)
	}
	if !defaultWS.Created.Equal(firstSignal) {
		t.Errorf("default created = %s, want %s", defaultWS.Created, firstSignal)
	}
	got, ok, err := st.GetWorkstream("ws-auth-refactor")
	if err != nil || !ok {
		t.Fatalf("discovered workstream not created: ok=%v err=%v", ok, err)
	}
	if got.Focus != "Migrate auth sessions." || !got.Created.Equal(firstSignal) {
		t.Errorf("workstream = %+v", got)
	}
}

func TestDiscoverAndProposeNoSignals(t *testing.T) {
	st := store.NewFS(t.TempDir())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	reader := &fakeSignalReader{}
	disc := &fakeDiscovery{discovered: []discovery.DiscoveredWorkstream{{
		Name:  "Should Not Run",
		Focus: "No signals.",
	}}}
	mgr := New(nil, reader, NewStatCollector(), models.Config{
		ApprovalMode: models.AutoApprove,
		Workspace:    workspace.Workspace{Name: "acme"},
	}, st, logger)
	mgr.disc = disc

	discovered, err := mgr.DiscoverAndPropose(context.Background(), time.Now().Add(-time.Hour), time.Now())
	if err != nil {
		t.Fatalf("DiscoverAndPropose: %v", err)
	}
	if len(discovered) != 0 {
		t.Fatalf("discovered = %+v, want none", discovered)
	}
	if len(disc.gotSignals) != 0 {
		t.Fatalf("discovery ran with signals = %+v", disc.gotSignals)
	}
	all, err := st.ListWorkstreams()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Fatalf("workstreams created = %+v, want none", all)
	}
}
