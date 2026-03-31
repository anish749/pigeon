package commands

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/anish/claude-msg-utils/internal/config"
	"github.com/anish/claude-msg-utils/internal/daemon"
	walistener "github.com/anish/claude-msg-utils/internal/listener/whatsapp"
	"github.com/anish/claude-msg-utils/internal/store"
	"github.com/anish/claude-msg-utils/internal/walog"
)

func RunSetupWhatsApp(dbPath string) error {
	if dbPath == "" {
		dbPath = store.DefaultDBPath()
	}

	// Acquire device lock to prevent daemon from using this device during pairing.
	lock, err := daemon.LockDevice(dbPath)
	if err != nil {
		return fmt.Errorf("cannot pair while daemon is connected to this device — run 'pigeon daemon stop' first")
	}
	defer lock.Close()

	ctx := context.Background()
	dsn := fmt.Sprintf("file:%s?_foreign_keys=on", dbPath)

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

			// Register listener to capture history sync events during setup.
			listener := walistener.New(client, account, nil)
			client.AddEventHandler(listener.EventHandler(ctx))

			// Track sync activity so we can detect completion.
			syncEvent := make(chan struct{}, 1)
			client.AddEventHandler(func(evt any) {
				if _, ok := evt.(*events.HistorySync); ok {
					select {
					case syncEvent <- struct{}{}:
					default:
					}
				}
			})

			// Save to config
			cfg, err := config.Load()
			if err != nil {
				slog.WarnContext(ctx, "failed to load config, creating new", "error", err)
				cfg = &config.Config{}
			}
			cfg.AddWhatsApp(config.WhatsAppConfig{
				DeviceJID: deviceJID,
				DB:        dbPath,
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

			// Block until history sync completes (30s idle) or interrupted.
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

			fmt.Println("Waiting for history sync...")

			const idleTimeout = 30 * time.Second

			// Wait for first sync event (or bail after 30s / signal).
			select {
			case <-syncEvent:
			case <-time.After(idleTimeout):
				fmt.Println("No history sync data received.")
				client.Disconnect()
				return nil
			case <-sigCh:
				fmt.Println("\nInterrupted.")
				client.Disconnect()
				return nil
			}

			// Got first event — keep resetting the idle timer on each new event.
			for {
				select {
				case <-syncEvent:
					// more data arriving, keep waiting
				case <-time.After(idleTimeout):
					fmt.Println("History sync complete!")
					fmt.Printf("\nStart listening with:\n")
					fmt.Printf("  pigeon daemon start\n")
					client.Disconnect()
					return nil
				case <-sigCh:
					fmt.Println("\nInterrupted.")
					client.Disconnect()
					return nil
				}
			}
		case "timeout":
			return fmt.Errorf("QR code pairing timed out — run setup-whatsapp again")
		}
	}
	return nil
}
