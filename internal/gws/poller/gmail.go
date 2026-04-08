package poller

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/anish749/pigeon/internal/gws"
	"github.com/anish749/pigeon/internal/gws/gmail"
	"github.com/anish749/pigeon/internal/gws/gwsstore"
	"github.com/anish749/pigeon/internal/gws/model"
	"github.com/anish749/pigeon/internal/paths"
)

// PollGmail polls for new Gmail messages and stores them as JSONL.
func PollGmail(account paths.AccountDir, cursors *gwsstore.Cursors) error {
	// Seed the history ID if we don't have one yet.
	if cursors.Gmail.HistoryID == "" {
		slog.Info("seeding gmail history ID")
		historyID, err := gmail.GetHistoryID()
		if err != nil {
			return fmt.Errorf("seed gmail history ID: %w", err)
		}
		cursors.Gmail.HistoryID = historyID
		slog.Info("seeded gmail history ID", "historyId", historyID)
		return nil
	}

	added, deleted, newHistoryID, err := gmail.ListHistory(cursors.Gmail.HistoryID)
	if err != nil {
		if gws.IsCursorExpired(err) {
			slog.Warn("gmail history ID expired, will re-seed")
			cursors.Gmail.HistoryID = ""
			return nil
		}
		return fmt.Errorf("poll gmail history: %w", err)
	}

	var errs []error

	// Fetch and store added messages.
	for _, msgID := range added {
		email, err := gmail.GetMessage(msgID)
		if err != nil {
			// Message may have been deleted between ListHistory and GetMessage.
			if gws.IsNotFound(err) {
				slog.Debug("gmail message deleted before fetch, skipping", "message_id", msgID)
				continue
			}
			errs = append(errs, fmt.Errorf("get message %s: %w", msgID, err))
			continue
		}
		datePath := account.Gmail().DateFile(email.Ts.Format("2006-01-02"))
		line := model.Line{Type: "email", Email: email}
		if err := gwsstore.AppendLine(datePath, line); err != nil {
			errs = append(errs, fmt.Errorf("append message %s: %w", msgID, err))
		}
	}

	// Record deleted messages.
	now := time.Now()
	for _, msgID := range deleted {
		del := &model.EmailDeleteLine{
			Type: "email-delete",
			ID:   msgID,
			Ts:   now,
		}
		datePath := account.Gmail().DateFile(now.Format("2006-01-02"))
		line := model.Line{Type: "email-delete", EmailDelete: del}
		if err := gwsstore.AppendLine(datePath, line); err != nil {
			errs = append(errs, fmt.Errorf("append delete %s: %w", msgID, err))
		}
	}

	if len(added) > 0 || len(deleted) > 0 {
		slog.Info("polled gmail", "added", len(added), "deleted", len(deleted))
	}

	cursors.Gmail.HistoryID = newHistoryID
	return errors.Join(errs...)
}
