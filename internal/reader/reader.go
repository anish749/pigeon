// Package reader implements per-source read algorithms for the read protocol.
//
// Each source (gmail, calendar, drive, slack, whatsapp, linear) has a reader
// function that returns structured data from the on-disk storage. The reader
// layer sits above the store and paths packages, applying dedup, filtering,
// and sorting as defined in docs/read-protocol.md.
//
// Reader functions do not format output — that is the CLI's responsibility.
package reader

import "time"

// Filters controls what data is returned by a reader.
type Filters struct {
	// Since returns items within this duration from now.
	Since time.Duration

	// Date returns items from a specific day (YYYY-MM-DD).
	Date string

	// Last returns the last N items after all other filtering.
	Last int
}

// DefaultLast is the default number of items returned when no filter is
// specified, for sources that support it.
const DefaultLast = 25
