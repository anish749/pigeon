package poller

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/anish749/pigeon/internal/gws"
)

const defaultCalendarID = "primary"

type calendarEventsResponse struct {
	Items         []calendarEvent `json:"items"`
	NextSyncToken string          `json:"nextSyncToken"`
	NextPageToken string          `json:"nextPageToken"`
}

type calendarEvent struct {
	ID      string          `json:"id"`
	Summary string          `json:"summary"`
	Status  string          `json:"status"`
	Start   calendarTimeRef `json:"start"`
	End     calendarTimeRef `json:"end"`
	Updated string          `json:"updated"`
}

type calendarTimeRef struct {
	DateTime string `json:"dateTime"`
	Date     string `json:"date"` // all-day events
}

func (t calendarTimeRef) String() string {
	if t.DateTime != "" {
		return t.DateTime
	}
	return t.Date
}

// PollCalendar checks for changed calendar events since the stored syncToken.
// On first run (no cursor), it fetches all upcoming events to seed the token.
func PollCalendar(cursors *Cursors) error {
	syncToken := cursors.Calendar[defaultCalendarID]
	if syncToken == "" {
		return seedCalendarCursor(cursors)
	}

	var resp calendarEventsResponse
	err := gws.RunParsed(&resp,
		"calendar", "events", "list",
		"--params", gws.ParamsJSON(map[string]string{
			"calendarId": defaultCalendarID,
			"syncToken":  syncToken,
		}),
	)
	if err != nil {
		return fmt.Errorf("poll calendar: %w", err)
	}

	for _, event := range resp.Items {
		if event.Status == "cancelled" {
			slog.Info("calendar: event cancelled",
				"event_id", event.ID,
				"summary", event.Summary,
			)
			continue
		}
		slog.Info("calendar: event changed",
			"event_id", event.ID,
			"summary", event.Summary,
			"start", event.Start.String(),
			"end", event.End.String(),
			"updated", event.Updated,
		)
	}

	if len(resp.Items) > 0 {
		slog.Info("calendar: poll complete", "changes", len(resp.Items))
	}

	// Paginate if needed — syncToken only appears on the last page.
	if resp.NextSyncToken != "" {
		cursors.Calendar[defaultCalendarID] = resp.NextSyncToken
	}
	return nil
}

func seedCalendarCursor(cursors *Cursors) error {
	// Fetch future events only — a single page is usually enough to get a
	// syncToken without paginating through the entire calendar history.
	var resp calendarEventsResponse
	err := gws.RunParsed(&resp,
		"calendar", "events", "list",
		"--params", gws.ParamsJSON(map[string]string{
			"calendarId": defaultCalendarID,
			"maxResults": "2500",
			"timeMin":    time.Now().UTC().Format(time.RFC3339),
		}),
	)
	if err != nil {
		return fmt.Errorf("seed calendar cursor: %w", err)
	}
	if resp.NextSyncToken == "" {
		return fmt.Errorf("seed calendar cursor: no syncToken in response (got pageToken — too many future events)")
	}
	cursors.Calendar[defaultCalendarID] = resp.NextSyncToken
	slog.Info("calendar: seeded cursor", "sync_token", resp.NextSyncToken)
	return nil
}
