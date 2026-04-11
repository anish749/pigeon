package identity_test

import (
	"os"
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

func TestObserve_NewPerson(t *testing.T) {
	svc, _ := testService(t)

	err := svc.Observe(identity.Signal{
		Email: "alice@company.com",
		Name:  "Alice Smith",
	})
	if err != nil {
		t.Fatal(err)
	}

	people, err := svc.People()
	if err != nil {
		t.Fatal(err)
	}
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

	people, _ := svc.People()
	if len(people) != 2 {
		t.Fatalf("got %d people, want 2", len(people))
	}

	// Verify Alice has both email and Slack.
	alice := people[0]
	if alice.Slack["acme"].ID != "U04ABCDEF" {
		t.Errorf("alice slack ID = %q, want U04ABCDEF", alice.Slack["acme"].ID)
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

	people, _ := svc.People()
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
	svc, _ := testService(t)

	// First: Slack-only signal.
	if err := svc.Observe(identity.Signal{
		Name:  "Bob",
		Slack: &identity.SlackIdentity{Workspace: "acme", ID: "U05BCDEFG", DisplayName: "bob"},
	}); err != nil {
		t.Fatal(err)
	}

	// Second: signal with email + same Slack ID → merge.
	if err := svc.Observe(identity.Signal{
		Email: "bob@company.com",
		Name:  "Bob Jones",
		Slack: &identity.SlackIdentity{Workspace: "acme", ID: "U05BCDEFG", DisplayName: "Bob Jones"},
	}); err != nil {
		t.Fatal(err)
	}

	people, _ := svc.People()
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
	svc, _ := testService(t)

	if err := svc.Observe(identity.Signal{Phone: "+15559876543", Name: "Dave"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Observe(identity.Signal{Phone: "+15559876543", Name: "Dave Wilson"}); err != nil {
		t.Fatal(err)
	}

	people, _ := svc.People()
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
	svc, _ := testService(t)

	if err := svc.Observe(identity.Signal{Email: "alice@company.com", Name: "Alice"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Observe(identity.Signal{Phone: "+15559876543", Name: "Dave"}); err != nil {
		t.Fatal(err)
	}

	people, _ := svc.People()
	if len(people) != 2 {
		t.Fatalf("got %d people, want 2 (no common identifier)", len(people))
	}
}

func TestMerge_DuplicateEmailNotAppended(t *testing.T) {
	svc, _ := testService(t)

	if err := svc.Observe(identity.Signal{Email: "alice@company.com", Name: "Alice"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Observe(identity.Signal{Email: "alice@company.com", Name: "Alice Smith"}); err != nil {
		t.Fatal(err)
	}

	people, _ := svc.People()
	if len(people[0].Email) != 1 {
		t.Errorf("email count = %d, want 1 (should not duplicate)", len(people[0].Email))
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

	people, _ := svc.People()
	if len(people) != 1 {
		t.Fatalf("got %d people, want 1", len(people))
	}
	if len(people[0].Email) != 2 {
		t.Errorf("email count = %d, want 2", len(people[0].Email))
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

	people, _ := svc.People()
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
	people, err := svc2.People()
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
	svc := identity.NewService(s, root.Identity("nonexistent"))

	people, err := svc.People()
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 0 {
		t.Errorf("got %d people from missing file, want 0", len(people))
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
