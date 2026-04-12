package identity_test

import (
	"os"
	"slices"
	"testing"

	"github.com/anish749/pigeon/internal/identity"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
)

// testWriterAndReader returns a Writer and a Reader both scoped to the same
// single identity directory. Writer tests need a read path to assert what
// was persisted; since per-source reads are just "the Reader over a single
// dir", we use it here.
func testWriterAndReader(t *testing.T) (*identity.Writer, *identity.Reader, paths.IdentityDir) {
	t.Helper()
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)
	dir := root.Platform("test").AccountFromSlug("acct").Identity()
	w := identity.NewWriter(s, dir)
	r := identity.NewReaderForDirs(s, []paths.IdentityDir{dir})
	return w, r, dir
}

func readAll(t *testing.T, r *identity.Reader) []identity.Person {
	t.Helper()
	people, err := r.People()
	if err != nil {
		t.Fatal(err)
	}
	return people
}

func TestObserve_NewPerson(t *testing.T) {
	w, r, _ := testWriterAndReader(t)

	err := w.Observe(identity.Signal{
		Email: "alice@company.com",
		Name:  "Alice Smith",
	})
	if err != nil {
		t.Fatal(err)
	}

	people := readAll(t, r)
	if len(people) != 1 {
		t.Fatalf("got %d people, want 1", len(people))
	}
	if people[0].Name != "Alice Smith" {
		t.Errorf("name = %q, want %q", people[0].Name, "Alice Smith")
	}
	if len(people[0].Email) != 1 || people[0].Email[0] != "alice@company.com" {
		t.Errorf("email = %v, want [alice@company.com]", people[0].Email)
	}
}

func TestObserveBatch_SlackUsers(t *testing.T) {
	w, r, _ := testWriterAndReader(t)

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
	if err := w.ObserveBatch(signals); err != nil {
		t.Fatal(err)
	}

	people := readAll(t, r)
	if len(people) != 2 {
		t.Fatalf("got %d people, want 2", len(people))
	}

	alice := people[0]
	if alice.Slack["acme"].ID != "U04ABCDEF" {
		t.Errorf("alice slack ID = %q, want U04ABCDEF", alice.Slack["acme"].ID)
	}
}

func TestMerge_EmailMatch(t *testing.T) {
	w, r, _ := testWriterAndReader(t)

	if err := w.Observe(identity.Signal{
		Email: "alice@company.com",
		Name:  "Alice S",
	}); err != nil {
		t.Fatal(err)
	}

	if err := w.Observe(identity.Signal{
		Email: "alice@company.com",
		Name:  "Alice Smith",
		Slack: &identity.SlackIdentity{Workspace: "acme", ID: "U04ABCDEF", DisplayName: "Alice Smith"},
	}); err != nil {
		t.Fatal(err)
	}

	people := readAll(t, r)
	if len(people) != 1 {
		t.Fatalf("got %d people, want 1 (should have merged)", len(people))
	}
	if people[0].Name != "Alice Smith" {
		t.Errorf("name = %q, want %q (should have updated)", people[0].Name, "Alice Smith")
	}
	if people[0].Slack["acme"].ID != "U04ABCDEF" {
		t.Error("slack identity should have been merged")
	}
}

func TestMerge_SlackIDMatch(t *testing.T) {
	w, r, _ := testWriterAndReader(t)

	if err := w.Observe(identity.Signal{
		Name:  "Bob",
		Slack: &identity.SlackIdentity{Workspace: "acme", ID: "U05BCDEFG", DisplayName: "bob"},
	}); err != nil {
		t.Fatal(err)
	}

	if err := w.Observe(identity.Signal{
		Email: "bob@company.com",
		Name:  "Bob Jones",
		Slack: &identity.SlackIdentity{Workspace: "acme", ID: "U05BCDEFG", DisplayName: "Bob Jones"},
	}); err != nil {
		t.Fatal(err)
	}

	people := readAll(t, r)
	if len(people) != 1 {
		t.Fatalf("got %d people, want 1", len(people))
	}
	if len(people[0].Email) != 1 || people[0].Email[0] != "bob@company.com" {
		t.Errorf("email = %v, want [bob@company.com]", people[0].Email)
	}
	if people[0].Slack["acme"].DisplayName != "Bob Jones" {
		t.Errorf("displayName = %q, want %q", people[0].Slack["acme"].DisplayName, "Bob Jones")
	}
}

