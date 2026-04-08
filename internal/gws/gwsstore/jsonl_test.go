package gwsstore

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/gws/model"
	"github.com/anish749/pigeon/internal/paths"
)

func emailLine(id string) model.Line {
	return model.Line{
		Type: "email",
		Email: &model.EmailLine{
			Type:    "email",
			ID:      id,
			Subject: "Subject " + id,
			Ts:      time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC),
			From:    "test@example.com",
			To:      []string{"to@example.com"},
			Labels:  []string{"INBOX"},
		},
	}
}

func emailDeleteLine(id string) model.Line {
	return model.Line{
		Type: "email-delete",
		EmailDelete: &model.EmailDeleteLine{
			Type: "email-delete",
			ID:   id,
			Ts:   time.Date(2026, 4, 7, 13, 0, 0, 0, time.UTC),
		},
	}
}

func TestAppendAndReadLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	for _, id := range []string{"a", "b", "c"} {
		if err := AppendLine(paths.DateFile(path), emailLine(id)); err != nil {
			t.Fatalf("AppendLine(%q): %v", id, err)
		}
	}

	lines, err := ReadLines(paths.DateFile(path))
	if err != nil {
		t.Fatalf("ReadLines: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	for i, want := range []string{"a", "b", "c"} {
		if lines[i].Email.ID != want {
			t.Errorf("lines[%d].ID = %q, want %q", i, lines[i].Email.ID, want)
		}
	}
}

func TestDedupKeepsLast(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dup.jsonl")

	// Append 3 lines with the same ID — Dedup should keep the last.
	for i := range 3 {
		l := emailLine("same")
		l.Email.Subject = "version-" + string(rune('0'+i))
		if err := AppendLine(paths.DateFile(path), l); err != nil {
			t.Fatalf("AppendLine: %v", err)
		}
	}

	lines, err := ReadLines(paths.DateFile(path))
	if err != nil {
		t.Fatalf("ReadLines: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("got %d raw lines, want 3", len(lines))
	}

	deduped := Dedup(lines)
	if len(deduped) != 1 {
		t.Fatalf("got %d deduped lines, want 1", len(deduped))
	}
	if deduped[0].Email.Subject != "version-2" {
		t.Errorf("kept subject %q, want %q", deduped[0].Email.Subject, "version-2")
	}
}

func TestDedupDeleteSemantics(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "del.jsonl")

	if err := AppendLine(paths.DateFile(path), emailLine("target")); err != nil {
		t.Fatal(err)
	}
	if err := AppendLine(paths.DateFile(path), emailDeleteLine("target")); err != nil {
		t.Fatal(err)
	}

	lines, err := ReadLines(paths.DateFile(path))
	if err != nil {
		t.Fatalf("ReadLines: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d raw lines, want 2", len(lines))
	}

	deduped := Dedup(lines)
	if len(deduped) != 0 {
		t.Fatalf("got %d deduped lines, want 0 (both should be removed)", len(deduped))
	}
}

func TestReadLinesNonExistent(t *testing.T) {
	lines, err := ReadLines(paths.DateFile(filepath.Join(t.TempDir(), "nope.jsonl")))
	if err != nil {
		t.Fatalf("ReadLines: %v", err)
	}
	if lines != nil {
		t.Fatalf("got %v, want nil", lines)
	}
}

func TestReadLinesCorruptLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.jsonl")

	// Write a valid line, a corrupt line, then another valid line.
	if err := AppendLine(paths.DateFile(path), emailLine("good1")); err != nil {
		t.Fatal(err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("not valid json\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	if err := AppendLine(paths.DateFile(path), emailLine("good2")); err != nil {
		t.Fatal(err)
	}

	lines, err := ReadLines(paths.DateFile(path))
	if err == nil {
		t.Fatal("expected error for corrupt line")
	}
	if len(lines) != 2 {
		t.Fatalf("got %d good lines, want 2", len(lines))
	}
	if lines[0].Email.ID != "good1" {
		t.Errorf("lines[0].ID = %q, want %q", lines[0].Email.ID, "good1")
	}
	if lines[1].Email.ID != "good2" {
		t.Errorf("lines[1].ID = %q, want %q", lines[1].Email.ID, "good2")
	}
}
