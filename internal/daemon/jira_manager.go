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
	// Reconcile by the jira_config path the entry literally carries.
	// Setup-jira writes absolute, fully-resolved paths and a non-empty
	// APIToken; entries that miss either field are user-typed and the
	// daemon refuses them with a clear instruction to run setup-jira.
	desiredEntries := make(map[string]config.JiraConfig)
	for _, jc := range desired {
		if jc.JiraConfig == "" {
			slog.Error("jira entry missing `jira_config` path, run `pigeon setup-jira` to populate it",
				"entry", jc)
			continue
		}
		if jc.APIToken == "" {
			slog.Error("jira entry missing `api_token`, run `pigeon setup-jira` to populate it",
				"jira_config", jc.JiraConfig)
			continue
		}
		if jc.AccountName == "" {
			slog.Error("jira entry missing `account`, run `pigeon setup-jira` to populate it",
				"jira_config", jc.JiraConfig)
			continue
		}
		if existing, ok := desiredEntries[jc.JiraConfig]; ok {
			slog.Warn("two jira entries with the same jira_config path, later one wins",
				"path", jc.JiraConfig,
				"previous_token_len", len(existing.APIToken),
				"current_token_len", len(jc.APIToken))
		}
		desiredEntries[jc.JiraConfig] = jc
	}

	for path, running := range m.running {
		if _, ok := desiredEntries[path]; !ok {
			slog.Info("jira config removed, stopping poller", "path", path)
			running.cancel()
			delete(m.running, path)
		}
	}

	for path, entry := range desiredEntries {
		if _, ok := m.running[path]; !ok {
			m.startPath(ctx, path, entry)
		}
	}
}

func (m *JiraManager) startPath(ctx context.Context, path string, entry config.JiraConfig) {
	child, cancel := context.WithCancel(ctx)
	m.running[path] = &runningJiraPoller{cancel: cancel}

	go runWithRestart(child, "jira/"+path, func(ctx context.Context) error {
		// LoadPigeonJiraConfig is called inside the restart loop so a
		// YAML edit (server URL change, project rename) is picked up on
		// the next backoff tick without needing a daemon restart.
		cfg, err := jirapkg.LoadPigeonJiraConfig(path)
		if err != nil {
			return fmt.Errorf("load jira-cli config: %w", err)
		}

		jcfg, err := cfg.JiraConfig(entry.APIToken)
		if err != nil {
			return fmt.Errorf("build jira client config: %w", err)
		}

		client := jira.NewClient(jcfg)
		acct := entry.Account()
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
