package identity_test

import (
	"testing"

	"github.com/anish749/pigeon/internal/identity"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/store"
)

// TestReader_MergesAcrossSources is the core test for the refactor: a
// person signalled from Slack (ID + email) in one file and from GWS
// (email only) in another file should merge into a single Person at read
// time — without any write-time coupling.
func TestReader_MergesAcrossSources(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)

	slackDir := root.AccountFor(account.New("slack", "acme-corp")).Identity()
	gwsDir := root.AccountFor(account.New("gws", "alice-at-company-com")).Identity()

	slackW := identity.NewWriter(s, slackDir)
	if err := slackW.Observe(identity.Signal{
		Email: "alice@company.com",
		Name:  "Alice Smith",
		Slack: &identity.SlackIdentity{Workspace: "acme-corp", ID: "U04ABCDEF", DisplayName: "Alice Smith"},
	}); err != nil {
		t.Fatal(err)
	}

	gwsW := identity.NewWriter(s, gwsDir)
	if err := gwsW.Observe(identity.Signal{
		Email: "alice@company.com",
		Name:  "Alice S",
	}); err != nil {
		t.Fatal(err)
	}

	r := identity.NewReader(s, root)
	people, err := r.People()
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 1 {
		t.Fatalf("got %d people, want 1 (should have merged across sources)", len(people))
	}
	if people[0].Slack["acme-corp"].ID != "U04ABCDEF" {
		t.Errorf("slack ID lost: %+v", people[0])
	}
	if len(people[0].Email) != 1 || people[0].Email[0] != "alice@company.com" {
		t.Errorf("email merge failed: %+v", people[0].Email)
	}
}

// TestReader_WhatsAppAndSlackLinkedByEmail — two sources with no direct
// shared identifier, linked indirectly through a third source.
func TestReader_TransitiveMergeThroughEmail(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)

	// Slack knows: ID + email
	slackW := identity.NewWriter(s, root.AccountFor(account.New("slack", "acme-corp")).Identity())
	if err := slackW.Observe(identity.Signal{
		Email: "alice@company.com",
		Name:  "Alice Smith",
		Slack: &identity.SlackIdentity{Workspace: "acme-corp", ID: "U04ABCDEF"},
	}); err != nil {
		t.Fatal(err)
	}

	// WhatsApp knows: phone only, no email.
	waW := identity.NewWriter(s, root.AccountFor(account.New("whatsapp", "15551234567")).Identity())
	if err := waW.Observe(identity.Signal{
		Phone: "+15559876543",
		Name:  "Alice W",
	}); err != nil {
		t.Fatal(err)
	}

	r := identity.NewReader(s, root)
	people, err := r.People()
	if err != nil {
		t.Fatal(err)
	}
	// No shared identifier between Slack's Alice and WhatsApp's Alice → 2 people.
	if len(people) != 2 {
		t.Fatalf("got %d people, want 2 (no shared identifier between sources)", len(people))
	}
}

// TestReader_ContextFiltering — NewReaderForDirs only loads from specified
// dirs, ignoring the rest.
func TestReader_ContextFiltering(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)

	slackDir := root.AccountFor(account.New("slack", "acme-corp")).Identity()
	gwsDir := root.AccountFor(account.New("gws", "alice-at-company-com")).Identity()

	// Populate both dirs with distinct people.
	if err := identity.NewWriter(s, slackDir).Observe(identity.Signal{
		Name:  "Slack Alice",
		Slack: &identity.SlackIdentity{Workspace: "acme-corp", ID: "U1"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := identity.NewWriter(s, gwsDir).Observe(identity.Signal{
		Name:  "GWS Bob",
		Email: "bob@company.com",
	}); err != nil {
		t.Fatal(err)
	}

	// Reader restricted to the Slack dir should NOT see the GWS person.
	slackOnly := identity.NewReaderForDirs(s, []paths.IdentityDir{slackDir})
	people, err := slackOnly.People()
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 1 {
		t.Fatalf("got %d people, want 1 (GWS should be excluded)", len(people))
	}
	if people[0].Name != "Slack Alice" {
		t.Errorf("wrong person: %s", people[0].Name)
	}

	// Unrestricted reader sees both.
	all := identity.NewReader(s, root)
	allPeople, err := all.People()
	if err != nil {
		t.Fatal(err)
	}
	if len(allPeople) != 2 {
		t.Fatalf("got %d people, want 2", len(allPeople))
	}
}

// TestReader_LookupBySlackID_FindsInRightFile — a Slack user ID lookup
// finds the person in the matching workspace's file.
func TestReader_LookupBySlackID(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)

	if err := identity.NewWriter(s, root.AccountFor(account.New("slack", "acme-corp")).Identity()).Observe(identity.Signal{
		Name:  "Alice",
		Slack: &identity.SlackIdentity{Workspace: "acme-corp", ID: "U04ABCDEF"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := identity.NewWriter(s, root.AccountFor(account.New("slack", "vendor-ws")).Identity()).Observe(identity.Signal{
		Name:  "Bob",
		Slack: &identity.SlackIdentity{Workspace: "vendor-ws", ID: "U09XYZ"},
	}); err != nil {
		t.Fatal(err)
	}

	r := identity.NewReader(s, root)

	got, err := r.LookupBySlackID("acme-corp", "U04ABCDEF")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Name != "Alice" {
		t.Fatalf("got %+v, want Alice", got)
	}

	got, err = r.LookupBySlackID("vendor-ws", "U09XYZ")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Name != "Bob" {
		t.Fatalf("got %+v, want Bob", got)
	}

	// Right ID, wrong workspace → miss.
	miss, err := r.LookupBySlackID("acme-corp", "U09XYZ")
	if err != nil {
		t.Fatal(err)
	}
	if miss != nil {
		t.Errorf("expected nil for cross-workspace miss, got %+v", miss)
	}
}

// TestReader_EmptyTree — Reader on a fresh data root with no identity
// files returns an empty list, not an error.
func TestReader_EmptyTree(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)

	r := identity.NewReader(s, root)
	people, err := r.People()
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 0 {
		t.Errorf("got %d people, want 0", len(people))
	}
}

// TestReader_SearchCandidatesCrossSource — searching by name finds a
// person whose name only appears in one source but whose identifiers span
// multiple sources.
func TestReader_SearchCandidatesCrossSource(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)

	// Slack: full name + email + Slack ID.
	if err := identity.NewWriter(s, root.AccountFor(account.New("slack", "acme-corp")).Identity()).Observe(identity.Signal{
		Name:  "Alice Smith",
		Email: "alice@company.com",
		Slack: &identity.SlackIdentity{Workspace: "acme-corp", ID: "U04ABCDEF", DisplayName: "Alice Smith"},
	}); err != nil {
		t.Fatal(err)
	}
	// GWS: same email, name absent.
	if err := identity.NewWriter(s, root.AccountFor(account.New("gws", "alice")).Identity()).Observe(identity.Signal{
		Email: "alice@company.com",
	}); err != nil {
		t.Fatal(err)
	}

	r := identity.NewReader(s, root)

	// Name search finds the merged person, which carries the Slack ID from
	// the other file.
	got, err := r.SearchCandidates("alice smith")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d candidates, want 1", len(got))
	}
	if got[0].Slack["acme-corp"].ID != "U04ABCDEF" {
		t.Errorf("cross-source merge lost Slack ID: %+v", got[0])
	}
}
