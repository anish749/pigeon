package modelv1

import (
	"slices"
	"testing"

	gcal "google.golang.org/api/calendar/v3"
)

func TestEventDateForStorage(t *testing.T) {
	tests := []struct {
		name string
		ev   *gcal.Event
		want string
	}{
		{
			name: "timed event uses Start.DateTime",
			ev:   &gcal.Event{Start: &gcal.EventDateTime{DateTime: "2026-04-09T14:00:00-07:00"}},
			want: "2026-04-09",
		},
		{
			name: "timed event with UTC offset",
			ev:   &gcal.Event{Start: &gcal.EventDateTime{DateTime: "2026-04-09T14:00:00Z"}},
			want: "2026-04-09",
		},
		{
			name: "all-day event uses Start.Date",
			ev:   &gcal.Event{Start: &gcal.EventDateTime{Date: "2026-04-10"}},
			want: "2026-04-10",
		},
		{
			name: "DateTime takes priority over Date",
			ev:   &gcal.Event{Start: &gcal.EventDateTime{DateTime: "2026-04-09T23:00:00Z", Date: "2026-04-10"}},
			want: "2026-04-09",
		},
		{
			name: "cancelled recurring instance with datetime OriginalStartTime",
			ev: &gcal.Event{
				Status:            "cancelled",
				OriginalStartTime: &gcal.EventDateTime{DateTime: "2026-04-07T14:00:00Z"},
			},
			want: "2026-04-07",
		},
		{
			name: "cancelled recurring all-day instance with date OriginalStartTime",
			ev: &gcal.Event{
				Status:            "cancelled",
				OriginalStartTime: &gcal.EventDateTime{Date: "2026-04-07"},
			},
			want: "2026-04-07",
		},
		{
			name: "falls back to Updated when no start fields",
			ev:   &gcal.Event{Updated: "2026-04-06T15:00:00Z"},
			want: "2026-04-06",
		},
		{
			name: "unknown when no parseable date",
			ev:   &gcal.Event{Id: "orphan", Status: "cancelled"},
			want: "unknown",
		},
		{
			name: "Start.Date preferred over OriginalStartTime",
			ev: &gcal.Event{
				Start:             &gcal.EventDateTime{Date: "2026-04-10"},
				OriginalStartTime: &gcal.EventDateTime{DateTime: "2026-04-07T14:00:00Z"},
			},
			want: "2026-04-10",
		},
		{
			name: "OriginalStartTime preferred over Updated",
			ev: &gcal.Event{
				OriginalStartTime: &gcal.EventDateTime{DateTime: "2026-04-07T09:00:00-05:00"},
				Updated:           "2026-04-08T12:00:00Z",
			},
			want: "2026-04-07",
		},
		{
			name: "nil Start falls through to OriginalStartTime",
			ev: &gcal.Event{
				OriginalStartTime: &gcal.EventDateTime{DateTime: "2026-04-07T14:00:00Z"},
			},
			want: "2026-04-07",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ce := &CalendarEvent{Runtime: *tt.ev}
			got := ce.DateForStorage()
			if got != tt.want {
				t.Errorf("DateForStorage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDatesForStorage(t *testing.T) {
	tests := []struct {
		name string
		ev   *gcal.Event
		want []string
	}{
		{
			name: "single-day all-day event",
			ev: &gcal.Event{
				Start: &gcal.EventDateTime{Date: "2026-04-07"},
				End:   &gcal.EventDateTime{Date: "2026-04-08"},
			},
			want: []string{"2026-04-07"},
		},
		{
			name: "multi-day all-day event (3 days)",
			ev: &gcal.Event{
				Start: &gcal.EventDateTime{Date: "2026-04-07"},
				End:   &gcal.EventDateTime{Date: "2026-04-10"},
			},
			want: []string{"2026-04-07", "2026-04-08", "2026-04-09"},
		},
		{
			name: "single-day timed event",
			ev: &gcal.Event{
				Start: &gcal.EventDateTime{DateTime: "2026-04-07T10:00:00Z"},
				End:   &gcal.EventDateTime{DateTime: "2026-04-07T11:00:00Z"},
			},
			want: []string{"2026-04-07"},
		},
		{
			name: "timed event spanning midnight",
			ev: &gcal.Event{
				Start: &gcal.EventDateTime{DateTime: "2026-04-07T23:00:00Z"},
				End:   &gcal.EventDateTime{DateTime: "2026-04-08T01:00:00Z"},
			},
			want: []string{"2026-04-07", "2026-04-08"},
		},
		{
			name: "missing end date falls back to single date",
			ev: &gcal.Event{
				Start: &gcal.EventDateTime{Date: "2026-04-07"},
			},
			want: []string{"2026-04-07"},
		},
		{
			name: "missing end dateTime falls back to single date",
			ev: &gcal.Event{
				Start: &gcal.EventDateTime{DateTime: "2026-04-07T10:00:00Z"},
			},
			want: []string{"2026-04-07"},
		},
		{
			name: "no start falls back to DateForStorage",
			ev: &gcal.Event{
				Updated: "2026-04-06T15:00:00Z",
			},
			want: []string{"2026-04-06"},
		},
		{
			name: "timed event spanning 3 days",
			ev: &gcal.Event{
				Start: &gcal.EventDateTime{DateTime: "2026-04-07T20:00:00Z"},
				End:   &gcal.EventDateTime{DateTime: "2026-04-09T06:00:00Z"},
			},
			want: []string{"2026-04-07", "2026-04-08", "2026-04-09"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ce := &CalendarEvent{Runtime: *tt.ev}
			got := ce.DatesForStorage()
			if !slices.Equal(got, tt.want) {
				t.Errorf("DatesForStorage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDateFromRFC3339(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"full RFC 3339 with offset", "2026-04-09T14:00:00-07:00", "2026-04-09"},
		{"RFC 3339 UTC", "2026-04-09T14:00:00Z", "2026-04-09"},
		{"bare date returns empty", "2026-04-09", ""},
		{"empty string", "", ""},
		{"short string", "2026", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dateFromRFC3339(tt.input)
			if got != tt.want {
				t.Errorf("dateFromRFC3339(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
