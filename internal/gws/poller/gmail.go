package poller

import (
	"fmt"
	"log/slog"

	"github.com/anish749/pigeon/internal/gws"
)

// gmailProfile is the subset of users.getProfile we need.
type gmailProfile struct {
	HistoryID string `json:"historyId"`
}

// gmailHistoryResponse is the response from users.history.list.
type gmailHistoryResponse struct {
	History       []gmailHistoryRecord `json:"history"`
	HistoryID     string               `json:"historyId"`
	NextPageToken string               `json:"nextPageToken"`
}

type gmailHistoryRecord struct {
	MessagesAdded   []gmailHistoryMsg `json:"messagesAdded"`
	MessagesDeleted []gmailHistoryMsg `json:"messagesDeleted"`
}

type gmailHistoryMsg struct {
	Message gmailMessageRef `json:"message"`
}

type gmailMessageRef struct {
	ID       string   `json:"id"`
	ThreadID string   `json:"threadId"`
	LabelIDs []string `json:"labelIds"`
}

// PollGmail checks for new Gmail activity since the stored historyId.
// On first run (no cursor), it seeds the historyId from the user's profile.
func PollGmail(cursors *Cursors) error {
	if cursors.Gmail.HistoryID == "" {
		return seedGmailCursor(cursors)
	}

	var added int
	pageToken := ""
	for {
		params := map[string]string{
			"userId":         "me",
			"startHistoryId": cursors.Gmail.HistoryID,
			"historyTypes":   "messageAdded",
		}
		if pageToken != "" {
			params["pageToken"] = pageToken
		}

		var resp gmailHistoryResponse
		err := gws.RunParsed(&resp,
			"gmail", "users", "history", "list",
			"--params", gws.ParamsJSON(params),
		)
		if err != nil {
			return fmt.Errorf("poll gmail: %w", err)
		}

		for _, record := range resp.History {
			for _, msg := range record.MessagesAdded {
				added++
				slog.Info("gmail: new message",
					"message_id", msg.Message.ID,
					"thread_id", msg.Message.ThreadID,
					"labels", msg.Message.LabelIDs,
				)
			}
		}

		if resp.HistoryID != "" {
			cursors.Gmail.HistoryID = resp.HistoryID
		}

		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	if added > 0 {
		slog.Info("gmail: poll complete", "new_messages", added)
	}
	return nil
}

func seedGmailCursor(cursors *Cursors) error {
	var profile gmailProfile
	err := gws.RunParsed(&profile,
		"gmail", "users", "getProfile",
		"--params", gws.ParamsJSON(map[string]string{"userId": "me"}),
	)
	if err != nil {
		return fmt.Errorf("seed gmail cursor: %w", err)
	}
	cursors.Gmail.HistoryID = profile.HistoryID
	slog.Info("gmail: seeded cursor", "history_id", profile.HistoryID)
	return nil
}
