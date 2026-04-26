package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	jira "github.com/ankitpokhrel/jira-cli/pkg/jira"

	"github.com/anish749/pigeon/internal/config"
	jirapkg "github.com/anish749/pigeon/internal/jira"
	jirapoller "github.com/anish749/pigeon/internal/jira/poller"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/syncstatus"
)

// jiraPollInterval matches Linear and the Jira protocol spec.
const jiraPollInterval = 30 * time.Second

// JiraManager owns the lifecycle of one poller goroutine per pigeon
// jira: entry. Each entry binds one jira-cli config, which holds one
// project (per jira-cli's own data model — pigeon does not invent
// multi-project semantics on top). The goroutine reads the YAML on
// every restart, so YAML edits and token rotations are picked up
// without restarting the daemon.
type JiraManager struct {
	store       *store.FSStore
	syncTracker *syncstatus.Tracker
	running     map[string]*runningJiraPoller // key = jira-cli config path
}

type runningJiraPoller struct {
	cancel context.CancelFunc
}

// NewJiraManager creates a new JiraManager.
func NewJiraManager(s *store.FSStore, syncTracker *syncstatus.Tracker) *JiraManager {
	return &JiraManager{
		store:       s,
		syncTracker: syncTracker,
		running:     make(map[string]*runningJiraPoller),
	}
}

// Run starts pollers for the configured jira-cli configs and reconciles
// on config changes. Blocks until ctx is cancelled.
func (m *JiraManager) Run(ctx context.Context, initial []config.JiraConfig) {
	m.reconcile(ctx, initial)
	for updated := range config.Watch(ctx) {
		m.reconcile(ctx, updated.Jira)
	}
}

// Count returns the number of running per-config pollers.
func (m *JiraManager) Count() int {
	return len(m.running)
}

func (m *JiraManager) reconcile(ctx context.Context, desired []config.JiraConfig) {
	// Reconcile by jira-cli config path. Two pigeon entries pointing at
	// the same path collapse to one running poller; that's intentional
	// since they would write to the same on-disk location anyway. If the
	// path can't be resolved (broken environment, no $HOME), log and skip
	// the entry — the daemon stays up for everything else.
	desiredPaths := make(map[string]struct{})
	for _, jc := range desired {
		path, err := jirapkg.ResolveConfigPath(jc.JiraConfig)
		if err != nil {
			slog.Error("resolve jira-cli config path, skipping entry",
				"jira_config", jc.JiraConfig, "err", err)
			continue
		}
		desiredPaths[path] = struct{}{}
	}

	for path, running := range m.running {
		if _, ok := desiredPaths[path]; !ok {
			slog.Info("jira config removed, stopping poller", "path", path)
			running.cancel()
			delete(m.running, path)
		}
	}

	for path := range desiredPaths {
		if _, ok := m.running[path]; !ok {
			m.startPath(ctx, path)
		}
	}
}

func (m *JiraManager) startPath(ctx context.Context, path string) {
	child, cancel := context.WithCancel(ctx)
	m.running[path] = &runningJiraPoller{cancel: cancel}

	go runWithRestart(child, "jira/"+path, func(ctx context.Context) error {
		// Load PigeonJiraConfig + token at every restart so YAML edits or
		// token rotation pick up automatically. runWithRestart's backoff
		// throttles the retry on persistent failures.
		cfg, err := jirapkg.LoadPigeonJiraConfig(path)
		if err != nil {
			return fmt.Errorf("load jira-cli config: %w", err)
		}
		jcfg, err := cfg.JiraConfig()
		if err != nil {
			return fmt.Errorf("build jira client config: %w", err)
		}

		client := jira.NewClient(jcfg)
		acct, err := cfg.Account()
		if err != nil {
			return fmt.Errorf("derive account from jira-cli config: %w", err)
		}
		projDir := paths.DefaultDataRoot().AccountFor(acct).Jira().Project(cfg.Project.Key)

		p := jirapoller.New(jiraPollInterval, client, cfg.APIVersion(), acct, cfg.Project.Key, projDir, m.store, m.syncTracker)
		slog.Info("jira poller started",
			"path", path,
			"account", acct.Display(),
			"project", cfg.Project.Key,
			"project_dir", projDir.Path())
		return p.Run(ctx)
	})
}
