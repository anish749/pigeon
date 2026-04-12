package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

func email(id string, ts time.Time, from, subject string) modelv1.Line {
	return modelv1.Line{
		Type:  modelv1.LineEmail,
		Email: &modelv1.EmailLine{ID: id, Ts: ts, From: from, Subject: subject, Labels: []string{"INBOX"}, To: []string{"me@x.com"}},
	}
}

func event(id, summary, status string, start time.Time) modelv1.Line {
	raw := map[string]any{
		"id": id, "summary": summary, "status": status,
		"start": map[string]any{"dateTime": start.Format(time.RFC3339)},
		"end":   map[string]any{"dateTime": start.Add(time.Hour).Format(time.RFC3339)},
	}
	l, _ := modelv1.Parse(mustMarshalEvent(raw))
	return l
}

func mustMarshalEvent(raw map[string]any) string {
	raw["type"] = "event"
	b, _ := modelv1.Marshal(modelv1.Line{
		Type: modelv1.LineEvent,
		Event: &modelv1.CalendarEvent{
			Serialized: raw,
		},
	})
	// Re-parse to get the Runtime populated.
	return string(b)
}

// --- ReadGWSLines tests ---

func TestReadGWSLines_GmailDedup(t *testing.T) {
	s, root := setupStore(t)
	acct := account.New("gws", "test@example.com")
	gmailDir := root.AccountFor(acct).Gmail()

	// Write two emails with same ID — last wins.
	if err := s.AppendLine(gmailDir.DateFile("2026-04-10"), email("abc", time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC), "a@x.com", "First")); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendLine(gmailDir.DateFile("2026-04-10"), email("abc", time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC), "a@x.com", "Updated")); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendLine(gmailDir.DateFile("2026-04-10"), email("def", time.Date(2026, 4, 10, 11, 0, 0, 0, time.UTC), "b@x.com", "Other")); err != nil {
		t.Fatal(err)
	}

	lines, err := ReadGWSLines(s, gmailDir.Path(), 0, "2026-04-10", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	// "abc" should be the updated version (last occurrence wins).
	if lines[0].Email.Subject != "Updated" {
		t.Errorf("first email subject = %q, want %q", lines[0].Email.Subject, "Updated")
	}
}

func TestReadGWSLines_GmailLastN(t *testing.T) {
	s, root := setupStore(t)
	acct := account.New("gws", "test@example.com")
	gmailDir := root.AccountFor(acct).Gmail()

	for i := 0; i < 5; i++ {
		ts := time.Date(2026, 4, 10, i, 0, 0, 0, time.UTC)
		id := string(rune('a' + i))
		if err := s.AppendLine(gmailDir.DateFile("2026-04-10"), email(id, ts, "a@x.com", "Msg")); err != nil {
			t.Fatal(err)
		}
	}

	lines, err := ReadGWSLines(s, gmailDir.Path(), 0, "2026-04-10", 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d, want 2", len(lines))
	}
	// Last 2 should be the latest.
	if lines[0].Email.ID != "d" || lines[1].Email.ID != "e" {
		t.Errorf("got IDs %q, %q — want d, e", lines[0].Email.ID, lines[1].Email.ID)
	}
}

func TestReadGWSLines_GmailSorted(t *testing.T) {
	s, root := setupStore(t)
	acct := account.New("gws", "test@example.com")
	gmailDir := root.AccountFor(acct).Gmail()

	// Write out of order.
	if err := s.AppendLine(gmailDir.DateFile("2026-04-10"), email("late", time.Date(2026, 4, 10, 15, 0, 0, 0, time.UTC), "a@x.com", "Late")); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendLine(gmailDir.DateFile("2026-04-10"), email("early", time.Date(2026, 4, 10, 8, 0, 0, 0, time.UTC), "b@x.com", "Early")); err != nil {
		t.Fatal(err)
	}

	lines, err := ReadGWSLines(s, gmailDir.Path(), 0, "2026-04-10", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d, want 2", len(lines))
	}
	if lines[0].Email.ID != "early" || lines[1].Email.ID != "late" {
		t.Errorf("not sorted: %q, %q", lines[0].Email.ID, lines[1].Email.ID)
	}
}

