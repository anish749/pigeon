package model

import "testing"

func TestDateForStorage(t *testing.T) {
	tests := []struct {
		name string
		ev   EventLine
		want string
	}{
		{
			name: "timed event uses Start",
			ev:   EventLine{Start: "2026-04-09T14:00:00-07:00"},
			want: "2026-04-09",
		},
		{
			name: "timed event with UTC offset",
			ev:   EventLine{Start: "2026-04-09T14:00:00Z"},
			want: "2026-04-09",
		},
		{
			name: "all-day event uses StartDate",
			ev:   EventLine{StartDate: "2026-04-10"},
			want: "2026-04-10",
		},
		{
			name: "Start takes priority over StartDate",
			ev:   EventLine{Start: "2026-04-09T23:00:00Z", StartDate: "2026-04-10"},
			want: "2026-04-09",
		},
		{
			name: "cancelled recurring instance with datetime OriginalStartTime",
			ev: EventLine{
				Status:            "cancelled",
				OriginalStartTime: "2026-04-07T14:00:00Z",
			},
			want: "2026-04-07",
		},
		{
			name: "cancelled recurring all-day instance with date OriginalStartTime",
			ev: EventLine{
				Status:            "cancelled",
				OriginalStartTime: "2026-04-07",
			},
			want: "2026-04-07",
		},
		{
			name: "falls back to Updated when no start fields",
			ev:   EventLine{Updated: "2026-04-06T15:00:00Z"},
			want: "2026-04-06",
		},
		{
			name: "unknown when no parseable date",
			ev:   EventLine{ID: "orphan", Status: "cancelled"},
			want: "unknown",
		},
		{
			name: "StartDate preferred over OriginalStartTime",
			ev: EventLine{
				StartDate:         "2026-04-10",
				OriginalStartTime: "2026-04-07T14:00:00Z",
			},
			want: "2026-04-10",
		},
		{
			name: "OriginalStartTime preferred over Updated",
			ev: EventLine{
				OriginalStartTime: "2026-04-07T09:00:00-05:00",
				Updated:           "2026-04-08T12:00:00Z",
			},
			want: "2026-04-07",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.ev.DateForStorage()
			if got != tt.want {
				t.Errorf("DateForStorage() = %q, want %q", got, tt.want)
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
