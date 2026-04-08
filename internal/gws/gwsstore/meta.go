package gwsstore

import (
	"encoding/json"
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
func SaveMeta(mf paths.MetaFile, m *model.DocMeta) error {
	path := mf.Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
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
	return nil
}
