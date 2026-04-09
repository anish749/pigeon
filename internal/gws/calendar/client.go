// Package calendar wraps the gws CLI for Google Calendar API calls.
package calendar

import (
	"fmt"
	"time"

	"github.com/anish749/pigeon/internal/gws"
	gcal "google.golang.org/api/calendar/v3"
)

// EventsResult holds the categorized output from a calendar list or seed call.
type EventsResult struct {
	// Events contains one-off events and recurring instances, ready to write to disk.
	Events []*gcal.Event
	// RecurringIDs contains IDs of active parent recurring events that need instance expansion.
	RecurringIDs []string
	// CancelledRecurringIDs contains IDs of deleted recurring events to remove from tracking.
	CancelledRecurringIDs []string
	// SyncToken is the new sync token for the next incremental call.
	SyncToken string
}

// classify separates raw API events into writable events, active recurring
// parent IDs (for expansion), and cancelled recurring parent IDs (for removal).
func classify(items []*gcal.Event) (events []*gcal.Event, recurringIDs, cancelledRecurringIDs []string) {
	for _, item := range items {
		if len(item.Recurrence) > 0 {
			if item.Status == "cancelled" {
				cancelledRecurringIDs = append(cancelledRecurringIDs, item.Id)
			} else {
				recurringIDs = append(recurringIDs, item.Id)
			}
			continue
		}
		events = append(events, item)
	}
	return
}

// ListEvents fetches changed events for a calendar using a syncToken.
// Paginates through all pages. Returns categorized events and the new syncToken.
func ListEvents(calendarID, syncToken string) (*EventsResult, error) {
	result := &EventsResult{}
	params := map[string]string{
		"calendarId": calendarID,
		"syncToken":  syncToken,
	}

	for {
		var resp gcal.Events
		if err := gws.RunParsed(&resp, "calendar", "events", "list", "--params", gws.ParamsJSON(params)); err != nil {
			return nil, fmt.Errorf("list events %s: %w", calendarID, err)
		}

		events, recurringIDs, cancelledIDs := classify(resp.Items)
		result.Events = append(result.Events, events...)
		result.RecurringIDs = append(result.RecurringIDs, recurringIDs...)
		result.CancelledRecurringIDs = append(result.CancelledRecurringIDs, cancelledIDs...)

		if resp.NextPageToken != "" {
			params["pageToken"] = resp.NextPageToken
			continue
		}

		result.SyncToken = resp.NextSyncToken
		break
	}

	return result, nil
}

// SeedSyncToken fetches a syncToken for a calendar by listing events from
// BackfillDays ago onward (singleEvents=false, so recurring events come as
// parents with RRULEs — no infinite expansion). Returns categorized events
// and the sync token for subsequent incremental calls.
func SeedSyncToken(calendarID string) (*EventsResult, error) {
	now := time.Now().UTC()
	result := &EventsResult{}
	params := map[string]string{
		"calendarId": calendarID,
		"timeMin":    now.AddDate(0, 0, -gws.BackfillDays).Format(time.RFC3339),
	}

	for {
		var resp gcal.Events
		if err := gws.RunParsed(&resp, "calendar", "events", "list", "--params", gws.ParamsJSON(params)); err != nil {
			return nil, fmt.Errorf("seed calendar sync token: %w", err)
		}

		events, recurringIDs, cancelledIDs := classify(resp.Items)
		result.Events = append(result.Events, events...)
		result.RecurringIDs = append(result.RecurringIDs, recurringIDs...)
		result.CancelledRecurringIDs = append(result.CancelledRecurringIDs, cancelledIDs...)

		if resp.NextPageToken != "" {
			params["pageToken"] = resp.NextPageToken
			continue
		}

		if resp.NextSyncToken == "" {
			return nil, fmt.Errorf("seed calendar sync token: no sync token in response")
		}
		result.SyncToken = resp.NextSyncToken
		return result, nil
	}
}

// ListInstances fetches expanded instances of a recurring event within a time window.
func ListInstances(calendarID, eventID, timeMin, timeMax string) ([]*gcal.Event, error) {
	params := map[string]string{
		"calendarId": calendarID,
		"eventId":    eventID,
		"timeMin":    timeMin,
		"timeMax":    timeMax,
	}

	var allEvents []*gcal.Event
	for {
		var resp gcal.Events
		if err := gws.RunParsed(&resp, "calendar", "events", "instances", "--params", gws.ParamsJSON(params)); err != nil {
			return nil, fmt.Errorf("list instances %s: %w", eventID, err)
		}

		allEvents = append(allEvents, resp.Items...)

		if resp.NextPageToken != "" {
			params["pageToken"] = resp.NextPageToken
			continue
		}
		break
	}

	return allEvents, nil
}
