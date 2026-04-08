package gwsstore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Cursors holds polling cursors for GWS services.
type Cursors struct {
	Gmail    GmailCursors    `yaml:"gmail,omitempty"`
	Drive    DriveCursors    `yaml:"drive,omitempty"`
	Calendar CalendarCursors `yaml:"calendar,omitempty"`
}

// GmailCursors holds the Gmail history cursor.
type GmailCursors struct {
	HistoryID string `yaml:"history_id,omitempty"`
}

// DriveCursors holds the Drive changes cursor.
type DriveCursors struct {
	PageToken string `yaml:"page_token,omitempty"`
}

// CalendarCursor holds the sync state for a single calendar.
type CalendarCursor struct {
	SyncToken       string   `yaml:"sync_token,omitempty"`
	ExpandedUntil   string   `yaml:"expanded_until,omitempty"`
	RecurringEvents []string `yaml:"recurring_events,omitempty"`
}

// CalendarCursors maps calendar ID to its cursor.
type CalendarCursors map[string]*CalendarCursor

// LoadCursors reads cursors from a YAML file.
// Returns an empty Cursors if the file doesn't exist.
func LoadCursors(path string) (*Cursors, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Cursors{}, nil
		}
		return nil, fmt.Errorf("read cursors %s: %w", path, err)
	}
	var c Cursors
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse cursors %s: %w", path, err)
	}
	return &c, nil
}

// SaveCursors writes cursors to a YAML file, creating parent directories.
func SaveCursors(path string, c *Cursors) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent dirs for %s: %w", path, err)
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal cursors: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write cursors %s: %w", path, err)
	}
	return nil
}
