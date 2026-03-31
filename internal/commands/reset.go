package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/anish/claude-msg-utils/internal/store"
)

func RunReset(platform, account string) error {
	dir := filepath.Join(store.DataDir(), platform, account)

	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("no data found for %s/%s", platform, account)
	}

	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("failed to delete %s: %w", dir, err)
	}

	fmt.Printf("Deleted all data for %s/%s\n", platform, account)
	return nil
}
