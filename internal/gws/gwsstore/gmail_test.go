package gwsstore

import (
	"bytes"
	"os"
	"testing"

	"github.com/anish749/pigeon/internal/paths"
)

func testGmailDir(t *testing.T) paths.GmailDir {
	t.Helper()
	return paths.NewDataRoot(t.TempDir()).
		Platform("gws").
		AccountFromSlug("test").
		Gmail()
}

func TestDeleteEmail_Found(t *testing.T) {
	gmailDir := testGmailDir(t)
	datePath := gmailDir.DateFile("2026-04-07")

	for _, id := range []string{"a", "target", "c"} {
		if err := AppendLine(datePath, emailLine(id)); err != nil {
			t.Fatal(err)
		}
	}

	if err := DeleteEmail(gmailDir, "target"); err != nil {
		t.Fatalf("DeleteEmail: %v", err)
	}

	lines, err := ReadLines(datePath)
	if err != nil {
		t.Fatalf("ReadLines: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	if lines[0].Email.ID != "a" || lines[1].Email.ID != "c" {
		t.Errorf("remaining = [%s, %s], want [a, c]", lines[0].Email.ID, lines[1].Email.ID)
	}
}

func TestDeleteEmail_NotFound(t *testing.T) {
	gmailDir := testGmailDir(t)
	datePath := gmailDir.DateFile("2026-04-07")

	if err := AppendLine(datePath, emailLine("keep")); err != nil {
		t.Fatal(err)
	}

	if err := DeleteEmail(gmailDir, "missing"); err != nil {
		t.Fatalf("DeleteEmail: %v", err)
	}

	lines, err := ReadLines(datePath)
	if err != nil {
		t.Fatalf("ReadLines: %v", err)
	}
	if len(lines) != 1 || lines[0].Email.ID != "keep" {
		t.Errorf("expected [keep], got %+v", lines)
	}
}

func TestDeleteEmail_MissingDir(t *testing.T) {
	gmailDir := paths.NewDataRoot(t.TempDir()).
		Platform("gws").
		AccountFromSlug("neversynced").
		Gmail()

	if err := DeleteEmail(gmailDir, "anything"); err != nil {
		t.Errorf("DeleteEmail on missing dir: %v", err)
	}
}

func TestDeleteEmail_OnlyEmail_RemovesFile(t *testing.T) {
	gmailDir := testGmailDir(t)
	datePath := gmailDir.DateFile("2026-04-07")

	if err := AppendLine(datePath, emailLine("sole")); err != nil {
		t.Fatal(err)
	}

	if err := DeleteEmail(gmailDir, "sole"); err != nil {
		t.Fatalf("DeleteEmail: %v", err)
	}

	if _, err := os.Stat(datePath.Path()); !os.IsNotExist(err) {
		t.Errorf("expected date file to be removed, got err=%v", err)
	}
}

func TestDeleteEmail_PreservesUnparseableLines(t *testing.T) {
	gmailDir := testGmailDir(t)
	datePath := gmailDir.DateFile("2026-04-07")

	if err := AppendLine(datePath, emailLine("keep")); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(datePath.Path(), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("corrupt garbage\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := AppendLine(datePath, emailLine("target")); err != nil {
		t.Fatal(err)
	}

	if err := DeleteEmail(gmailDir, "target"); err != nil {
		t.Fatalf("DeleteEmail: %v", err)
	}

	data, err := os.ReadFile(datePath.Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var nonEmpty int
	for _, raw := range bytes.Split(data, []byte{'\n'}) {
		if len(bytes.TrimSpace(raw)) > 0 {
			nonEmpty++
		}
	}
	if nonEmpty != 2 {
		t.Fatalf("got %d non-empty lines, want 2 (kept email + corrupt)", nonEmpty)
	}
}

func TestDeleteEmail_ScansMostRecentFirst(t *testing.T) {
	gmailDir := testGmailDir(t)

	if err := AppendLine(gmailDir.DateFile("2026-04-01"), emailLine("old")); err != nil {
		t.Fatal(err)
	}
	if err := AppendLine(gmailDir.DateFile("2026-04-07"), emailLine("new")); err != nil {
		t.Fatal(err)
	}

	if err := DeleteEmail(gmailDir, "old"); err != nil {
		t.Fatalf("DeleteEmail: %v", err)
	}

	// Old file should be removed (it only had one email).
	if _, err := os.Stat(gmailDir.DateFile("2026-04-01").Path()); !os.IsNotExist(err) {
		t.Errorf("old date file still exists: %v", err)
	}
	// New file untouched.
	lines, err := ReadLines(gmailDir.DateFile("2026-04-07"))
	if err != nil {
		t.Fatalf("ReadLines new: %v", err)
	}
	if len(lines) != 1 || lines[0].Email.ID != "new" {
		t.Errorf("new file tampered: %+v", lines)
	}
}
