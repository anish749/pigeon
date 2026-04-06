package slack

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anish749/pigeon/internal/account"
)

func TestLoadCursors_EmptyFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PIGEON_DATA_DIR", tmp)

	acct := account.New("slack", "test-workspace")

	// Create the account directory with an empty cursor file,
	// matching the state after a reset.
	acctDir := filepath.Join(tmp, "slack", "test-workspace")
	if err := os.MkdirAll(acctDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(acctDir, ".sync-cursors.yaml"), nil, 0644); err != nil {
		t.Fatal(err)
	}

	c := loadCursors(acct)
	if c == nil {
		t.Fatal("loadCursors returned nil map for empty file")
	}

	// Must be safe to write to.
	c["C12345"] = "1234567890.000001"
}
