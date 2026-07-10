package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/identity"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

// whoisFixture writes two people to a slack account's identity file and
// gives Alice two messages in #eng. Bob has no message activity.
func whoisFixture(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not available")
	}
	t.Setenv("PIGEON_DATA_DIR", t.TempDir())

	root := paths.DefaultDataRoot()
	s := store.NewFSStore(root)
	acct := account.New("slack", "acme-corp")

	w := identity.NewWriter(s, root.AccountFor(acct).Identity())
	if err := w.ObserveBatch([]identity.Signal{
		{
			Name:  "Alice Smith",
			Email: "alice@example.com",
			Slack: &identity.SlackIdentity{Workspace: "acme-corp", ID: "U04ALICE1", DisplayName: "Alice Smith", Name: "alice"},
		},
		{
			Name:  "Bob Smith",
			Slack: &identity.SlackIdentity{Workspace: "acme-corp", ID: "U04BOB222", DisplayName: "Bob Smith", Name: "bob"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	ts := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	for i, text := range []string{"first", "second"} {
		line := modelv1.Line{Type: modelv1.LineMessage, Msg: &modelv1.MsgLine{
			ID: string(rune('1' + i)), Ts: ts.Add(time.Duration(i) * time.Minute),
			Sender: "Alice Smith", SenderID: "U04ALICE1", Text: text,
		}}
		if err := s.Append(acct, "#eng", line); err != nil {
			t.Fatal(err)
		}
	}
}

func runWhois(t *testing.T, p WhoisParams) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := RunWhois(nil, p, &stdout, &stderr)
	return stdout.String(), stderr.String(), err
}

func TestRunWhois_ListWithActivity(t *testing.T) {
	whoisFixture(t)

	stdout, _, err := runWhois(t, WhoisParams{Query: "smith"})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d result lines, want 2:\n%s", len(lines), stdout)
	}

	var first, second WhoisResult
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatal(err)
	}

	// Alice has activity, so she sorts first.
	if first.Name != "Alice Smith" {
		t.Errorf("first result = %q, want Alice Smith (most recently active)", first.Name)
	}
	if first.Activity.Events != 2 {
		t.Errorf("Alice events = %d, want 2", first.Activity.Events)
	}
	if first.Activity.LastActive == "" {
		t.Error("Alice lastActive should be set")
	}
	if len(first.Activity.Conversations) != 1 || first.Activity.Conversations[0] != "slack/acme-corp/#eng" {
		t.Errorf("Alice conversations = %v, want [slack/acme-corp/#eng]", first.Activity.Conversations)
	}
	if second.Name != "Bob Smith" || second.Activity.Events != 0 {
		t.Errorf("second result = %q with %d events, want inactive Bob Smith", second.Name, second.Activity.Events)
	}
}

func TestRunWhois_EmailFragment(t *testing.T) {
	whoisFixture(t)

	stdout, _, err := runWhois(t, WhoisParams{Query: "@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	var r WhoisResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &r); err != nil {
		t.Fatal(err)
	}
	if r.Name != "Alice Smith" {
		t.Errorf("got %q, want Alice Smith", r.Name)
	}
}

func TestRunWhois_IDOnly_Single(t *testing.T) {
	whoisFixture(t)

	stdout, _, err := runWhois(t, WhoisParams{Query: "alice", IDOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "U04ALICE1\n" {
		t.Errorf("stdout = %q, want bare ID", stdout)
	}
}

func TestRunWhois_IDOnly_Ambiguous(t *testing.T) {
	whoisFixture(t)

	stdout, _, err := runWhois(t, WhoisParams{Query: "smith", IDOnly: true})
	if !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("err = %v, want ErrAmbiguous", err)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, must be empty on ambiguity", stdout)
	}
	// The error detail must carry the stable IDs so the caller can retry.
	if !strings.Contains(err.Error(), "U04ALICE1") || !strings.Contains(err.Error(), "U04BOB222") {
		t.Errorf("ambiguity detail missing candidate IDs: %v", err)
	}
}

func TestRunWhois_NoMatch(t *testing.T) {
	whoisFixture(t)

	stdout, _, err := runWhois(t, WhoisParams{Query: "nobody"})
	if err == nil {
		t.Fatal("expected error for no match")
	}
	if errors.Is(err, ErrAmbiguous) {
		t.Error("no-match must not be ambiguous")
	}
	if stdout != "" {
		t.Errorf("stdout = %q, must be empty", stdout)
	}
}

func TestRunWhois_ExactIDShortCircuits(t *testing.T) {
	whoisFixture(t)

	// "Smith" is ambiguous, but an exact Slack ID resolves to one person —
	// this is the documented retry after an ambiguous --id.
	stdout, _, err := runWhois(t, WhoisParams{Query: "U04BOB222", IDOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "U04BOB222\n" {
		t.Errorf("stdout = %q, want bare ID", stdout)
	}
}

func TestRunWhois_AccountRequiresPlatform(t *testing.T) {
	whoisFixture(t)

	_, _, err := runWhois(t, WhoisParams{Query: "alice", Account: "acme-corp"})
	if err == nil || !strings.Contains(err.Error(), "--account requires --platform") {
		t.Fatalf("err = %v, want --account requires --platform", err)
	}

	// With the platform set, the account scope applies.
	stdout, _, err := runWhois(t, WhoisParams{Query: "alice", Platform: "slack", Account: "acme-corp"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "U04ALICE1") {
		t.Errorf("scoped lookup missing result: %q", stdout)
	}

	// --id is slack-only, so a bare --account names a slack workspace.
	stdout, _, err = runWhois(t, WhoisParams{Query: "alice", Account: "acme-corp", IDOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "U04ALICE1\n" {
		t.Errorf("stdout = %q, want bare ID", stdout)
	}
}
