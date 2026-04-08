// Package calendar wraps the gws CLI for Google Calendar API calls.
package calendar

import (
	"fmt"
	"time"

	"github.com/anish749/pigeon/internal/gws"
	"github.com/anish749/pigeon/internal/gws/model"
)

// calendarEventsResponse is the response from calendar events list.
type calendarEventsResponse struct {
	Items         []calendarEvent `json:"items"`
	NextSyncToken string          `json:"nextSyncToken"`
	NextPageToken string          `json:"nextPageToken"`
}

// calendarEvent is the raw API response for a single event.
type calendarEvent struct {
	ID                string           `json:"id"`
	Status            string           `json:"status"`
	Summary           string           `json:"summary"`
	Description       string           `json:"description"`
	Start             calendarTimeRef  `json:"start"`
	End               calendarTimeRef  `json:"end"`
	Location          string           `json:"location"`
	Created           string           `json:"created"`
	Updated           string           `json:"updated"`
	Creator           calendarPerson   `json:"creator"`
	Organizer         calendarPerson   `json:"organizer"`
	Attendees         []calendarPerson `json:"attendees"`
	HangoutLink       string           `json:"hangoutLink"`
	EventType         string           `json:"eventType"`
	RecurringEventId  string           `json:"recurringEventId"`
	Recurrence        []string         `json:"recurrence"`
	OriginalStartTime calendarTimeRef  `json:"originalStartTime"`
}

// calendarTimeRef holds either a dateTime or a date (all-day events).
type calendarTimeRef struct {
	DateTime string `json:"dateTime"`
	Date     string `json:"date"`
}

// calendarPerson holds an email address for a creator, organizer, or attendee.
type calendarPerson struct {
	Email string `json:"email"`
}

// EventsResult holds the categorized output from a calendar list or seed call.
type EventsResult struct {
	// Events contains one-off events and recurring instances, ready to write to disk.
	Events []model.EventLine
	// RecurringIDs contains IDs of active parent recurring events that need instance expansion.
	RecurringIDs []string
	// CancelledRecurringIDs contains IDs of deleted recurring events to remove from tracking.
	CancelledRecurringIDs []string
	// SyncToken is the new sync token for the next incremental call.
	SyncToken string
}

// ToEventLine converts a raw API event to the storage model.
func (e calendarEvent) ToEventLine() model.EventLine {
	var attendees []string
	for _, a := range e.Attendees {
		attendees = append(attendees, a.Email)
	}

	ev := model.EventLine{
		Type:        "event",
		ID:          e.ID,
		Ts:          e.Created,
		Updated:     e.Updated,
		Status:      e.Status,
		Summary:     e.Summary,
		Description: e.Description,
		Location:    e.Location,
		Organizer:   e.Organizer.Email,
		Attendees:   attendees,
		MeetLink:    e.HangoutLink,
		EventType:   e.EventType,
		Recurring:   e.RecurringEventId != "",
	}

	// Timed events have DateTime; all-day events have Date.
	if e.Start.DateTime != "" {
		ev.Start = e.Start.DateTime
	} else {
		ev.StartDate = e.Start.Date
	}
	if e.End.DateTime != "" {
		ev.End = e.End.DateTime
	} else {
		ev.EndDate = e.End.Date
	}

	// Cancelled recurring instances carry the original start time instead of start/end.
	if e.OriginalStartTime.DateTime != "" {
		ev.OriginalStartTime = e.OriginalStartTime.DateTime
	} else if e.OriginalStartTime.Date != "" {
		ev.OriginalStartTime = e.OriginalStartTime.Date
	}

	return ev
}

// isRecurringParent reports whether the event is a parent recurring event
// (has a recurrence rule, as opposed to an instance which has recurringEventId).
func (e calendarEvent) isRecurringParent() bool {
	return len(e.Recurrence) > 0
}

// classify separates raw API events into writable events, active recurring
// parent IDs (for expansion), and cancelled recurring parent IDs (for removal).
func classify(items []calendarEvent) (events []model.EventLine, recurringIDs, cancelledRecurringIDs []string) {
	for _, item := range items {
		if item.isRecurringParent() {
			if item.Status == "cancelled" {
				cancelledRecurringIDs = append(cancelledRecurringIDs, item.ID)
			} else {
				recurringIDs = append(recurringIDs, item.ID)
			}
			continue
		}
		events = append(events, item.ToEventLine())
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
		var resp calendarEventsResponse
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
		var resp calendarEventsResponse
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
func ListInstances(calendarID, eventID, timeMin, timeMax string) ([]model.EventLine, error) {
	params := map[string]string{
		"calendarId": calendarID,
		"eventId":    eventID,
		"timeMin":    timeMin,
		"timeMax":    timeMax,
	}

	var allEvents []model.EventLine
	for {
		var resp calendarEventsResponse
		if err := gws.RunParsed(&resp, "calendar", "events", "instances", "--params", gws.ParamsJSON(params)); err != nil {
			return nil, fmt.Errorf("list instances %s: %w", eventID, err)
		}

		for _, item := range resp.Items {
			allEvents = append(allEvents, item.ToEventLine())
		}

		if resp.NextPageToken != "" {
			params["pageToken"] = resp.NextPageToken
			continue
		}
		break
	}

	return allEvents, nil
}