func TestReadGWSLines_DefaultLast(t *testing.T) {
	s, root := setupStore(t)
	acct := account.New("gws", "test@example.com")
	gmailDir := root.AccountFor(acct).Gmail()

	// Write 30 emails.
	for i := 0; i < 30; i++ {
		ts := time.Date(2026, 4, 10, 0, i, 0, 0, time.UTC)
		id := fmt.Sprintf("msg-%02d", i)
		if err := s.AppendLine(gmailDir.DateFile("2026-04-10"), email(id, ts, "a@x.com", "Msg")); err != nil {
			t.Fatal(err)
		}
	}

	// No filter, defaultLast=25 — should return 25.
	lines, err := ReadGWSLines(s, gmailDir.Path(), 0, "", 0, 25)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 25 {
		t.Fatalf("got %d, want 25", len(lines))
	}
}

func TestReadGWSLines_EmptyDir(t *testing.T) {
	s, _ := setupStore(t)

	lines, err := ReadGWSLines(s, filepath.Join(t.TempDir(), "nonexistent"), 0, "", 0, 25)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 0 {
		t.Fatalf("got %d, want 0", len(lines))
	}
}

// --- ReadMessaging tests ---

func TestReadMessaging_FuzzyMatch(t *testing.T) {
	s, _ := setupStore(t)
	acct := account.New("slack", "acme-corp")

	if err := s.Append(acct, "#engineering", msg("1", time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC), "Alice", "U1", "hello")); err != nil {
		t.Fatal(err)
	}

	df, conv, err := ReadMessaging(s, acct, "eng", 0, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if conv != "#engineering" {
		t.Errorf("conv = %q, want #engineering", conv)
	}
	if df == nil || len(df.Messages) == 0 {
		t.Fatal("expected messages")
	}
}

func TestReadMessaging_NoMatch(t *testing.T) {
	s, _ := setupStore(t)
	acct := account.New("slack", "acme-corp")

	if err := s.Append(acct, "#engineering", msg("1", time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC), "Alice", "U1", "hello")); err != nil {
		t.Fatal(err)
	}

	_, _, err := ReadMessaging(s, acct, "nonexistent", 0, "", 0)
	if err == nil {
		t.Fatal("expected error for no match")
	}
}

// --- FuzzyMatchConversation tests ---

func TestFuzzyMatchConversation_Exact(t *testing.T) {
	convs := []string{"#engineering", "#eng-oncall", "#general"}

	// Exact match should win even when multiple contain the substring.
	got, err := FuzzyMatchConversation(convs, "#engineering")
	if err != nil {
		t.Fatal(err)
	}
	if got != "#engineering" {
		t.Errorf("got %q, want #engineering", got)
	}
}

func TestFuzzyMatchConversation_Single(t *testing.T) {
	convs := []string{"#engineering", "#general"}

	got, err := FuzzyMatchConversation(convs, "gen")
	if err != nil {
		t.Fatal(err)
	}
	if got != "#general" {
		t.Errorf("got %q, want #general", got)
	}
}

func TestFuzzyMatchConversation_Ambiguous(t *testing.T) {
	convs := []string{"#engineering", "#eng-oncall"}

	_, err := FuzzyMatchConversation(convs, "eng")
	if err == nil {
		t.Fatal("expected ambiguity error")
	}
}

func TestFuzzyMatchConversation_None(t *testing.T) {
	convs := []string{"#engineering", "#general"}

	_, err := FuzzyMatchConversation(convs, "random")
	if err == nil {
		t.Fatal("expected no match error")
	}
}

// --- ReadDriveContent tests ---

