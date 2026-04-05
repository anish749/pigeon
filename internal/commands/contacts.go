package commands

import (
	"context"
	"log/slog"

	"go.mau.fi/whatsmeow/types"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/config"
	walistener "github.com/anish749/pigeon/internal/listener/whatsapp"
)

// loadAliases returns contact name aliases for a given account.
// For WhatsApp accounts, it reads from the whatsmeow contact store.
// Returns nil (no aliases) for other platforms or on error.
func loadAliases(acct account.Account) map[string][]string {
	if acct.Platform != "whatsapp" {
		return nil
	}
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	for _, wa := range cfg.WhatsApp {
		waAcct := account.New("whatsapp", wa.Account)
		if waAcct.NameSlug() == acct.NameSlug() {
			jid, err := types.ParseJID(wa.DeviceJID)
			if err != nil {
				slog.Warn("invalid device JID in config", "jid", wa.DeviceJID, "error", err)
				return nil
			}
			aliases, err := walistener.LoadContactAliases(context.Background(), wa.DB, jid)
			if err != nil {
				slog.Warn("failed to load WhatsApp contacts", "account", acct, "error", err)
				return nil
			}
			return aliases
		}
	}
	return nil
}

