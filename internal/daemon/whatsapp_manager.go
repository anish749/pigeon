package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	_ "modernc.org/sqlite"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/api"
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/hub"
	"github.com/anish749/pigeon/internal/identity"
	walistener "github.com/anish749/pigeon/internal/listener/whatsapp"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/store/modelv1"
	"github.com/anish749/pigeon/internal/syncstatus"
	"github.com/anish749/pigeon/internal/walog"
)

// WhatsAppManager owns the lifecycle of all WhatsApp account listeners.
// It starts initial accounts, watches for config changes, and
// starts/stops accounts as they are added or removed.
type WhatsAppManager struct {
	apiServer   *api.Server
	onMessage   hub.NotifyFunc[modelv1.MsgLine]
	store       store.Store
	idStore     identity.Store
	dataRoot    paths.DataRoot
	syncTracker *syncstatus.Tracker
	running     map[string]*runningWAAccount // account → state
}

type runningWAAccount struct {
	cancel context.CancelFunc
	lock   *os.File
}

// NewWhatsAppManager creates a manager that registers WhatsApp senders with
// the given API server. onMessage is called when a message is received (may be nil).
//
// Each WhatsApp account gets its own identity.Writer scoped to
// whatsapp/<account-slug>/identity/people.jsonl.
func NewWhatsAppManager(apiServer *api.Server, s store.Store, onMessage hub.NotifyFunc[modelv1.MsgLine], idStore identity.Store, dataRoot paths.DataRoot, syncTracker *syncstatus.Tracker) *WhatsAppManager {
	return &WhatsAppManager{
		apiServer:   apiServer,
		onMessage:   onMessage,
		store:       s,
		idStore:     idStore,
		dataRoot:    dataRoot,
		syncTracker: syncTracker,
		running:     make(map[string]*runningWAAccount),
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

	lock, err := LockDevice()
	if err != nil {
		slog.ErrorContext(ctx, "could not lock WhatsApp device, skipping", "account", wa.Account, "error", err)
		return
	}

	acctCtx, cancel := context.WithCancel(ctx)
	m.running[wa.Account] = &runningWAAccount{cancel: cancel, lock: lock}

	go runWithRestart(acctCtx, "whatsapp/"+wa.Account, func(ctx context.Context) error {
		return m.runWhatsAppAccount(ctx, wa, jid)
	})
}

// runWhatsAppAccount creates a WhatsApp client, connects, registers the event
// handler and API sender, then blocks until ctx is cancelled. On restart the
// whole connection is re-established.
func (m *WhatsAppManager) runWhatsAppAccount(ctx context.Context, wa config.WhatsAppConfig, jid types.JID) error {
	client, err := ConnectWhatsApp(ctx, wa.DB, jid)
	if err != nil {
		return fmt.Errorf("create whatsapp client: %w", err)
	}
	defer client.Disconnect()

	acct := account.New("whatsapp", wa.Account)
	onLogout := func() {
		slog.InfoContext(ctx, "removing logged-out account from config", "account", acct)
		cfg, err := config.Load()
		if err == nil {
			cfg.RemoveWhatsApp(wa.Account)
			config.Save(cfg)
		}
	}
	listener := walistener.New(client, acct, m.store, onLogout, m.onMessage, m.syncTracker)
	client.AddEventHandler(listener.EventHandler(ctx))

	if err := client.Connect(); err != nil {
		return fmt.Errorf("connect whatsapp: %w", err)
	}

	// Push identity signals from WhatsApp contacts.
	writer := identity.NewWriter(m.idStore, m.dataRoot.AccountFor(account.New("whatsapp", wa.Account)).Identity())
	go observeWhatsAppContacts(ctx, client, writer)

	m.apiServer.RegisterWhatsApp(&api.WhatsAppSender{
		Client:   client,
		Acct:     acct,
		Resolver: listener.Resolver(),
	})

	slog.InfoContext(ctx, "whatsapp listener started", "account", wa.Account, "device", wa.DeviceJID)

	// Block until context is cancelled. The whatsmeow client manages its
	// own reconnection internally; we just keep this goroutine alive so
	// the restart wrapper can recover from panics or permanent failures.
	<-ctx.Done()
	return nil
}

// ConnectWhatsApp creates a whatsmeow client for a known device. Does not call Connect().
func ConnectWhatsApp(ctx context.Context, dbPath string, jid types.JID) (*whatsmeow.Client, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)", dbPath)
	container, err := sqlstore.New(ctx, "sqlite", dsn, walog.New(ctx, "whatsapp-db"))
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

// observeWhatsAppContacts loads contacts from the whatsmeow store and pushes
// identity signals. Runs in a goroutine so it doesn't block listener startup.
func observeWhatsAppContacts(ctx context.Context, client *whatsmeow.Client, id identity.Observer) {
	contacts, err := client.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "identity: failed to load WhatsApp contacts", "error", err)
		return
	}

	signals := make([]identity.Signal, 0, len(contacts))
	for jid, info := range contacts {
		if jid.Server != types.DefaultUserServer {
			continue
		}
		phone := "+" + jid.User

		name := info.FullName
		if name == "" {
			name = info.PushName
		}
		if name == "" {
			name = info.BusinessName
		}
		if name == "" {
			continue
		}

		signals = append(signals, identity.Signal{
			Phone: phone,
			Name:  name,
		})
	}

	if err := id.ObserveBatch(signals); err != nil {
		slog.ErrorContext(ctx, "identity: failed to observe WhatsApp contacts", "error", err)
		return
	}
	slog.InfoContext(ctx, "identity: observed WhatsApp contacts", "count", len(signals))
}
