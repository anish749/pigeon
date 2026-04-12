package identity

import (
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
)

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

func TestMatchesAnyExactID(t *testing.T) {
	cases := []struct {
		name  string
		p     Person
		q     string
		want  bool
	}{
		{
			name: "empty query",
			p:    Person{Email: []string{"a@b.c"}},
			q:    "",
			want: false,
		},
		{
			name: "slack ID",
			p: Person{Slack: map[string]PersonSlack{
				"w": {ID: "U111"},
			}},
			q:    "U111",
			want: true,
		},
		{
			name: "phone",
			p:    Person{WhatsApp: []string{"+15551234567"}},
			q:    "+15551234567",
			want: true,
		},
		{
			name: "email hasExactEmail",
			p:    Person{Email: []string{"Alice@Company.com"}},
			q:    "alice@company.com",
			want: true,
		},
		{
			name: "no match",
			p:    Person{Name: "Pat"},
			q:    "Pat",
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.p.matchesAnyExactID(tc.q); got != tc.want {
				t.Errorf("matchesAnyExactID(%q) = %v, want %v", tc.q, got, tc.want)
			}
		})
	}
}

func TestNameMatchesSubstring(t *testing.T) {
	cases := []struct {
		name string
		p    Person
		q    string
		want bool
	}{
		{
			name: "empty query",
			p:    Person{Name: "Alice"},
			q:    "",
			want: false,
		},
		{
			name: "person name",
			p:    Person{Name: "Alice Smith"},
			q:    "smith",
			want: true,
		},
		{
			name: "slack display name",
			p: Person{Slack: map[string]PersonSlack{
				"w": {DisplayName: "bobcat"},
			}},
			q:    "bob",
			want: true,
		},
		{
			name: "slack real name",
			p: Person{Slack: map[string]PersonSlack{
				"w": {RealName: "Robert Jones"},
			}},
			q:    "jones",
			want: true,
		},
		{
			name: "slack username",
			p: Person{Slack: map[string]PersonSlack{
				"w": {Name: "alice.dev"},
			}},
			q:    "alice",
			want: true,
		},
		{
			name: "case insensitive",
			p:    Person{Name: "UPPERCASE"},
			q:    "upper",
			want: true,
		},
		{
			name: "no match",
			p:    Person{Name: "Zed"},
			q:    "alice",
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.p.nameMatchesSubstring(tc.q); got != tc.want {
				t.Errorf("nameMatchesSubstring(%q) = %v, want %v", tc.q, got, tc.want)
			}
		})
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

func TestFindPersonMatch(t *testing.T) {
	cases := []struct {
		name    string
		people  []Person
		q       Person
		wantIdx int
	}{
		{
			name:    "by email",
			people:  []Person{{Name: "Alice", Email: []string{"alice@company.com"}}, {Name: "Bob", Email: []string{"bob@company.com"}}},
			q:       Person{Email: []string{"bob@company.com"}},
			wantIdx: 1,
		},
		{
			// matchesEmail is used (not hasExactEmail), so Gmail dot-normalization applies.
			name:    "by email normalized",
			people:  []Person{{Name: "Alice", Email: []string{"alice@gmail.com"}}},
			q:       Person{Email: []string{"a.l.i.c.e@gmail.com"}},
			wantIdx: 0,
		},
		{
			name:    "by slack ID",
			people:  []Person{{Name: "Alice", Slack: map[string]PersonSlack{"acme": {ID: "U04AAAA"}}}, {Name: "Bob", Slack: map[string]PersonSlack{"acme": {ID: "U04BBBBB"}}}},
			q:       Person{Slack: map[string]PersonSlack{"acme": {ID: "U04AAAA"}}},
			wantIdx: 0,
		},
		{
			// q has the same ID in a different workspace name — should still match by ID.
			name:    "by slack ID any workspace",
			people:  []Person{{Slack: map[string]PersonSlack{"acme": {ID: "U04AAAA"}}}},
			q:       Person{Slack: map[string]PersonSlack{"other": {ID: "U04AAAA"}}},
			wantIdx: 0,
		},
		{
			name:    "skips empty slack ID",
			people:  []Person{{Slack: map[string]PersonSlack{"acme": {ID: "U04AAAA"}}}},
			q:       Person{Slack: map[string]PersonSlack{"acme": {ID: ""}}},
			wantIdx: -1,
		},
		{
			name:    "by phone",
			people:  []Person{{Name: "Alice", WhatsApp: []string{"+15551234567"}}},
			q:       Person{WhatsApp: []string{"+15551234567"}},
			wantIdx: 0,
		},
		{
			name:    "no match",
			people:  []Person{{Email: []string{"alice@company.com"}}},
			q:       Person{Email: []string{"nobody@example.com"}},
			wantIdx: -1,
		},
		{
			name:    "empty people",
			people:  nil,
			q:       Person{Email: []string{"alice@company.com"}},
			wantIdx: -1,
		},
		{
			name:    "empty query",
			people:  []Person{{Email: []string{"alice@company.com"}}},
			q:       Person{},
			wantIdx: -1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := findPersonMatch(tc.people, tc.q); got != tc.wantIdx {
				t.Errorf("findPersonMatch = %d, want %d", got, tc.wantIdx)
			}
		})
	}
}

