package commands

import (
	"context"
	"log/slog"

	"go.mau.fi/whatsmeow/types"

	"github.com/anish/claude-msg-utils/internal/config"
	walistener "github.com/anish/claude-msg-utils/internal/listener/whatsapp"
)

// loadAliases returns contact name aliases for a given platform/account.
// For WhatsApp accounts, it reads from the whatsmeow contact store.
// Returns nil (no aliases) for other platforms or on error.
func loadAliases(platform, account string) map[string][]string {
	if platform != "whatsapp" {
		return nil
	}
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	for _, wa := range cfg.WhatsApp {
		if wa.Account == account {
			jid, err := types.ParseJID(wa.DeviceJID)
			if err != nil {
				slog.Warn("invalid device JID in config", "jid", wa.DeviceJID, "error", err)
				return nil
			}
			aliases, err := walistener.LoadContactAliases(context.Background(), wa.DB, jid)
			if err != nil {
				slog.Warn("failed to load WhatsApp contacts", "account", account, "error", err)
				return nil
			}
			return aliases
		}
	}
	return nil
}
