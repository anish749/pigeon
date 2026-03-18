package commands

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"

	"github.com/anish/claude-msg-utils/internal/config"
	"github.com/anish/claude-msg-utils/internal/store"
	"github.com/anish/claude-msg-utils/internal/walog"
)

func RunSetupWhatsApp(args []string) error {
	fs := flag.NewFlagSet("setup-whatsapp", flag.ExitOnError)
	dbPath := fs.String("db", "", "SQLite database path (default: <data-dir>/whatsapp.db)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *dbPath == "" {
		*dbPath = store.DefaultDBPath()
	}

	ctx := context.Background()
	dsn := fmt.Sprintf("file:%s?_foreign_keys=on", *dbPath)

	container, err := sqlstore.New(ctx, "sqlite3", dsn, walog.New(ctx, "whatsapp-db"))
	if err != nil {
		return fmt.Errorf("create device store: %w", err)
	}

	device := container.NewDevice()
	client := whatsmeow.NewClient(device, walog.New(ctx, "whatsapp"))

	qrChan, err := client.GetQRChannel(ctx)
	if err != nil {
		return fmt.Errorf("get QR channel: %w", err)
	}
	if err := client.Connect(); err != nil {
		return fmt.Errorf("connect for QR pairing: %w", err)
	}

	for evt := range qrChan {
		switch evt.Event {
		case "code":
			fmt.Println("Scan this QR code with WhatsApp:")
			qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
		case "success":
			slog.InfoContext(ctx, "QR code scanned successfully")
			deviceJID := client.Store.ID.String()
			account := "+" + client.Store.ID.User

			// Save to config
			cfg, err := config.Load()
			if err != nil {
				slog.WarnContext(ctx, "failed to load config, creating new", "error", err)
				cfg = &config.Config{}
			}
			cfg.AddWhatsApp(config.WhatsAppConfig{
				DeviceJID: deviceJID,
				DB:        *dbPath,
				Account:   account,
			})
			if err := config.Save(cfg); err != nil {
				slog.ErrorContext(ctx, "failed to save config", "error", err)
			} else {
				fmt.Printf("\nSaved to config: %s\n", config.ConfigPath())
			}

			fmt.Printf("\nDevice paired successfully!\n\n")
			fmt.Printf("  Device JID: %s\n", deviceJID)
			fmt.Printf("  Account:    %s\n\n", account)
			fmt.Printf("Start listening with:\n")
			fmt.Printf("  cmu daemon start\n\n")
			fmt.Println("Press Ctrl+C to exit.")

			c := make(chan os.Signal, 1)
			signal.Notify(c, os.Interrupt, syscall.SIGTERM)
			<-c
			client.Disconnect()
			return nil
		case "timeout":
			return fmt.Errorf("QR code pairing timed out — run setup-whatsapp again")
		}
	}
	return nil
}
