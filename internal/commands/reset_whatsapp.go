package commands

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"go.mau.fi/whatsmeow/types"
	_ "modernc.org/sqlite"

	acctpkg "github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/daemon"
	"github.com/anish749/pigeon/internal/paths"
)

// RunUnlinkWhatsApp unpairs the WhatsApp device, deletes message data,
// and removes the account from config. This is the inverse of setup-whatsapp.
func RunUnlinkWhatsApp(account string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Find the matching config entry (or the only one).
	var wa *config.WhatsAppConfig
	if account != "" {
		for i := range cfg.WhatsApp {
			if cfg.WhatsApp[i].Account == account {
				wa = &cfg.WhatsApp[i]
				break
			}
		}
		if wa == nil {
			return fmt.Errorf("no WhatsApp account %q in config", account)
		}
	} else if len(cfg.WhatsApp) == 1 {
		wa = &cfg.WhatsApp[0]
	} else if len(cfg.WhatsApp) == 0 {
		return fmt.Errorf("no WhatsApp accounts configured")
	} else {
		msg := "multiple accounts configured, specify --account:\n"
		for _, w := range cfg.WhatsApp {
			msg += fmt.Sprintf("  %s\n", w.Account)
		}
		return fmt.Errorf("%s", msg)
	}

	ctx := context.Background()

	// Acquire device lock to ensure daemon isn't connected.
	lock, err := daemon.LockDevice()
	if err != nil {
		return fmt.Errorf("cannot reset while daemon is connected to this device — run 'pigeon daemon stop' first")
	}
	defer lock.Close()

	// Connect and logout from WhatsApp (unlinks device from phone + deletes from db).
	jid, err := types.ParseJID(wa.DeviceJID)
	if err != nil {
		slog.WarnContext(ctx, "invalid device JID, skipping logout", "jid", wa.DeviceJID, "error", err)
	} else {
		client, err := daemon.ConnectWhatsApp(ctx, wa.DB, jid)
		if err != nil {
			slog.WarnContext(ctx, "could not connect to WhatsApp, skipping logout", "error", err)
		} else {
			if err := client.Connect(); err != nil {
				slog.WarnContext(ctx, "could not connect to WhatsApp, skipping logout", "error", err)
			} else if err := client.Logout(ctx); err != nil {
				slog.WarnContext(ctx, "logout failed, deleting local data anyway", "error", err)
			} else {
				fmt.Println("Unlinked device from WhatsApp.")
			}
		}
	}

	waAccount := wa.Account

	// Delete message data.
	dataDir := paths.DefaultDataRoot().AccountFor(acctpkg.New("whatsapp", waAccount)).Path()
	if err := os.RemoveAll(dataDir); err != nil {
		slog.WarnContext(ctx, "failed to delete message data", "dir", dataDir, "error", err)
	} else {
		fmt.Printf("Deleted message data: %s\n", dataDir)
	}

	// Remove config entry.
	cfg.RemoveWhatsApp(waAccount)
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	fmt.Printf("Removed %s from config.\n", waAccount)

	return nil
}
