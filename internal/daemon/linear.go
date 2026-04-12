package daemon

import (
	"context"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/lifecycle"
	linearpoller "github.com/anish749/pigeon/internal/linear/poller"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
)

const linearPollInterval = 30 * time.Second

// linearFactory builds a linear poller as a lifecycle.Listener.
type linearFactory struct {
	cfg   config.LinearConfig
	store *store.FSStore
}

func (f *linearFactory) Key() lifecycle.Key {
	return lifecycle.Key{Kind: "linear", ID: f.cfg.Workspace}
}

func (f *linearFactory) Build(_ context.Context) (lifecycle.Listener, error) {
	acctDir := paths.DefaultDataRoot().AccountFor(account.New("linear-issues", f.cfg.Workspace))
	return linearpoller.New(linearPollInterval, f.cfg.Workspace, acctDir, f.store), nil
}

// linearFactories returns one Factory per configured Linear workspace.
func linearFactories(cfgs []config.LinearConfig, s *store.FSStore) []lifecycle.Factory {
	out := make([]lifecycle.Factory, 0, len(cfgs))
	for _, lc := range cfgs {
		out = append(out, &linearFactory{cfg: lc, store: s})
	}
	return out
}
