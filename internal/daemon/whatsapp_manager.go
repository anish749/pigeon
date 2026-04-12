package daemon

import (
	"context"
	"fmt"
	"log/slog"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	_ "modernc.org/sqlite"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/api"
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/hub"
	"github.com/anish749/pigeon/internal/identity"
	"github.com/anish749/pigeon/internal/lifecycle"
	walistener "github.com/anish749/pigeon/internal/listener/whatsapp"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/walog"
)

// WhatsAppManager translates WhatsApp config entries into supervised
// Listeners. Actual lifetime, restarts, and backoff live in the Supervisor.
type WhatsAppManager struct {
	sup       *lifecycle.Supervisor
	apiServer *api.Server
	onMessage hub.MessageNotifyFunc
	store     store.Store
	identity  *identity.Service
}

// NewWhatsAppManager creates a manager that registers WhatsApp senders with
// the given API server. onMessage is called when a message is received (may be nil).
func NewWhatsAppManager(sup *lifecycle.Supervisor, apiServer *api.Server, s store.Store, onMessage hub.MessageNotifyFunc, id *identity.Service) *WhatsAppManager {
	return &WhatsAppManager{
		sup:       sup,
		apiServer: apiServer,
		onMessage: onMessage,
		store:     s,
		identity:  id,
	}
}

// Run applies the initial config and reconciles on every change.
func (m *WhatsAppManager) Run(ctx context.Context, initial []config.WhatsAppConfig) {
	m.reconcile(ctx, initial)
	for updated := range config.Watch(ctx) {
		m.reconcile(ctx, updated.WhatsApp)
	}
}

func (m *WhatsAppManager) reconcile(ctx context.Context, desired []config.WhatsAppConfig) {
	listeners := make([]lifecycle.Listener, 0, len(desired))
	for _, wa := range desired {
		listeners = append(listeners, &whatsappListener{
			cfg:       wa,
			apiServer: m.apiServer,
			store:     m.store,
			identity:  m.identity,
			onMessage: m.onMessage,
		})
	}
	if err := m.sup.Reconcile(listeners); err != nil {
		slog.ErrorContext(ctx, "whatsapp reconcile failed", "error", err)
	}
}

// whatsappListener encapsulates all per-account setup and teardown for a
// single WhatsApp device. Every resource it acquires (device flock, whatsmeow
// client) is released via defer so that Remove / restart cannot leak state.
type whatsappListener struct {
	cfg       config.WhatsAppConfig
	apiServer *api.Server
	store     store.Store
	identity  *identity.Service
	onMessage hub.MessageNotifyFunc
}

func (l *whatsappListener) ID() string { return "whatsapp/" + l.cfg.Account }

func (l *whatsappListener) Run(ctx context.Context) error {
	jid, err := types.ParseJID(l.cfg.DeviceJID)
	if err != nil {
		return fmt.Errorf("parse device JID %q: %w", l.cfg.DeviceJID, err)
	}

	lock, err := LockDevice()
	if err != nil {
		return fmt.Errorf("lock whatsapp device %s: %w", l.cfg.Account, err)
	}
	defer lock.Close()

	client, err := ConnectWhatsApp(ctx, l.cfg.DB, jid)
	if err != nil {
		return fmt.Errorf("create whatsapp client %s: %w", l.cfg.Account, err)
	}

	acct := account.New("whatsapp", l.cfg.Account)

	// loggedOut is set when the WhatsApp server has logged this device out.
	// In that case we must not ask the supervisor to restart us: the account
	// has been pulled from config already, and a restart would just repeat
	// the failed login. Return nil to signal clean exit.
	var loggedOut bool
	onLogout := func() {
		slog.InfoContext(ctx, "removing logged-out account from config", "account", acct)
		cfg, cerr := config.Load()
		if cerr == nil {
			cfg.RemoveWhatsApp(l.cfg.Account)
			if serr := config.Save(cfg); serr != nil {
				slog.ErrorContext(ctx, "save config after logout failed", "account", acct, "error", serr)
			}
		}
		loggedOut = true
	}

	listener := walistener.New(client, acct, l.store, onLogout, l.onMessage)
	client.AddEventHandler(listener.EventHandler(ctx))

	if err := client.Connect(); err != nil {
		return fmt.Errorf("connect whatsapp %s: %w", l.cfg.Account, err)
	}
	defer client.Disconnect()

	// Push identity signals from WhatsApp contacts (non-blocking).
	go observeWhatsAppContacts(ctx, client, l.identity)

	l.apiServer.RegisterWhatsApp(&api.WhatsAppSender{
		Client:   client,
		Acct:     acct,
		Resolver: listener.Resolver(),
	})
	defer l.apiServer.UnregisterWhatsApp(acct)

	slog.InfoContext(ctx, "whatsapp listener started", "account", l.cfg.Account, "device", l.cfg.DeviceJID)
	<-ctx.Done()

	if loggedOut {
		slog.InfoContext(ctx, "whatsapp listener stopping after logout", "account", l.cfg.Account)
	}
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
func observeWhatsAppContacts(ctx context.Context, client *whatsmeow.Client, id *identity.Service) {
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

