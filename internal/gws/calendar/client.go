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

// ListEvents fetches changed events for a calendar using a syncToken.
// Paginates through all pages. Returns events and the new syncToken.
func ListEvents(calendarID, syncToken string) ([]model.EventLine, string, error) {
	var allEvents []model.EventLine
	var newSyncToken string

	params := map[string]string{
		"calendarId": calendarID,
		"syncToken":  syncToken,
	}

	for {
		var resp calendarEventsResponse
		if err := gws.RunParsed(&resp, "calendar", "events", "list", "--params", gws.ParamsJSON(params)); err != nil {
			return nil, "", err
		}

		for _, item := range resp.Items {
			allEvents = append(allEvents, item.ToEventLine())
		}

		if resp.NextPageToken != "" {
			params["pageToken"] = resp.NextPageToken
			continue
		}

		newSyncToken = resp.NextSyncToken
		break
	}

	return allEvents, newSyncToken, nil
}

// SeedSyncToken fetches a syncToken for a calendar by listing events in a
// ±90-day window around now. Returns the events found during seeding alongside
// the sync token, so the caller can store them as a backfill.
func SeedSyncToken(calendarID string) ([]model.EventLine, string, error) {
	now := time.Now().UTC()
	params := map[string]string{
		"calendarId": calendarID,
		"maxResults": "2500",
		"timeMin":    now.AddDate(0, 0, -gws.BackfillDays).Format(time.RFC3339),
		"timeMax":    now.AddDate(0, 0, gws.BackfillDays).Format(time.RFC3339),
	}

	var allEvents []model.EventLine
	for {
		var resp calendarEventsResponse
		if err := gws.RunParsed(&resp, "calendar", "events", "list", "--params", gws.ParamsJSON(params)); err != nil {
			return nil, "", fmt.Errorf("seed calendar sync token: %w", err)
		}

		for _, item := range resp.Items {
			allEvents = append(allEvents, item.ToEventLine())
		}

		if resp.NextPageToken != "" {
			params["pageToken"] = resp.NextPageToken
			continue
		}

		if resp.NextSyncToken == "" {
			return nil, "", fmt.Errorf("seed calendar sync token: no sync token in response")
		}
		return allEvents, resp.NextSyncToken, nil
	}
}
