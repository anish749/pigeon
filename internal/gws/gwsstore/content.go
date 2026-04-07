package gwsstore

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteContent writes content bytes to a file, creating parent directories.
// Replaces the file if it already exists. Used for markdown and CSV files.
func WriteContent(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent dirs for %s: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
