package calendar

import (
	"testing"

	gcal "google.golang.org/api/calendar/v3"
)

func TestClassify_OneOffEvent(t *testing.T) {
	items := []*gcal.Event{
		{
			Id:      "evt-123",
			Status:  "confirmed",
			Summary: "Team Standup",
		},
	}

	events, recurringIDs, cancelledIDs := classify(items)

	if len(events) != 1 {
		t.Fatalf("events count = %d, want 1", len(events))
	}
	if events[0].Id != "evt-123" {
		t.Errorf("events[0].Id = %q, want %q", events[0].Id, "evt-123")
	}
	if len(recurringIDs) != 0 {
		t.Errorf("recurringIDs = %v, want empty", recurringIDs)
	}
	if len(cancelledIDs) != 0 {
		t.Errorf("cancelledIDs = %v, want empty", cancelledIDs)
	}
}

func TestClassify_RecurringParent(t *testing.T) {
	items := []*gcal.Event{
		{
			Id:         "evt-recurring",
			Status:     "confirmed",
			Recurrence: []string{"RRULE:FREQ=WEEKLY"},
		},
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
	items := []*gcal.Event{
		{
			Id:         "evt-deleted",
			Status:     "cancelled",
			Recurrence: []string{"RRULE:FREQ=DAILY"},
		},
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
	items := []*gcal.Event{
		{
			Id:               "evt-instance-1",
			Status:           "confirmed",
			RecurringEventId: "evt-recurring",
		},
	}

	events, recurringIDs, cancelledIDs := classify(items)

	if len(events) != 1 {
		t.Fatalf("events count = %d, want 1 (instances are writable)", len(events))
	}
	if events[0].Id != "evt-instance-1" {
		t.Errorf("events[0].Id = %q, want %q", events[0].Id, "evt-instance-1")
	}
	if len(recurringIDs) != 0 {
		t.Errorf("recurringIDs = %v, want empty", recurringIDs)
	}
	if len(cancelledIDs) != 0 {
		t.Errorf("cancelledIDs = %v, want empty", cancelledIDs)
	}
}

func TestClassify_Mixed(t *testing.T) {
	items := []*gcal.Event{
		{Id: "oneoff", Status: "confirmed"},
		{Id: "parent", Recurrence: []string{"RRULE:FREQ=WEEKLY"}},
		{Id: "instance", RecurringEventId: "parent"},
		{Id: "deleted-parent", Status: "cancelled", Recurrence: []string{"RRULE:FREQ=DAILY"}},
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
