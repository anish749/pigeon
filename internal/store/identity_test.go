package store_test

import (
	"os"
	"testing"

	"github.com/anish749/pigeon/internal/identity"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/store"
)

func testIdentityStore(t *testing.T) (*store.FSStore, paths.IdentityDir) {
	t.Helper()
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)
	return s, root.AccountFor(account.New("test", "acct")).Identity()
}

func TestLoadPeople_MissingFile(t *testing.T) {
	s, dir := testIdentityStore(t)
	people, err := s.LoadPeople(dir.PeopleFile())
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 0 {
		t.Errorf("got %d people from missing file, want 0", len(people))
	}
}

func TestSavePeople_CreatesDir(t *testing.T) {
	s, dir := testIdentityStore(t)

	people := []identity.Person{
		{Name: "Alice", Email: []string{"alice@company.com"}, Seen: "2026-04-11"},
	}
	if err := s.SavePeople(dir.PeopleFile(), people); err != nil {
		t.Fatal(err)
	}

	// Verify file exists.
	if _, err := os.Stat(string(dir.PeopleFile())); err != nil {
		t.Errorf("people file should exist: %v", err)
	}
}

func TestSavePeople_AtomicWrite(t *testing.T) {
	s, dir := testIdentityStore(t)

	people := []identity.Person{
		{Name: "Alice", Seen: "2026-04-11"},
	}
	if err := s.SavePeople(dir.PeopleFile(), people); err != nil {
		t.Fatal(err)
	}

	// No .tmp file should remain.
	if _, err := os.Stat(string(dir.PeopleFile()) + ".tmp"); !os.IsNotExist(err) {
		t.Error("temp file should not exist after successful write")
	}
}

func TestLoadSave_RoundTrip(t *testing.T) {
	s, dir := testIdentityStore(t)

	original := []identity.Person{
		{
			Name:  "Alice Smith",
			Email: []string{"alice@company.com"},
			Slack: map[string]identity.PersonSlack{
				"acme": {ID: "U04ABCDEF", DisplayName: "Alice Smith", RealName: "Alice S", Name: "alice"},
			},
			Seen: "2026-04-11",
		},
		{
			Name:     "Dave",
			WhatsApp: []string{"+15559876543"},
			Seen:     "2026-04-09",
		},
	}

	if err := s.SavePeople(dir.PeopleFile(), original); err != nil {
		t.Fatal(err)
	}

	loaded, err := s.LoadPeople(dir.PeopleFile())
	if err != nil {
		t.Fatal(err)
	}

	if len(loaded) != 2 {
		t.Fatalf("got %d people, want 2", len(loaded))
	}
	if loaded[0].Name != "Alice Smith" {
		t.Errorf("person 0 name = %q", loaded[0].Name)
	}
	if loaded[0].Slack["acme"].ID != "U04ABCDEF" {
		t.Error("person 0 slack ID not round-tripped")
	}
	if loaded[0].Slack["acme"].RealName != "Alice S" {
		t.Error("person 0 slack real name not round-tripped")
	}
	if loaded[1].WhatsApp[0] != "+15559876543" {
		t.Error("person 1 phone not round-tripped")
	}
}

func TestLoadPeople_SkipsMalformedLines(t *testing.T) {
	s, dir := testIdentityStore(t)

	// Write a file with one good line and one bad line.
	if err := os.MkdirAll(dir.Path(), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"name":"Alice","email":["alice@company.com"],"seen":"2026-04-11"}
this is not json
{"name":"Bob","seen":"2026-04-11"}
`
	if err := os.WriteFile(string(dir.PeopleFile()),[]byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	people, err := s.LoadPeople(dir.PeopleFile())
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 2 {
		t.Fatalf("got %d people, want 2 (malformed line skipped)", len(people))
	}
	if people[0].Name != "Alice" {
		t.Errorf("person 0 name = %q", people[0].Name)
	}
	if people[1].Name != "Bob" {
		t.Errorf("person 1 name = %q", people[1].Name)
	}
}

func TestSavePeople_EmptyList(t *testing.T) {
	s, dir := testIdentityStore(t)

	if err := s.SavePeople(dir.PeopleFile(), nil); err != nil {
		t.Fatal(err)
	}

	// File should exist but be empty.
	data, err := os.ReadFile(string(dir.PeopleFile()))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 {
		t.Errorf("expected empty file, got %d bytes", len(data))
	}
}

func TestSavePeople_OverwritesPrevious(t *testing.T) {
	s, dir := testIdentityStore(t)

	// Write two people.
	if err := s.SavePeople(dir.PeopleFile(), []identity.Person{
		{Name: "Alice", Seen: "2026-04-11"},
		{Name: "Bob", Seen: "2026-04-11"},
	}); err != nil {
		t.Fatal(err)
	}

	// Overwrite with one person.
	if err := s.SavePeople(dir.PeopleFile(), []identity.Person{
		{Name: "Carol", Seen: "2026-04-11"},
	}); err != nil {
		t.Fatal(err)
	}

	loaded, err := s.LoadPeople(dir.PeopleFile())
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 {
		t.Fatalf("got %d people, want 1 (overwrite)", len(loaded))
	}
	if loaded[0].Name != "Carol" {
		t.Errorf("person name = %q, want Carol", loaded[0].Name)
	}
}

func TestLoadPeople_SkipsEmptyLines(t *testing.T) {
	s, dir := testIdentityStore(t)

	if err := os.MkdirAll(dir.Path(), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"name":"Alice","seen":"2026-04-11"}

{"name":"Bob","seen":"2026-04-11"}
`
	if err := os.WriteFile(string(dir.PeopleFile()),[]byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	people, err := s.LoadPeople(dir.PeopleFile())
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 2 {
		t.Fatalf("got %d people, want 2 (empty line skipped)", len(people))
	}
}