func TestReadDriveContent(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)
	acct := account.New("gws", "test@example.com")
	driveDir := root.AccountFor(acct).Drive()
	fileDir := driveDir.File("q2-planning-abc123")

	if err := os.MkdirAll(fileDir.Path(), 0755); err != nil {
		t.Fatal(err)
	}

	// Write a metadata file.
	metaPath := fileDir.MetaFile("2026-04-10").Path()
	if err := os.WriteFile(metaPath, []byte(`{"fileId":"abc123","mimeType":"application/vnd.google-apps.document","title":"Q2 Planning"}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Write markdown content.
	if err := os.WriteFile(fileDir.TabFile("Tab 1").Path(), []byte("# Q2 Planning\n\nRoadmap here.\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Write comments.
	commentLine := `{"type":"comment","id":"c1","content":"Looks good","author":{"displayName":"Alice"},"createdTime":"2026-04-10T12:00:00Z","resolved":false}`
	if err := os.WriteFile(fileDir.CommentsFile().Path(), []byte(commentLine+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	content, comments, err := ReadDriveContent(s, driveDir, "q2")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "# Q2 Planning") {
		t.Errorf("content missing expected markdown: %q", content)
	}
	if len(comments) != 1 {
		t.Fatalf("got %d comments, want 1", len(comments))
	}
}

func TestReadDriveContent_NoMatch(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)
	acct := account.New("gws", "test@example.com")
	driveDir := root.AccountFor(acct).Drive()

	if err := os.MkdirAll(driveDir.Path(), 0755); err != nil {
		t.Fatal(err)
	}

	_, _, err := ReadDriveContent(s, driveDir, "nonexistent")
	if err == nil {
		t.Fatal("expected error for no match")
	}
}

// --- ReadLinearIssue tests ---

func TestReadLinearIssue(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)
	acct := account.New("linear", "trudy")
	issuesDir := filepath.Join(root.AccountFor(acct).Path(), "issues")
	if err := os.MkdirAll(issuesDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write an issue file with duplicate issue snapshots and a comment.
	// Storage type discriminators are "linear-issue" and "linear-comment"
	// (not "issue"/"comment" — those are the Linear API's type field).
	issueLine1 := `{"type":"linear-issue","id":"uuid1","identifier":"TRU-253","title":"Deploy runner","updatedAt":"2026-04-08T14:00:00Z"}`
	issueLine2 := `{"type":"linear-issue","id":"uuid1","identifier":"TRU-253","title":"Deploy runner v2","updatedAt":"2026-04-09T10:00:00Z"}`
	commentLine := `{"type":"linear-comment","id":"cmt1","body":"Ready to merge","createdAt":"2026-04-09T11:00:00Z"}`
	data := issueLine1 + "\n" + commentLine + "\n" + issueLine2 + "\n"
	if err := os.WriteFile(filepath.Join(issuesDir, "TRU-253.jsonl"), []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	lines, err := ReadLinearIssue(s, issuesDir, "TRU-253")
	if err != nil {
		t.Fatal(err)
	}

	// Should have 1 issue (deduped) + 1 comment = 2 lines.
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
}

func TestReadLinearIssue_FuzzyMatch(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)
	acct := account.New("linear", "trudy")
	issuesDir := filepath.Join(root.AccountFor(acct).Path(), "issues")
	if err := os.MkdirAll(issuesDir, 0755); err != nil {
		t.Fatal(err)
	}

	data := `{"type":"linear-issue","id":"uuid1","identifier":"TRU-253","title":"Deploy"}`
	if err := os.WriteFile(filepath.Join(issuesDir, "TRU-253.jsonl"), []byte(data+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Fuzzy match on "253".
	lines, err := ReadLinearIssue(s, issuesDir, "253")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
}

func TestReadLinearIssue_NotFound(t *testing.T) {
	issuesDir := filepath.Join(t.TempDir(), "issues")
	if err := os.MkdirAll(issuesDir, 0755); err != nil {
		t.Fatal(err)
	}

	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)

	_, err := ReadLinearIssue(s, issuesDir, "TRU-999")
	if err == nil {
		t.Fatal("expected error for not found")
	}
}

// --- filterCancelled tests ---

func TestFilterCancelled(t *testing.T) {
	lines := []modelv1.Line{
		email("e1", time.Now(), "a@x.com", "Keep"),
		email("e2", time.Now(), "b@x.com", "Also keep"),
	}

	// filterCancelled should be a no-op for non-event lines.
	got := filterCancelled(lines)
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
}
