package calendar

import (
	"testing"

	"github.com/anish749/pigeon/internal/gws/model"
	gcal "google.golang.org/api/calendar/v3"
)

// newEvent is a test helper that wraps a gcal.Event into the CalendarEvent
// shape the production code expects. Serialized is left nil because
// classify() only reads from Runtime.
func newEvent(e gcal.Event) *model.CalendarEvent {
	return &model.CalendarEvent{Runtime: &e}
}

func TestClassify_OneOffEvent(t *testing.T) {
	items := []*model.CalendarEvent{
		newEvent(gcal.Event{
			Id:      "evt-123",
			Status:  "confirmed",
			Summary: "Team Standup",
		}),
	}

	events, recurringIDs, cancelledIDs := classify(items)

	if len(events) != 1 {
		t.Fatalf("events count = %d, want 1", len(events))
	}
	if events[0].Runtime.Id != "evt-123" {
		t.Errorf("events[0].Runtime.Id = %q, want %q", events[0].Runtime.Id, "evt-123")
	}
	if len(recurringIDs) != 0 {
		t.Errorf("recurringIDs = %v, want empty", recurringIDs)
	}
	if len(cancelledIDs) != 0 {
		t.Errorf("cancelledIDs = %v, want empty", cancelledIDs)
	}
}

func TestClassify_RecurringParent(t *testing.T) {
	items := []*model.CalendarEvent{
		newEvent(gcal.Event{
			Id:         "evt-recurring",
			Status:     "confirmed",
			Recurrence: []string{"RRULE:FREQ=WEEKLY"},
		}),
	}

	events, recurringIDs, cancelledIDs := classify(items)

	if len(events) != 0 {
		t.Errorf("events count = %d, want 0 (parent not written)", len(events))
	}
	if len(recurringIDs) != 1 || recurringIDs[0] != "evt-recurring" {
		t.Errorf("recurringIDs = %v, want [evt-recurring]", recurringIDs)
	}
	if len(cancelledIDs) != 0 {
		t.Errorf("cancelledIDs = %v, want empty", cancelledIDs)
	}
}

func TestClassify_CancelledRecurringParent(t *testing.T) {
	items := []*model.CalendarEvent{
		newEvent(gcal.Event{
			Id:         "evt-deleted",
			Status:     "cancelled",
			Recurrence: []string{"RRULE:FREQ=DAILY"},
		}),
	}

	events, recurringIDs, cancelledIDs := classify(items)

	if len(events) != 0 {
		t.Errorf("events count = %d, want 0", len(events))
	}
	if len(recurringIDs) != 0 {
		t.Errorf("recurringIDs = %v, want empty", recurringIDs)
	}
	if len(cancelledIDs) != 1 || cancelledIDs[0] != "evt-deleted" {
		t.Errorf("cancelledIDs = %v, want [evt-deleted]", cancelledIDs)
	}
}

func TestClassify_RecurringInstance(t *testing.T) {
	items := []*model.CalendarEvent{
		newEvent(gcal.Event{
			Id:               "evt-instance-1",
			Status:           "confirmed",
			RecurringEventId: "evt-recurring",
		}),
	}

	events, recurringIDs, cancelledIDs := classify(items)

	if len(events) != 1 {
		t.Fatalf("events count = %d, want 1 (instances are writable)", len(events))
	}
	if events[0].Runtime.Id != "evt-instance-1" {
		t.Errorf("events[0].Runtime.Id = %q, want %q", events[0].Runtime.Id, "evt-instance-1")
	}
	if len(recurringIDs) != 0 {
		t.Errorf("recurringIDs = %v, want empty", recurringIDs)
	}
	if len(cancelledIDs) != 0 {
		t.Errorf("cancelledIDs = %v, want empty", cancelledIDs)
	}
}

func TestClassify_Mixed(t *testing.T) {
	items := []*model.CalendarEvent{
		newEvent(gcal.Event{Id: "oneoff", Status: "confirmed"}),
		newEvent(gcal.Event{Id: "parent", Recurrence: []string{"RRULE:FREQ=WEEKLY"}}),
		newEvent(gcal.Event{Id: "instance", RecurringEventId: "parent"}),
		newEvent(gcal.Event{Id: "deleted-parent", Status: "cancelled", Recurrence: []string{"RRULE:FREQ=DAILY"}}),
	}

	events, recurringIDs, cancelledIDs := classify(items)

	if len(events) != 2 {
		t.Errorf("events count = %d, want 2 (oneoff + instance)", len(events))
	}
	if len(recurringIDs) != 1 || recurringIDs[0] != "parent" {
		t.Errorf("recurringIDs = %v, want [parent]", recurringIDs)
	}
	if len(cancelledIDs) != 1 || cancelledIDs[0] != "deleted-parent" {
		t.Errorf("cancelledIDs = %v, want [deleted-parent]", cancelledIDs)
	}
}
