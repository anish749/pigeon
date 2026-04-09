package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

func gwsEmailLine(id string) modelv1.GWSLine {
	return modelv1.GWSLine{
		Type: "email",
		Email: &modelv1.EmailLine{
			ID:      id,
			Subject: "Subject " + id,
			Ts:      time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC),
			From:    "test@example.com",
			To:      []string{"to@example.com"},
			Labels:  []string{"INBOX"},
		},
	}
}

func gwsEmailDeleteLine(id string) modelv1.GWSLine {
	return modelv1.GWSLine{
		Type: "email-delete",
		EmailDelete: &modelv1.EmailDeleteLine{
			ID: id,
			Ts: time.Date(2026, 4, 7, 13, 0, 0, 0, time.UTC),
		},
	}
}

func TestGWSAppendAndReadLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	for _, id := range []string{"a", "b", "c"} {
		if err := AppendLine(paths.DateFile(path), gwsEmailLine(id)); err != nil {
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

func TestGWSWriteLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "write.jsonl")

	// Write initial lines.
	initial := []modelv1.GWSLine{gwsEmailLine("a"), gwsEmailLine("b")}
	if err := WriteLines(paths.DateFile(path), initial); err != nil {
		t.Fatalf("WriteLines: %v", err)
	}

	lines, err := ReadLines(paths.DateFile(path))
	if err != nil {
		t.Fatalf("ReadLines: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}

	// Overwrite with fewer lines — verifies replacement, not append.
	replacement := []modelv1.GWSLine{gwsEmailLine("c")}
	if err := WriteLines(paths.DateFile(path), replacement); err != nil {
		t.Fatalf("WriteLines overwrite: %v", err)
	}

	lines, err = ReadLines(paths.DateFile(path))
	if err != nil {
		t.Fatalf("ReadLines after overwrite: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("got %d lines after overwrite, want 1", len(lines))
	}
	if lines[0].Email.ID != "c" {
		t.Errorf("lines[0].ID = %q, want %q", lines[0].Email.ID, "c")
	}
}

func TestGWSWriteLinesEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")

	// Write initial content.
	if err := WriteLines(paths.DateFile(path), []modelv1.GWSLine{gwsEmailLine("a")}); err != nil {
		t.Fatal(err)
	}

	// Overwrite with empty — file should exist but be empty.
	if err := WriteLines(paths.DateFile(path), nil); err != nil {
		t.Fatalf("WriteLines empty: %v", err)
	}

	lines, err := ReadLines(paths.DateFile(path))
	if err != nil {
		t.Fatalf("ReadLines: %v", err)
	}
	if len(lines) != 0 {
		t.Fatalf("got %d lines, want 0", len(lines))
	}
}

func TestGWSDedupKeepsLast(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dup.jsonl")

	// Append 3 lines with the same ID — Dedup should keep the last.
	for i := range 3 {
		l := gwsEmailLine("same")
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

func TestGWSDedupDeleteSemantics(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "del.jsonl")

	if err := AppendLine(paths.DateFile(path), gwsEmailLine("target")); err != nil {
		t.Fatal(err)
	}
	if err := AppendLine(paths.DateFile(path), gwsEmailDeleteLine("target")); err != nil {
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

func TestGWSReadLinesNonExistent(t *testing.T) {
	lines, err := ReadLines(paths.DateFile(filepath.Join(t.TempDir(), "nope.jsonl")))
	if err != nil {
		t.Fatalf("ReadLines: %v", err)
	}
	if lines != nil {
		t.Fatalf("got %v, want nil", lines)
	}
}

func TestGWSReadLinesCorruptLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.jsonl")

	// Write a valid line, a corrupt line, then another valid line.
	if err := AppendLine(paths.DateFile(path), gwsEmailLine("good1")); err != nil {
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

	if err := AppendLine(paths.DateFile(path), gwsEmailLine("good2")); err != nil {
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
