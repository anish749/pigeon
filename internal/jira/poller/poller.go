// Package poller runs periodic polls against Jira via the pkg/jira library.
package poller

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	jira "github.com/ankitpokhrel/jira-cli/pkg/jira"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/syncstatus"
)

// Poller runs periodic polls against one Jira project. Each (account,
// project) pair gets its own Poller — the Atlassian client is shared
// at construction time but cursor state is per-project.
type Poller struct {
	interval    time.Duration
	client      *jira.Client
	apiVersion  APIVersion
	acct        account.Account
	project     string
	projectDir  paths.JiraProjectDir
	store       *store.FSStore
	syncTracker *syncstatus.Tracker
}

// New creates a Poller for the given project. The caller owns lifetime of
// the jira.Client; one client may be shared across multiple pollers when
// they target projects on the same Atlassian site (auth is site-scoped).
func New(
	interval time.Duration,
	client *jira.Client,
	apiVersion APIVersion,
	acct account.Account,
	project string,
	projectDir paths.JiraProjectDir,
	s *store.FSStore,
	syncTracker *syncstatus.Tracker,
) *Poller {
	return &Poller{
		interval:    interval,
		client:      client,
		apiVersion:  apiVersion,
		acct:        acct,
		project:     project,
		projectDir:  projectDir,
		store:       s,
		syncTracker: syncTracker,
	}
}

// Run starts the polling loop. Blocks until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) error {
	cursors, err := p.store.LoadJiraCursors(p.projectDir)
	if err != nil {
		return fmt.Errorf("load cursors: %w", err)
	}

	// Initial poll so the first sync runs immediately rather than waiting
	// for the first tick. Mirrors the Linear poller's shape.
	p.poll(ctx, cursors)
	if err := p.store.SaveJiraCursors(p.projectDir, cursors); err != nil {
		slog.Error("save jira cursors", "project", p.project, "err", err)
	}

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			p.poll(ctx, cursors)
			if err := p.store.SaveJiraCursors(p.projectDir, cursors); err != nil {
				slog.Error("save jira cursors", "project", p.project, "err", err)
			}
		}
	}
}

// label returns a syncstatus key that distinguishes per-project pollers
// within the same account. Account.Display is something like "jira-issues/Tubular";
// appending the project key avoids collisions when one account has many
// projects.
func (p *Poller) label() string {
	return p.acct.Display() + ":" + p.project
}

func (p *Poller) poll(ctx context.Context, cursors *store.JiraCursors) {
	if ctx.Err() != nil {
		return
	}
	p.syncTracker.Start(p.label(), syncstatus.KindPoll)
	n, err := PollIssues(ctx, p.client, p.store, p.projectDir, p.project, p.apiVersion, cursors)
	p.syncTracker.Done(p.label(), err)
	if err != nil {
		slog.Error("poll jira issues", "project", p.project, "err", err)
	} else if n > 0 {
		slog.Info("poll jira issues", "project", p.project, "changes", n)
	}
}
