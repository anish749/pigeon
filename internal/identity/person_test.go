package identity

import "testing"

func TestMatchesEmail_Exact(t *testing.T) {
	p := Person{Email: []string{"alice@company.com"}}
	if !p.matchesEmail("alice@company.com") {
		t.Error("exact match should succeed")
	}
}

func TestMatchesEmail_NoEmails(t *testing.T) {
	p := Person{}
	if p.matchesEmail("alice@company.com") {
		t.Error("empty email list should never match")
	}
}

func TestHasExactEmail_CaseInsensitive(t *testing.T) {
	p := Person{Email: []string{"Alice@Company.com"}}
	if !p.hasExactEmail("alice@company.com") {
		t.Error("case-insensitive exact check should match")
	}
}

func TestHasExactEmail_NotFound(t *testing.T) {
	p := Person{Email: []string{"alice@company.com"}}
	if p.hasExactEmail("bob@company.com") {
		t.Error("different email should not match")
	}
}

func TestMatchesSlackID_Found(t *testing.T) {
	p := Person{Slack: map[string]PersonSlack{
		"acme": {ID: "U04ABCDEF"},
	}}
	if !p.matchesSlackID("U04ABCDEF") {
		t.Error("exact slack ID match should succeed")
	}
}

func TestMatchesSlackID_WrongID(t *testing.T) {
	p := Person{Slack: map[string]PersonSlack{
		"acme": {ID: "U04ABCDEF"},
	}}
	if p.matchesSlackID("U99XXXXXX") {
		t.Error("wrong slack ID should not match")
	}
}

func TestMatchesSlackID_MultipleWorkspaces(t *testing.T) {
	p := Person{Slack: map[string]PersonSlack{
		"acme":   {ID: "U04ABCDEF"},
		"vendor": {ID: "U09XYZABC"},
	}}
	if !p.matchesSlackID("U09XYZABC") {
		t.Error("should match slack ID in any workspace")
	}
}

func TestMatchesSlackID_NoSlack(t *testing.T) {
	p := Person{}
	if p.matchesSlackID("U04ABCDEF") {
		t.Error("nil slack map should never match")
	}
}

func TestMatchesPhone_Found(t *testing.T) {
	p := Person{WhatsApp: []string{"+15551234567"}}
	if !p.matchesPhone("+15551234567") {
		t.Error("exact phone match should succeed")
	}
}

func TestMatchesPhone_NotFound(t *testing.T) {
	p := Person{WhatsApp: []string{"+15551234567"}}
	if p.matchesPhone("+19999999999") {
		t.Error("different phone should not match")
	}
}

func TestMatchesPhone_NoPhones(t *testing.T) {
	p := Person{}
	if p.matchesPhone("+15551234567") {
		t.Error("empty phone list should never match")
	}
}

func TestMerge_UpdatesName(t *testing.T) {
	p := Person{Name: "Alice", Seen: "2026-01-01"}
	p.merge(Signal{Name: "Alice Smith"}, "2026-04-11")
	if p.Name != "Alice Smith" {
		t.Errorf("name = %q, want %q", p.Name, "Alice Smith")
	}
}

func TestMerge_EmptyNameDoesNotOverwrite(t *testing.T) {
	p := Person{Name: "Alice", Seen: "2026-01-01"}
	p.merge(Signal{Email: "alice@company.com"}, "2026-04-11")
	if p.Name != "Alice" {
		t.Errorf("empty signal name should not overwrite; got %q", p.Name)
	}
}

func TestMerge_AppendsNewEmail(t *testing.T) {
	p := Person{Email: []string{"alice@company.com"}, Seen: "2026-01-01"}
	p.merge(Signal{Email: "alice.personal@gmail.com"}, "2026-04-11")
	if len(p.Email) != 2 {
		t.Fatalf("email count = %d, want 2", len(p.Email))
	}
}

func TestMerge_DoesNotDuplicateEmail(t *testing.T) {
	p := Person{Email: []string{"alice@company.com"}, Seen: "2026-01-01"}
	p.merge(Signal{Email: "Alice@Company.com"}, "2026-04-11")
	if len(p.Email) != 1 {
		t.Errorf("email count = %d, want 1 (case-insensitive dedup)", len(p.Email))
	}
}

func TestMerge_AddsSlackWorkspace(t *testing.T) {
	p := Person{Seen: "2026-01-01"}
	p.merge(Signal{Slack: &SlackIdentity{Workspace: "acme", ID: "U04ABCDEF", DisplayName: "Alice"}}, "2026-04-11")
	if p.Slack["acme"].ID != "U04ABCDEF" {
		t.Error("slack workspace should be added")
	}
}

