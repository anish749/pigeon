package commands

import (
	"encoding/json"
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

// A thread file is selected whole-file when anyone was active in the
// window, so the person's out-of-window messages in it must not count.
func TestRunWhois_ThreadActivityRespectsWindow(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not available")
	}
	t.Setenv("PIGEON_DATA_DIR", t.TempDir())

	root := paths.DefaultDataRoot()
	s := store.NewFSStore(root)
	acct := account.New("slack", "acme-corp")

	w := identity.NewWriter(s, root.AccountFor(acct).Identity())
	if err := w.Observe(identity.Signal{
		Name:  "Alice Smith",
		Slack: &identity.SlackIdentity{Workspace: "acme-corp", ID: "U04ALICE1", DisplayName: "Alice Smith", Name: "alice"},
	}); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	msg := func(id string, ts time.Time, sender, senderID string) modelv1.Line {
		return modelv1.Line{Type: modelv1.LineMessage, Msg: &modelv1.MsgLine{
			ID: id, Ts: ts, Sender: sender, SenderID: senderID, Text: "x",
		}}
	}

	// One in-window message from Alice in a date file.
	if err := s.Append(acct, "#eng", msg("1", now.Add(-24*time.Hour), "Alice Smith", "U04ALICE1")); err != nil {
		t.Fatal(err)
	}
	// A thread revived by Bob yesterday: Alice's reply is 60 days old.
	threadTS := "1700000000.000001"
	if err := s.AppendThread(acct, "#eng", threadTS, msg("2", now.Add(-60*24*time.Hour), "Alice Smith", "U04ALICE1")); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendThread(acct, "#eng", threadTS, msg("3", now.Add(-time.Hour), "Bob Smith", "U04BOB222")); err != nil {
		t.Fatal(err)
	}

	stdout, _, err := runWhois(t, WhoisParams{Query: "alice", Since: 30 * 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	var r WhoisResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &r); err != nil {
		t.Fatal(err)
	}
	if r.Activity.Events != 1 {
		t.Errorf("events = %d, want 1 — out-of-window thread replies must not count", r.Activity.Events)
	}
}
