package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anish749/pigeon/internal/paths"
)

func TestWriteContentCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output.md")

	content := []byte("# Hello\nWorld\n")
	if err := WriteContent(paths.TabFile(path), content); err != nil {
		t.Fatalf("WriteContent: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("got %q, want %q", got, content)
	}
}

func TestWriteContentReplacesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output.md")

	if err := WriteContent(paths.TabFile(path), []byte("old content")); err != nil {
		t.Fatal(err)
	}

	newContent := []byte("new content")
	if err := WriteContent(paths.TabFile(path), newContent); err != nil {
		t.Fatalf("WriteContent (replace): %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(newContent) {
		t.Errorf("got %q, want %q", got, newContent)
	}
}

func TestWriteContentCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c", "output.csv")

	content := []byte("col1,col2\n1,2\n")
	if err := WriteContent(paths.TabFile(path), content); err != nil {
		t.Fatalf("WriteContent: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("got %q, want %q", got, content)
	}
}
