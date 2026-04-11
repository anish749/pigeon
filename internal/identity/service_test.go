package identity_test

import (
	"os"
	"slices"
	"testing"

	"github.com/anish749/pigeon/internal/identity"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
)

func testService(t *testing.T) (*identity.Service, paths.IdentityDir) {
	t.Helper()
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)
	dir := root.Identity("test")
	return identity.NewService(s, dir), dir
}

// lookup is a test helper that searches for a person by query and expects exactly one result.
func lookup(t *testing.T, svc *identity.Service, query string) identity.Person {
	t.Helper()
	results, err := svc.SearchCandidates(query)
	if err != nil {
		t.Fatalf("SearchCandidates(%q): %v", query, err)
	}
	if len(results) != 1 {
		t.Fatalf("SearchCandidates(%q) returned %d results, want 1", query, len(results))
	}
	return results[0]
}

func TestObserve_NewPerson(t *testing.T) {
	svc, _ := testService(t)

	if err := svc.Observe(identity.Signal{
		Email: "alice@company.com",
		Name:  "Alice Smith",
	}); err != nil {
		t.Fatal(err)
	}

	p := lookup(t, svc, "alice@company.com")
	if p.Name != "Alice Smith" {
		t.Errorf("name = %q, want %q", p.Name, "Alice Smith")
	}
	if len(p.Email) != 1 || p.Email[0] != "alice@company.com" {
		t.Errorf("email = %v, want [alice@company.com]", p.Email)
	}
}

func TestObserveBatch_SlackUsers(t *testing.T) {
	svc, _ := testService(t)

	signals := []identity.Signal{
		{
			Email: "alice@company.com",
			Name:  "Alice Smith",
			Slack: &identity.SlackIdentity{Workspace: "acme", ID: "U04ABCDEF", DisplayName: "Alice Smith"},
		},
		{
			Email: "bob@company.com",
			Name:  "Bob Jones",
			Slack: &identity.SlackIdentity{Workspace: "acme", ID: "U05BCDEFG", DisplayName: "bob.jones"},
		},
	}
	if err := svc.ObserveBatch(signals); err != nil {
		t.Fatal(err)
	}

	alice := lookup(t, svc, "U04ABCDEF")
	if alice.Slack["acme"].ID != "U04ABCDEF" {
		t.Errorf("alice slack ID = %q, want U04ABCDEF", alice.Slack["acme"].ID)
	}

	bob := lookup(t, svc, "U05BCDEFG")
	if bob.Name != "Bob Jones" {
		t.Errorf("bob name = %q, want Bob Jones", bob.Name)
	}
}

func TestMerge_EmailMatch(t *testing.T) {
	svc, _ := testService(t)

	// First: email-only person from Gmail.
	if err := svc.Observe(identity.Signal{
		Email: "alice@company.com",
		Name:  "Alice S",
	}); err != nil {
		t.Fatal(err)
	}

	// Second: Slack signal with the same email → should merge.
	if err := svc.Observe(identity.Signal{
		Email: "alice@company.com",
		Name:  "Alice Smith",
		Slack: &identity.SlackIdentity{Workspace: "acme", ID: "U04ABCDEF", DisplayName: "Alice Smith"},
	}); err != nil {
		t.Fatal(err)
	}

	// Should find one person via email or Slack ID.
	p := lookup(t, svc, "U04ABCDEF")
	if p.Name != "Alice Smith" {
		t.Errorf("name = %q, want %q (should have updated)", p.Name, "Alice Smith")
	}
	if p.Slack["acme"].ID != "U04ABCDEF" {
		t.Error("slack identity should have been merged")
	}
}

func TestMerge_SlackIDMatch(t *testing.T) {
	svc, _ := testService(t)

	if err := svc.Observe(identity.Signal{
		Name:  "Bob",
		Slack: &identity.SlackIdentity{Workspace: "acme", ID: "U05BCDEFG", DisplayName: "bob"},
	}); err != nil {
		t.Fatal(err)
	}

	// Signal with email + same Slack ID → merge.
	if err := svc.Observe(identity.Signal{
		Email: "bob@company.com",
		Name:  "Bob Jones",
		Slack: &identity.SlackIdentity{Workspace: "acme", ID: "U05BCDEFG", DisplayName: "Bob Jones"},
	}); err != nil {
		t.Fatal(err)
	}

	p := lookup(t, svc, "U05BCDEFG")
	if len(p.Email) != 1 || p.Email[0] != "bob@company.com" {
		t.Errorf("email = %v, want [bob@company.com]", p.Email)
	}
	if p.Slack["acme"].DisplayName != "Bob Jones" {
		t.Errorf("displayName = %q, want %q", p.Slack["acme"].DisplayName, "Bob Jones")
	}
}