func TestMerge_UpdatesSlackWorkspace(t *testing.T) {
	p := Person{
		Slack: map[string]PersonSlack{"acme": {ID: "U04ABCDEF", DisplayName: "alice"}},
		Seen:  "2026-01-01",
	}
	p.merge(Signal{Slack: &SlackIdentity{Workspace: "acme", ID: "U04ABCDEF", DisplayName: "Alice Smith"}}, "2026-04-11")
	if p.Slack["acme"].DisplayName != "Alice Smith" {
		t.Errorf("slack display name = %q, want %q", p.Slack["acme"].DisplayName, "Alice Smith")
	}
}

func TestMerge_AppendsNewPhone(t *testing.T) {
	p := Person{WhatsApp: []string{"+15551234567"}, Seen: "2026-01-01"}
	p.merge(Signal{Phone: "+15559876543"}, "2026-04-11")
	if len(p.WhatsApp) != 2 {
		t.Fatalf("phone count = %d, want 2", len(p.WhatsApp))
	}
}

func TestMerge_DoesNotDuplicatePhone(t *testing.T) {
	p := Person{WhatsApp: []string{"+15551234567"}, Seen: "2026-01-01"}
	p.merge(Signal{Phone: "+15551234567"}, "2026-04-11")
	if len(p.WhatsApp) != 1 {
		t.Errorf("phone count = %d, want 1", len(p.WhatsApp))
	}
}

func TestMerge_UpdatesSeen(t *testing.T) {
	p := Person{Seen: "2026-01-01"}
	p.merge(Signal{Name: "x"}, "2026-04-11")
	if p.Seen != "2026-04-11" {
		t.Errorf("seen = %q, want %q", p.Seen, "2026-04-11")
	}
}

func TestMerge_DoesNotRegressSeen(t *testing.T) {
	p := Person{Seen: "2026-04-11"}
	p.merge(Signal{Name: "x"}, "2026-01-01")
	if p.Seen != "2026-04-11" {
		t.Errorf("seen should not regress; got %q", p.Seen)
	}
}

func TestFindMatch_EmailFirst(t *testing.T) {
	people := []Person{
		{Email: []string{"alice@company.com"}},
		{WhatsApp: []string{"+15551234567"}},
	}
	idx := findMatch(people, Signal{Email: "alice@company.com", Phone: "+15551234567"})
	if idx != 0 {
		t.Errorf("findMatch = %d, want 0 (email match takes priority)", idx)
	}
}

func TestFindMatch_SlackID(t *testing.T) {
	people := []Person{
		{Slack: map[string]PersonSlack{"acme": {ID: "U04ABCDEF"}}},
	}
	idx := findMatch(people, Signal{Slack: &SlackIdentity{ID: "U04ABCDEF"}})
	if idx != 0 {
		t.Errorf("findMatch = %d, want 0", idx)
	}
}

func TestFindMatch_Phone(t *testing.T) {
	people := []Person{
		{WhatsApp: []string{"+15551234567"}},
	}
	idx := findMatch(people, Signal{Phone: "+15551234567"})
	if idx != 0 {
		t.Errorf("findMatch = %d, want 0", idx)
	}
}

func TestFindMatch_NoMatch(t *testing.T) {
	people := []Person{
		{Email: []string{"alice@company.com"}},
	}
	idx := findMatch(people, Signal{Phone: "+15551234567"})
	if idx != -1 {
		t.Errorf("findMatch = %d, want -1", idx)
	}
}

func TestFindMatch_EmptyPeople(t *testing.T) {
	idx := findMatch(nil, Signal{Email: "alice@company.com"})
	if idx != -1 {
		t.Errorf("findMatch on nil = %d, want -1", idx)
	}
}

func TestNewPerson_AllFields(t *testing.T) {
	sig := Signal{
		Email: "alice@company.com",
		Name:  "Alice",
		Slack: &SlackIdentity{Workspace: "acme", ID: "U04ABCDEF", DisplayName: "Alice S"},
		Phone: "+15551234567",
	}
	p := newPerson(sig, "2026-04-11")

	if p.Name != "Alice" {
		t.Errorf("name = %q", p.Name)
	}
	if len(p.Email) != 1 || p.Email[0] != "alice@company.com" {
		t.Errorf("email = %v", p.Email)
	}
	if p.Slack["acme"].ID != "U04ABCDEF" {
		t.Error("slack not set")
	}
	if len(p.WhatsApp) != 1 || p.WhatsApp[0] != "+15551234567" {
		t.Errorf("whatsapp = %v", p.WhatsApp)
	}
	if p.Seen != "2026-04-11" {
		t.Errorf("seen = %q", p.Seen)
	}
}

func TestNewPerson_MinimalSignal(t *testing.T) {
	p := newPerson(Signal{Email: "bob@company.com", Name: "Bob"}, "2026-04-11")
	if p.Slack != nil {
		t.Error("slack should be nil for email-only signal")
	}
	if p.WhatsApp != nil {
		t.Error("whatsapp should be nil for email-only signal")
	}
}