func TestMergePerson(t *testing.T) {
	cases := []struct {
		name string
		dst  Person
		src  Person
		want Person
	}{
		{
			name: "name: src newer wins",
			dst:  Person{Name: "alice", Seen: "2026-01-01"},
			src:  Person{Name: "Alice Smith", Seen: "2026-04-11"},
			want: Person{Name: "Alice Smith", Seen: "2026-04-11"},
		},
		{
			name: "name: dst kept when src older",
			dst:  Person{Name: "Alice Smith", Seen: "2026-04-11"},
			src:  Person{Name: "alice", Seen: "2026-01-01"},
			want: Person{Name: "Alice Smith", Seen: "2026-04-11"},
		},
		{
			name: "name: src fills empty dst",
			dst:  Person{Name: "", Seen: "2026-04-11"},
			src:  Person{Name: "Alice", Seen: "2026-01-01"},
			want: Person{Name: "Alice", Seen: "2026-04-11"},
		},
		{
			name: "name: empty src does not clear",
			dst:  Person{Name: "Alice", Seen: "2026-01-01"},
			src:  Person{Name: "", Seen: "2026-04-11"},
			want: Person{Name: "Alice", Seen: "2026-04-11"},
		},
		{
			name: "email: union of distinct addresses",
			dst:  Person{Email: []string{"alice@company.com"}, Seen: "2026-01-01"},
			src:  Person{Email: []string{"alice@personal.com"}, Seen: "2026-04-11"},
			want: Person{Email: []string{"alice@company.com", "alice@personal.com"}, Seen: "2026-04-11"},
		},
		{
			name: "email: case-insensitive dedup",
			dst:  Person{Email: []string{"alice@company.com"}, Seen: "2026-01-01"},
			src:  Person{Email: []string{"Alice@Company.com"}, Seen: "2026-04-11"},
			want: Person{Email: []string{"alice@company.com"}, Seen: "2026-04-11"},
		},
		{
			name: "slack: new workspace added",
			dst:  Person{Slack: map[string]PersonSlack{"acme": {ID: "U04AAAA"}}, Seen: "2026-01-01"},
			src:  Person{Slack: map[string]PersonSlack{"vendor": {ID: "U09BBBBB"}}, Seen: "2026-04-11"},
			want: Person{Slack: map[string]PersonSlack{"acme": {ID: "U04AAAA"}, "vendor": {ID: "U09BBBBB"}}, Seen: "2026-04-11"},
		},
		{
			name: "slack: src newer wins for existing workspace",
			dst:  Person{Slack: map[string]PersonSlack{"acme": {ID: "U04AAAA", DisplayName: "old"}}, Seen: "2026-01-01"},
			src:  Person{Slack: map[string]PersonSlack{"acme": {ID: "U04AAAA", DisplayName: "new"}}, Seen: "2026-04-11"},
			want: Person{Slack: map[string]PersonSlack{"acme": {ID: "U04AAAA", DisplayName: "new"}}, Seen: "2026-04-11"},
		},
		{
			name: "slack: dst kept when src older",
			dst:  Person{Slack: map[string]PersonSlack{"acme": {ID: "U04AAAA", DisplayName: "current"}}, Seen: "2026-04-11"},
			src:  Person{Slack: map[string]PersonSlack{"acme": {ID: "U04AAAA", DisplayName: "stale"}}, Seen: "2026-01-01"},
			want: Person{Slack: map[string]PersonSlack{"acme": {ID: "U04AAAA", DisplayName: "current"}}, Seen: "2026-04-11"},
		},
		{
			name: "phone: union of distinct numbers",
			dst:  Person{WhatsApp: []string{"+15551234567"}, Seen: "2026-01-01"},
			src:  Person{WhatsApp: []string{"+15559876543"}, Seen: "2026-04-11"},
			want: Person{WhatsApp: []string{"+15551234567", "+15559876543"}, Seen: "2026-04-11"},
		},
		{
			name: "phone: dedup",
			dst:  Person{WhatsApp: []string{"+15551234567"}, Seen: "2026-01-01"},
			src:  Person{WhatsApp: []string{"+15551234567"}, Seen: "2026-04-11"},
			want: Person{WhatsApp: []string{"+15551234567"}, Seen: "2026-04-11"},
		},
		{
			name: "seen: takes max",
			dst:  Person{Seen: "2026-01-01"},
			src:  Person{Seen: "2026-04-11"},
			want: Person{Seen: "2026-04-11"},
		},
		{
			name: "seen: does not regress",
			dst:  Person{Seen: "2026-04-11"},
			src:  Person{Seen: "2026-01-01"},
			want: Person{Seen: "2026-04-11"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dstBefore := tc.dst // snapshot before call
			got := mergePerson(tc.dst, tc.src)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mergePerson mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(dstBefore, tc.dst); diff != "" {
				t.Errorf("dst was mutated (-before +after):\n%s", diff)
			}
		})
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

func TestSearchCandidates(t *testing.T) {
	cases := []struct {
		name        string
		people      []Person
		query       string
		wantLen     int
		wantName    string   // when len(got)==1, expected Person.Name
		wantSlackID string   // when len(got)==1, expected Slack["w"].ID
		wantNames   []string // when non-nil, expected Person.Name set (order-independent)
	}{
		{
			name: "slack ID exact",
			people: []Person{
				{Name: "Alice", Slack: map[string]PersonSlack{"w": {ID: "U111", DisplayName: "alice"}}},
				{Name: "Bob", Slack: map[string]PersonSlack{"w": {ID: "U222", DisplayName: "bob"}}},
			},
			query:       "U111",
			wantLen:     1,
			wantName:    "Alice",
			wantSlackID: "U111",
		},
		{
			name: "email exact",
			people: []Person{
				{Name: "Alice", Email: []string{"alice@company.com"}},
				{Name: "Bob", Email: []string{"bob@company.com"}},
			},
			query:    "Alice@Company.Com",
			wantLen:  1,
			wantName: "Alice",
		},
		{
			name: "name substring single match",
			people: []Person{
				{Name: "Alice Smith", Slack: map[string]PersonSlack{"w": {ID: "U1", DisplayName: "asmith"}}},
				{Name: "Bob Jones", Slack: map[string]PersonSlack{"w": {ID: "U2", DisplayName: "bjones"}}},
			},
			query:    "smith",
			wantLen:  1,
			wantName: "Alice Smith",
		},
		{
			name: "name substring ambiguous",
			people: []Person{
				{Name: "Alex One", Slack: map[string]PersonSlack{"w": {ID: "U1", DisplayName: "Alex"}}},
				{Name: "Alex Two", Slack: map[string]PersonSlack{"w": {ID: "U2", DisplayName: "Alex"}}},
			},
			query:     "Alex",
			wantLen:   2,
			wantNames: []string{"Alex One", "Alex Two"},
		},
		{
			name: "ID precedence over shared name substring",
			people: []Person{
				{Name: "Pat", Slack: map[string]PersonSlack{"w": {ID: "U99", DisplayName: "Pat"}}},
				{Name: "Pat Lee", Slack: map[string]PersonSlack{"w": {ID: "U88", DisplayName: "Pat"}}},
			},
			query:       "U99",
			wantLen:     1,
			wantName:    "Pat",
			wantSlackID: "U99",
		},
		{
			name:    "empty query",
			people:  []Person{{Name: "X", Email: []string{"x@y.z"}}},
			query:   "   ",
			wantLen: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := searchCandidates(tc.people, tc.query)
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
				if tc.wantSlackID != "" && got[0].Slack["w"].ID != tc.wantSlackID {
					t.Errorf("Slack[w].ID = %q, want %q", got[0].Slack["w"].ID, tc.wantSlackID)
				}
			}
		})
	}
}