func TestMerge_PhoneMatch(t *testing.T) {
	svc, _ := testService(t)

	if err := svc.Observe(identity.Signal{Phone: "+15559876543", Name: "Dave"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Observe(identity.Signal{Phone: "+15559876543", Name: "Dave Wilson"}); err != nil {
		t.Fatal(err)
	}

	p := lookup(t, svc, "+15559876543")
	if p.Name != "Dave Wilson" {
		t.Errorf("name = %q, want %q", p.Name, "Dave Wilson")
	}
	if len(p.WhatsApp) != 1 {
		t.Errorf("whatsapp = %v, want 1 entry", p.WhatsApp)
	}
}

func TestMerge_NoMatchCreatesNew(t *testing.T) {
	svc, _ := testService(t)

	if err := svc.Observe(identity.Signal{Email: "alice@company.com", Name: "Alice"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Observe(identity.Signal{Phone: "+15559876543", Name: "Dave"}); err != nil {
		t.Fatal(err)
	}

	// Both should exist as separate people.
	_ = lookup(t, svc, "alice@company.com")
	_ = lookup(t, svc, "+15559876543")
}

func TestMerge_DuplicateEmailNotAppended(t *testing.T) {
	svc, _ := testService(t)

	if err := svc.Observe(identity.Signal{Email: "alice@company.com", Name: "Alice"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Observe(identity.Signal{Email: "alice@company.com", Name: "Alice Smith"}); err != nil {
		t.Fatal(err)
	}

	p := lookup(t, svc, "alice@company.com")
	if len(p.Email) != 1 {
		t.Errorf("email count = %d, want 1 (should not duplicate)", len(p.Email))
	}
}

func TestMerge_MultipleEmails(t *testing.T) {
	svc, _ := testService(t)

	if err := svc.Observe(identity.Signal{
		Email: "alice@company.com",
		Name:  "Alice",
		Slack: &identity.SlackIdentity{Workspace: "acme", ID: "U04ABCDEF", DisplayName: "Alice"},
	}); err != nil {
		t.Fatal(err)
	}

	// Different email, same Slack ID → merge, add second email.
	if err := svc.Observe(identity.Signal{
		Email: "alice.personal@gmail.com",
		Slack: &identity.SlackIdentity{Workspace: "acme", ID: "U04ABCDEF", DisplayName: "Alice"},
	}); err != nil {
		t.Fatal(err)
	}

	p := lookup(t, svc, "U04ABCDEF")
	if len(p.Email) != 2 {
		t.Errorf("email count = %d, want 2", len(p.Email))
	}
}

func TestMerge_MultiWorkspace(t *testing.T) {
	svc, _ := testService(t)

	if err := svc.Observe(identity.Signal{
		Email: "carol@company.com",
		Name:  "Carol Davis",
		Slack: &identity.SlackIdentity{Workspace: "acme", ID: "U06CDEFGH", DisplayName: "Carol Davis"},
	}); err != nil {
		t.Fatal(err)
	}

	if err := svc.Observe(identity.Signal{
		Email: "carol@company.com",
		Slack: &identity.SlackIdentity{Workspace: "vendor-ws", ID: "U09XYZABC", DisplayName: "Carol (Acme)"},
	}); err != nil {
		t.Fatal(err)
	}

	p := lookup(t, svc, "carol@company.com")
	if len(p.Slack) != 2 {
		t.Errorf("slack workspaces = %d, want 2", len(p.Slack))
	}
	if p.Slack["vendor-ws"].DisplayName != "Carol (Acme)" {
		t.Errorf("vendor displayName = %q, want %q", p.Slack["vendor-ws"].DisplayName, "Carol (Acme)")
	}
}

func TestPersistence_RoundTrip(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)
	dir := root.Identity("test")

	// Write with one service instance.
	svc1 := identity.NewService(s, dir)
	if err := svc1.ObserveBatch([]identity.Signal{
		{Email: "alice@company.com", Name: "Alice", Slack: &identity.SlackIdentity{Workspace: "acme", ID: "U04ABCDEF", DisplayName: "Alice"}},
		{Phone: "+15559876543", Name: "Dave"},
	}); err != nil {
		t.Fatal(err)
	}

	// Read with a fresh service instance (proves it went through disk).
	svc2 := identity.NewService(s, dir)

	alice := lookup(t, svc2, "U04ABCDEF")
	if alice.Slack["acme"].ID != "U04ABCDEF" {
		t.Error("alice slack ID not persisted")
	}

	dave := lookup(t, svc2, "+15559876543")
	if dave.WhatsApp[0] != "+15559876543" {
		t.Error("dave phone not persisted")
	}
}

func TestLoadPeople_MissingFile(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)
	svc := identity.NewService(s, root.Identity("nonexistent"))

	results, err := svc.SearchCandidates("anything")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("got %d results from missing file, want 0", len(results))
	}
}

func TestObserveBatch_Empty(t *testing.T) {
	svc, _ := testService(t)
	if err := svc.ObserveBatch(nil); err != nil {
		t.Fatal(err)
	}
}

func TestAtomicWrite_TempFileCleanup(t *testing.T) {
	svc, dir := testService(t)

	if err := svc.Observe(identity.Signal{Email: "test@example.com", Name: "Test"}); err != nil {
		t.Fatal(err)
	}

	path := dir.PeopleFile()

	// The .tmp file should not exist after a successful write.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("temp file should not exist after successful write")
	}

	// The actual file should exist.
	if _, err := os.Stat(path); err != nil {
		t.Errorf("people file should exist: %v", err)
	}
}

func TestSearchCandidates(t *testing.T) {
	cases := []struct {
		name        string
		signals     []identity.Signal
		query       string
		wantLen     int
		wantName    string   // when len(got)==1, expected Person.Name
		wantSlackID string   // when len(got)==1, expected Slack["w"].ID
		wantNames   []string // when non-nil, expected Person.Name set (order-independent)
	}{
		{
			name: "slack ID exact",
			signals: []identity.Signal{
				{Name: "Alice", Slack: &identity.SlackIdentity{Workspace: "w", ID: "U111", DisplayName: "alice"}},
				{Name: "Bob", Slack: &identity.SlackIdentity{Workspace: "w", ID: "U222", DisplayName: "bob"}},
			},
			query:       "U111",
			wantLen:     1,
			wantName:    "Alice",
			wantSlackID: "U111",
		},
		{
			name: "email exact",
			signals: []identity.Signal{
				{Name: "Alice", Email: "alice@company.com"},
				{Name: "Bob", Email: "bob@company.com"},
			},
			query:    "Alice@Company.Com",
			wantLen:  1,
			wantName: "Alice",
		},
		{
			name: "name substring single match",
			signals: []identity.Signal{
				{Name: "Alice Smith", Slack: &identity.SlackIdentity{Workspace: "w", ID: "U1", DisplayName: "asmith"}},
				{Name: "Bob Jones", Slack: &identity.SlackIdentity{Workspace: "w", ID: "U2", DisplayName: "bjones"}},
			},
			query:    "smith",
			wantLen:  1,
			wantName: "Alice Smith",
		},
		{
			name: "name substring ambiguous",
			signals: []identity.Signal{
				{Name: "Alex One", Slack: &identity.SlackIdentity{Workspace: "w", ID: "U1", DisplayName: "Alex"}},
				{Name: "Alex Two", Slack: &identity.SlackIdentity{Workspace: "w", ID: "U2", DisplayName: "Alex"}},
			},
			query:     "Alex",
			wantLen:   2,
			wantNames: []string{"Alex One", "Alex Two"},
		},
		{
			name: "ID precedence over shared name substring",
			signals: []identity.Signal{
				{Name: "Pat", Slack: &identity.SlackIdentity{Workspace: "w", ID: "U99", DisplayName: "Pat"}},
				{Name: "Pat Lee", Slack: &identity.SlackIdentity{Workspace: "w", ID: "U88", DisplayName: "Pat"}},
			},
			query:       "U99",
			wantLen:     1,
			wantName:    "Pat",
			wantSlackID: "U99",
		},
		{
			name: "empty query",
			signals: []identity.Signal{
				{Name: "X", Email: "x@y.z"},
			},
			query:   "   ",
			wantLen: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := testService(t)
			if err := svc.ObserveBatch(tc.signals); err != nil {
				t.Fatal(err)
			}

			got, err := svc.SearchCandidates(tc.query)
			if err != nil {
				t.Fatalf("SearchCandidates: %v", err)
			}
			if len(got) != tc.wantLen {
				t.Fatalf("len(got) = %d, want %d (got %#v)", len(got), tc.wantLen, got)
			}
			if tc.wantNames != nil {
				names := make([]string, len(got))
				for i := range got {
					names[i] = got[i].Name
				}
				slices.Sort(names)
				want := slices.Clone(tc.wantNames)
				slices.Sort(want)
				if !slices.Equal(names, want) {
					t.Fatalf("names = %v, want %v", names, want)
				}
			}
			if tc.wantLen == 1 {
				if tc.wantName != "" && got[0].Name != tc.wantName {
					t.Errorf("Name = %q, want %q", got[0].Name, tc.wantName)
				}
				if tc.wantSlackID != "" {
					if got[0].Slack["w"].ID != tc.wantSlackID {
						t.Errorf("Slack[w].ID = %q, want %q", got[0].Slack["w"].ID, tc.wantSlackID)
					}
				}
			}
		})
	}
}
