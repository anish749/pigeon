package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	jira "github.com/ankitpokhrel/jira-cli/pkg/jira"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/config"
	jirapkg "github.com/anish749/pigeon/internal/jira"
	jirapoller "github.com/anish749/pigeon/internal/jira/poller"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/syncstatus"
)

// jiraPollInterval matches Linear and the Jira protocol spec.
const jiraPollInterval = 30 * time.Second

// JiraManager owns the lifecycle of one poller goroutine per (account,
// project) pair. Two projects under the same account share one Atlassian
// REST client because auth is site-scoped, but each runs its own loop and
// has its own cursor.
type JiraManager struct {
	store       *store.FSStore
	syncTracker *syncstatus.Tracker
	running     map[string]*runningJiraProject // key = "<accountSlug>/<projectKey>"
}

type runningJiraProject struct {
	cancel context.CancelFunc
}

// NewJiraManager creates a new JiraManager.
func NewJiraManager(s *store.FSStore, syncTracker *syncstatus.Tracker) *JiraManager {
	return &JiraManager{
		store:       s,
		syncTracker: syncTracker,
		running:     make(map[string]*runningJiraProject),
	}
}

// Run starts pollers for all configured (account, project) pairs and
// reconciles on config changes. Blocks until ctx is cancelled.
func (m *JiraManager) Run(ctx context.Context, initial []config.JiraConfig) {
	m.reconcile(ctx, initial)
	for updated := range config.Watch(ctx) {
		m.reconcile(ctx, updated.Jira)
	}
}

// Count returns the number of running per-project pollers.
func (m *JiraManager) Count() int {
	return len(m.running)
}

// projectKey is the unique key for a (account, project) running poller.
// account.Account.NameSlug is used (not the raw display label) so the key
// matches the on-disk slug — same string everywhere.
func projectKey(accountSlug, project string) string {
	return accountSlug + "/" + project
}

func (m *JiraManager) reconcile(ctx context.Context, desired []config.JiraConfig) {
	desiredKeys := make(map[string]projectStart)
	for _, jc := range desired {
		acct := account.New(paths.JiraPlatform, jc.Account)
		for _, project := range jc.Projects {
			desiredKeys[projectKey(acct.NameSlug(), project)] = projectStart{
				entry:   jc,
				project: project,
				acct:    acct,
			}
		}
	}

	// Stop pollers whose (account, project) is no longer desired.
	for key, running := range m.running {
		if _, ok := desiredKeys[key]; !ok {
			slog.Info("jira project removed, stopping", "key", key)
			running.cancel()
			delete(m.running, key)
		}
	}

	// Start new ones.
	for key, ps := range desiredKeys {
		if _, ok := m.running[key]; !ok {
			m.startProject(ctx, key, ps)
		}
	}
}

// projectStart bundles the parameters needed to start one project poller.
type projectStart struct {
	entry   config.JiraConfig
	project string
	acct    account.Account
}

func (m *JiraManager) startProject(ctx context.Context, key string, ps projectStart) {
	projDir := paths.DefaultDataRoot().AccountFor(ps.acct).Jira().Project(ps.project)

	child, cancel := context.WithCancel(ctx)
	m.running[key] = &runningJiraProject{cancel: cancel}

	go runWithRestart(child, "jira/"+key, func(ctx context.Context) error {
		// Read jira-cli config + build client INSIDE the restart loop, so
		// that a YAML edit or token rotation gets picked up after the
		// goroutine restarts.
		path := jirapkg.ResolveConfigPath(ps.entry.JiraConfig)
		jcfg, apiVer, err := jirapkg.LoadClientConfig(path)
		if err != nil {
			return fmt.Errorf("load client config: %w", err)
		}
		c := jira.NewClient(jcfg, jira.WithTimeout(30*time.Second))

		p := jirapoller.New(jiraPollInterval, c, apiVer, ps.acct, ps.project, projDir, m.store, m.syncTracker)
		slog.Info("jira poller started",
			"account", ps.entry.Account, "project", ps.project,
			"jira_config", path, "project_dir", projDir.Path())
		return p.Run(ctx)
	})
}
