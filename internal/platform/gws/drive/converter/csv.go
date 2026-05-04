package converter

import (
	"bytes"
	"encoding/csv"
	"fmt"
)

// ToCSV converts a 2D string array to CSV bytes.
// Pads rows to uniform width with empty strings.
func ToCSV(values [][]string) ([]byte, error) {
	if len(values) == 0 {
		return nil, nil
	}

	// Find max row width.
	maxCols := 0
	for _, row := range values {
		if len(row) > maxCols {
			maxCols = len(row)
		}
	}

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)

	for _, row := range values {
		// Pad shorter rows with empty strings.
		padded := make([]string, maxCols)
		copy(padded, row)
		if err := w.Write(padded); err != nil {
			return nil, fmt.Errorf("write csv row: %w", err)
		}
	}

	w.Flush()
	if err := w.Error(); err != nil {
		return nil, fmt.Errorf("flush csv: %w", err)
	}

	return buf.Bytes(), nil
}
