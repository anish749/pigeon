package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/api"
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/hub"
	"github.com/anish749/pigeon/internal/identity"
	"github.com/anish749/pigeon/internal/lifecycle"
	walistener "github.com/anish749/pigeon/internal/listener/whatsapp"
	"github.com/anish749/pigeon/internal/store"
)

// whatsappFactory builds a lifecycle.Listener for one WhatsApp account.
type whatsappFactory struct {
	cfg       config.WhatsAppConfig
	store     store.Store
	apiServer *api.Server
	identity  *identity.Service
	onMessage hub.MessageNotifyFunc
}

// Key uses the account slug as the stable identifier — device JIDs are
// not known until we open the device store, but the account name is
// stable across config reloads.
func (f *whatsappFactory) Key() lifecycle.Key {
	return lifecycle.Key{Kind: "whatsapp", ID: f.cfg.Account}
}

// Build validates the JID and returns a fresh whatsappListener. The file
// lock is acquired inside Run so it is held only while the listener is
// alive and released on every crash.
func (f *whatsappFactory) Build(_ context.Context) (lifecycle.Listener, error) {
	jid, err := types.ParseJID(f.cfg.DeviceJID)
	if err != nil {
		return nil, fmt.Errorf("whatsapp %s: invalid device JID %q: %w", f.cfg.Account, f.cfg.DeviceJID, err)
	}
	return &whatsappListener{
		cfg:       f.cfg,
		jid:       jid,
		store:     f.store,
		apiServer: f.apiServer,
		identity:  f.identity,
		onMessage: f.onMessage,
	}, nil
}

// whatsappListener owns one account's whatsmeow client for the duration
// of a single supervised run. It acquires the DB file lock on start and
// releases it on exit, so a crashed listener does not keep the DB locked.
type whatsappListener struct {
	cfg       config.WhatsAppConfig
	jid       types.JID
	store     store.Store
	apiServer *api.Server
	identity  *identity.Service
	onMessage hub.MessageNotifyFunc
}

func (l *whatsappListener) Run(ctx context.Context) error {
	acct := account.New("whatsapp", l.cfg.Account)

	// Acquire the DB lock for the duration of this incarnation.
	lock, err := LockDevice()
	if err != nil {
		return fmt.Errorf("whatsapp %s: lock device: %w", l.cfg.Account, err)
	}
	defer lock.Close()

	client, err := ConnectWhatsApp(ctx, l.cfg.DB, l.jid)
	if err != nil {
		return fmt.Errorf("whatsapp %s: connect: %w", l.cfg.Account, err)
	}

	// onLogout removes the account from config. It is a terminal event:
	// after it runs, the supervisor's next Reconcile will not find the
	// account in config and will stop this listener — no need to signal
	// it directly from here.
	onLogout := func() {
		slog.InfoContext(ctx, "removing logged-out account from config", "account", acct)
		if cfg, err := config.Load(); err == nil {
			cfg.RemoveWhatsApp(l.cfg.Account)
			if err := config.Save(cfg); err != nil {
				slog.Error("failed to save config after logout", "account", acct, "error", err)
			}
		} else {
			slog.Error("failed to load config for logout cleanup", "account", acct, "error", err)
		}
	}

	// loggedOut is closed when the device is unpaired remotely. Treated
	// as a clean exit: Run returns nil so the supervisor moves on without
	// a crash restart. Reconcile (driven by config change from onLogout)
	// will then remove the listener entirely.
	loggedOut := make(chan struct{})
	var loggedOutOnce sync.Once

	listener := walistener.New(client, acct, l.store,
		func() {
			onLogout()
			loggedOutOnce.Do(func() { close(loggedOut) })
		},
		l.onMessage)

	client.AddEventHandler(listener.EventHandler(ctx))

	if err := client.Connect(); err != nil {
		return fmt.Errorf("whatsapp %s: connect client: %w", l.cfg.Account, err)
	}
	defer func() {
		client.Disconnect()
	}()

	observeWhatsAppContacts(ctx, client, l.identity)

	l.apiServer.RegisterWhatsApp(&api.WhatsAppSender{
		Client:   client,
		Acct:     acct,
		Resolver: listener.Resolver(),
	})
	defer l.apiServer.UnregisterWhatsApp(acct.NameSlug())

	slog.InfoContext(ctx, "whatsapp listener started",
		"account", l.cfg.Account, "device", l.cfg.DeviceJID)

	// The whatsmeow client handles its own reconnection for transient
	// network errors — we do not treat individual Disconnected events as
	// fatal, because doing so would tear down the device state on every
	// network blip. The supervisor will restart this listener if the
	// goroutine panics in an event handler, or if the daemon cancels
	// ctx. Remote logout is treated as a clean exit: Reconcile will
	// drop the listener from the supervised set on the next config load.
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-loggedOut:
		return nil
	}
}

// observeWhatsAppContacts loads contacts from the whatsmeow store and
// pushes identity signals. Called synchronously from Run since it is
// fast and its errors should surface in the supervisor log.
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

// whatsappFactories returns one Factory per configured WhatsApp account.
func whatsappFactories(cfgs []config.WhatsAppConfig, s store.Store, apiServer *api.Server, id *identity.Service, onMessage hub.MessageNotifyFunc) []lifecycle.Factory {
	out := make([]lifecycle.Factory, 0, len(cfgs))
	for _, wa := range cfgs {
		out = append(out, &whatsappFactory{
			cfg:       wa,
			store:     s,
			apiServer: apiServer,
			identity:  id,
			onMessage: onMessage,
		})
	}
	return out
}

