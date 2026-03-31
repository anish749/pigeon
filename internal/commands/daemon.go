package commands

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"

	"github.com/anish/claude-msg-utils/internal/api"
	"github.com/anish/claude-msg-utils/internal/config"
	"github.com/anish/claude-msg-utils/internal/daemon"
	walistener "github.com/anish/claude-msg-utils/internal/listener/whatsapp"
	"github.com/anish/claude-msg-utils/internal/walog"
)

func RunDaemon(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: pigeon daemon [start|stop|status|restart]")
	}

	switch args[0] {
	case "start":
		return daemonStart()
	case "stop":
		return daemonStop()
	case "status":
		return daemonStatus()
	case "restart":
		return daemonRestart()
	case "_run":
		return daemonRun()
	default:
		return fmt.Errorf("unknown daemon command: %s", args[0])
	}
}

func daemonStart() error {
	if err := daemon.Start(); err != nil {
		return err
	}
	fmt.Println("Daemon started.")
	return nil
}

func daemonStop() error {
	if err := daemon.Stop(); err != nil {
		return err
	}
	fmt.Println("Daemon stopped.")
	return nil
}

func daemonStatus() error {
	running, pid := daemon.Status()
	if running {
		fmt.Printf("Running (pid=%d, log=%s)\n", pid, daemon.LogPath())
	} else {
		fmt.Println("Not running.")
	}
	return nil
}

func daemonRestart() error {
	if daemon.IsRunning() {
		if err := daemon.Stop(); err != nil {
			return err
		}
	}
	if err := daemon.Start(); err != nil {
		return err
	}
	fmt.Println("Daemon restarted.")
	return nil
}

// daemonRun is the actual daemon process, invoked via "daemon _run".
func daemonRun() error {
	if err := daemon.WritePID(); err != nil {
		return fmt.Errorf("write PID file: %w", err)
	}
	defer daemon.RemovePID()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if len(cfg.WhatsApp) == 0 && len(cfg.Slack) == 0 {
		return fmt.Errorf("no listeners configured in %s\nRun 'pigeon setup-whatsapp' or 'pigeon setup-slack' first", config.ConfigPath())
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	apiServer := api.NewServer()

	var whatsappCount int

	// Start WhatsApp listeners
	for _, wa := range cfg.WhatsApp {
		jid, err := types.ParseJID(wa.DeviceJID)
		if err != nil {
			slog.ErrorContext(ctx, "invalid WhatsApp device JID, skipping", "jid", wa.DeviceJID, "error", err)
			continue
		}

		client, err := connectWhatsApp(ctx, wa.DB, jid)
		if err != nil {
			slog.ErrorContext(ctx, "failed to create WhatsApp client, skipping", "account", wa.Account, "error", err)
			continue
		}

		waAccount := wa.Account
		onLogout := func() {
			slog.InfoContext(ctx, "removing logged-out account from config", "account", waAccount)
			cfg, err := config.Load()
			if err == nil {
				cfg.RemoveWhatsApp(waAccount)
				config.Save(cfg)
			}
		}
		listener := walistener.New(client, wa.Account, onLogout)
		client.AddEventHandler(listener.EventHandler(ctx))

		if err := client.Connect(); err != nil {
			slog.ErrorContext(ctx, "failed to connect WhatsApp, skipping", "account", wa.Account, "error", err)
			continue
		}

		apiServer.RegisterWhatsApp(&api.WhatsAppSender{
			Client:   client,
			Account:  wa.Account,
			Resolver: listener.Resolver(),
		})

		slog.InfoContext(ctx, "whatsapp listener started", "account", wa.Account, "device", wa.DeviceJID)
		whatsappCount++
	}

	// Start Slack workspaces and watch for config changes.
	slackMgr := NewSlackManager(apiServer)
	go slackMgr.Run(ctx, cfg.Slack)

	if whatsappCount == 0 && len(cfg.Slack) == 0 {
		return fmt.Errorf("no listeners configured — check config and credentials")
	}

	go apiServer.Start(ctx)

	var parts []string
	if whatsappCount > 0 {
		parts = append(parts, fmt.Sprintf("%d WhatsApp account(s)", whatsappCount))
	}
	if len(cfg.Slack) > 0 {
		parts = append(parts, fmt.Sprintf("%d Slack workspace(s)", len(cfg.Slack)))
	}
	slog.Info("daemon started", "listeners", strings.Join(parts, ", "))

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	slog.Info("shutting down")
	cancel()
	return nil
}

// connectWhatsApp creates a whatsmeow client for a known device. Does not call Connect().
func connectWhatsApp(ctx context.Context, dbPath string, jid types.JID) (*whatsmeow.Client, error) {
	dsn := fmt.Sprintf("file:%s?_foreign_keys=on", dbPath)
	container, err := sqlstore.New(ctx, "sqlite3", dsn, walog.New(ctx, "whatsapp-db"))
	if err != nil {
		return nil, fmt.Errorf("create device store: %w", err)
	}

	device, err := container.GetDevice(ctx, jid)
	if err != nil {
		return nil, fmt.Errorf("get device for JID %s: %w", jid.String(), err)
	}
	if device == nil {
		return nil, fmt.Errorf("no device found for JID %s — run setup-whatsapp first", jid.String())
	}

	return whatsmeow.NewClient(device, walog.New(ctx, "whatsapp")), nil
}
