// Package ingress defines the store-backed entry point into the live
// workstream routing pipeline. It reads persisted platform data and
// normalizes it into routeable signals.
package ingress

import (
	"context"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/workstream/models"
)

// Source enumerates normalized signals for a single persisted conversation
// after an absolute routing cursor.
type Source interface {
	ListSignals(ctx context.Context, acct account.Account, conversation string, since time.Time) ([]models.Signal, error)
}
