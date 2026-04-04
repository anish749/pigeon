package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"

	"github.com/anish/claude-msg-utils/internal/api"
	"github.com/anish/claude-msg-utils/internal/config"
	"github.com/anish/claude-msg-utils/internal/hub"
	walistener "github.com/anish/claude-msg-utils/internal/listener/whatsapp"
	"github.com/anish/claude-msg-utils/internal/walog"
)

// WhatsAppManager owns the lifecycle of all WhatsApp account listeners.
// It starts initial accounts, watches for config changes, and
// starts/stops accounts as they are added or removed.
type WhatsAppManager struct {
	apiServer *api.Server
	onMessage hub.MessageNotifyFunc
	running   map[string]*runningWAAccount // account → state
}

type runningWAAccount struct {
	cancel context.CancelFunc
	lock   *os.File
}

// NewWhatsAppManager creates a manager that registers WhatsApp senders with
// the given API server. onMessage is called when a message is received (may be nil).
func NewWhatsAppManager(apiServer *api.Server, onMessage hub.MessageNotifyFunc) *WhatsAppManager {
	return &WhatsAppManager{
		apiServer: apiServer,
		onMessage: onMessage,
		running:   make(map[string]*runningWAAccount),
	}
}

// Run starts listeners for the initial config, then watches for changes.
// Blocks until ctx is cancelled.
func (m *WhatsAppManager) Run(ctx context.Context, initial []config.WhatsAppConfig) {
	for _, wa := range initial {
		m.startAccount(ctx, wa)
	}

	for updated := range config.Watch(ctx) {
		m.reconcile(ctx, updated.WhatsApp)
	}
}

// Count returns the number of running accounts.
func (m *WhatsAppManager) Count() int {
	return len(m.running)
}

// reconcile diffs the desired config against running accounts,
// starting new ones and stopping removed ones.
func (m *WhatsAppManager) reconcile(ctx context.Context, desired []config.WhatsAppConfig) {
	desiredAccounts := make(map[string]config.WhatsAppConfig)
	for _, wa := range desired {
		desiredAccounts[wa.Account] = wa
	}

	// Stop accounts that were removed from config.
	for account, ra := range m.running {
		if _, ok := desiredAccounts[account]; !ok {
			slog.InfoContext(ctx, "whatsapp account removed from config, stopping", "account", account)
			ra.cancel()
			ra.lock.Close()
			delete(m.running, account)
		}
	}

	// Start accounts that are new in config.
	for _, wa := range desired {
		if _, ok := m.running[wa.Account]; ok {
			continue
		}
		m.startAccount(ctx, wa)
	}
}

func (m *WhatsAppManager) startAccount(ctx context.Context, wa config.WhatsAppConfig) {
	jid, err := types.ParseJID(wa.DeviceJID)
	if err != nil {
		slog.ErrorContext(ctx, "invalid WhatsApp device JID, skipping", "jid", wa.DeviceJID, "error", err)
		return
	}

	lock, err := LockDevice(wa.DB)
	if err != nil {
		slog.ErrorContext(ctx, "could not lock WhatsApp device, skipping", "account", wa.Account, "error", err)
		return
	}

	acctCtx, cancel := context.WithCancel(ctx)

	client, err := ConnectWhatsApp(acctCtx, wa.DB, jid)
	if err != nil {
		slog.ErrorContext(ctx, "failed to create WhatsApp client, skipping", "account", wa.Account, "error", err)
		cancel()
		lock.Close()
		return
	}

	account := wa.Account
	onLogout := func() {
		slog.InfoContext(ctx, "removing logged-out account from config", "account", account)
		cfg, err := config.Load()
		if err == nil {
			cfg.RemoveWhatsApp(account)
			config.Save(cfg)
		}
	}
	listener := walistener.New(client, wa.Account, onLogout, m.onMessage)
	client.AddEventHandler(listener.EventHandler(acctCtx))

	if err := client.Connect(); err != nil {
		slog.ErrorContext(ctx, "failed to connect WhatsApp, skipping", "account", wa.Account, "error", err)
		cancel()
		lock.Close()
		return
	}

	m.apiServer.RegisterWhatsApp(&api.WhatsAppSender{
		Client:   client,
		Account:  wa.Account,
		Resolver: listener.Resolver(),
	})

	m.running[wa.Account] = &runningWAAccount{cancel: cancel, lock: lock}
	slog.InfoContext(ctx, "whatsapp listener started", "account", wa.Account, "device", wa.DeviceJID)
}

// ConnectWhatsApp creates a whatsmeow client for a known device. Does not call Connect().
func ConnectWhatsApp(ctx context.Context, dbPath string, jid types.JID) (*whatsmeow.Client, error) {
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
