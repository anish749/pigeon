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
	goslack "github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"

	"github.com/anish/claude-msg-utils/internal/config"
	slacklistener "github.com/anish/claude-msg-utils/internal/listener/slack"
	walistener "github.com/anish/claude-msg-utils/internal/listener/whatsapp"
	"github.com/anish/claude-msg-utils/internal/walog"
)

func RunDaemon(args []string) error {
	if len(args) < 1 || args[0] != "start" {
		return fmt.Errorf("usage: pigeon daemon start")
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if len(cfg.WhatsApp) == 0 && len(cfg.Slack) == 0 {
		return fmt.Errorf("no listeners configured in %s\nRun 'pigeon setup-whatsapp' or 'pigeon setup-slack' first", config.ConfigPath())
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var whatsappCount, slackCount int

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

		listener := walistener.New(client, wa.Account)
		client.AddEventHandler(listener.EventHandler(ctx))

		if err := client.Connect(); err != nil {
			slog.ErrorContext(ctx, "failed to connect WhatsApp, skipping", "account", wa.Account, "error", err)
			continue
		}

		slog.InfoContext(ctx, "whatsapp listener started", "account", wa.Account, "device", wa.DeviceJID)
		whatsappCount++
	}

	// Start Slack listeners — one independent Socket Mode connection per workspace
	for _, sl := range cfg.Slack {
		if sl.AppToken == "" || sl.BotToken == "" {
			slog.ErrorContext(ctx, "slack workspace missing app_token or bot_token, skipping", "workspace", sl.Workspace)
			continue
		}
		startSlackWorkspace(ctx, sl)
		slackCount++
	}

	if whatsappCount == 0 && slackCount == 0 {
		return fmt.Errorf("no listeners could be started — check config and credentials")
	}

	var parts []string
	if whatsappCount > 0 {
		parts = append(parts, fmt.Sprintf("%d WhatsApp account(s)", whatsappCount))
	}
	if slackCount > 0 {
		parts = append(parts, fmt.Sprintf("%d Slack workspace(s)", slackCount))
	}
	fmt.Printf("Daemon running: %s. Press Ctrl+C to stop.\n", strings.Join(parts, ", "))

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	fmt.Println("\nShutting down...")
	cancel()
	return nil
}

// startSlackWorkspace creates an independent Socket Mode connection, resolver,
// listener, and sync for a single workspace.
func startSlackWorkspace(ctx context.Context, sl config.SlackConfig) {
	api := goslack.New(sl.BotToken, goslack.OptionAppLevelToken(sl.AppToken))
	smClient := socketmode.New(api)

	resolver := slacklistener.NewResolver(goslack.New(sl.BotToken))
	users, channels, err := resolver.Load(ctx)
	if err != nil {
		slog.WarnContext(ctx, "failed to preload Slack names", "workspace", sl.Workspace, "error", err)
	}

	listener := slacklistener.New(smClient, resolver, sl.Workspace, sl.TeamID)
	go listener.Run(ctx)

	go func() {
		if err := smClient.RunContext(ctx); err != nil {
			slog.ErrorContext(ctx, "slack socket mode error", "workspace", sl.Workspace, "error", err)
		}
	}()

	if sl.UserToken != "" {
		go func() {
			if err := slacklistener.Sync(ctx, sl.UserToken, resolver, sl.Workspace); err != nil {
				slog.ErrorContext(ctx, "slack sync failed", "workspace", sl.Workspace, "error", err)
			}
		}()
	}

	slog.InfoContext(ctx, "slack listener started", "workspace", sl.Workspace, "users", users, "channels", channels)
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
