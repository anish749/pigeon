package commands

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/whatsmeow/types"

	"github.com/anish/claude-msg-utils/internal/config"
	"github.com/anish/claude-msg-utils/internal/store"
)

func RunResetWhatsApp(args []string) error {
	fs := flag.NewFlagSet("reset-whatsapp", flag.ExitOnError)
	account := fs.String("account", "", "WhatsApp account (e.g. +46725323840)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Find the matching config entry (or the only one).
	var wa *config.WhatsAppConfig
	if *account != "" {
		for i := range cfg.WhatsApp {
			if cfg.WhatsApp[i].Account == *account {
				wa = &cfg.WhatsApp[i]
				break
			}
		}
		if wa == nil {
			return fmt.Errorf("no WhatsApp account %q in config", *account)
		}
	} else if len(cfg.WhatsApp) == 1 {
		wa = &cfg.WhatsApp[0]
	} else if len(cfg.WhatsApp) == 0 {
		return fmt.Errorf("no WhatsApp accounts configured")
	} else {
		msg := "multiple accounts configured, specify -account:\n"
		for _, w := range cfg.WhatsApp {
			msg += fmt.Sprintf("  %s\n", w.Account)
		}
		return fmt.Errorf("%s", msg)
	}

	ctx := context.Background()

	// Connect and logout from WhatsApp (unlinks device from phone + deletes from db).
	jid, err := types.ParseJID(wa.DeviceJID)
	if err != nil {
		slog.WarnContext(ctx, "invalid device JID, skipping logout", "jid", wa.DeviceJID, "error", err)
	} else {
		client, err := connectWhatsApp(ctx, wa.DB, jid)
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

	// Delete message data.
	dataDir := filepath.Join(store.DataDir(), "whatsapp", wa.Account)
	if err := os.RemoveAll(dataDir); err != nil {
		slog.WarnContext(ctx, "failed to delete message data", "dir", dataDir, "error", err)
	} else {
		fmt.Printf("Deleted message data: %s\n", dataDir)
	}

	// Remove config entry.
	cfg.RemoveWhatsApp(wa.Account)
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	fmt.Printf("Removed %s from config.\n", wa.Account)

	return nil
}