func TestMerge_PhoneMatch(t *testing.T) {
	w, r, _ := testWriterAndReader(t)

	if err := w.Observe(identity.Signal{Phone: "+15559876543", Name: "Dave"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Observe(identity.Signal{Phone: "+15559876543", Name: "Dave Wilson"}); err != nil {
		t.Fatal(err)
	}

	people := readAll(t, r)
	if len(people) != 1 {
		t.Fatalf("got %d people, want 1", len(people))
	}
	if people[0].Name != "Dave Wilson" {
		t.Errorf("name = %q, want %q", people[0].Name, "Dave Wilson")
	}
	if len(people[0].WhatsApp) != 1 {
		t.Errorf("whatsapp = %v, want 1 entry", people[0].WhatsApp)
	}
}

func TestMerge_NoMatchCreatesNew(t *testing.T) {
	w, r, _ := testWriterAndReader(t)

	if err := w.Observe(identity.Signal{Email: "alice@company.com", Name: "Alice"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Observe(identity.Signal{Phone: "+15559876543", Name: "Dave"}); err != nil {
		t.Fatal(err)
	}

	people := readAll(t, r)
	if len(people) != 2 {
		t.Fatalf("got %d people, want 2 (no common identifier)", len(people))
	}
}

func TestMerge_DuplicateEmailNotAppended(t *testing.T) {
	w, r, _ := testWriterAndReader(t)

	if err := w.Observe(identity.Signal{Email: "alice@company.com", Name: "Alice"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Observe(identity.Signal{Email: "alice@company.com", Name: "Alice Smith"}); err != nil {
		t.Fatal(err)
	}

	people := readAll(t, r)
	if len(people[0].Email) != 1 {
		t.Errorf("email count = %d, want 1 (should not duplicate)", len(people[0].Email))
	}
}

func TestMerge_MultipleEmails(t *testing.T) {
	w, r, _ := testWriterAndReader(t)

	if err := w.Observe(identity.Signal{
		Email: "alice@company.com",
		Name:  "Alice",
		Slack: &identity.SlackIdentity{Workspace: "acme", ID: "U04ABCDEF", DisplayName: "Alice"},
	}); err != nil {
		t.Fatal(err)
	}

	if err := w.Observe(identity.Signal{
		Email: "alice.personal@gmail.com",
		Slack: &identity.SlackIdentity{Workspace: "acme", ID: "U04ABCDEF", DisplayName: "Alice"},
	}); err != nil {
		t.Fatal(err)
	}

	people := readAll(t, r)
	if len(people) != 1 {
		t.Fatalf("got %d people, want 1", len(people))
	}
	if len(people[0].Email) != 2 {
		t.Errorf("email count = %d, want 2", len(people[0].Email))
	}
}

func TestMerge_MultiWorkspace(t *testing.T) {
	w, r, _ := testWriterAndReader(t)

	if err := w.Observe(identity.Signal{
		Email: "carol@company.com",
		Name:  "Carol Davis",
		Slack: &identity.SlackIdentity{Workspace: "acme", ID: "U06CDEFGH", DisplayName: "Carol Davis"},
	}); err != nil {
		t.Fatal(err)
	}

	if err := w.Observe(identity.Signal{
		Email: "carol@company.com",
		Slack: &identity.SlackIdentity{Workspace: "vendor-ws", ID: "U09XYZABC", DisplayName: "Carol (Acme)"},
	}); err != nil {
		t.Fatal(err)
	}

	people := readAll(t, r)
	if len(people) != 1 {
		t.Fatalf("got %d people, want 1", len(people))
	}
	if len(people[0].Slack) != 2 {
		t.Errorf("slack workspaces = %d, want 2", len(people[0].Slack))
	}
	if people[0].Slack["vendor-ws"].DisplayName != "Carol (Acme)" {
		t.Errorf("vendor displayName = %q, want %q", people[0].Slack["vendor-ws"].DisplayName, "Carol (Acme)")
	}
}

func TestPersistence_RoundTrip(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)
	dir := root.Platform("test").AccountFromSlug("acct").Identity()

	w1 := identity.NewWriter(s, dir)
	if err := w1.ObserveBatch([]identity.Signal{
		{Email: "alice@company.com", Name: "Alice", Slack: &identity.SlackIdentity{Workspace: "acme", ID: "U04ABCDEF", DisplayName: "Alice"}},
		{Phone: "+15559876543", Name: "Dave"},
	}); err != nil {
		t.Fatal(err)
	}

	// Read with a fresh reader (proves it went through disk).
	r := identity.NewReaderForDirs(s, []paths.IdentityDir{dir})
	people, err := r.People()
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 2 {
		t.Fatalf("got %d people, want 2", len(people))
	}
	if people[0].Slack["acme"].ID != "U04ABCDEF" {
		t.Error("alice slack ID not persisted")
	}
	if people[1].WhatsApp[0] != "+15559876543" {
		t.Error("dave phone not persisted")
	}
}

func TestLoadPeople_MissingFile(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)
	r := identity.NewReaderForDirs(s, []paths.IdentityDir{root.Platform("nonexistent").AccountFromSlug("acct").Identity()})

	people, err := r.People()
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 0 {
		t.Errorf("got %d people from missing file, want 0", len(people))
	}
}

func TestObserveBatch_Empty(t *testing.T) {
	w, _, _ := testWriterAndReader(t)
	if err := w.ObserveBatch(nil); err != nil {
		t.Fatal(err)
	}
}

func TestAtomicWrite_TempFileCleanup(t *testing.T) {
	w, _, dir := testWriterAndReader(t)

	if err := w.Observe(identity.Signal{Email: "test@example.com", Name: "Test"}); err != nil {
		t.Fatal(err)
	}

	path := dir.PeopleFile()

	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("temp file should not exist after successful write")
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("people file should exist: %v", err)
	}
}

func TestLookupBySlackID(t *testing.T) {
	w, _, _ := testWriterAndReader(t)

	if err := w.Observe(identity.Signal{
		Name:  "Alice",
		Email: "alice@company.com",
		Slack: &identity.SlackIdentity{Workspace: "acme", ID: "U04ABCDEF", DisplayName: "Alice"},
	}); err != nil {
		t.Fatal(err)
	}

	got, err := w.LookupBySlackID("acme", "U04ABCDEF")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected person, got nil")
	}
	if got.Name != "Alice" {
		t.Errorf("name = %q, want Alice", got.Name)
	}

	miss, err := w.LookupBySlackID("acme", "UNONEXISTENT")
	if err != nil {
		t.Fatal(err)
	}
	if miss != nil {
		t.Errorf("expected nil for missing ID, got %+v", miss)
	}
}

func TestSearchCandidates(t *testing.T) {
	cases := []struct {
		name        string
		signals     []identity.Signal
		query       string
		wantLen     int
		wantName    string
		wantSlackID string
		wantNames   []string
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
			w, r, _ := testWriterAndReader(t)
			if err := w.ObserveBatch(tc.signals); err != nil {
				t.Fatal(err)
			}

			got, err := r.SearchCandidates(tc.query)
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
