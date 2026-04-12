package daemon

import (
	"context"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/gws/poller"
	"github.com/anish749/pigeon/internal/identity"
	"github.com/anish749/pigeon/internal/lifecycle"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
)

// gwsPollInterval controls how often each GWS account polls Gmail, Calendar
// and Drive for changes. Kept here rather than on the Factory so every
// account polls at the same cadence.
const gwsPollInterval = 20 * time.Second

// gwsFactory builds a poller.Poller as a lifecycle.Listener. The poller
// already blocks on ctx and returns an error on failure, so it satisfies
// the Listener contract directly.
type gwsFactory struct {
	cfg      config.GWSConfig
	store    *store.FSStore
	identity *identity.Service
}

func (f *gwsFactory) Key() lifecycle.Key {
	return lifecycle.Key{Kind: "gws", ID: f.cfg.Email}
}

func (f *gwsFactory) Build(_ context.Context) (lifecycle.Listener, error) {
	acctDir := paths.DefaultDataRoot().AccountFor(account.New("gws", f.cfg.Email))
	return poller.New(gwsPollInterval, acctDir, f.store, f.identity), nil
}

// gwsFactories returns one Factory per configured GWS account.
func gwsFactories(cfgs []config.GWSConfig, s *store.FSStore, id *identity.Service) []lifecycle.Factory {
	out := make([]lifecycle.Factory, 0, len(cfgs))
	for _, g := range cfgs {
		out = append(out, &gwsFactory{cfg: g, store: s, identity: id})
	}
	return out
}
