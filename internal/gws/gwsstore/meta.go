package gwsstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/anish749/pigeon/internal/gws/model"
	"github.com/anish749/pigeon/internal/paths"
)

// LoadMeta reads document metadata from a JSON file.
func LoadMeta(mf paths.MetaFile) (*model.DocMeta, error) {
	path := mf.Path()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read meta %s: %w", path, err)
	}
	var m model.DocMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse meta %s: %w", path, err)
	}
	return &m, nil
}

// SaveMeta writes document metadata to a JSON file, creating parent directories.
// After successfully writing, it removes any stale Drive meta files in the same
// directory (drive-meta-*.json with different dates) to avoid accumulation.
// The write-then-delete order ensures we never lose metadata on a crash.
func SaveMeta(mf paths.MetaFile, m *model.DocMeta) error {
	path := mf.Path()
	dir := mf.Dir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create parent dirs for %s: %w", path, err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal meta: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write meta %s: %w", path, err)
	}

	// Clean up stale Drive meta files from previous syncs.
	if err := cleanupStaleDriveMeta(dir, filepath.Base(path)); err != nil {
		return fmt.Errorf("cleanup stale meta in %s: %w", dir, err)
	}
	return nil
}

// cleanupStaleDriveMeta removes any drive-meta-*.json files in dir except
// keepName. Called after writing a new meta file to remove previous versions.
func cleanupStaleDriveMeta(dir, keepName string) error {
	matches, err := filepath.Glob(filepath.Join(dir, paths.DriveMetaFileGlob))
	if err != nil {
		return fmt.Errorf("glob stale meta: %w", err)
	}
	var errs []error
	for _, match := range matches {
		if filepath.Base(match) == keepName {
			continue
		}
		if err := os.Remove(match); err != nil {
			errs = append(errs, fmt.Errorf("remove %s: %w", match, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("remove stale meta files: %w", errors.Join(errs...))
	}
	return nil
}
