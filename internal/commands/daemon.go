package commands

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
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

	if len(cfg.WhatsApp) == 0 && len(cfg.Slack) == 0 && cfg.SlackApp == nil {
		return fmt.Errorf("no listeners configured in %s\nRun 'pigeon setup-whatsapp' or 'pigeon setup-slack' first", config.ConfigPath())
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var started int

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
		started++
	}

	// Start Slack listeners (need app token from SlackApp)
	if cfg.SlackApp != nil && cfg.SlackApp.AppToken != "" {
		appToken := cfg.SlackApp.AppToken

		for _, sl := range cfg.Slack {
			startSlackListener(ctx, sl, appToken)
			started++
		}

		// Start OAuth server for adding new workspaces at runtime
		if cfg.SlackApp.ClientID != "" && cfg.SlackApp.ClientSecret != "" && slacklistener.HasTLSCerts() {
			oauthSrv := slacklistener.NewAuthServer(cfg.SlackApp.ClientID, cfg.SlackApp.ClientSecret, func(entry config.SlackConfig) {
				slog.InfoContext(ctx, "new slack workspace installed via OAuth", "workspace", entry.Workspace)
				startSlackListener(ctx, entry, appToken)
			})
			go func() {
				if err := oauthSrv.Start(ctx); err != nil {
					slog.ErrorContext(ctx, "slack oauth server error", "error", err)
				}
			}()
			fmt.Printf("Slack OAuth server running at https://localhost:9876/slack/install\n")
		} else if cfg.SlackApp.ClientID != "" && !slacklistener.HasTLSCerts() {
			slog.WarnContext(ctx, "TLS certs not found, OAuth server disabled. Run 'pigeon setup-slack' for instructions.")
		}
	}

	if started == 0 && cfg.SlackApp != nil {
		fmt.Printf("No listeners running yet. Install a Slack workspace at:\n  https://localhost:9876/slack/install\n\n")
	} else if started == 0 {
		return fmt.Errorf("no listeners could be started — check config and credentials")
	}

	fmt.Printf("Daemon running with %d listener(s). Press Ctrl+C to stop.\n", started)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	fmt.Println("\nShutting down...")
	cancel()
	return nil
}

func startSlackListener(ctx context.Context, sl config.SlackConfig, appToken string) {
	api, smClient := createSlackClients(sl.BotToken, appToken)

	resolver := slacklistener.NewResolver(api)
	users, channels, err := resolver.Load(ctx)
	if err != nil {
		slog.WarnContext(ctx, "failed to preload Slack names", "workspace", sl.Workspace, "error", err)
	}

	// Sync historical messages if user token is available
	if sl.UserToken != "" {
		go func() {
			if err := slacklistener.Sync(ctx, sl.UserToken, resolver, sl.Workspace); err != nil {
				slog.ErrorContext(ctx, "slack sync failed", "workspace", sl.Workspace, "error", err)
			}
		}()
	}

	listener := slacklistener.New(smClient, resolver, sl.Workspace)
	go listener.Run(ctx)

	go func() {
		if err := smClient.RunContext(ctx); err != nil {
			slog.ErrorContext(ctx, "slack socket mode error", "workspace", sl.Workspace, "error", err)
		}
	}()

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

// createSlackClients creates a Slack API client and Socket Mode client.
func createSlackClients(botToken, appToken string) (*goslack.Client, *socketmode.Client) {
	api := goslack.New(botToken, goslack.OptionAppLevelToken(appToken))
	smClient := socketmode.New(api)
	return api, smClient
}
