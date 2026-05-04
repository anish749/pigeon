package slack

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
)

func TestLoadSlackCursors_EmptyFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PIGEON_DATA_DIR", tmp)

	root := paths.NewDataRoot(tmp)
	s := store.NewFSStore(root)
	acct := account.New("slack", "test-workspace")
	acctDir := root.AccountFor(acct)

	// Create the account directory with an empty cursor file,
	// matching the state after a reset.
	dir := filepath.Join(tmp, "slack", "test-workspace")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".sync-cursors.yaml"), nil, 0644); err != nil {
		t.Fatal(err)
	}

	c, err := s.LoadSlackCursors(acctDir)
	if err != nil {
		t.Fatalf("LoadSlackCursors returned error: %v", err)
	}
	if c == nil {
		t.Fatal("LoadSlackCursors returned nil map for empty file")
	}

	// Must be safe to write to.
	c["C12345"] = "1234567890.000001"
}
