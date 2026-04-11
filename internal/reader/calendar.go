package reader

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

// CalendarResult holds the output of reading calendar data.
type CalendarResult struct {
	Events []modelv1.CalendarEvent
}

// ReadCalendar reads calendar events from a CalendarDir, applying dedup,
// cancellation filtering, and sorting.
//
// Algorithm (from read-protocol.md):
//  1. Collect all event lines from JSONL date files in the requested range.
//  2. Deduplicate by id (keep last occurrence — latest state wins).
//  3. Exclude cancelled events (status: "cancelled").
//  4. Sort by start time.
func ReadCalendar(dir paths.CalendarDir, filters Filters) (*CalendarResult, error) {
	dateFiles, err := listSortedJSONL(dir.Path())
	if err != nil {
		return nil, fmt.Errorf("list calendar date files: %w", err)
	}

	selected := selectCalendarDateFiles(dir.Path(), dateFiles, filters)
	if len(selected) == 0 {
		return &CalendarResult{}, nil
	}

	// Parse all events from selected files. Collect parse errors so the
	// caller knows about partial failures.
	var allEvents []modelv1.CalendarEvent
	var errs []error
	for _, f := range selected {
		events, err := parseEventFile(f)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		allEvents = append(allEvents, events...)
	}

	// Dedup by ID (keep last occurrence).
	allEvents = dedupEvents(allEvents)

	// Exclude cancelled events.
	var live []modelv1.CalendarEvent
	for _, e := range allEvents {
		if e.Runtime.Status != "cancelled" {
			live = append(live, e)
		}
	}

	// Sort by start time.
	sort.SliceStable(live, func(i, j int) bool {
		return eventStartTime(live[i]).Before(eventStartTime(live[j]))
	})

	return &CalendarResult{Events: live}, errors.Join(errs...)
}

// selectCalendarDateFiles picks date files based on filters. Calendar
// defaults to today's events when no filter is specified.
func selectCalendarDateFiles(dir string, files []string, filters Filters) []string {
	switch {
	case filters.Date != "" || filters.Since > 0:
		return selectDateFiles(dir, files, filters)
	default:
		// Default: today's events.
		return selectDateFiles(dir, files, Filters{Date: time.Now().Format("2006-01-02")})
	}
}

// parseEventFile reads a JSONL file and returns all calendar event lines.
// Parse errors for individual lines are collected and returned alongside
// successfully parsed events.
func parseEventFile(path string) ([]modelv1.CalendarEvent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var events []modelv1.CalendarEvent
	var errs []error
	for _, rawLine := range splitLines(data) {
		line, err := modelv1.Parse(rawLine)
		if err != nil {
			errs = append(errs, fmt.Errorf("parse line in %s: %w", filepath.Base(path), err))
			continue
		}
		if line.Type == modelv1.LineEvent && line.Event != nil {
			events = append(events, *line.Event)
		}
	}
	return events, errors.Join(errs...)
}

// dedupEvents deduplicates events by ID, keeping the last occurrence.
func dedupEvents(events []modelv1.CalendarEvent) []modelv1.CalendarEvent {
	lastIndex := make(map[string]int, len(events))
	for i, e := range events {
		lastIndex[e.Runtime.Id] = i
	}
	var result []modelv1.CalendarEvent
	for i, e := range events {
		if lastIndex[e.Runtime.Id] == i {
			result = append(result, e)
		}
	}
	return result
}

// eventStartTime extracts the start time of a calendar event for sorting.
func eventStartTime(e modelv1.CalendarEvent) time.Time {
	if e.Runtime.Start != nil {
		if e.Runtime.Start.DateTime != "" {
			if t, err := time.Parse(time.RFC3339, e.Runtime.Start.DateTime); err == nil {
				return t
			}
		}
		if e.Runtime.Start.Date != "" {
			if t, err := time.Parse("2006-01-02", e.Runtime.Start.Date); err == nil {
				return t
			}
		}
	}
	// Fallback for cancelled instances.
	if e.Runtime.OriginalStartTime != nil {
		if e.Runtime.OriginalStartTime.DateTime != "" {
			if t, err := time.Parse(time.RFC3339, e.Runtime.OriginalStartTime.DateTime); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}
