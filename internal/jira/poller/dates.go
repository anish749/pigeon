package poller

import (
	"fmt"
	"time"

	jira "github.com/ankitpokhrel/jira-cli/pkg/jira"
)

// jqlDateLayout is the format JQL accepts for date-comparison cutoffs.
// JQL silently returns zero matches for any other format (including
// RFC 3339), so emitting this layout for cursor interpolation is mandatory.
// Verified against Jira Cloud, April 2026.
const jqlDateLayout = "2006-01-02 15:04"

// jqlCutoff converts a stored RFC 3339 cursor (or Jira's "+0000"-offset
// form) into a JQL-compatible "yyyy-MM-dd HH:mm" UTC string. JQL date
// filters are minute-precision; we round down to the minute when
// formatting, which means a single overlap minute may be re-fetched on
// the next poll. Dedup-on-ID absorbs the duplicate.
func jqlCutoff(stored string) (string, error) {
	if stored == "" {
		return "", nil
	}
	if t, err := time.Parse(jira.RFC3339MilliLayout, stored); err == nil {
		return t.UTC().Format(jqlDateLayout), nil
	}
	if t, err := time.Parse(time.RFC3339, stored); err == nil {
		return t.UTC().Format(jqlDateLayout), nil
	}
	return "", fmt.Errorf("unparseable cursor %q (must be RFC 3339 or Jira numeric-offset)", stored)
}
