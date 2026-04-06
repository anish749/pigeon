package poller

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Cursors holds polling cursors for all three services.
type Cursors struct {
	Gmail    GmailCursors    `yaml:"gmail,omitempty"`
	Drive    DriveCursors    `yaml:"drive,omitempty"`
	Calendar CalendarCursors `yaml:"calendar,omitempty"`
}

type GmailCursors struct {
	HistoryID string `yaml:"history_id,omitempty"`
}

type DriveCursors struct {
	PageToken string `yaml:"page_token,omitempty"`
}

// CalendarCursors maps calendar ID to its sync token.
type CalendarCursors map[string]string

func LoadCursors(path string) (*Cursors, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Cursors{Calendar: make(CalendarCursors)}, nil
		}
		return nil, fmt.Errorf("load cursors: %w", err)
	}
	var c Cursors
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse cursors: %w", err)
	}
	if c.Calendar == nil {
		c.Calendar = make(CalendarCursors)
	}
	return &c, nil
}

func SaveCursors(path string, c *Cursors) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create cursor dir: %w", err)
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal cursors: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}
