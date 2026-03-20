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

	"github.com/anish/claude-msg-utils/internal/api"
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

	apiServer := api.NewServer()

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

	// Start Slack listeners — one independent Socket Mode connection per workspace
	for _, sl := range cfg.Slack {
		if sl.AppToken == "" || sl.BotToken == "" || sl.UserToken == "" {
			slog.ErrorContext(ctx, "slack workspace missing required token(s), skipping",
				"workspace", sl.Workspace,
				"has_app_token", sl.AppToken != "",
				"has_bot_token", sl.BotToken != "",
				"has_user_token", sl.UserToken != "")
			continue
		}
		slackSender := startSlackWorkspace(ctx, sl)
		if slackSender != nil {
			apiServer.RegisterSlack(slackSender)
		}
		slackCount++
	}

	if whatsappCount == 0 && slackCount == 0 {
		return fmt.Errorf("no listeners could be started — check config and credentials")
	}

	go apiServer.Start(ctx)

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
// listener, and sync for a single workspace. Returns a SlackSender for the API server.
func startSlackWorkspace(ctx context.Context, sl config.SlackConfig) *api.SlackSender {
	botAPI := goslack.New(sl.BotToken, goslack.OptionAppLevelToken(sl.AppToken))
	smClient := socketmode.New(botAPI)

	userAPI := goslack.New(sl.UserToken)
	resolver := slacklistener.NewResolver(userAPI)
	users, channels, err := resolver.Load(ctx)
	if err != nil {
		slog.WarnContext(ctx, "failed to preload Slack names", "workspace", sl.Workspace, "error", err)
	}

	// Resolve the authenticated user's display name for sent messages.
	var userName string
	if authResp, err := userAPI.AuthTestContext(ctx); err == nil {
		userName = resolver.UserName(ctx, authResp.UserID)
	} else {
		slog.WarnContext(ctx, "failed to get Slack auth info", "workspace", sl.Workspace, "error", err)
	}

	messages := slacklistener.NewMessageStore(sl.Workspace)
	listener := slacklistener.NewListener(smClient, resolver, messages, sl.UserToken, sl.Workspace, sl.TeamID)
	go listener.Run(ctx)

	go func() {
		if err := smClient.RunContext(ctx); err != nil {
			slog.ErrorContext(ctx, "slack socket mode error", "workspace", sl.Workspace, "error", err)
		}
	}()

	slog.InfoContext(ctx, "slack listener started", "workspace", sl.Workspace, "users", users, "channels", channels)

	return &api.SlackSender{
		API:       userAPI,
		Resolver:  resolver,
		Messages:  messages,
		Workspace: sl.Workspace,
		UserName:  userName,
	}
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
