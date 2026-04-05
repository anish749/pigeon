package modelv1

import (
	"encoding/json"
	"fmt"
)

// SeparatorLine is the JSON representation of a separator event.
const SeparatorLine = `{"type":"separator"}`

// Marshal serialises a Line to its JSONL representation (one JSON object, no trailing newline).
func Marshal(l Line) (string, error) {
	data, err := json.Marshal(l)
	if err != nil {
		return "", fmt.Errorf("marshal line: %w", err)
	}
	return string(data), nil
}

// Parse parses a single JSONL line into a Line.
func Parse(line string) (Line, error) {
	var l Line
	if err := json.Unmarshal([]byte(line), &l); err != nil {
		return Line{}, fmt.Errorf("parse line: %w", err)
	}
	return l, nil
}
