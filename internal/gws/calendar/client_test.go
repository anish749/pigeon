package calendar

import (
	"testing"
)

func TestToEventLine(t *testing.T) {
	ev := calendarEvent{
		ID:          "evt-123",
		Status:      "confirmed",
		Summary:     "Team Standup",
		Description: "Daily sync",
		Start:       calendarTimeRef{DateTime: "2026-04-07T09:00:00-07:00"},
		End:         calendarTimeRef{DateTime: "2026-04-07T09:30:00-07:00"},
		Location:    "Room 42",
		Created:     "2026-04-01T10:00:00Z",
		Updated:     "2026-04-06T15:00:00Z",
		Creator:     calendarPerson{Email: "creator@example.com"},
		Organizer:   calendarPerson{Email: "organizer@example.com"},
		Attendees: []calendarPerson{
			{Email: "alice@example.com"},
			{Email: "bob@example.com"},
		},
		HangoutLink: "https://meet.google.com/abc-defg-hij",
		EventType:   "default",
	}

	line := ev.ToEventLine()

	if line.ID != "evt-123" {
		t.Errorf("ID = %q, want %q", line.ID, "evt-123")
	}
	if line.Status != "confirmed" {
		t.Errorf("Status = %q, want %q", line.Status, "confirmed")
	}
	if line.Summary != "Team Standup" {
		t.Errorf("Summary = %q, want %q", line.Summary, "Team Standup")
	}
	if line.Description != "Daily sync" {
		t.Errorf("Description = %q, want %q", line.Description, "Daily sync")
	}
	if line.Start != "2026-04-07T09:00:00-07:00" {
		t.Errorf("Start = %q, want %q", line.Start, "2026-04-07T09:00:00-07:00")
	}
	if line.End != "2026-04-07T09:30:00-07:00" {
		t.Errorf("End = %q, want %q", line.End, "2026-04-07T09:30:00-07:00")
	}
	if line.StartDate != "" {
		t.Errorf("StartDate = %q, want empty for timed event", line.StartDate)
	}
	if line.EndDate != "" {
		t.Errorf("EndDate = %q, want empty for timed event", line.EndDate)
	}
	if line.Location != "Room 42" {
		t.Errorf("Location = %q, want %q", line.Location, "Room 42")
	}
	if line.Ts != "2026-04-01T10:00:00Z" {
		t.Errorf("Ts = %q, want %q", line.Ts, "2026-04-01T10:00:00Z")
	}
	if line.Updated != "2026-04-06T15:00:00Z" {
		t.Errorf("Updated = %q, want %q", line.Updated, "2026-04-06T15:00:00Z")
	}
	if line.Organizer != "organizer@example.com" {
		t.Errorf("Organizer = %q, want %q", line.Organizer, "organizer@example.com")
	}
	if len(line.Attendees) != 2 {
		t.Fatalf("len(Attendees) = %d, want 2", len(line.Attendees))
	}
	if line.Attendees[0] != "alice@example.com" {
		t.Errorf("Attendees[0] = %q, want %q", line.Attendees[0], "alice@example.com")
	}
	if line.Attendees[1] != "bob@example.com" {
		t.Errorf("Attendees[1] = %q, want %q", line.Attendees[1], "bob@example.com")
	}
	if line.MeetLink != "https://meet.google.com/abc-defg-hij" {
		t.Errorf("MeetLink = %q, want %q", line.MeetLink, "https://meet.google.com/abc-defg-hij")
	}
	if line.EventType != "default" {
		t.Errorf("EventType = %q, want %q", line.EventType, "default")
	}
	if line.Recurring {
		t.Errorf("Recurring = true, want false for non-recurring event")
	}
}

func TestToEventLine_AllDay(t *testing.T) {
	ev := calendarEvent{
		ID:        "evt-allday",
		Status:    "confirmed",
		Summary:   "Company Holiday",
		Start:     calendarTimeRef{Date: "2026-04-10"},
		End:       calendarTimeRef{Date: "2026-04-11"},
		Created:   "2026-04-01T08:00:00Z",
		Updated:   "2026-04-01T08:00:00Z",
		Organizer: calendarPerson{Email: "admin@example.com"},
		EventType: "default",
	}

	line := ev.ToEventLine()

	if line.Start != "" {
		t.Errorf("Start = %q, want empty for all-day event", line.Start)
	}
	if line.End != "" {
		t.Errorf("End = %q, want empty for all-day event", line.End)
	}
	if line.StartDate != "2026-04-10" {
		t.Errorf("StartDate = %q, want %q", line.StartDate, "2026-04-10")
	}
	if line.EndDate != "2026-04-11" {
		t.Errorf("EndDate = %q, want %q", line.EndDate, "2026-04-11")
	}
}

func TestToEventLine_Recurring(t *testing.T) {
	ev := calendarEvent{
		ID:               "evt-instance-1",
		Status:           "confirmed",
		Summary:          "Weekly Sync",
		Start:            calendarTimeRef{DateTime: "2026-04-07T14:00:00Z"},
		End:              calendarTimeRef{DateTime: "2026-04-07T15:00:00Z"},
		Created:          "2026-03-01T10:00:00Z",
		Updated:          "2026-04-06T12:00:00Z",
		Organizer:        calendarPerson{Email: "lead@example.com"},
		EventType:        "default",
		RecurringEventId: "evt-recurring-base",
	}

	line := ev.ToEventLine()

	if !line.Recurring {
		t.Errorf("Recurring = false, want true for recurring event instance")
	}
}
