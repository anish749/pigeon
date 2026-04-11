package poller

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/anish749/pigeon/internal/gws"
	"github.com/anish749/pigeon/internal/gws/gmail"
	"github.com/anish749/pigeon/internal/identity"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

// PollGmail polls for new Gmail messages and stores them as JSONL.
// Returns the number of changes observed (added + deleted) plus any error.
// On initial seed it returns the backfilled message count.
func PollGmail(s *store.FSStore, account paths.AccountDir, cursors *store.GWSCursors, id *identity.Service) (int, error) {
	if cursors.Gmail.HistoryID == "" {
		return seedGmail(s, account, cursors, id)
	}

	added, deleted, newHistoryID, err := gmail.ListHistory(cursors.Gmail.HistoryID)
	if err != nil {
		if gws.IsCursorExpired(err) {
			slog.Warn("gmail history ID expired, will re-seed")
			cursors.Gmail.HistoryID = ""
			return 0, nil
		}
		return 0, fmt.Errorf("poll gmail history: %w", err)
	}

	errs := fetchAndStoreMessages(s, account, added, id)

	// Record deleted messages for deferred removal during maintenance.
	for _, msgID := range deleted {
		if err := s.AppendPendingDelete(account.Gmail(), msgID); err != nil {
			errs = append(errs, fmt.Errorf("record delete %s: %w", msgID, err))
		}
	}

	changes := len(added) + len(deleted)
	if changes > 0 {
		slog.Info("polled gmail", "added", len(added), "deleted", len(deleted))
	}

	cursors.Gmail.HistoryID = newHistoryID
	return changes, errors.Join(errs...)
}

// seedGmail acquires the history cursor, backfills messages from the last
// BackfillDays, then saves the cursor. The cursor is acquired BEFORE backfill
// so that messages arriving during the (potentially slow) backfill are captured
// by the first incremental poll.
func seedGmail(s *store.FSStore, account paths.AccountDir, cursors *store.GWSCursors, id *identity.Service) (int, error) {
	slog.Info("seeding gmail with backfill")

	// Get the history cursor first — backfill can take many minutes.
	historyID, err := gmail.GetHistoryID()
	if err != nil {
		return 0, fmt.Errorf("seed gmail history ID: %w", err)
	}

	cutoff := time.Now().UTC().AddDate(0, 0, -gws.BackfillDays)
	query := fmt.Sprintf("after:%s", cutoff.Format("2006/01/02"))

	ids, err := gmail.ListMessages(query)
	if err != nil {
		return 0, fmt.Errorf("backfill gmail: %w", err)
	}

	slog.Info("gmail backfill enumerated messages", "count", len(ids))

	errs := fetchAndStoreMessages(s, account, ids, id)

	cursors.Gmail.HistoryID = historyID
	slog.Info("seeded gmail with backfill", "messages", len(ids), "historyId", historyID)
	return len(ids), errors.Join(errs...)
}

// fetchAndStoreMessages fetches each message by ID and writes it to disk.
// Messages deleted between enumeration and fetch are skipped.
func fetchAndStoreMessages(s *store.FSStore, account paths.AccountDir, msgIDs []string, id *identity.Service) []error {
	var errs []error
	var signals []identity.Signal
	for i, msgID := range msgIDs {
		email, err := gmail.GetMessage(msgID)
		if err != nil {
			if gws.IsNotFound(err) {
				slog.Warn("gmail message deleted before fetch, skipping", "message_id", msgID)
				continue
			}
			errs = append(errs, fmt.Errorf("get message %s: %w", msgID, err))
			continue
		}
		datePath := account.Gmail().DateFile(email.Ts.Format("2006-01-02"))
		line := modelv1.Line{Type: modelv1.LineEmail, Email: email}
		if err := s.AppendLine(datePath, line); err != nil {
			errs = append(errs, fmt.Errorf("append message %s: %w", msgID, err))
		}

		// Collect identity signals from email participants.
		if email.From != "" {
			signals = append(signals, identity.Signal{
				Email: email.From,
				Name:  email.FromName,
			})
		}

		// Log progress every 100 messages during large backfills.
		if (i+1)%100 == 0 {
			slog.Info("gmail backfill progress", "fetched", i+1, "total", len(msgIDs))
		}
	}

	// Push identity signals as a batch after processing all messages.
	if err := id.ObserveBatch(signals); err != nil {
		slog.Warn("identity: gmail signal batch failed", "error", err)
	}
	return errs
}
