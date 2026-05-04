// Package calendar wraps the gws CLI for Google Calendar API calls.
package calendar

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/anish749/pigeon/internal/platform/gws"
	"github.com/anish749/pigeon/internal/store/modelv1"
	gcal "google.golang.org/api/calendar/v3"
)

// Client wraps a gws.Client for Calendar API calls.
type Client struct {
	gws *gws.Client
}

// NewClient creates a Calendar client backed by the given gws.Client.
func NewClient(g *gws.Client) *Client {
	return &Client{gws: g}
}

// EventsResult holds the categorized output from a calendar list or seed call.
type EventsResult struct {
	// Events contains one-off events and recurring instances, ready to write to disk.
	Events []*modelv1.CalendarEvent
	// RecurringIDs contains IDs of active parent recurring events that need instance expansion.
	RecurringIDs []string
	// CancelledRecurringIDs contains IDs of deleted recurring events to remove from tracking.
	CancelledRecurringIDs []string
	// SyncToken is the new sync token for the next incremental call.
	SyncToken string
}

// fetchEvents runs a gws events command and returns both the typed response
// and the per-item raw JSON maps. The two are parsed from the same bytes so
// resp.Items and the returned raw items are guaranteed to line up by index.
func (c *Client) fetchEvents(args ...string) (*gcal.Events, []map[string]any, error) {
	out, err := c.gws.Run(args...)
	if err != nil {
		return nil, nil, err
	}

	var resp gcal.Events
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, nil, fmt.Errorf("parse gws output: %w", err)
	}

	var rawResp map[string]any
	if err := json.Unmarshal(out, &rawResp); err != nil {
		return nil, nil, fmt.Errorf("parse gws output as map: %w", err)
	}

	rawItems, err := extractItems(rawResp, len(resp.Items))
	if err != nil {
		return nil, nil, err
	}
	return &resp, rawItems, nil
}

// extractItems pulls the per-event raw maps from a calendar.Events response's
// "items" array and validates the shape against the expected count.
func extractItems(rawResp map[string]any, expected int) ([]map[string]any, error) {
	if expected == 0 {
		return nil, nil
	}
	rawItemsAny, ok := rawResp["items"]
	if !ok || rawItemsAny == nil {
		return nil, fmt.Errorf("raw response missing items field but typed response has %d items", expected)
	}
	rawSlice, ok := rawItemsAny.([]any)
	if !ok {
		return nil, fmt.Errorf("raw items is not an array: got %T", rawItemsAny)
	}
	if len(rawSlice) != expected {
		return nil, fmt.Errorf("raw items count %d does not match typed items count %d", len(rawSlice), expected)
	}
	result := make([]map[string]any, len(rawSlice))
	for i, item := range rawSlice {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("raw items[%d] is not an object: got %T", i, item)
		}
		result[i] = m
	}
	return result, nil
}

// pairItems zips typed events with their per-item raw maps into CalendarEvents.
func pairItems(items []*gcal.Event, raws []map[string]any) []*modelv1.CalendarEvent {
	result := make([]*modelv1.CalendarEvent, len(items))
	for i, item := range items {
		result[i] = &modelv1.CalendarEvent{
			Runtime:    *item,
			Serialized: raws[i],
		}
	}
	return result
}

// classify separates events into writable events, active recurring parent IDs
// (for expansion), and cancelled recurring parent IDs (for removal).
func classify(items []*modelv1.CalendarEvent) (events []*modelv1.CalendarEvent, recurringIDs, cancelledRecurringIDs []string) {
	for _, item := range items {
		if len(item.Runtime.Recurrence) > 0 {
			if item.Runtime.Status == "cancelled" {
				cancelledRecurringIDs = append(cancelledRecurringIDs, item.Runtime.Id)
			} else {
				recurringIDs = append(recurringIDs, item.Runtime.Id)
			}
			continue
		}
		events = append(events, item)
	}
	return
}

// ListEvents fetches changed events for a calendar using a syncToken.
// Paginates through all pages. Returns categorized events and the new syncToken.
func (c *Client) ListEvents(calendarID, syncToken string) (*EventsResult, error) {
	result := &EventsResult{}
	params := map[string]string{
		"calendarId": calendarID,
		"syncToken":  syncToken,
	}

	for {
		resp, rawItems, err := c.fetchEvents("calendar", "events", "list", "--params", gws.ParamsJSON(params))
		if err != nil {
			return nil, fmt.Errorf("list events %s: %w", calendarID, err)
		}

		paired := pairItems(resp.Items, rawItems)
		events, recurringIDs, cancelledIDs := classify(paired)
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
func (c *Client) SeedSyncToken(calendarID string) (*EventsResult, error) {
	now := time.Now().UTC()
	result := &EventsResult{}
	params := map[string]string{
		"calendarId": calendarID,
		"timeMin":    now.AddDate(0, 0, -gws.BackfillDays).Format(time.RFC3339),
	}

	for {
		resp, rawItems, err := c.fetchEvents("calendar", "events", "list", "--params", gws.ParamsJSON(params))
		if err != nil {
			return nil, fmt.Errorf("seed calendar sync token: %w", err)
		}

		paired := pairItems(resp.Items, rawItems)
		events, recurringIDs, cancelledIDs := classify(paired)
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
func (c *Client) ListInstances(calendarID, eventID, timeMin, timeMax string) ([]*modelv1.CalendarEvent, error) {
	params := map[string]string{
		"calendarId": calendarID,
		"eventId":    eventID,
		"timeMin":    timeMin,
		"timeMax":    timeMax,
	}

	var allEvents []*modelv1.CalendarEvent
	for {
		resp, rawItems, err := c.fetchEvents("calendar", "events", "instances", "--params", gws.ParamsJSON(params))
		if err != nil {
			return nil, fmt.Errorf("list instances %s: %w", eventID, err)
		}

		allEvents = append(allEvents, pairItems(resp.Items, rawItems)...)

		if resp.NextPageToken != "" {
			params["pageToken"] = resp.NextPageToken
			continue
		}
		break
	}

	return allEvents, nil
}
