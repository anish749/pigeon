package read

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGrep_BasicQuery(t *testing.T) {
	dir := setupFixture(t)

	out, err := Grep(dir, GrepOpts{Query: "hello"})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if len(out) == 0 {
		t.Error("Grep returned no output for 'hello'")
	}
	if !strings.Contains(string(out), "Alice") {
		t.Error("Grep output should contain 'Alice'")
	}
}

func TestGrep_NoMatch(t *testing.T) {
	dir := setupFixture(t)

	out, err := Grep(dir, GrepOpts{Query: "nonexistent_query_xyz"})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("Grep should return empty for no matches, got %d bytes", len(out))
	}
}

func TestGrep_FilesOnly(t *testing.T) {
	dir := setupFixture(t)

	out, err := Grep(dir, GrepOpts{Query: "hello", FilesOnly: true})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("Grep -l returned no output")
	}

	// Should return file paths, not content.
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		if !strings.HasSuffix(line, ".jsonl") {
			t.Errorf("expected file path, got: %s", line)
		}
	}
}

func TestGrep_Count(t *testing.T) {
	dir := setupFixture(t)

	out, err := Grep(dir, GrepOpts{Query: "hello", Count: true})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("Grep -c returned no output")
	}
	// Output format: filepath:count
	if !strings.Contains(string(out), ":1") {
		t.Errorf("expected count of 1, got: %s", string(out))
	}
}

func TestGrep_CaseInsensitive(t *testing.T) {
	dir := setupFixture(t)

	// "HELLO" should not match with default (case sensitive).
	out, err := Grep(dir, GrepOpts{Query: "HELLO"})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if len(out) != 0 {
		t.Error("case-sensitive Grep should not match 'HELLO' against 'hello'")
	}

	// With -i it should match.
	out, err = Grep(dir, GrepOpts{Query: "HELLO", CaseInsensitive: true})
	if err != nil {
		t.Fatalf("Grep -i: %v", err)
	}
	if len(out) == 0 {
		t.Error("Grep -i should match 'HELLO' against 'hello'")
	}
}

func TestGrep_Since(t *testing.T) {
	dir := setupFixture(t)

	// "old" only appears in the old date file (30 days ago).
	// With --since=7d it should not be found in date files.
	out, err := Grep(dir, GrepOpts{Query: "old", Since: 7 * 24 * time.Hour})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}

	// Check that the old date file is not in results.
	old := time.Now().UTC().AddDate(0, 0, -30).Format("2006-01-02")
	if strings.Contains(string(out), filepath.Join("#archive", old+".jsonl")) {
		t.Error("Grep --since=7d should not search old date files")
	}
}

func TestGrep_ThreadsIncludedWithSince(t *testing.T) {
	dir := setupFixture(t)

	// "thread reply" only appears in the recent thread file.
	out, err := Grep(dir, GrepOpts{Query: "thread reply", Since: 7 * 24 * time.Hour})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if len(out) == 0 {
		t.Error("Grep --since should include thread files containing recent messages")
	}
}
